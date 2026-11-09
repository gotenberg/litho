package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("annotdrawn_2b", annotdrawn2b) }

// annotdrawn2b builds annotdrawn_2b.pdf: annotations with no appearance stream drawn where
// their /Rect does not sit, at PDF/A-2b.
//
// A viewer draws an annotation with no appearance stream from its dictionary
// where ISO 32000-1, 12.5.6 defines how: a Line from /L (12.5.6.7), a PolyLine
// from /Vertices (12.5.6.9), wherever /Rect says it is. Redaction placed an
// annotation by /Rect alone, so a region crossed by such a drawing kept the
// annotation and the page went on showing it inside the region. The page draws,
// in DeviceRGB:
//
// | Annotation | /Rect               | Draws                                         |
// | ---------- | ------------------- | --------------------------------------------- |
// | Line       | [0 0 0 0]           | a red line 2 wide across the page at y 100    |
// | PolyLine   | [170 170 190 190]   | a blue zig-zag 2 wide along the bottom edge   |
//
// A grey bar under the centre gives the page content to redact. Both
// annotations set /F 4 and lack an /AP. ISO 19005-2, 6.3.3 exempts the Line,
// whose /Rect has no area, and 6.3.3 t1 fires for the PolyLine. The file
// declares no OutputIntent, so DeviceRGB fails 6.2.4.3 t2.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func annotdrawn2b() ([]byte, error) {
	pdf := pdfgen.New()

	content := []byte("0.6 0.6 0.6 rg 60 90 80 20 re f\n")

	annot := func(subtype string, rect *pdfgen.Array, entries ...any) *pdfgen.Dict {
		dd := d("Type", n("Annot"), "Subtype", n("/"+subtype), "Rect", rect, "F", 4)
		for i := 0; i < len(entries); i += 2 {
			dd.Set(entries[i].(string), entries[i+1])
		}
		return pdf.Indirect(dd)
	}

	annots := a(
		annot(
			"Line",
			a(0, 0, 0, 0),
			"L", a(20, 100, 180, 100),
			"C", a(1, 0, 0),
			"BS", d("W", 2),
		),
		annot(
			"PolyLine",
			a(170, 170, 190, 190),
			"Vertices", a(20, 20, 60, 40, 100, 20, 140, 40, 180, 20),
			"C", a(0, 0, 1),
			"BS", d("W", 2),
		),
	)

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d(),
		"Contents", pdf.Stream(content),
		"Annots", annots,
	)
	pdf.AddPage(pdf.Indirect(page))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
