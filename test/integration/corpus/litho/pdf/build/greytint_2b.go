package build

import (
	"bytes"
	"math"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("greytint_2b", greytint2b) }

// greytint2b builds greytint_2b.pdf: a grey ICC profile that tints its greys, PDF/A-2b.
//
// An ICCBased space reads its colours through its profile (ISO 32000-1,
// 8.6.5.5), and a grey profile's AToB table maps a grey sample to any PCS
// colour (ICC.1:2001-04, 6.5.8), not only to the neutral axis a grey tone curve
// stays on. `tint` is a version 2.1 grey input profile over Lab whose lut8
// AToB0 table, on a three-point grid, reads sample 0 as black (L* 0), sample
// 255 as white (L* 100), and the middle sample as a warm brown, L* 50, a* 30,
// b* 20, the samples between interpolated. Black and white stay neutral, so
// neither black point compensation nor Little CMS's white fix-up moves them.
//
// One 100 x 200 page paints the space four ways:
//
// | y       | x                                                                       |
// | ------- | ----------------------------------------------------------------------- |
// | 110-190 | 0-100: `Im`, an image of 4 x 4 samples, each row 0, 85, 170 and 255     |
// | 60-100  | 10-90: a fill of colour 0.5 (`/S cs 0.5 sc`)                            |
// | 10-45   | 10-50: an axial shading from 0 to 1; 50-90: an inline image of 4 x 1    |
// |         | samples 0, 85, 170 and 255                                              |
//
// so every middle grey reads brown, as poppler draws it. The shading and the
// inline image lie below the page's centred quarter, x 25-75 and y 50-150, the
// region the redaction cells clear, since a redaction refuses to cross a
// shading. The region's edges fall between `Im`'s samples, so it covers the
// middle two samples of the bottom two rows whole.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func greytint2b() ([]byte, error) {
	pdf := pdfgen.New()

	// greyToTintedLab is a lut8 (ICC.1:2001-04, 6.5.8) from one grey input to Lab
	// over a three-point grid: L* 0, then L* 50 a* 30 b* 20, then L* 100, in the v2
	// 8-bit Lab encoding (L* * 255 / 100, a* and b* + 128).
	greyToTintedLab := func() []byte {
		ramp := make([]byte, 256)
		for i := range ramp {
			ramp[i] = byte(i)
		}
		l50 := byte(math.RoundToEven(50.0 * 255 / 100))
		clut := []byte{0, 128, 128, l50, 128 + 30, 128 + 20, 255, 128, 128}
		var out []byte
		out = append(out, "mft1"...)
		out = append(out, 0, 0, 0, 0)
		out = append(out, 1, 3, 3, 0)
		out = append(out, iccS15(iccIdentity...)...)
		out = append(out, ramp...)
		out = append(out, clut...)
		return append(out, bytes.Repeat(ramp, 3)...)
	}

	tintProfile := func() []byte {
		tags := []iccTag{{"desc", iccTextDescription("litho tinted grey")}}
		tags = append(tags, iccCommonTags...)
		tags = append(tags, iccTag{"A2B0", greyToTintedLab()})
		return iccProfile("scnr", "GRAY", "Lab ", tags)
	}

	profile := pdf.Stream(tintProfile(), "N", 1)
	tint := pdf.MakeIndirect(a(n("ICCBased"), pdf.MakeIndirect(profile)))

	ramp := []byte{0, 85, 170, 255}

	image := pdf.Stream(bytes.Repeat(ramp, 4),
		"Type", n("XObject"), "Subtype", n("Image"),
		"Width", len(ramp), "Height", 4, "BitsPerComponent", 8, "ColorSpace", tint)

	shading := d(
		"ShadingType", 2,
		"ColorSpace", tint,
		"Coords", a(10, 0, 50, 0),
		"Function", d("FunctionType", 2, "Domain", a(0, 1), "C0", a(0), "C1", a(1), "N", 1),
	)

	resources := d(
		"ColorSpace", d("S", tint),
		"XObject", d("Im", pdf.MakeIndirect(image)),
		"Shading", d("Sh", pdf.Indirect(shading)),
	)
	inline := append(append([]byte("BI /W 4 /H 1 /BPC 8 /CS /S ID "), ramp...), " EI"...)
	content := append([]byte("q 100 0 0 80 0 110 cm /Im Do Q "+
		"q /S cs 0.5 sc 10 60 80 40 re f Q "+
		"q 10 10 40 35 re W n /Sh sh Q "+
		"q 40 0 0 35 50 10 cm "), inline...)
	content = append(content, " Q\n"...)
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 100, 200),
		"Resources", resources,
		"Contents", pdf.Stream(content),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
