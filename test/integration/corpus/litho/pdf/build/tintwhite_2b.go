package build

import (
	"bytes"
	"math"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("tintwhite_2b", tintwhite2b) }

// tintwhite2b builds tintwhite_2b.pdf: an RGB ICC profile whose white is tinted, PDF/A-2b.
//
// An ICCBased space reads its colours through its profile (ISO 32000-1,
// 8.6.5.5). `tw` is a version 2.1 RGB display profile of sRGB's tone curves
// and red and green colorants, with its blue colorant at 90 per cent, so its
// colorants no longer sum to the PCS white (ICC.1:2001-04, 6.3.4.2) and its
// white, every component 1, reads a pale yellow. Little CMS moves a transform's
// white onto the destination's when it optimizes it, which poppler and PDFium
// never let it do and Ghostscript and MuPDF do; litho draws the profile's white
// as the profile reads it and converts it to bare paper.
//
// One 100 x 200 page paints the space three ways:
//
// | y       | x                                                                      |
// | ------- | ---------------------------------------------------------------------- |
// | 110-190 | 0-100: `Im`, an image of 4 x 4 samples, each row the greys 255, 250    |
// |         | and 240 and the colour 128 64 32                                       |
// | 60-100  | 10-90: a fill of white (`/T cs 1 1 1 sc`)                              |
// | 10-45   | 10-50: a fill of 0.97; 50-90: a fill of 0.5 0.6 0.7                    |
//
// The region the redaction cells clear, the page's centred quarter, x 25-75 and
// y 50-150, falls on `Im`'s sample edges and covers its middle two columns and
// the middle of the white fill.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func tintwhite2b() ([]byte, error) {
	pdf := pdfgen.New()

	// srgbCurve is a curveType (ICC.1:2001-04, 6.5.3) sampling the sRGB transfer
	// function (IEC 61966-2-1) at 1024 points.
	srgbCurve := func() []byte {
		var values []int
		for i := range 1024 {
			v := float64(i) / 1023
			var lin float64
			if v <= 0.04045 {
				lin = v / 12.92
			} else {
				lin = math.Pow((v+0.055)/1.055, 2.4)
			}
			values = append(values, int(math.RoundToEven(lin*65535)))
		}
		out := iccU32(append([]byte("curv"), make([]byte, 4)...), len(values))
		for _, v := range values {
			out = iccU16(out, v)
		}
		return out
	}

	tintedProfile := func() []byte {
		tags := []iccTag{{"desc", iccTextDescription("litho tinted white RGB")}}
		tags = append(tags, iccCommonTags...)
		for _, p := range iccPrimaries {
			xyz := p.XYZ
			if p.Sig == "bXYZ" {
				xyz = make([]float64, len(p.XYZ))
				for i, v := range p.XYZ {
					xyz[i] = 0.9 * v
				}
			}
			tags = append(tags, iccTag{p.Sig, iccXYZ(xyz...)})
		}
		for _, sig := range []string{"rTRC", "gTRC", "bTRC"} {
			tags = append(tags, iccTag{sig, srgbCurve()})
		}
		return iccProfile("mntr", "RGB ", "XYZ ", tags)
	}

	profile := pdf.Stream(tintedProfile())
	profile.Set("N", 3)
	tw := pdf.MakeIndirect(a(n("ICCBased"), pdf.MakeIndirect(profile)))

	row := []byte{255, 255, 255, 250, 250, 250, 240, 240, 240, 128, 64, 32}

	image := pdf.Stream(bytes.Repeat(row, 4))
	image.Set("Type", n("XObject"))
	image.Set("Subtype", n("Image"))
	image.Set("Width", 4)
	image.Set("Height", 4)
	image.Set("BitsPerComponent", 8)
	image.Set("ColorSpace", tw)

	resources := d(
		"ColorSpace", d("T", tw),
		"XObject", d("Im", pdf.MakeIndirect(image)),
	)
	content := "q 100 0 0 80 0 110 cm /Im Do Q " +
		"q /T cs 1 1 1 sc 10 60 80 40 re f Q " +
		"q /T cs 0.97 0.97 0.97 sc 10 10 40 35 re f 0.5 0.6 0.7 sc 50 10 40 35 re f Q\n"
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 100, 200),
		"Resources", resources,
		"Contents", pdf.Stream([]byte(content)),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
