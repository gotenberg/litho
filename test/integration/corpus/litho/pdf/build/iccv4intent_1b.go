package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("iccv4intent_1b", iccv4intent1b) }

// iccv4intent1b builds iccv4intent_1b.pdf: an output intent carrying an ICC v4 profile, at
// PDF/A-1b.
//
// PDF/A-1 rests on PDF 1.4, which predates ICC v4, so ISO 19005-1, 6.2.2 takes
// the destination profile PDF Reference 1.4, 9.10.4 describes, an ICC.1:1998-09
// (v2) profile, and veraPDF's test reads "version < 3.0". ISO 19005-2, -3 and -4,
// 6.2.3 admit a v4 profile. litho accepted major versions 2 and 4 in every part,
// so a v4 destination profile passed PDF/A-1 where veraPDF fails it. No file of
// the litho or veraPDF corpora reaches it; the pdfjs and govdocs files that do
// are not remediated by the gate.
//
// The file is otherwise a conforming PDF/A-1b document: a 1.4 header, an XMP
// claim, a CMYK printer profile in the one output intent, and its text in the
// conforming embedded TrueType of baretree_2b.pdf, in the initial DeviceGray the
// CMYK intent covers. So at PDF/A-1a and 1b the profile version is the verdict,
// and at the later parts the file is as conforming as their claims allow.
//
// The profile is the structurally minimal header iccbased_4.go builds, with the
// version field set to 4.2.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func iccv4intent1b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccHeaderSize = 128
	const iccVersion42 = 0x04200000

	// iccProfile is a structurally minimal ICC profile, as iccbased_4.go builds it.
	iccProfile := func(deviceClass, colourSpace string, version uint32, tag string) []byte {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize, iccHeaderSize+len(tagTable))
		binary.BigEndian.PutUint32(header[0:4], uint32(iccHeaderSize+len(tagTable))) //nolint:gosec // a 132-byte profile.
		copy(header[4:8], tag)
		binary.BigEndian.PutUint32(header[8:12], version)
		copy(header[12:16], deviceClass)
		copy(header[16:20], colourSpace)
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		binary.BigEndian.PutUint32(header[68:72], 0x0000F6D6)
		binary.BigEndian.PutUint32(header[72:76], 0x00010000)
		binary.BigEndian.PutUint32(header[76:80], 0x0000D32D)
		return append(header, tagTable...)
	}

	destination := pdf.Stream(iccProfile("prtr", "CMYK", iccVersion42, "OUT4"))
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho v4 destination"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	font := latinTrueTypeFont(pdf, "/LithoBareSans")

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
