package build

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("greygroup_2b", greygroup2b) }

// greygroup2b builds greygroup_2b.pdf: colours entering grey ICCBased transparency
// groups, PDF/A-2b.
//
// ISO 32000-1, 11.3.4 converts a colour painted into a transparency group into
// the group's blending colour space, which for an ICCBased space is its
// profile's (8.6.5.5). A grey profile whose tone curve is not sRGB's makes the
// conversion visible: DeviceGray's formula of 10.3 (0.3 red, 0.59 green, 0.11
// blue) gives a grey that the profile then reads as another colour. Every page
// names a space of its own:
//
// | Page | Group /CS                     | Draws                                                    |
// | ---- | ----------------------------- | -------------------------------------------------------- |
// | 1    | `linear`: XYZ, tone curve 1.0 | an RGB grey 0.66 over the page                           |
// | 2    | RGB (page), `linear` (mask)   | black through a luminosity soft mask of an RGB grey 0.5  |
// | 3    | `lstar`: Lab, AToB0/1, BToA0/1 | a fill of the group's own space at 0.5 under /Perceptual |
//
// Pages 1 and 3 also paint a 2 x 2 point square at ca 0.5 in their bottom left
// corner, the transparency that makes the page group blend in its space.
//
// `linear` is a version 2.1 grey display profile over XYZ with a tone curve of
// gamma 1.0, so a grey component is the luminance Y. `lstar` is a version 2.1
// grey display profile over Lab whose relative colorimetric tables (AToB1,
// BToA1) give L* = 100 x grey, while its perceptual ones (AToB0, BToA0) read
// 0.5 as L* 70: the perceptual colour cannot keep its components in the
// group, read back under relative colorimetric, and reaches it through sRGB.
// RGB colours are in `rgb`, a gamma 2.2 display profile with the sRGB
// primaries.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func greygroup2b() ([]byte, error) {
	pdf := pdfgen.New()

	iccSpace := func(data []byte, nComp int) *pdfgen.Array {
		stream := pdf.Stream(data)
		stream.Set("N", nComp)
		arr := a(n("ICCBased"), pdf.MakeIndirect(stream))
		pdf.MakeIndirect(arr)
		return arr
	}

	linearProfile := func() []byte {
		tags := slices.Concat(
			[]iccTag{{"desc", iccTextDescription("litho linear grey")}},
			iccCommonTags,
			[]iccTag{{"kTRC", iccGammaCurve(1.0)}},
		)
		return iccProfile("mntr", "GRAY", "XYZ ", tags)
	}

	ramp := make([]byte, 256)
	for i := range ramp {
		ramp[i] = byte(i)
	}

	// greyToLab is a lut8 (ICC.1:2001-04, 6.5.8) from one grey input to neutral Lab over
	// a grid of the given L* values, in the v2 8-bit encoding (L* * 255 / 100).
	greyToLab := func(lightness []float64) []byte {
		clut := make([]byte, 0, 3*len(lightness))
		for _, v := range lightness {
			clut = append(clut, byte(math.RoundToEven(v*255/100)), 128, 128)
		}
		out := append([]byte("mft1"), make([]byte, 4)...)
		out = append(out, 1, 3, byte(len(lightness)), 0) //nolint:gosec // a handful of grid points.
		out = append(out, iccS15(iccIdentity...)...)
		out = append(out, ramp...)
		out = append(out, clut...)
		for range 3 {
			out = append(out, ramp...)
		}
		return out
	}

	const labGrid = 11

	// labToGrey is a lut8 from Lab to one grey output over an 11-point grid, L* 0, 10 ...
	// 100 (a* and b* ignored), the grey of each L* given by grey.
	labToGrey := func(grey func(float64) float64) []byte {
		clut := make([]byte, 0, labGrid*labGrid*labGrid)
		for i := range labGrid {
			v := byte(math.RoundToEven(255 * grey(float64(10*i))))
			for range labGrid * labGrid {
				clut = append(clut, v)
			}
		}
		out := append([]byte("mft1"), make([]byte, 4)...)
		out = append(out, 3, 1, labGrid, 0)
		out = append(out, iccS15(iccIdentity...)...)
		for range 3 {
			out = append(out, ramp...)
		}
		out = append(out, clut...)
		out = append(out, ramp...)
		return out
	}

	// perceptualGrey is the inverse of AToB0's L* 0, 70, 100 at grey 0, 0.5, 1.
	perceptualGrey := func(lightness float64) float64 {
		if lightness <= 70 {
			return 0.5 * lightness / 70
		}
		return 0.5 + 0.5*(lightness-70)/30
	}

	lstarProfile := func() []byte {
		tags := slices.Concat(
			[]iccTag{{"desc", iccTextDescription("litho L* grey")}},
			iccCommonTags,
			[]iccTag{
				{"kTRC", iccGammaCurve(1.0)},
				{"A2B0", greyToLab([]float64{0, 70, 100})},
				{"A2B1", greyToLab([]float64{0, 100})},
				{"B2A0", labToGrey(perceptualGrey)},
				{"B2A1", labToGrey(func(lightness float64) float64 { return lightness / 100 })},
			},
		)
		return iccProfile("mntr", "GRAY", "Lab ", tags)
	}

	transparencyGroup := func(cs any) *pdfgen.Dict {
		return d("Type", n("Group"), "S", n("Transparency"), "CS", cs)
	}

	addPage := func(content string, resources, group *pdfgen.Dict) {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 100, 100),
			"Resources", resources,
			"Contents", pdf.Stream([]byte(content)),
			"Group", group,
		)
		pdf.AddPage(pdf.Indirect(page))
	}

	half := d("Type", n("ExtGState"), "ca", 0.5)

	rgb1 := iccSpace(iccRGBDisplayProfile(), 3)
	linear1 := iccSpace(linearProfile(), 1)
	addPage(
		"/RGB cs 0.66 0.66 0.66 sc 0 0 100 100 re f q /A gs 0.2 0.2 0.2 sc 0 0 2 2 re f Q\n",
		d("ColorSpace", d("RGB", rgb1), "ExtGState", d("A", half)),
		transparencyGroup(linear1),
	)

	rgb2 := iccSpace(iccRGBDisplayProfile(), 3)
	linear2 := iccSpace(linearProfile(), 1)
	maskGroup := pdf.Stream([]byte("/RGB cs 0.5 0.5 0.5 sc 0 0 100 100 re f\n"))
	maskGroup.Set("Type", n("XObject"))
	maskGroup.Set("Subtype", n("Form"))
	maskGroup.Set("BBox", a(0, 0, 100, 100))
	maskGroup.Set("Group", transparencyGroup(linear2))
	maskGroup.Set("Resources", d("ColorSpace", d("RGB", rgb2)))
	masked := d(
		"Type", n("ExtGState"),
		"SMask", d("Type", n("Mask"), "S", n("Luminosity"), "G", pdf.MakeIndirect(maskGroup)),
	)
	addPage(
		"/RGB cs 1 1 1 sc 0 0 100 100 re f q /M gs 0 0 0 sc 10 10 80 80 re f Q\n",
		d("ColorSpace", d("RGB", rgb2), "ExtGState", d("M", masked)),
		transparencyGroup(rgb2),
	)

	lstar3 := iccSpace(lstarProfile(), 1)
	addPage(
		"q /Perceptual ri /L cs 0.5 sc 0 50 100 50 re f Q q /A gs /L cs 0 sc 0 0 2 2 re f Q\n",
		d("ColorSpace", d("L", lstar3), "ExtGState", d("A", half)),
		transparencyGroup(lstar3),
	)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
