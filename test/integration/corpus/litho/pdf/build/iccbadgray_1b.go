package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("iccbadgray_1b", iccbadgray1b) }

// iccbadgray1b builds iccbadgray_1b.pdf: a grey output intent whose ICC profile states
// version 5.0, at PDF/A-1b.
//
// ISO 19005-1, 6.2.2 takes the destination profile of PDF Reference 1.4,
// 9.10.4, a version 2 profile, and ISO 19005-2, -3 and -4, 6.2.3 admit version 2
// or 4 (ISO 15076-1:2005). A version 5 (iccMAX) profile is admitted by no part,
// so every level fails the intent, where iccv4gray_1b.pdf fails PDF/A-1 alone.
//
// Remediation cannot re-encode a version 5 profile, so it replaces it with a
// stock profile of the same data colour space. The header names GRAY, so the
// replacement is litho's generated v2.4 sRGB grey profile (sgray-v2.icc), not
// sRGB: an RGB destination under a grey output condition would license
// DeviceRGB content the source never had (ISO 19005-1, 6.2.3.3). No file of the
// litho corpus reached that grey replacement before this one.
//
// A version no part admits fails an ICCBased space sharing the profile as well
// (ISO 19005-2, 6.2.4.2 t1), so remediation writes the replacement at the
// profile's own object number and leaves the intent as it is.
// iccbadgray_2b.pdf plants a profile only the device class makes unfit, which
// remediation replaces with a new object instead.
//
// The profile carries the version 2 grey tags a colour engine reads: desc as
// textDescriptionType, cprt as textType, the D65 white of IEC 61966-2-1 in wtpt,
// and a gamma 2.2 kTRC curve. Only the header version is wrong. The page paints
// a DeviceGray square and its text in the conforming embedded TrueType of
// baretree_2b.pdf, both in DeviceGray, which the grey intent covers.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func iccbadgray1b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccVersion50 = 0x05000000
	// Runtime float64 arithmetic, as Python computes it.
	x, y := 0.3127, 0.3290
	d65 := []float64{x / y, 1.0, (1 - x - y) / y}

	// iccProfileV is icc_profile: a grey ICC profile of tags, (signature,
	// data) pairs, its header stating version.
	iccProfileV := func(version uint32, deviceClass string, tags []iccTag) []byte {
		p := iccProfile(deviceClass, "GRAY", "XYZ ", tags)
		binary.BigEndian.PutUint32(p[8:12], version)
		return p
	}

	profile := iccProfileV(
		iccVersion50,
		"mntr",
		[]iccTag{
			{"desc", iccTextDescription("litho grey, version 5.0 header")},
			{"cprt", append(append([]byte("text"), make([]byte, 4)...), "No copyright\x00"...)},
			{"wtpt", append(append([]byte("XYZ "), make([]byte, 4)...), iccS15(d65...)...)},
			{"kTRC", append([]byte("curv"), 0, 0, 0, 0, 0, 0, 0, 1, 0x02, 0x33)}, // gamma 2.2 in u8Fixed8
		},
	)

	destination := pdf.Stream(profile)
	destination.Set("N", 1)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho grey"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	page := lithoBareSansPage(pdf, "0.5 g 20 20 60 60 re f\n0 g BT /F1 24 Tf 20 120 Td (AB) Tj ET\n")
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 1, Conformance: "B"}))
	return save(pdf, "1.4", noFixMetadataVersion)
}
