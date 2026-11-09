package build

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("texlig_2b", texlig2b) }

// texlig2b builds texlig_2b.pdf: a TeX-style ligature code and a code 32 whose name
// maps nothing, in an embedded Type 1 font with no /ToUnicode.
//
// ISO 32000-1, 9.10.2 derives a simple font's Unicode value from the glyph name
// a code selects. TeX text fonts set the "fi" ligature at code 0x0C under a name
// no glyph list holds, so the code has no Unicode value and reads as no
// character: the word stays whole. PDF.js shows an unmapped code as itself and
// reads 0x0C, a form feed, as a word space, which cuts the word in two. ISO
// 32000-1, 9.3.3 applies word spacing to the single-byte code 32 whatever glyph
// it selects, so code 32 reads as a word space whatever its name.
//
// Font F1 embeds a /FontFile Type 1 program on StandardEncoding holding the
// letters it shows and T0C, and carries /Differences [12 /T0C 32 /T20] with no
// /BaseEncoding. The program holds no T20, so code 32 selects .notdef, which
// draws nothing, and /Widths gives it 0, so no gap separates the words it
// divides: only the code can. The page shows two lines:
//
//	veri <0C> cation <20> text   reads "verication text"
//	code <20> space              reads "code space", the code-32 control
//
// Read with every unmapped whitespace code as a space, the first line was
// "veri cation text". A CMYK output intent covers the initial DeviceGray and the
// XMP claims PDF/A-2b, so the planted glyph, width and Unicode gaps are what
// fires.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func texlig2b() ([]byte, error) {
	pdf := pdfgen.New()

	const nonsymbolic = 32
	const iccHeaderSize = 128
	const iccVersion21 = 0x02100000

	// iccProfile is a structurally minimal ICC profile, as iccbased_4.go
	// builds it.
	iccProfile := func(deviceClass, colourSpace, tag string) []byte {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize)
		copy(header[0:4], iccU32(nil, iccHeaderSize+len(tagTable)))
		copy(header[4:8], tag)
		binary.BigEndian.PutUint32(header[8:12], iccVersion21)
		copy(header[12:16], deviceClass)
		copy(header[16:20], colourSpace)
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		binary.BigEndian.PutUint32(header[68:72], 0x0000F6D6)
		binary.BigEndian.PutUint32(header[72:76], 0x00010000)
		binary.BigEndian.PutUint32(header[76:80], 0x0000D32D)
		return append(header, tagTable...)
	}

	destination := pdf.Stream(iccProfile("prtr", "CMYK", "OUTI"))
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho destination"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	lines := []string{"veri\x0ccation text", "code space"}
	letters := sortedLetters(lines...)
	data, l1, l2, l3 := t1Build(t1Options{Name: "LithoTeX", Glyphs: append(letters, "T0C")})
	program := pdf.Stream(data)
	program.Set("Length1", l1)
	program.Set("Length2", l2)
	program.Set("Length3", l3)

	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoTeX"),
		"Flags", nonsymbolic,
		"FontBBox", a(0, -200, 550, 700),
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontFile", pdf.MakeIndirect(program),
	)
	widths := a()
	for code := range 256 {
		if code == 32 {
			widths.Append(0)
		} else {
			widths.Append(t1AdvanceWidth)
		}
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/LithoTeX"),
		"FirstChar", 0,
		"LastChar", 255,
		"Widths", widths,
		"Encoding", d("Type", n("Encoding"), "Differences", a(12, n("/T0C"), 32, n("/T20"))),
		"FontDescriptor", pdf.Indirect(descriptor),
	))

	var body []byte
	y := 700
	for _, line := range lines {
		body = fmt.Appendf(body, "BT /F1 12 Tf 72 %d Td <%s> Tj ET\n", y, hex.EncodeToString([]byte(line)))
		y -= 20
	}
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.MakeIndirect(pdf.Stream(body)),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
