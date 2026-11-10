package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("kidsarray_2b", kidsarray2b) }

// kidsarray2b builds kidsarray_2b.pdf: pages written directly in a /Kids array that its
// node names by reference, at PDF/A-2b.
//
// ISO 32000-1, 7.7.3.2, Table 29 makes /Kids an array of indirect references. A
// Writer gives every page written directly in one an object number before it
// edits or repairs the document, and has the array name it by reference.
// kidsrepair_1b.pdf holds two pages written directly in a /Kids array written
// inline in its node; no corpus file holds one in an array its node names by
// reference, where the array, not the node, is what the numbering rewrites:
//
// | Page | Where it sits                                    | Shows | Carries         |
// | ---- | ------------------------------------------------ | ----- | --------------- |
// | 1    | the root's /Kids, by reference                   | A     | nothing         |
// | 2    | the array node L names as its /Kids by reference | B     | a field, an /AA |
// | 3    | the same array, after page 2                     | AB    | a note, no /F   |
//
// Each page states its own /Resources and shows its text in the conforming
// embedded TrueType of ttcmap_2b.pdf, so no font or resource clause fires; the
// text paints in the initial DeviceGray, with no output intent. Page 2 holds a
// text field's widget, which the form mutations flatten, and a page /AA holding a
// permitted GoTo action (ISO 19005-2, 6.5.2 test 2). Page 3 holds a Text
// annotation with no /F (6.3.2 test 1). Every appearance paints nothing.
//
// veraPDF 1.30.2 refuses a file whose page tree holds an intermediate node
// written directly in /Kids ("doesn't appear to be a valid PDF"), so the other
// shape the numbering handles has no fixture.
//
// The tree is built by hand: pdf.AddPage flattens it, which would delete
// the construct the fixture exists for.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func kidsarray2b() ([]byte, error) {
	pdf := pdfgen.New()

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoKidsSans"),
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
	for range last - first + 1 {
		widths.Append(ttAdvanceWidth)
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoKidsSans"),
		"FirstChar", first,
		"LastChar", last,
		"Widths", widths,
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))

	resources := func() *pdfgen.Dict {
		return d("Font", d("F1", font))
	}

	content := func(text string) pdfgen.Object {
		return pdf.MakeIndirect(pdf.Stream([]byte("BT /F1 24 Tf 72 700 Td (" + text + ") Tj ET\n")))
	}

	// appearance is a 20 by 20 Form XObject appearance that paints nothing.
	appearance := func() pdfgen.Object {
		s := pdf.Stream([]byte("0 0 20 20 re n\n"))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", a(0, 0, 20, 20))
		return pdf.MakeIndirect(s)
	}

	root := pdf.Pages
	root.Set("MediaBox", a(0, 0, 612, 792))

	page1 := pdf.Indirect(d("Type", n("Page"), "Parent", root, "Resources", resources(), "Contents", content("A")))
	nodeL := pdf.Indirect(d("Type", n("Pages"), "Parent", root, "Count", 2))

	field := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"FT", n("Tx"),
		"T", text("city"),
		"V", text(""),
		"Rect", a(72, 600, 92, 620),
		"F", 4,
		"AP", d("N", appearance()),
	))
	page2 := d(
		"Type", n("Page"),
		"Parent", nodeL,
		"Resources", resources(),
		"Contents", content("B"),
		"Annots", a(field),
		"AA", d("O", d("S", n("GoTo"), "D", a(page1, n("Fit")))),
	)

	note := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Text"),
		"Rect", a(72, 500, 92, 520),
		"Contents", text("note"),
		"AP", d("N", appearance()),
	))
	page3 := d(
		"Type", n("Page"),
		"Parent", nodeL,
		"Resources", resources(),
		"Contents", content("AB"),
		"Annots", a(note),
	)
	nodeL.Set("Kids", pdf.MakeIndirect(a(page2, page3)))

	root.Set("Kids", a(page1, nodeL))
	root.Set("Count", 3)

	pdf.Root.Set("AcroForm", d("Fields", a(field)))
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
