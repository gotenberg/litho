package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("formcycle_ua1", formcycleUa1) }

// formcycleUa1 builds formcycle_ua1.pdf: forms whose resources list each other, auto-tagged.
//
// Remediating an untagged file at PDF/UA-1 or at a level-A PDF/A profile tags it,
// and the tagging marks every region of page content that is not a structure
// leaf as an /Artifact, except the regions that draw a Form XObject painting
// text (ISO 14289-1, 7.1). It decides which forms paint text by following the
// XObjects each form's resources list, and a form's resources can list a form
// that lists them back. No form here invokes itself, which ISO 32000-1, 8.10.1
// forbids: the cycle is in the resources, not in the drawing.
//
// The page draws three forms between three lines of its own text:
//
//   - /X2 is form B, which lists and draws A. A lists B and C and draws only C,
//     and C shows text. The page also lists A as /X1 without drawing it. The
//     scan met B from A, with A still on its path, cached "no text" for B, and
//     swept /X2 into an /Artifact whenever Go's map order put /X1 first and B
//     before C, about three runs in four. The name C shows then stopped
//     extracting, and the integrity probe refused the remediation.
//   - /X3 is form D, which draws E, which shows text: a chain with no cycle, the
//     shape pdfjs issue15629 draws its field values with.
//   - /X4 is form F, which paints a rectangle and no text: the control, which
//     becomes an /Artifact.
//
// The font is the conforming non-symbolic TrueType of ttcmap_2b.pdf, embedded,
// and every string shows only A and B, the glyphs its program holds, so no font
// clause fires. The file has no structure tree, no XMP packet and no /Lang.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func formcycleUa1() ([]byte, error) {
	pdf := pdfgen.New()

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoCycleSans"),
		"Flags", 32, // non-symbolic, ISO 32000-1, Table 123
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontBBox", a(0, -200, 550, 700),
		"FontFile2", pdf.MakeIndirect(fontFile),
	)
	const first, last = 0x41, 0x42
	widths := a()
	for i := first; i <= last; i++ {
		widths.Append(ttAdvanceWidth)
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoCycleSans"),
		"FirstChar", first,
		"LastChar", last,
		"Widths", widths,
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))

	// form is an indirect Form XObject drawing content, with resources if
	// given.
	form := func(content string, resources *pdfgen.Dict) *pdfgen.Stream {
		stream := pdf.Stream([]byte(content))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 200, 20))
		if resources != nil {
			stream.Set("Resources", resources)
		}
		return pdf.MakeIndirect(stream).(*pdfgen.Stream)
	}

	fonts := d("F1", font)

	// C shows text; A draws C and lists B; B draws A. A and B are made first with
	// empty resources and wired afterwards, since each names the other.
	formC := form("BT /F1 9 Tf 0 5 Td (AB) Tj ET", d("Font", fonts))
	formA := form("/C Do", nil)
	formB := form("/A Do", nil)
	formA.Set("Resources", d("XObject", d("B", formB, "C", formC)))
	formB.Set("Resources", d("XObject", d("A", formA)))

	formE := form("BT /F1 9 Tf 0 5 Td (BB) Tj ET", d("Font", fonts))
	formD := form("/E Do", d("XObject", d("E", formE)))

	formF := form("0 0 200 20 re f", nil)

	content := "BT /F1 12 Tf 50 740 Td (AB) Tj ET\n" +
		"q 1 0 0 1 50 710 cm /X2 Do Q\n" +
		"BT /F1 12 Tf 50 680 Td (BA) Tj ET\n" +
		"q 1 0 0 1 50 650 cm /X3 Do Q\n" +
		"BT /F1 12 Tf 50 620 Td (ABBA) Tj ET\n" +
		"q 1 0 0 1 50 590 cm /X4 Do Q\n"

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", fonts,
			"XObject", d("X1", formA, "X2", formB, "X3", formD, "X4", formF),
		),
		"Contents", pdf.Stream([]byte(content)),
	)
	pdf.AddPage(pdf.Indirect(page))

	// Deliberately untagged: no /StructTreeRoot, /MarkInfo, /Lang or /Metadata.
	return save(pdf, "1.7")
}
