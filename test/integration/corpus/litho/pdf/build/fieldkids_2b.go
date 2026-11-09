package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("fieldkids_2b", fieldkids2b) }

// fieldkids2b builds fieldkids_2b.pdf: form fields whose /Kids arrays are objects of their
// own, at PDF/A-2b.
//
// ISO 32000-1, 12.7.3.1, Table 220 makes a field's /Kids an array of indirect
// references; nothing stops the array itself being written as an object of its
// own and named by reference. Every walk over the field tree reads such an array
// once, because the direct fields it holds could each name it again. No corpus
// file names a field's /Kids by reference, so this fixture gives the gate the
// shape:
//
// | Field       | Kind          | /Kids                   | Widgets                         |
// | ----------- | ------------- | ----------------------- | ------------------------------- |
// | group       | non-terminal  | an array, by reference  | none of its own                 |
// | group.first | text, merged  | none                    | itself, with an appearance      |
// | group.inner | text          | an array, by reference  | one, whose /AA holds JavaScript |
// | group.note  | text, direct  | none                    | none: written in group's /Kids  |
// | check       | check box     | an array, by reference  | one, with no appearance         |
//
// The page shows nothing. Every appearance paints nothing. The widget of
// group.inner fails ISO 19005-2, 6.4.1 test 1 (/AA on a widget), and the widget
// of check fails 6.3.3 test 1 (no /AP); at part 1 they fail ISO 19005-1, 6.6.2
// test 1 and 6.9 test 2. The XMP packet claims PDF/A-2b.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func fieldkids2b() ([]byte, error) {
	pdf := pdfgen.New()

	// appearance is a 20 by 20 Form XObject appearance that paints nothing.
	appearance := func() pdfgen.Object {
		s := pdf.Stream([]byte("0 0 20 20 re n\n"))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", a(0, 0, 20, 20))
		return pdf.MakeIndirect(s)
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"Parent", pdf.Pages,
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte{})),
	))
	pdf.Pages.Set("Kids", a(page))
	pdf.Pages.Set("Count", 1)

	group := pdf.Indirect(d("T", text("group")))
	first := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Parent", group,
		"T", text("first"),
		"FT", n("Tx"),
		"V", text("one"),
		"Rect", a(72, 700, 92, 720),
		"P", page,
		"F", 4,
		"AP", d("N", appearance()),
	))
	inner := pdf.Indirect(d("Parent", group, "T", text("inner"), "FT", n("Tx"), "V", text("two")))
	innerWidget := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Parent", inner,
		"Rect", a(72, 650, 92, 670),
		"P", page,
		"F", 4,
		"AP", d("N", appearance()),
		"AA", d("K", d("S", n("JavaScript"), "JS", text("1;"))),
	))
	inner.Set("Kids", pdf.MakeIndirect(a(innerWidget)))
	note := d("T", text("note"), "FT", n("Tx"), "V", text("three"))
	group.Set("Kids", pdf.MakeIndirect(a(first, inner, note)))

	check := pdf.Indirect(d("T", text("check"), "FT", n("Btn"), "V", n("Off")))
	checkWidget := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Parent", check,
		"Rect", a(72, 600, 92, 620),
		"P", page,
		"F", 4,
		"AS", n("Off"),
	))
	check.Set("Kids", pdf.MakeIndirect(a(checkWidget)))

	page.Set("Annots", a(first, innerWidget, checkWidget))
	pdf.Root.Set("AcroForm", d("Fields", a(group, check)))
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
