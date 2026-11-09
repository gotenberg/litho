package build

import (
	"bytes"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("cmykintent_2b", cmykintent2b) }

// cmykintent2b builds cmykintent_2b.pdf: colours under a rendering intent in a CMYK
// page group, PDF/A-2b.
//
// ISO 32000-1, 8.6.5.8 has the ri operator set the intent the colours painted
// after it convert under, and 11.3.4 converts each colour painted into a
// transparency group into the group's blending colour space. A colour already
// in that space keeps its components, which the group's composite reads back
// under relative colorimetric, the initial intent: a colour painted under
// another intent would then read as relative colorimetric reads it. The
// fixture paints in `split`, a version 2.1 CMYK output profile over Lab whose
// colorimetric table (A2B1) reads black ink K 0.5 at L* 50 and whose
// perceptual table (A2B0) at L* 75; both tables back from Lab invert the
// colorimetric one on the black axis.
//
// Two 100 x 200 pages paint the same rows, the first with no page group, the
// second with a page group whose /CS is `split`, where a 5 x 5 square of full
// black ink painted with Multiply at ca 0.5 in the top right corner makes the
// page composite in its group's space. Each row is 80 x 50 from x = 10:
//
// | Row       | Draws                                                          |
// | --------- | -------------------------------------------------------------- |
// | y 140-190 | `Im`, an 8 x 8 image of K 0.5 in `split`, no ri: L* 50         |
// | y 80-130  | `Im` after `/Perceptual ri`: L* 75                             |
// | y 20-70   | a fill of `0 0 0 0.5 sc` in `split` after `/Perceptual ri`     |
//
// See ../FIXTURES.md for the authoritative expected firing set.
func cmykintent2b() ([]byte, error) {
	pdf := pdfgen.New()

	// toLab is a lut8 from CMYK to Lab whose lightness falls by kDark (encoded
	// L*, ICC.1:2001-04, 6.5.8) with full black ink and by 64 with each full
	// colour ink, a* and b* 0.
	toLab := func(kDark int) []byte {
		var clut []int
		for c := 0; c <= 1; c++ {
			for m := 0; m <= 1; m++ {
				for y := 0; y <= 1; y++ {
					for k := 0; k <= 1; k++ {
						clut = append(clut, max(0, 255-kDark*k-64*(c+m+y)), 128, 128)
					}
				}
			}
		}
		return iccLut8(4, 3, clut)
	}

	// fromLab is a lut8 from Lab to CMYK taking lightness to black ink alone.
	fromLab := func() []byte {
		var clut []int
		for lightness := 0; lightness <= 1; lightness++ {
			for range 2 {
				for range 2 {
					k := 255
					if lightness != 0 {
						k = 0
					}
					clut = append(clut, 0, 0, 0, k)
				}
			}
		}
		return iccLut8(3, 4, clut)
	}

	splitProfile := func() []byte {
		tags := []iccTag{{"desc", iccTextDescription("litho split CMYK intents")}}
		tags = append(tags, iccCommonTags...)
		tags = append(tags,
			iccTag{"A2B0", toLab(128)}, iccTag{"A2B1", toLab(255)},
			iccTag{"B2A0", fromLab()}, iccTag{"B2A1", fromLab()})
		return iccProfile("prtr", "CMYK", "Lab ", tags)
	}

	profile := pdf.Stream(splitProfile())
	profile.Set("N", 4)
	profileRef := pdf.MakeIndirect(profile)

	const samples = 8

	// pageResources returns the resources of one page: its own `split` space
	// and image over the one profile stream. PDF/A-4's 6.2.4.2 test 3 judges an
	// indirect ICCBased CMYK space once for the document, under the first page
	// that reaches it, as veraPDF 1.30.2 does; a space both pages shared would
	// be judged under page 1, which has no group, and a reordering of the pages
	// would move the verdict.
	pageResources := func() (*pdfgen.Array, *pdfgen.Dict) {
		split := a(n("ICCBased"), profileRef)
		pdf.MakeIndirect(split)
		image := pdf.Stream(bytes.Repeat([]byte{0, 0, 0, 128}, samples*samples))
		image.Set("Type", n("XObject"))
		image.Set("Subtype", n("Image"))
		image.Set("Width", samples)
		image.Set("Height", samples)
		image.Set("BitsPerComponent", 8)
		image.Set("ColorSpace", split)
		return split, d(
			"ColorSpace", d("S", split),
			"XObject", d("Im", pdf.MakeIndirect(image)),
			"ExtGState", d("M", d("Type", n("ExtGState"), "BM", n("Multiply"), "ca", 0.5)),
		)
	}

	rows := "q 80 0 0 50 10 140 cm /Im Do Q " +
		"q /Perceptual ri 80 0 0 50 10 80 cm /Im Do Q " +
		"q /Perceptual ri /S cs 0 0 0 0.5 sc 10 20 80 50 re f Q\n"
	blend := "q /M gs /S cs 0 0 0 1 sc 95 195 5 5 re f Q "

	// Page 1 paints no transparency, which ISO 19005-2, 6.2.10 would otherwise
	// have carry a page group with no output intent.
	for _, p := range []struct {
		content string
		grouped bool
	}{{rows, false}, {blend + rows, true}} {
		split, resources := pageResources()
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 100, 200),
			"Resources", resources,
			"Contents", pdf.Stream([]byte(p.content)),
		)
		if p.grouped {
			page.Set("Group", d("Type", n("Group"), "S", n("Transparency"), "CS", split))
		}
		pdf.AddPage(pdf.Indirect(page))
	}

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
