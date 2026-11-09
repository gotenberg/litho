package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("iccbadcmyk_2b", iccbadcmyk2b) }

// iccbadcmyk2b builds iccbadcmyk_2b.pdf: two output intents sharing one CMYK ICC profile
// of the scanner device class, at PDF/A-2b.
//
// ISO 19005-2, 6.2.3 asks the destination profile of a PDF/A output intent to
// be an output (prtr) or display (mntr) profile, and ISO 19005-1, 6.2.2 and
// 19005-4, 6.2.3 ask the same, so every level fails this file, whose profile
// header declares the input device class scnr.
//
// Remediation replaces the profile with a stock profile of the same data colour
// space. The header names CMYK, so the replacement is litho's stock CMYK
// printer profile (graphics/icc/profiles/cgats001-cmyk-v2.icc), not sRGB: an
// RGB destination under a CMYK output condition would stop covering the
// DeviceCMYK content the page paints (ISO 19005-2, 6.2.4.3).
//
// An ICCBased space admits a scanner profile (ISO 19005-2, 6.2.4.2), and the
// page paints one over the same profile stream, so remediation keeps the source
// profile for that space and points the intents at a new profile object. The
// GTS_PDFA1 and GTS_PDFX intents name one profile, and ISO 19005-2, 6.2.3 t2
// asks every intent's /DestOutputProfile to be one indirect object, so both
// must name one replacement. iccbadcmyk_1b.pdf takes the in-place branch.
//
// The profile is a version 2.1 output profile in every other respect: desc as
// textDescriptionType, cprt as textType, the D50 media white in wtpt, and lut8
// A2B0 and B2A0 transforms over a two-point grid. Only the device class is
// wrong. The page paints a DeviceCMYK square, an ICCBased square and its text
// in the conforming embedded TrueType of baretree_2b.pdf.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func iccbadcmyk2b() ([]byte, error) {
	pdf := pdfgen.New()

	// The Python builder defines its own s15, text_description, lut8, a2b0,
	// b2a0 and icc_profile; they are byte for byte those of _icc, whose ports
	// (icc.go) this uses: icc_profile(ICC_VERSION_2_1, device_class, tags) is
	// iccProfile over a CMYK colour space and a Lab PCS.
	profile := iccProfile("scnr", "CMYK", "Lab ", []iccTag{
		{"desc", iccTextDescription("litho CMYK, scanner class")},
		{"cprt", append(append([]byte("text"), make([]byte, 4)...), "No copyright\x00"...)},
		{"wtpt", iccXYZ(iccD50...)},
		{"A2B0", iccA2B0()},
		{"B2A0", iccB2A0()},
	})

	destination := pdf.Stream(profile)
	destination.Set("N", 4)
	profileRef := pdf.MakeIndirect(destination)
	intents := a()
	for _, subtype := range []string{"/GTS_PDFA1", "/GTS_PDFX"} {
		intents.Append(pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n(subtype),
			"OutputConditionIdentifier", text("litho CMYK"),
			"DestOutputProfile", profileRef,
		)))
	}
	pdf.Root.Set("OutputIntents", intents)

	font := latinTrueTypeFont(pdf, "/LithoBareSans")

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d(
			"Font", d("F1", font),
			"ColorSpace", d("CS0", a(n("ICCBased"), profileRef)),
		),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(
			"0.6 0.2 0 0.1 k 20 20 60 60 re f\n"+
				"/CS0 cs 0.1 0.5 0.9 0 scn 110 20 60 60 re f\n"+
				"0 0 0 1 k BT /F1 24 Tf 20 120 Td (AB) Tj ET\n",
		))),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7", noFixMetadataVersion)
}
