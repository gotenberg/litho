package build

import (
	"encoding/binary"
	"math"
	"slices"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("iccbadgray_2b", iccbadgray2b) }

// iccbadgray2b builds iccbadgray_2b.pdf: a direct grey output intent whose ICC profile
// is a scanner profile, at PDF/A-2b.
//
// ISO 19005-2, 6.2.3 asks the destination profile of a PDF/A output intent to
// be an output (prtr) or display (mntr) profile, and ISO 19005-1, 6.2.2 and
// 19005-4, 6.2.3 ask the same, so every level fails this intent, whose header
// declares the input device class scnr. outputintent_2b.pdf fails an RGB intent
// the same way.
//
// Remediation replaces the profile with a stock profile of the same data colour
// space. The header names GRAY, so the replacement is litho's generated v2.4
// sRGB grey profile (sgray-v2.icc), not sRGB: an RGB destination under a grey
// output condition would license DeviceRGB content the source never had (ISO
// 19005-2, 6.2.4.3).
//
// An ICCBased space admits a scanner profile (ISO 19005-2, 6.2.4.2), so
// remediation keeps the source profile for any space sharing it and points the
// intent at a new profile object. The intent sits directly in /OutputIntents,
// with no object number of its own (ISO 32000-1, 7.3.10), so remediation
// rewrites the catalog's array. It used to overwrite the profile stream at its
// own object number instead, after allocating the replacement once more under a
// number nothing referenced, so every output carried an orphan copy of the
// profile. iccbadgray_1b.pdf takes the in-place branch.
//
// The profile carries the version 2.1 grey tags a colour engine reads: desc as
// textDescriptionType, cprt as textType, the D65 white of IEC 61966-2-1 in wtpt,
// and a gamma 2.2 kTRC curve. Only the device class is wrong. The page paints a
// DeviceGray square and its text in the conforming embedded TrueType of
// baretree_2b.pdf, both in DeviceGray, which the grey intent covers.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func iccbadgray2b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccVersion21 = 0x02100000
	d50 := []float64{0.9642, 1.0, 0.8249}
	d65 := []float64{0.3127 / 0.3290, 1.0, (1 - 0.3127 - 0.3290) / 0.3290}

	s15 := func(values ...float64) []byte {
		var out []byte
		for _, v := range values {
			out = binary.BigEndian.AppendUint32(out, uint32(int32(math.RoundToEven(v*65536)))) //nolint:gosec // struct's ">i".
		}
		return out
	}

	// textDescription is a v2 textDescriptionType (ICC.1:2001-04, 6.5.17):
	// ASCII only.
	textDescription := func(text string) []byte {
		asciiText := append([]byte(text), 0)
		return slices.Concat([]byte("desc"), make([]byte, 4),
			binary.BigEndian.AppendUint32(nil, uint32(len(asciiText))), asciiText, //nolint:gosec // short ASCII text.
			make([]byte, 4+4+2+1+67))
	}

	// iccProfile is a grey ICC profile of tags, (signature, data) pairs.
	iccProfile := func(version uint32, deviceClass string, tags []iccTag) []byte {
		table := binary.BigEndian.AppendUint32(nil, uint32(len(tags))) //nolint:gosec // a handful of tags.
		var body []byte
		offset := 128 + 4 + 12*len(tags)
		for _, t := range tags {
			table = append(table, t.Sig...)
			table = binary.BigEndian.AppendUint32(table, uint32(offset+len(body))) //nolint:gosec // small offsets.
			table = binary.BigEndian.AppendUint32(table, uint32(len(t.Data)))      //nolint:gosec // small tags.
			body = append(body, t.Data...)
			body = append(body, make([]byte, (4-len(t.Data)%4)%4)...)
		}
		header := make([]byte, 128)
		binary.BigEndian.PutUint32(header[0:4], uint32(128+len(table)+len(body))) //nolint:gosec // small profile.
		binary.BigEndian.PutUint32(header[8:12], version)
		copy(header[12:16], deviceClass)
		copy(header[16:20], "GRAY")
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		copy(header[68:80], s15(d50...))
		return slices.Concat(header, table, body)
	}

	profile := iccProfile(
		iccVersion21,
		"scnr",
		[]iccTag{
			{"desc", textDescription("litho grey, scanner class")},
			{"cprt", slices.Concat([]byte("text"), make([]byte, 4), []byte("No copyright\x00"))},
			{"wtpt", slices.Concat([]byte("XYZ "), make([]byte, 4), s15(d65...))},
			{"kTRC", slices.Concat([]byte("curv"), make([]byte, 4), []byte{0, 0, 0, 1, 0x02, 0x33})}, // gamma 2.2 in u8Fixed8
		},
	)

	destination := pdf.Stream(profile)
	destination.Set("N", 1)
	pdf.Root.Set("OutputIntents", a(
		d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho grey"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		),
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
		"Contents", pdf.MakeIndirect(
			pdf.Stream([]byte("0.5 g 20 20 60 60 re f\n0 g BT /F1 24 Tf 20 120 Td (AB) Tj ET\n")),
		),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7", noFixMetadataVersion)
}
