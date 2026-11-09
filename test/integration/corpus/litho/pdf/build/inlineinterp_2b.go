package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("inlineinterp_2b", inlineinterp2b) }

// inlineinterp2b builds inlineinterp_2b.pdf: inline images that ask for interpolation in
// every kind of stream veraPDF parses, for the repair that respells their
// dictionaries in place.
//
// ISO 19005-2, 6.2.8 t3 holds every image dictionary to an /Interpolate of
// false, and an inline image writes the key /I (ISO 32000-1, 8.9.7, Table 93).
// veraPDF judges each inline image as an image of its own, wherever a content
// stream it parses holds it, so each image here fails 6.2.8 t3 once:
//
//	Fm     a form XObject the page draws, whose image writes /I true.
//	T3     the procedure of glyph a of a Type 3 font the page shows, whose image
//	       writes /Interpolate true.
//	Ap     the normal appearance of a Square annotation, whose image writes
//	       /I true.
//	page   the page's own content, whose image writes both /Interpolate true and
//	       /I true. The page's /Contents is a reference to an array of two
//	       streams, the image in the second.
//
// Every image's data holds a NUL, a high byte, a delimiter and an end-of-line
// marker, so a repair that moves a byte of it shows. The remediation rewrites
// all four streams, and the page's through the array its /Contents names.
//
// The first part of the page's content also shows text in F1, Helvetica, which
// is not embedded: ISO 19005-2, 6.2.11.4.1 t1, which no repair answers. It stays
// in every remediated output, so a rewrite of the page that a validator can no
// longer read shows as a finding one tool reports and the other does not.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func inlineinterp2b() ([]byte, error) {
	pdf := pdfgen.New()

	inline := func(key string) string {
		return "q 10 0 0 10 0 0 cm BI /W 4 /H 1 /CS /G /BPC 8 " + key + " ID \x00\xff)\r EI Q"
	}

	fm := pdf.Stream([]byte(inline("/I true")))
	fm.Set("Type", n("XObject"))
	fm.Set("Subtype", n("Form"))
	fm.Set("BBox", a(0, 0, 100, 100))
	fm.Set("Resources", d())
	pdf.MakeIndirect(fm)

	glyphA := pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0 " + inline("/Interpolate true"))))
	t3 := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type3"),
		"FontBBox", a(0, 0, 1000, 1000),
		"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
		"CharProcs", d("a", glyphA),
		"Encoding", d("Type", n("Encoding"), "Differences", a(97, n("a"))),
		"FirstChar", 97,
		"LastChar", 97,
		"Widths", a(1000),
		"Resources", d(),
	))

	ap := pdf.Stream([]byte(inline("/I true")))
	ap.Set("Type", n("XObject"))
	ap.Set("Subtype", n("Form"))
	ap.Set("BBox", a(0, 0, 40, 40))
	ap.Set("Resources", d())
	pdf.MakeIndirect(ap)
	annot := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(10, 10, 50, 50),
		"F", 4,
		"AP", d("N", ap),
	))

	helvetica := pdf.Indirect(d("Type", n("Font"), "Subtype", n("Type1"), "BaseFont", n("Helvetica")))

	part1 := pdf.MakeIndirect(pdf.Stream([]byte(
		"q 100 0 0 100 100 400 cm /Fm Do Q\n" +
			"BT /T3 12 Tf 10 700 Td (a) Tj ET\n" +
			"BT /F1 12 Tf 10 650 Td (b) Tj ET\n",
	)))
	part2 := pdf.MakeIndirect(pdf.Stream([]byte(inline("/Interpolate true /I true") + "\n")))
	contents := pdf.MakeIndirect(a(part1, part2))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("T3", t3, "F1", helvetica), "XObject", d("Fm", fm)),
		"Contents", contents,
		"Annots", a(annot),
	))
	pdf.AddPage(page)

	// Deliberately no /OutputIntents and no /Metadata.
	return save(pdf, "1.7")
}
