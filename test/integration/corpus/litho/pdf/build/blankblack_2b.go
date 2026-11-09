package build

import (
	"fmt"
	"slices"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("blankblack_2b", blankblack2b) }

// blankblack2b builds blankblack_2b.pdf: images whose zero samples are not black, at
// PDF/A-2b.
//
// Redaction blanks the samples an image paints inside a region to the image's
// black. Zero on every component is that black in the device and CIE-based
// spaces, but ISO 32000-1, 8.6.5.5 lets an ICC profile give a sample any
// meaning: in a YCC profile zero is a saturated colour. The one corpus file
// holding such an image (pdfjs issue2856, a version 4 YCC-RGB profile) is
// outside the render sample the redact-render cell reads, so nothing gated
// what a blanked YCC image shows. The other images hold the colour spaces
// blanking rewrites in sRGB or whose /Decode it folds into the samples.
//
// | Draws | Space                                                  | Blanking writes                              |
// | ----- | ------------------------------------------------------ | -------------------------------------------- |
// | Im0   | CalGray with /Decode [1 0]                             | 0, the array folded in and dropped           |
// | Im1   | Indexed over CalRGB, entry 0 white                     | sRGB black, in litho's sRGB profile          |
// | Im2   | ICCBased, a YCC-like profile (Y, Cb, Cr over Lab), /N 3 | the profile's black, Y 0 and Cb, Cr 128      |
// | Im3   | Lab                                                    | sRGB black, in litho's sRGB profile          |
// | Im4   | Separation /Spot, CalGray alternate, tint 0 white      | sRGB black, in litho's sRGB profile          |
//
// The YCC-like profile is a version 2.1 input profile whose lut8 A2B0 maps Y
// to L* and Cb, Cr to b*, a* over a two-point grid: its black is Y 0, Cb and
// Cr 128, and zero samples are L* 0, a* and b* -128, which litho, poppler and
// PDFium all show as a saturated blue. Each image is a 16x16 ramp, drawn
// side by side, the YCC image in the middle of the page, where every scenario
// of the redact-render cell blanks part of it.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func blankblack2b() ([]byte, error) {
	pdf := pdfgen.New()

	d50 := []any{0.9642, 1, 0.8249}
	const size = 16

	// yccA2B0 maps Y, Cb, Cr to Lab: L* follows Y, b* follows Cb and a* Cr,
	// each over its full range, so the neutral axis is Cb = Cr = 128.
	yccA2B0 := func() []byte {
		var clut []int
		for y := 0; y < 2; y++ {
			for cb := 0; cb < 2; cb++ {
				for cr := 0; cr < 2; cr++ {
					clut = append(clut, 255*y, 255*cr, 255*cb)
				}
			}
		}
		return iccLut8(3, 3, clut)
	}

	tags := slices.Concat(
		[]iccTag{{"desc", iccTextDescription("litho YCC-like input")}},
		iccCommonTags,
		[]iccTag{{"A2B0", yccA2B0()}},
	)
	yccProfile := iccProfile("scnr", "RGB ", "Lab ", tags)
	yccStream := pdf.Stream(yccProfile)
	yccStream.Set("N", 3)
	ycc := a(n("ICCBased"), pdf.MakeIndirect(yccStream))

	calGray := a(n("CalGray"), d("WhitePoint", a(d50...), "Gamma", 1))
	calRGB := a(n("CalRGB"), d("WhitePoint", a(d50...)))
	lab := a(n("Lab"), d("WhitePoint", a(d50...), "Range", a(-128, 127, -128, 127)))
	spot := a(
		n("Separation"),
		n("Spot"),
		calGray,
		d("FunctionType", 2, "Domain", a(0, 1), "C0", a(1), "C1", a(0), "N", 1),
	)

	// ramp is a 16x16 ramp: each sample's components run from light to dark
	// across the image, a different phase per component.
	ramp := func(components int) []byte {
		var out []byte
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				for c := 0; c < components; c++ {
					out = append(out, byte((x*16+y*4+c*64)%256))
				}
			}
		}
		return out
	}

	image := func(colourSpace pdfgen.Object, samples []byte, extra ...any) *pdfgen.Stream {
		img := pdf.Stream(samples)
		img.Set("Type", n("XObject"))
		img.Set("Subtype", n("Image"))
		img.Set("Width", size)
		img.Set("Height", size)
		img.Set("ColorSpace", colourSpace)
		img.Set("BitsPerComponent", 8)
		for i := 0; i < len(extra); i += 2 {
			img.Set(extra[i].(string), extra[i+1])
		}
		pdf.MakeIndirect(img)
		return img
	}

	// Y high, Cb and Cr near neutral: a light picture with a tint.
	var yccSamples []byte
	for i := 0; i < size*size; i++ {
		yccSamples = append(yccSamples, byte(160+i%64), byte(96+i%32), byte(150-i%32))
	}
	// A 16-entry palette, white first, then a ramp of greens.
	palette := []byte{255, 255, 255}
	for i := 1; i < 16; i++ {
		palette = append(palette, 40, byte(60+12*i), 40)
	}
	var indexedSamples []byte
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			indexedSamples = append(indexedSamples, byte(1+(x+y)%15))
		}
	}

	images := []*pdfgen.Stream{
		image(calGray, ramp(1), "Decode", a(1, 0)),
		image(a(n("Indexed"), calRGB, 15, pdfgen.String(palette)), indexedSamples),
		image(ycc, yccSamples),
		image(lab, ramp(3)),
		image(spot, ramp(1)),
	}

	var content []byte
	xobjects := d()
	for i, im := range images {
		content = fmt.Appendf(content, "q 112 0 0 288 %d 43 cm /Im%d Do Q\n", 16+120*i, i)
		xobjects.Set(fmt.Sprintf("Im%d", i), im)
	}
	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 392),
		"Resources", d("XObject", xobjects),
		"Contents", pdf.Stream(content),
	)
	pdf.AddPage(pdf.Indirect(page))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
