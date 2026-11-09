package build

import (
	"bytes"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("inlineimage_2b", inlineimage2b) }

// inlineimage2b builds inlineimage_2b.pdf: content repairs on a page that holds inline images.
//
// Four remediation fixers rewrite a page's content stream from its parsed
// operations: the string limit (ISO 19005-2, 6.1.13 t3), the q/Q nesting limit
// (6.1.13 t8), the `.notdef` control bytes (6.2.11.8 t1), and an unusable
// marked-content `/Lang` (ISO 14289-1, 7.2 t29). The content parser reads an
// inline image's data off-token (ISO 32000-1, 8.9.7), so writing the operations
// back dropped every image on the page and left `<< ... >> BI` with no `ID` or
// `EI`, which no reader can parse. No corpus page these fixers repair held an
// inline image, so the gate never saw it.
//
// One page carries all four defects beside two inline images. That is the
// point of packing them: string-limit runs in the resource tier and the other
// three in the content tier, so the same stream is rewritten up to four times in
// one remediation, and each rewrite has to build on the one before it and carry
// both images across byte for byte.
//
//	image 1  drawn at the deepest q, 29 levels down, where the nesting repair
//	         drops the q around it
//	text     `(A\x01B)`, whose 0x01 has no glyph under WinAnsiEncoding and
//	         shows `.notdef`
//	text     a 40000-byte string in a marked-content sequence carrying the
//	         unusable `/Lang (-pt)`, shown in rendering mode 3
//	image 2  after the text, at the top level
//
// The font is the conforming non-symbolic TrueType of `ttcmap_2b.pdf`, embedded,
// so the `.notdef` verdict judges a real program. Its `/Widths` cover the codes
// from 1, so the width of the `.notdef` glyph the control byte shows agrees with
// the dictionary and 6.2.11.5 stays silent.
//
// The marked-content `/Lang` fires at PDF/UA-1 and PDF/UA-2 only: ISO 19005-2
// judges no language outside the structure tree, so at the home profile it is a
// passenger that the PDF/UA cells of the same file answer.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func inlineimage2b() ([]byte, error) {
	pdf := pdfgen.New()

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoInlineSans"),
		"Flags", 32, // non-symbolic, ISO 32000-1, Table 123
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontBBox", a(0, -200, 550, 700),
		"FontFile2", pdf.MakeIndirect(fontFile),
	)
	const first, last = 1, 66
	widths := a()
	for range last - first + 1 {
		widths.Append(ttAdvanceWidth)
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoInlineSans"),
		"FirstChar", first,
		"LastChar", last,
		"Widths", widths,
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))

	// Each image's data holds bytes no token reading keeps, a NUL, a high byte, a
	// delimiter and an end-of-line marker, so a rewrite that re-tokenizes it cannot
	// carry it across unchanged.
	image1 := "BI /W 4 /H 1 /BPC 8 /CS /G ID \x00\xff)\r EI"
	image2 := "BI /W 2 /H 2 /BPC 8 /CS /G ID \x10(\xfe\n EI"

	const depth = 29 // one past the 28 levels ISO 19005-2, 6.1.13 t8 allows
	var content bytes.Buffer
	content.Write(bytes.Repeat([]byte("q\n"), depth))
	content.WriteString("40 0 0 10 50 740 cm " + image1 + "\n")
	content.Write(bytes.Repeat([]byte("Q\n"), depth))
	content.WriteString("BT /F1 24 Tf 50 700 Td (A\x01B) Tj ET\n")
	content.WriteString("/Span <</Lang (-pt)>> BDC\n")
	content.WriteString("BT /F1 12 Tf 3 Tr 50 650 Td (")
	content.Write(bytes.Repeat([]byte("A"), 40000))
	content.WriteString(") Tj ET\n")
	content.WriteString("EMC\n")
	content.WriteString("q 20 0 0 20 50 600 cm " + image2 + " Q\n")

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream(content.Bytes()),
	)
	pdf.AddPage(pdf.Indirect(page))

	// Deliberately no /Metadata and no /OutputIntents.
	return save(pdf, "1.7")
}
