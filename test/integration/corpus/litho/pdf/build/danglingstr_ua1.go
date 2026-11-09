package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("danglingstr_ua1", danglingstrUa1) }

// danglingstrUa1 builds danglingstr_ua1.pdf: an untagged page whose content stream ends inside
// a literal string that never terminates, at PDF/UA-1.
//
// A literal string runs until its balancing right parenthesis (ISO 32000-1,
// 7.3.4.2), so one the stream never closes swallows every byte after it. When
// remediation auto-tags the page it wraps the trailing content in an /Artifact
// sequence, and an EMC appended after the last byte is lexed as string bytes,
// not as the operator, which leaves the sequence open. ISO 32000-1, 14.6 makes no
// marked-content sequence of one that never ends, so the remediated output
// failed ISO 14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1 on veraPDF. No file of the
// litho or veraPDF corpora reaches it; the pdfjs file that does is not
// remediated by the gate.
//
// The page shows a line of text and strokes a rule, then opens the string
// "(dangling ( string", whose nested parenthesis leaves it two levels deep at
// the end of the stream. Neither tool reports the string as a syntax error, and
// it paints nothing, since no operator follows it.
//
// The document is otherwise ready for PDF/UA-1 but for its tagging: /Lang,
// DisplayDocTitle, an XMP packet carrying dc:title and the pdfuaid claim, and the
// conforming embedded TrueType of baretree_2b.pdf. So the input fails only the
// tagging clauses, and auto-tagging is what remediation has to get right.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func danglingstrUa1() ([]byte, error) {
	pdf := pdfgen.New()

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoBareSans"),
		"Flags", 32, // non-symbolic, ISO 32000-1, Table 123
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontBBox", a(0, -200, 550, 700),
		"FontFile2", pdf.MakeIndirect(fontFile),
	)
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoBareSans"),
		"FirstChar", 0x41,
		"LastChar", 0x42,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))

	content := "BT /F1 24 Tf 20 100 Td (AB) Tj ET\n" +
		"20 90 m 180 90 l S\n" +
		"(dangling ( string"
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
	))
	pdf.AddPage(page)

	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	// Deliberately no /StructTreeRoot and no /MarkInfo: the page is untagged.
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	return save(pdf, "1.7", noFixMetadataVersion)
}
