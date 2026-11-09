package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("transferfb_2b", transferfb2b) }

// transferfb2b builds transferfb_2b.pdf: a shading drawn under an inverting transfer function.
//
// The raster fallback of a redaction (pdf.WithRedactionRasterFallback) draws a
// shading paint the regions cut as an image of the page, which litho's renderer
// composites with every graphics state in force applied, the transfer function
// among them (ISO 32000-1, 10.5). The image is drawn where the shading was, so
// the transfer function the content set is still in force there: applied a
// second time, an inverting one turned the composite back into its negative,
// and every pixel the image holds outside the regions showed inverted (pdfjs
// issue20722, whose /TR inverts the whole page). No fixture here set a transfer
// function under a unit the fallback rasterises, and issue20722 lies outside the
// pdfjs render sample the redact-render cell reads.
//
// One 400 by 400 page, its content in one graphics state that sets /TR to
// f(x) = 1 - x (a Type 2 function, ISO 32000-1, 7.10.3):
//
// | Paint                                      | Source colour | Shown as |
// | ------------------------------------------ | ------------- | -------- |
// | `rg` fill of the page                      | white         | black    |
// | `rg` fill, `[50 50 350 350]`               | 0 0.5 1       | orange   |
// | axial shading `sh`, clipped to the quarter | red to blue   | inverted |
//
// The centred quarter of the page, [100 100 300 300], is the shading's clip, so
// the redact-render cell's p1-quarter and p1-band regions cut it: the default
// mode refuses the shading, and the fallback rasterises it.
//
// /TR fails 6.2.5/1 at PDF/A-2b (ISO 19005-2, 6.2.5 forbids the key). See
// ../FIXTURES.md for the authoritative expected firing set.
func transferfb2b() ([]byte, error) {
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

	invert := d("FunctionType", 2, "Domain", a(0, 1), "C0", a(1), "C1", a(0), "N", 1)
	shading := d(
		"ShadingType", 2,
		"ColorSpace", n("DeviceRGB"),
		"Coords", a(100, 0, 300, 0),
		"Function", d("FunctionType", 2, "Domain", a(0, 1), "C0", a(1, 0, 0), "C1", a(0, 0, 1), "N", 1),
	)
	body := "/Inv gs 1 1 1 rg 0 0 400 400 re f\n" +
		"0 0.5 1 rg 50 50 300 300 re f\n" +
		"q 100 100 200 200 re W n /Sh0 sh Q\n"
	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 400, 400),
		"Resources", d(
			"ExtGState", d("Inv", d("Type", n("ExtGState"), "TR", invert)),
			"Shading", d("Sh0", pdf.Indirect(shading)),
		),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(body))),
	)
	pdf.AddPage(pdf.Indirect(page))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
