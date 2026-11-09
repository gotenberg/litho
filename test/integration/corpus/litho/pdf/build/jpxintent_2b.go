package build

import (
	"slices"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("jpxintent_2b", jpxintent2b) }

// jpxintent2b builds jpxintent_2b.pdf: a JPEG 2000 image taking its colour from its own
// data under the graphics state's rendering intent, PDF/A-2b.
//
// ISO 32000-1, 7.4.9 has a JPXDecode image without /ColorSpace take its colour
// from the JPEG 2000 data, and 8.6.5.8 has an image that states no /Intent
// converted with the graphics state's, which the ri operator and an ExtGState
// /RI set. The image here is one 8 x 8 grey component of samples 128 in a JP2
// container whose one colour specification, METH 3 (ISO/IEC 15444-2, M.11.7.2:
// any ICC profile; ISO 19005-2, 6.2.8.3 test 3 permits it), carries `rsplit`,
// a version 2.1 grey input profile over Lab whose colorimetric table (A2B1)
// reads the samples at L* 50 and whose perceptual table (A2B0) at L* 13: the
// tables of imageintent_2b's `split`, swapped, so the initial intent draws the
// mid grey a reader that ignores the profile draws for sample 128.
//
// One 100 x 200 page paints `Im` three times:
//
// | Where                  | Painted                    | Reads     |
// | ---------------------- | -------------------------- | --------- |
// | 80 x 50 at (10, 140)   | with no ri                 | mid, 119  |
// | 8 x 8 at (10, 80)      | after `/Perceptual ri`     | dark, 33  |
// | 8 x 8 at (30, 80)      | after `/P gs`, /RI         | dark, 33  |
//
// The codestream was produced once with OpenJPEG 2.5.4, its COM marker
// stripped, as jp2.go describes:
//
//	python3 -c "open('one.raw','wb').write(bytes([128]*64))"
//	opj_compress -i one.raw -o one.j2k -F 8,8,1,8,u -n 1 -r 1
//
// See ../FIXTURES.md for the authoritative expected firing set.
func jpxintent2b() ([]byte, error) {
	pdf := pdfgen.New()

	one := jp2MustDecode(
		"/0//UQApAAAAAAAIAAAACAAAAAAAAAAAAAAACAAAAAgAAAAAAAAAAAABBwEB/1IADAAAAAEA" +
			"AAQEAAH/XAAEQED/kAAKAAAAAAAPAAH/k4D/2Q==",
	)

	// greyToLab is a lut8 from one grey input to Lab whose lightness rises
	// from 0 to white, an encoded L* (ICC.1:2001-04, 6.5.8: v2 Lab encoding,
	// L* * 255 / 100), with a* and b* 0.
	greyToLab := func(white int) []byte {
		return iccLut8(1, 3, []int{0, 128, 128, white, 128, 128})
	}

	rsplitProfile := func() []byte {
		tags := []iccTag{{"desc", iccTextDescription("litho reversed split intents")}}
		tags = append(tags, iccCommonTags...)
		tags = append(tags, iccTag{"A2B0", greyToLab(64)}, iccTag{"A2B1", greyToLab(255)})
		return iccProfile("scnr", "GRAY", "Lab ", tags)
	}

	// METH 3, PREC 0, APPROX 1 (ISO/IEC 15444-2, M.11.7.2), then the profile.
	colr := jp2Box("colr", slices.Concat([]byte{3, 0, 1}, rsplitProfile()))
	image := pdf.Stream(jp2Container(one, 1, 8, [][]byte{colr}, nil))
	image.Set("Type", n("XObject"))
	image.Set("Subtype", n("Image"))
	image.Set("Width", jp2Size)
	image.Set("Height", jp2Size)
	image.Set("Filter", n("JPXDecode"))

	resources := d(
		"XObject", d("Im", pdf.MakeIndirect(image)),
		"ExtGState", d("P", d("Type", n("ExtGState"), "RI", n("Perceptual"))),
	)
	content := "q 80 0 0 50 10 140 cm /Im Do Q " +
		"q /Perceptual ri 8 0 0 8 10 80 cm /Im Do Q " +
		"q /P gs 8 0 0 8 30 80 cm /Im Do Q\n"
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
