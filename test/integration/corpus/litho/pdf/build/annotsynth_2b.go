package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("annotsynth_2b", annotsynth2b) }

// annotsynth2b builds annotsynth_2b.pdf: annotations with no appearance stream that the
// renderer draws from their dictionaries, at PDF/A-2b.
//
// ISO 32000-1, 12.5.6 defines a Polygon (12.5.6.9), an Underline and a Squiggly
// (12.5.6.10) by their dictionaries alone, and viewers draw them when the file
// holds no appearance stream. The render goldens of the corpora held Square,
// Circle, Line, PolyLine, Ink, Highlight, StrikeOut and Link annotations with no
// /AP, but no Polygon, Underline or Squiggly, so the render cell compared none
// of them. The page draws, in DeviceRGB:
//
// | Annotation | Draws                                                               |
// | ---------- | ------------------------------------------------------------------- |
// | Polygon    | a triangle filled blue with a red border 3 wide                     |
// | Polygon    | a pentagon filled yellow with a dashed border, at /CA 0.5 over a    |
// |            | grey bar                                                            |
// | Underline  | a red line along the bottom of two quadrilaterals over cyan bars    |
// | Squiggly   | a green zig-zag along the bottom of a quadrilateral over a cyan bar |
//
// Each annotation sets /F 4 and lacks the /AP that ISO 19005-2, 6.3.3 requires,
// so 6.3.3 t1 fires. The file declares no OutputIntent, so DeviceRGB fails
// 6.2.4.3 t2, and the half-opaque polygon is no transparency the page content
// holds.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func annotsynth2b() ([]byte, error) {
	pdf := pdfgen.New()

	content := "0.6 0.6 0.6 rg 100 110 90 20 re f\n" +
		"0 1 1 rg 10 60 80 12 re f 10 40 80 12 re f 110 50 80 12 re f\n"

	annot := func(subtype string, rect *pdfgen.Array, entries ...any) *pdfgen.Dict {
		dd := d("Type", n("Annot"), "Subtype", n(subtype), "Rect", rect, "F", 4)
		for i := 0; i < len(entries); i += 2 {
			dd.Set(entries[i].(string), entries[i+1])
		}
		return pdf.Indirect(dd)
	}

	annots := a(
		annot(
			"Polygon",
			a(10, 100, 90, 190),
			"Vertices", a(15, 105, 85, 105, 50, 185),
			"C", a(1, 0, 0),
			"IC", a(0, 0, 1),
			"BS", d("W", 3),
		),
		annot(
			"Polygon",
			a(100, 100, 190, 190),
			"Vertices", a(145, 185, 185, 155, 170, 105, 120, 105, 105, 155),
			"C", a(0, 0, 0),
			"IC", a(1, 1, 0),
			"BS", d("W", 2, "S", n("D"), "D", a(6, 3)),
			"CA", 0.5,
		),
		annot(
			"Underline",
			a(10, 40, 90, 72),
			"QuadPoints", a(10, 72, 90, 72, 10, 60, 90, 60, 10, 52, 90, 52, 10, 40, 90, 40),
			"C", a(1, 0, 0),
		),
		annot(
			"Squiggly",
			a(110, 50, 190, 62),
			"QuadPoints", a(110, 62, 190, 62, 110, 50, 190, 50),
			"C", a(0, 0.5, 0),
		),
	)

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d(),
		"Contents", pdf.Stream([]byte(content)),
		"Annots", annots,
	)
	pdf.AddPage(pdf.Indirect(page))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
