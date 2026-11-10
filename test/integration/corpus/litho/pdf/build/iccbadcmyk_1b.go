package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("iccbadcmyk_1b", iccbadcmyk1b) }

// iccbadcmyk1b builds iccbadcmyk_1b.pdf: a CMYK output intent whose ICC profile states
// version 5.0, at PDF/A-1b.
//
// ISO 19005-1, 6.2.2 takes the destination profile of PDF Reference 1.4,
// 9.10.4, a version 2 profile, and ISO 19005-2, -3 and -4, 6.2.3 admit version 2
// or 4 (ISO 15076-1:2005). A version 5 (iccMAX) profile is admitted by no part,
// so every level fails the intent.
//
// Remediation cannot re-encode a version 5 profile, so it replaces it with a
// stock profile of the same data colour space. The header names CMYK, so the
// replacement is litho's stock CMYK printer profile
// (graphics/icc/profiles/cgats001-cmyk-v2.icc), not sRGB: an RGB destination
// under a CMYK output condition would stop covering the DeviceCMYK content the
// page paints (ISO 19005-1, 6.2.3.3).
// iccbadgray_1b.pdf reaches the grey replacement the same way; no fixture
// reached the CMYK one before this one and iccbadcmyk_2b.pdf.
//
// A version no part admits fails an ICCBased space sharing the profile as well
// (ISO 19005-2, 6.2.4.2 t1), so remediation writes the replacement at the
// profile's own object number and leaves the intent as it is.
//
// The profile is a version 2 output profile in every other respect: desc as
// textDescriptionType, cprt as textType, the D50 media white in wtpt, and lut8
// A2B0 and B2A0 transforms over a two-point grid. Only the header version is
// wrong. The page paints a DeviceCMYK square and its text in the conforming
// embedded TrueType of baretree_2b.pdf, both in DeviceCMYK, which the CMYK intent
// covers.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func iccbadcmyk1b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccVersion50 = 0x05000000

	// iccProfileV is a CMYK ICC profile of tags, (signature,
	// data) pairs, over a Lab PCS, its header stating version.
	iccProfileV := func(version uint32, deviceClass string, tags []iccTag) []byte {
		p := iccProfile(deviceClass, "CMYK", "Lab ", tags)
		binary.BigEndian.PutUint32(p[8:12], version)
		return p
	}

	profile := iccProfileV(
		iccVersion50,
		"prtr",
		[]iccTag{
			{"desc", iccTextDescription("litho CMYK, version 5.0 header")},
			{"cprt", append(append([]byte("text"), make([]byte, 4)...), "No copyright\x00"...)},
			{"wtpt", append(append([]byte("XYZ "), make([]byte, 4)...), iccS15(iccD50...)...)},
			{"A2B0", iccA2B0()},
			{"B2A0", iccB2A0()},
		},
	)

	destination := pdf.Stream(profile)
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho CMYK"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	page := lithoBareSansPage(pdf, "0.6 0.2 0 0.1 k 20 20 60 60 re f\n0 0 0 1 k BT /F1 24 Tf 20 120 Td (AB) Tj ET\n")
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 1, Conformance: "B"}))
	return save(pdf, "1.4", noFixMetadataVersion)
}
