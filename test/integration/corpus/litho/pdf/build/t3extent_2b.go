package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("t3extent_2b", t3extent2b) }

// t3extent2b builds t3extent_2b.pdf: Type 3 glyphs that paint far past their font's box, at PDF/A-2b.
//
// A Type 3 glyph paints whatever its description paints (ISO 32000-1, 9.6.5):
// /FontBBox is the producer's statement of the glyphs' bounds, and a d1 box
// does not clip either. Redaction once bounded a glyph whose description it
// could not bound, a shading or a form, by /FontBBox, so a region over the far
// side of what the glyph painted selected nothing and left the paint standing
// under it. No corpus file shows such a glyph painting past its font's box, so
// no cell reached it.
//
// The page, 400 by 400, shows three glyphs at Tf 10. Font T3 states a
// /FontBBox of [0 0 1000 1000], a 10-point square at each origin:
//
// | Glyph   | Opens with | Paints                                                     |
// | ------- | ---------- | ---------------------------------------------------------- |
// | T3 `A`  | d0         | a grey shading over its own 120-point square clip at 20 20 |
// | T3 `B`  | d0         | a form whose 120 by 20 box, at 20 260, it fills            |
// | T4 `A`  | d1         | a black bar 120 by 30 at 20 180, past its 10-point d1 box  |
//
// T4 states a /FontBBox that holds the bar: poppler draws a d1 glyph into a
// cache the size of that box, and clips what lies past it. The centred quarter
// [100 100 300 300] meets all three paintings and none of the d1 box or the
// box T3 states.
//
// The RGB output intent covers DeviceGray, so the file is conforming at
// PDF/A-2b.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func t3extent2b() ([]byte, error) {
	pdf := pdfgen.New()

	destination := pdf.Stream(iccRGBDisplayProfile())
	destination.Set("N", 3)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho RGB display"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	shading := pdf.Indirect(d(
		"ShadingType", 2,
		"ColorSpace", n("DeviceGray"),
		"Coords", a(0, 0, 12000, 0),
		"Function", d("FunctionType", 2, "Domain", a(0, 1), "C0", a(0.2), "C1", a(0.6), "N", 1),
		"Extend", a(true, true),
	))
	form := pdf.Stream([]byte("0 g 0 0 12000 2000 re f\n"))
	form.Set("Type", n("XObject"))
	form.Set("Subtype", n("Form"))
	form.Set("BBox", a(0, 0, 12000, 2000))
	form.Set("Resources", d())

	// type3 is a Type 3 font of the glyphs procs, A onwards, stating bbox.
	type3 := func(bbox *pdfgen.Array, procs []string, resources *pdfgen.Dict) *pdfgen.Dict {
		diffs := a(65)
		charProcs := d()
		widths := a()
		for i, p := range procs {
			name := string(rune(65 + i))
			diffs.Append(n("/" + name))
			charProcs.Set("/"+name, pdf.MakeIndirect(pdf.Stream([]byte(p))))
			widths.Append(1000)
		}
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"FontBBox", bbox,
			"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
			"CharProcs", charProcs,
			"Encoding", d("Type", n("Encoding"), "Differences", diffs),
			"FirstChar", 65,
			"LastChar", 64+len(procs),
			"Widths", widths,
			"Resources", resources,
		))
	}

	font := type3(
		a(0, 0, 1000, 1000),
		[]string{"1000 0 d0\nq 0 0 12000 12000 re W n /Sh sh Q\n", "1000 0 d0\n/Fm Do\n"},
		d("Shading", d("Sh", shading), "XObject", d("Fm", pdf.MakeIndirect(form))),
	)
	// A renderer may draw a d1 glyph into a cache the size of /FontBBox (poppler
	// does), so this font states a box that holds the bar its d1 box does not.
	cached := type3(a(0, 0, 13000, 4000), []string{"1000 0 0 0 1000 1000 d1\n0 0 12000 3000 re f\n"}, d())

	content := "0 g BT /T3 10 Tf\n" +
		"1 0 0 1 20 20 Tm (A) Tj\n" +
		"1 0 0 1 20 260 Tm (B) Tj\n" +
		"/T4 10 Tf 1 0 0 1 20 180 Tm (A) Tj\n" +
		"ET\n"
	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 400, 400),
		"Resources", d("Font", d("T3", font, "T4", cached)),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
	)
	pdf.AddPage(pdf.Indirect(page))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
