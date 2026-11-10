package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen/zdeflate"
)

func init() { register("flattentiles_1b", flattentiles1b) }

// flattentiles1b builds flattentiles_1b.pdf: transparency over a photograph and a drawing
// side by side, at PDF/A-1b.
//
// A PDF/A-1 remediation flattens the transparency ISO 19005-1, 6.4 forbids and
// encodes a composite of images and shadings only as JPEG. A composite that
// mixes a photograph with vector art is cut into tiles: those only the
// photograph reaches are JPEG, the others stay lossless. The corpus files that
// reach this are government reports of tens of megabytes. This page holds one
// such region:
//
// | Draws                                                                     |
// | ------------------------------------------------------------------------- |
// | a 160 x 120 photograph, smooth colour with grain, over 192 x 144 pt       |
// | a stroked chart beside it: a frame, a polyline and filled triangle marks  |
// | a red band at ca 0.5 across both, which joins them into one region        |
//
// At the default 300 dpi the band's composite is cut into 128-pixel cells: the
// cells over the photograph are JPEG, those the chart's frame, line and marks
// reach stay lossless, and so does the short last row of cells along the band's
// bottom, too few pixels to hold 256 colours.
//
// The file declares no OutputIntent, so the DeviceRGB paint fails 6.2.3.3 t1 and
// the DeviceGray paint 6.2.3.3 t3 at PDF/A-1b, and the constant alpha fails 6.4
// t6.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func flattentiles1b() ([]byte, error) {
	pdf := pdfgen.New()

	const w, h = 160, 120

	photoSamples := func() []byte {
		var out []byte
		seed := 12345
		for y := range h {
			for x := range w {
				seed = (seed*1103515245 + 12345) & 0x7FFFFFFF
				grain := (seed>>16)%7 - 3
				r := 40 + x*160/w + grain
				g := 60 + y*140/h + grain
				b := 150 - (x+y)*90/(w+h) + grain
				for _, v := range []int{r, g, b} {
					out = append(out, byte(max(0, min(255, v))))
				}
			}
		}
		return out
	}

	photo := pdf.Stream(zdeflate.Compress(photoSamples(), 9))
	photo.Set("Type", n("XObject"))
	photo.Set("Subtype", n("Image"))
	photo.Set("Width", w)
	photo.Set("Height", h)
	photo.Set("ColorSpace", n("DeviceRGB"))
	photo.Set("BitsPerComponent", 8)
	photo.Set("Filter", n("FlateDecode"))
	pdf.MakeIndirect(photo)

	half := pdf.Indirect(d("Type", n("ExtGState"), "ca", 0.5))

	content := "" +
		// The photograph, 2.67 x 2 inches at the page's left.
		"q 192 0 0 144 48 480 cm /Photo Do Q\n" +
		// The chart at its right: a frame, a polyline and triangle marks.
		"0 g 1 w 260 480 204 144 re S\n" +
		"0 0 0.6 RG 2 w 272 490 m 310 540 l 350 520 l 390 600 l 452 570 l S\n" +
		"0.8 0 0 rg 305 535 m 315 535 l 310 545 l h f 385 595 m 395 595 l 390 605 l h f\n" +
		// A red band at ca 0.5 across the photograph and the chart.
		"q /Half gs 1 0 0 rg 48 540 416 40 re f Q\n"

	pdf.AddPage(
		pdf.Indirect(
			d(
				"Type", n("Page"),
				"MediaBox", a(0, 0, 612, 792),
				"Resources", d(
					"XObject", d("Photo", photo),
					"ExtGState", d("Half", half),
				),
				"Contents", pdf.Stream([]byte(content)),
			),
		),
	)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 1, Conformance: "B"}))

	return save(pdf, "1.4")
}
