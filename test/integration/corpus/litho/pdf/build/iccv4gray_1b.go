package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("iccv4gray_1b", iccv4gray1b) }

// iccv4gray1b builds iccv4gray_1b.pdf: a grey output intent whose ICC v4 profile carries
// the tags a colour engine reads, at PDF/A-1b.
//
// PDF/A-1 rests on PDF 1.4, which predates ICC v4, so ISO 19005-1, 6.2.2 takes
// the destination profile of PDF Reference 1.4, 9.10.4, an ICC.1:1998-09 (v2)
// profile, and veraPDF's test reads "version < 3.0". iccv4intent_1b.go plants a
// v4 profile with no tags at all; this one carries the shape of the v4.2 grey
// display profile pdfjs issue19532.pdf has in its intent, whose every tag has a
// version 2 form only through another type:
//
//   - desc and cprt are multiLocalizedUnicodeType (ICC.1:2010, 10.15), which
//     version 2 writes as textDescriptionType and textType;
//   - kTRC is a parametricCurveType of function type 3 (10.18), the sRGB
//     transfer function of IEC 61966-2-1, which version 2 can only sample;
//   - wtpt is the D50 illuminant, as ICC.1:2010, 9.2.36 has a v4 display
//     profile state it, with chad the Bradford adaptation from D65 (9.2.15),
//     where a v2 display profile records the display's own white.
//
// Remediation re-encodes it as version 2 with the same colour transforms. No
// file of the litho or veraPDF corpora reaches that conversion; the pdfjs file
// that does is not remediated by the gate.
//
// Every profile value is computed here from published constants: the sRGB
// curve parameters and D65 chromaticity of IEC 61966-2-1, and the Bradford cone
// response matrix of ICC.1:2010, Annex E.3.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func iccv4gray1b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccVersion42 = 0x04200000
	// Variables, not constants: Go would fold a constant quotient exactly,
	// where Python divides two rounded floats.
	x, y, one := 0.3127, 0.3290, 1.0
	d65 := [3]float64{x / y, 1.0, (one - x - y) / y}
	d50 := [3]float64{0.9642, 1.0, 0.8249}

	chad := iccBradford(d65, d50)
	var chadValues []float64
	for _, row := range chad {
		chadValues = append(chadValues, row[:]...)
	}
	curve := iccU16(iccU16(append([]byte("para"), make([]byte, 4)...), 3), 0)
	curve = append(curve, iccS15(2.4, one/1.055, 0.055/1.055, one/12.92, 0.04045)...)
	profile := iccGreyProfile(iccVersion42, []iccTag{
		{"desc", iccMluc("litho sRGB grey v4")},
		{"cprt", iccMluc("No copyright")},
		{"wtpt", append(append([]byte("XYZ "), make([]byte, 4)...), iccS15(d50[:]...)...)},
		{"chad", append(append([]byte("sf32"), make([]byte, 4)...), iccS15(chadValues...)...)},
		{"kTRC", curve},
	})

	destination := pdf.Stream(profile)
	destination.Set("N", 1)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("sGray"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
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

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte("BT /F1 24 Tf 20 100 Td (AB) Tj ET\n"))),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 1, Conformance: "B"}))
	return save(pdf, "1.4", noFixMetadataVersion)
}
