package build

import (
	"bytes"
	"slices"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("imageintent_2b", imageintent2b) }

// imageintent2b builds imageintent_2b.pdf: images under the graphics state's rendering
// intent, PDF/A-2b.
//
// ISO 32000-1, 8.6.5.8 has an image's /Intent override the current rendering
// intent, which the ri operator and an ExtGState /RI set: an image that states
// no /Intent is converted with the graphics state's. Every image here is an
// ICCBased grey image of 8 x 8 samples, all 128, under profile `split`, a
// version 2.1 grey input profile over Lab whose perceptual table (A2B0) reads
// its samples as greys of their own lightness and whose colorimetric table
// (A2B1) reads them at a quarter of that lightness. Both tables stay on the
// neutral axis, which is all litho reads a grey profile on. Perceptual and
// saturation (no A2B2: readers fall back on A2B0) draw sample 128 as a mid
// grey, L* 50; relative and absolute colorimetric, and the initial intent, draw
// it dark, L* 13. Black is L* 0 under both tables, so black point compensation
// moves neither.
//
// Two 100 x 200 pages, each painting three 80 x 50 images from x = 10:
//
// | Page | y 140-190                | y 80-130                                | y 20-70                                |
// | ---- | ------------------------ | --------------------------------------- | -------------------------------------- |
// | 1    | `Im`, no ri              | `Im` after `/Perceptual ri`             | `Im` after `/AbsoluteColorimetric ri`  |
// | 2    | `Im` after `/P gs` (/RI) | `Own`, /Intent /RelativeColorimetric,   | an inline image after `/Perceptual ri` |
// |      |                          | after `/Perceptual ri`                  |                                        |
//
// so page 1 reads dark, mid, dark, and page 2 mid, dark, mid.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func imageintent2b() ([]byte, error) {
	pdf := pdfgen.New()

	// greyToLab is a lut8 from one grey input to Lab whose lightness rises
	// from 0 to white, an encoded L* (ICC.1:2001-04, 6.5.8: v2 Lab encoding,
	// L* * 255 / 100), with a* and b* 0.
	greyToLab := func(white int) []byte {
		return iccLut8(1, 3, []int{0, 128, 128, white, 128, 128})
	}

	splitProfile := func() []byte {
		tags := slices.Concat(
			[]iccTag{{"desc", iccTextDescription("litho split intents")}},
			iccCommonTags,
			[]iccTag{{"A2B0", greyToLab(255)}, {"A2B1", greyToLab(64)}},
		)
		return iccProfile("scnr", "GRAY", "Lab ", tags)
	}

	profile := pdf.Stream(splitProfile())
	profile.Set("N", 1)
	split := pdf.MakeIndirect(a(n("ICCBased"), pdf.MakeIndirect(profile)))

	// Eight samples a side, so a region over part of an image covers whole samples.
	const samples = 8

	image := func(extra ...any) *pdfgen.Stream {
		stream := pdf.Stream(bytes.Repeat([]byte{128}, samples*samples))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Image"))
		stream.Set("Width", samples)
		stream.Set("Height", samples)
		stream.Set("BitsPerComponent", 8)
		stream.Set("ColorSpace", split)
		for i := 0; i < len(extra); i += 2 {
			stream.Set(extra[i].(string), extra[i+1])
		}
		pdf.MakeIndirect(stream)
		return stream
	}

	resources := d(
		"ColorSpace", d("S", split),
		"XObject", d("Im", image(), "Own", image("Intent", n("RelativeColorimetric"))),
		"ExtGState", d("P", d("Type", n("ExtGState"), "RI", n("Perceptual"))),
	)
	inline := "BI /W 8 /H 8 /BPC 8 /CS /S ID " + strings.Repeat("\x80", samples*samples) + " EI"
	for _, content := range []string{
		"q 80 0 0 50 10 140 cm /Im Do Q " +
			"q /Perceptual ri 80 0 0 50 10 80 cm /Im Do Q " +
			"q /AbsoluteColorimetric ri 80 0 0 50 10 20 cm /Im Do Q\n",
		"q /P gs 80 0 0 50 10 140 cm /Im Do Q " +
			"q /Perceptual ri 80 0 0 50 10 80 cm /Own Do Q " +
			"q /Perceptual ri 80 0 0 50 10 20 cm " + inline + " Q\n",
	} {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 100, 200),
			"Resources", resources,
			"Contents", pdf.Stream([]byte(content)),
		))
		pdf.AddPage(page)
	}

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
