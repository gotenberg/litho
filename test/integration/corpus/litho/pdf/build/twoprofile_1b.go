package build

import (
	"slices"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("twoprofile_1b", twoprofile1b) }

// twoprofile1b builds twoprofile_1b.pdf: a PDF/X output intent listed before the PDF/A one,
// each naming its own destination profile of another colour space, at PDF/A-1b.
//
// ISO 19005-1, 6.2.2 asks every output intent carrying /DestOutputProfile to
// name the same indirect object, and ISO 19005-2/3/4, 6.2.3 t2 ask the same, so
// every level fails this file: its GTS_PDFX intent names a CMYK printer profile
// and its GTS_PDFA1 intent an RGB display profile.
//
// The order is the probe. The PDF/A intent is the one PDF/A reads device colour
// through (ISO 19005-1, 6.2.3.3), and the page paints DeviceRGB, which only an
// RGB PDF/A intent covers. A repair that unifies on the first entry gives the
// PDF/A intent the CMYK profile and leaves the DeviceRGB square uncovered; one
// that keeps the PDF/A intent's profile and points the PDF/X intent at it leaves
// the page conforming. Both intents are well formed, so the shared-object rule is
// the only output-intent rule the file fails.
//
// Both profiles are version 2.1, which PDF/A-1 asks of a destination profile,
// and carry real transforms: the RGB one a matrix and gamma 2.2 curves over an
// XYZ connection space, the CMYK one lut8 A2B0 and B2A0 tables over Lab, as in
// iccbadcmyk_1b.pdf. The page carries no text, so no font rule fires.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func twoprofile1b() ([]byte, error) {
	pdf := pdfgen.New()

	// The Python builder defines its own copy of _icc's helpers (s15,
	// text_description, xyz, gamma_curve, lut8, a2b0, b2a0, icc_profile), byte
	// for byte the same as _icc's, ported in icc.go.

	destination := func(profile []byte, components int) *pdfgen.Stream {
		stream := pdf.Stream(profile)
		stream.Set("N", components)
		pdf.MakeIndirect(stream)
		return stream
	}

	commonTags := []iccTag{
		{"cprt", append(append([]byte("text"), make([]byte, 4)...), "No copyright\x00"...)},
		{"wtpt", iccXYZ(iccD50...)},
	}
	rgbTags := make([]iccTag, 0, 1+len(commonTags)+len(iccPrimaries)+3)
	rgbTags = append(rgbTags, iccTag{"desc", iccTextDescription("litho RGB display")})
	rgbTags = append(rgbTags, commonTags...)
	// The sRGB primaries adapted to D50, the columns of its RGB-to-XYZ matrix.
	for _, p := range iccPrimaries {
		rgbTags = append(rgbTags, iccTag{p.Sig, iccXYZ(p.XYZ...)})
	}
	for _, sig := range []string{"rTRC", "gTRC", "bTRC"} {
		rgbTags = append(rgbTags, iccTag{sig, iccGammaCurve(2.2)})
	}
	rgb := iccProfile("mntr", "RGB ", "XYZ ", rgbTags)
	cmykTags := slices.Concat(
		[]iccTag{{"desc", iccTextDescription("litho CMYK printer")}},
		commonTags,
		[]iccTag{{"A2B0", iccA2B0()}, {"B2A0", iccB2A0()}},
	)
	cmyk := iccProfile("prtr", "CMYK", "Lab ", cmykTags)

	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFX"),
			"OutputConditionIdentifier", text("litho CMYK printer"),
			"DestOutputProfile", destination(cmyk, 4),
		)),
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho RGB display"),
			"DestOutputProfile", destination(rgb, 3),
		)),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d(),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte("0.8 0.1 0.1 rg 20 20 60 60 re f\n"))),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 1, Conformance: "B"}))
	return save(pdf, "1.4", noFixMetadataVersion)
}
