package build

import (
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen/zdeflate"
)

func init() { register("usebpcgroup_4", usebpcgroup4) }

// usebpcgroup4 builds usebpcgroup_4.pdf: /UseBlackPtComp /OFF into a CMYK group, PDF/A-4.
//
// ISO 32000-2, Table 57 lets a graphics state turn black point compensation off
// (/UseBlackPtComp /OFF) for the colours painted under it. usebpc_4 reaches the
// entry in conversions to sRGB alone; no corpus file paints under /OFF into a
// CMYK blending colour space, where a colour converts from sRGB into the group's
// CMYK profile. This fixture does, on its one 100 x 200 page.
//
// The page paints one form, `G`, over the whole page: an isolated transparency
// group whose /CS is `[/ICCBased cmyk]`, the version 4 CMYK output profile of
// litho's ICC test data (pkg/internal/graphics/icc/testdata/v4-cmyk-lut.icc),
// whose black is the version 4 perceptual reference black, lighter than sRGB's.
// A 10 x 5 square painted with Multiply at ca 0.5 in the form's top right corner
// makes it blend, so the group is composited in its /CS (an isolated group whose
// content blends Normal alone paints straight into its parent). Each row is
// eight 10-point steps from x = 10, in `[/ICCBased display]`, the version 2.1
// display profile of `icc.go` (sRGB's primaries, gamma 2.2), whose black is
// sRGB's, so only the trip into the group's CMYK depends on the state:
//
// | Row       | Draws                                                        |
// | --------- | ------------------------------------------------------------ |
// | y 140-190 | fills of `v v v sc`, v = 0, 0.04 ... 0.28, under /ON         |
// | y 80-130  | the same fills under /OFF                                    |
// | y 20-70   | `Rm`, an 8 x 1 image of the same ramp, samples 0 to 71, /OFF |
//
// Under /ON a colour enters the group compensated and reads back as itself;
// under /OFF it enters without compensation, as Little CMS converts without
// cmsFLAGS_BLACKPOINTCOMPENSATION, so the shadows below the profile's black
// crush to black when the composite is read back.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func usebpcgroup4() ([]byte, error) {
	cmykProfile := "pkg/internal/graphics/icc/testdata/v4-cmyk-lut.icc"

	pdf := pdfgen.New()

	profile, err := repoFile(cmykProfile)
	if err != nil {
		return nil, err
	}
	cmykStream := pdf.Stream(zdeflate.Compress(profile, 9))
	cmykStream.Set("N", 4)
	cmykStream.Set("Filter", n("FlateDecode"))
	cmyk := pdf.MakeIndirect(a(n("ICCBased"), pdf.MakeIndirect(cmykStream)))

	displayStream := pdf.Stream(iccRGBDisplayProfile())
	displayStream.Set("N", 3)
	display := pdf.MakeIndirect(a(n("ICCBased"), pdf.MakeIndirect(displayStream)))

	steps := make([]float64, 8)
	for i := range steps {
		steps[i] = 0.04 * float64(i)
	}
	var samples []byte
	for _, v := range steps {
		for range 3 {
			samples = append(samples, byte(math.RoundToEven(255*v)))
		}
	}
	rampImage := pdf.Stream(samples)
	rampImage.Set("Type", n("XObject"))
	rampImage.Set("Subtype", n("Image"))
	rampImage.Set("Width", len(steps))
	rampImage.Set("Height", 1)
	rampImage.Set("BitsPerComponent", 8)
	rampImage.Set("ColorSpace", display)

	fills := func(y int) string {
		var b strings.Builder
		for i, v := range steps {
			fmt.Fprintf(&b, "%.2f %.2f %.2f sc %d %d 10 50 re f ", v, v, v, 10+10*i, y)
		}
		return b.String()
	}

	content := "/S cs " +
		"q /On gs " + fills(140) + "Q " +
		"q /Off gs " + fills(80) + "Q " +
		"q /Off gs 80 0 0 50 10 20 cm /Rm Do Q " +
		"q /H gs 0.5 0.5 0.5 sc 90 195 10 5 re f Q\n"
	form := pdf.Stream([]byte(content))
	form.Set("Type", n("XObject"))
	form.Set("Subtype", n("Form"))
	form.Set("BBox", a(0, 0, 100, 200))
	form.Set("Group", d("Type", n("Group"), "S", n("Transparency"), "I", true, "CS", cmyk))
	form.Set("Resources", d(
		"ColorSpace", d("S", display),
		"XObject", d("Rm", pdf.MakeIndirect(rampImage)),
		"ExtGState", d(
			"Off", d("Type", n("ExtGState"), "UseBlackPtComp", n("OFF")),
			"On", d("Type", n("ExtGState"), "UseBlackPtComp", n("ON")),
			"H", d("Type", n("ExtGState"), "ca", 0.5, "BM", n("Multiply")),
		),
	))
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 100, 200),
		// ISO 19005-4, 6.2.9: with no output intent, a page that holds
		// transparency names its group's colour space.
		"Group", d("Type", n("Group"), "S", n("Transparency"), "CS", display),
		"Resources", d("XObject", d("G", pdf.MakeIndirect(form))),
		"Contents", pdf.Stream([]byte("/G Do\n")),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 4, Rev: 2020}))
	return save(pdf, "2.0")
}
