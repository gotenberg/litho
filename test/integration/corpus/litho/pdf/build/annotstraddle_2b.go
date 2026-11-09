package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("annotstraddle_2b", annotstraddle2b) }

// annotstraddle2b builds annotstraddle_2b.pdf: an annotation reaching outside the regions
// redaction selects it by, and the pop-up and reply of one it removes, at
// PDF/A-2b.
//
// An annotation is atomic (ISO 32000-1, 12.5.2), so redaction refuses one a
// region meets without containing. The raster fallback draws such an
// annotation into the page as a viewer draws it (12.5.5, Algorithm 8.1) and
// removes it, so its look is page content the region cuts. A pop-up names its
// parent by /Parent (12.5.6.14) and a reply names the annotation it answers by
// /IRT (12.5.6.2): redaction removed an annotation from /Annots and left both,
// and the rewrite wrote the removed annotation through them. Both pages are 200
// by 200 and draw in DeviceGray:
//
// | Page | Annotation | /Rect              | Draws, and what names it                          |
// | ---- | ---------- | ------------------ | ------------------------------------------------- |
// | 1    | Square     | [120 60 180 120]   | a dark square, across the quarter and the band    |
// | 1    | Popup      | [150 160 195 195]  | nothing, closed; /Parent the Square               |
// | 2    | Ink        | [70 70 130 130]    | a stroke, inside the centred quarter              |
// | 2    | Popup      | [155 155 195 195]  | nothing, closed; /Parent the Ink                  |
// | 2    | Text       | [10 10 34 34]      | a small square; /IRT the Ink, /RT /R              |
//
// Page 1 also draws a grey bar across the middle. Every annotation but the
// pop-ups sets /F 4 and has a normal appearance stream.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func annotstraddle2b() ([]byte, error) {
	pdf := pdfgen.New()

	form := func(bbox *pdfgen.Array, data string) *pdfgen.Stream {
		return pdf.MakeIndirect(
			pdf.Stream([]byte(data), "Type", n("XObject"), "Subtype", n("Form"), "BBox", bbox, "Resources", d()),
		).(*pdfgen.Stream)
	}

	annot := func(subtype string, rect *pdfgen.Array, entries ...any) *pdfgen.Dict {
		dd := d("Type", n("Annot"), "Subtype", n("/"+subtype), "Rect", rect)
		for i := 0; i < len(entries); i += 2 {
			dd.Set(entries[i].(string), entries[i+1])
		}
		return pdf.Indirect(dd)
	}

	square := annot(
		"Square",
		a(120, 60, 180, 120),
		"F", 4,
		"Contents", text("Square comment"),
		"AP", d("N", form(a(0, 0, 60, 60), "0.2 g 0 0 60 60 re f\n")),
	)
	squarePopup := annot("Popup", a(150, 160, 195, 195), "Parent", square)
	square.Set("Popup", squarePopup)

	ink := annot(
		"Ink",
		a(70, 70, 130, 130),
		"F", 4,
		"InkList", a(a(75, 75, 100, 125, 125, 75)),
		"Contents", text("Ink comment"),
		"AP", d("N", form(a(0, 0, 60, 60), "0 G 2 w 5 5 m 30 55 l 55 5 l S\n")),
	)
	inkPopup := annot("Popup", a(155, 155, 195, 195), "Parent", ink)
	ink.Set("Popup", inkPopup)
	reply := annot(
		"Text",
		a(10, 10, 34, 34),
		"F", 4,
		"IRT", ink,
		"RT", n("R"),
		"Contents", text("Reply"),
		"AP", d("N", form(a(0, 0, 24, 24), "0.5 g 2 2 20 20 re f\n")),
	)

	page1 := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d(),
		"Contents", pdf.Stream([]byte("0.6 g 20 90 160 20 re f\n")),
		"Annots", a(square, squarePopup),
	)
	page2 := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d(),
		"Contents", pdf.Stream([]byte("0.8 g 20 160 40 20 re f\n")),
		"Annots", a(ink, inkPopup, reply),
	)
	pdf.AddPage(pdf.Indirect(page1))
	pdf.AddPage(pdf.Indirect(page2))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
