package build

import (
	"math"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("iccv4space_1b", iccv4space1b) }

// iccv4space1b builds iccv4space_1b.pdf: ICCBased colour spaces over an ICC v4 profile, at
// PDF/A-1b.
//
// PDF/A-1 rests on PDF 1.4, whose ICCBased colour spaces take ICC.1:1998-09 (v2)
// profiles (PDF Reference 1.4, 4.5.4), so ISO 19005-1, 6.2.3.2 fails a v4 one,
// and veraPDF's ICCInputProfile test reads "version < 3.0". PDF/A-2 and later
// admit v4 (ISO 19005-2, 6.2.4.2). iccv4gray_1b.go puts the same profile in the
// output intent; this one puts it where the content paints:
//
//   - CS0, a grey ICCBased space over the v4.2 display profile, fills a square;
//   - Im0, a 2x2 grey image, names the same profile stream in its own
//     /ColorSpace, so the page and the image share one profile object;
//   - CS1, the control, fills a second square in a grey ICCBased space over a
//     v2.4 profile of the same transfer function, which no level fails.
//
// The v4 profile has the shape of the v4.2 grey display profile pdfjs
// issue19532.pdf carries: desc and cprt as multiLocalizedUnicodeType, the sRGB
// transfer function as a type 3 parametricCurveType, and a D50 white point with
// the Bradford adaptation from D65 in chad. None of those tags has its v4 type in
// v2 (ICC.1:2001-04, 6.5), so the PDF/A-1 remediation must re-encode each one to
// keep the colours. The file declares no output intent: ICCBased spaces need
// none (ISO 19005-1, 6.2.3.3 judges device spaces alone).
//
// Every profile value is computed here from published constants: the sRGB curve
// parameters and D65 chromaticity of IEC 61966-2-1, and the Bradford cone
// response matrix of ICC.1:2010, Annex E.3.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func iccv4space1b() ([]byte, error) {
	pdf := pdfgen.New()

	const (
		iccVersion42 = 0x04200000
		iccVersion24 = 0x02400000
	)
	// Variables, not constants: Go would fold a constant quotient exactly, and
	// the committed profile holds the quotient of two rounded floats.
	x, y, one := 0.3127, 0.3290, 1.0
	d65 := [3]float64{x / y, 1.0, (one - x - y) / y}
	d50 := [3]float64{0.9642, 1.0, 0.8249}
	srgbCurve := []float64{2.4, one / 1.055, 0.055 / 1.055, one / 12.92, 0.04045}

	// srgb is the sRGB transfer function of IEC 61966-2-1.
	srgb := func(x float64) float64 {
		g, a, b, c, d := srgbCurve[0], srgbCurve[1], srgbCurve[2], srgbCurve[3], srgbCurve[4]
		if x >= d {
			return math.Pow(float64(a*x)+b, g)
		}
		return c * x
	}

	chad := iccBradford(d65, d50)
	var chadValues []float64
	for _, row := range chad {
		chadValues = append(chadValues, row[:]...)
	}
	zero4 := make([]byte, 4)
	tagged := func(sig string, rest ...[]byte) []byte {
		out := append([]byte(sig), zero4...)
		for _, r := range rest {
			out = append(out, r...)
		}
		return out
	}
	v4Profile := iccGreyProfile(iccVersion42, []iccTag{
		{"desc", iccMluc("litho sRGB grey v4")},
		{"cprt", iccMluc("No copyright")},
		{"wtpt", tagged("XYZ ", iccS15(d50[:]...))},
		{"chad", tagged("sf32", iccS15(chadValues...))},
		{"kTRC", tagged("para", iccU16(iccU16(nil, 3), 0), iccS15(srgbCurve...))},
	})
	const samples = 1024
	curve := iccU32(nil, samples)
	for i := range samples {
		curve = iccU16(curve, int(math.RoundToEven(srgb(float64(i)/(samples-1))*65535)))
	}
	v2Profile := iccGreyProfile(iccVersion24, []iccTag{
		{"desc", iccTextDescription("litho sRGB grey v2")},
		{"cprt", tagged("text", []byte("No copyright\x00"))},
		{"wtpt", tagged("XYZ ", iccS15(d65[:]...))},
		{"kTRC", tagged("curv", curve)},
	})

	iccbased := func(profile []byte) pdfgen.Object {
		stream := pdf.Stream(profile)
		stream.Set("N", 1)
		return pdf.MakeIndirect(stream)
	}

	v4 := iccbased(v4Profile)
	v2 := iccbased(v2Profile)

	image := pdf.MakeIndirect(pdf.Stream(
		[]byte{0x00, 0x55, 0xAA, 0xFF},
		"Type", n("XObject"),
		"Subtype", n("Image"),
		"Width", 2,
		"Height", 2,
		"BitsPerComponent", 8,
		"ColorSpace", a(n("ICCBased"), v4),
	))

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoBareSans"),
		"Flags", 32, // non-symbolic, ISO 32000-1, Table 123
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontBBox", a(0, -200, 550, 700),
		"FontFile2", pdf.MakeIndirect(fontFile),
	)
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoBareSans"),
		"FirstChar", 0x41,
		"LastChar", 0x42,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))

	content := "/CS0 cs 0.5 sc 20 20 40 40 re f\n" +
		"/CS1 cs 0.5 sc 140 20 40 40 re f\n" +
		"q 40 0 0 40 80 20 cm /Im0 Do Q\n" +
		"/CS0 cs 0 sc BT /F1 24 Tf 20 120 Td (AB) Tj ET\n"
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d(
			"ColorSpace", d("CS0", a(n("ICCBased"), v4), "CS1", a(n("ICCBased"), v2)),
			"XObject", d("Im0", image),
			"Font", d("F1", font),
		),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 1, Conformance: "B"}))
	return save(pdf, "1.4", noFixMetadataVersion)
}
