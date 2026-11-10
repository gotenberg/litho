package build

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("tounicodepair_2u", tounicodepair2u) }

// tounicodepair2u builds tounicodepair_2u.pdf: a simple font whose /ToUnicode CMap declares a
// two-byte codespace and maps a byte pair of the shown string.
//
// ISO 32000-1, 9.6.1 makes a simple font's codes one byte each, so a /ToUnicode
// CMap whose codespace is two bytes wide does not make a byte pair one code.
// PDF.js keys a /ToUnicode map by the code's value and looks each byte up there,
// so <006C> maps code 6C; veraPDF looks a simple font's code up by its value
// too. A byte pair the CMap maps, <6C6C> or <6F20>, names no code of the font.
//
// Font F1 embeds a /FontFile Type 1 program on StandardEncoding holding the
// glyphs it shows, and its /ToUnicode CMap declares <0000> to <FFFF>, maps every
// shown code by its two-byte value, and maps the pairs <6C6C> to "X" and <6F20>
// to "Y". The page shows "Hello world":
//
//	read a byte a code         "Hello world"
//	read two bytes a mapped pair  "HeXYworld"
//
// A CMYK output intent covers the initial DeviceGray and the XMP claims PDF/A-2u,
// so every shown code has a Unicode value and none of the font rules fire.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func tounicodepair2u() ([]byte, error) {
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

	const line = "Hello world"
	glyphNames := map[int]string{' ': "space"}
	seen := map[int]bool{}
	var codes []int
	for i := range len(line) {
		c := int(line[i])
		if !seen[c] {
			seen[c] = true
			codes = append(codes, c)
		}
	}
	sort.Ints(codes)
	var glyphs []string
	for _, c := range codes {
		if name, ok := glyphNames[c]; ok {
			glyphs = append(glyphs, name)
		} else {
			glyphs = append(glyphs, string(rune(c))) //nolint:gosec // c is a byte.
		}
	}
	data, l1, l2, l3 := t1Build(t1Options{Name: "LithoPair", Glyphs: glyphs})
	program := pdf.Stream(data)
	program.Set("Length1", l1)
	program.Set("Length2", l2)
	program.Set("Length3", l3)

	pairs := map[string]rune{"ll": 'X', "o ": 'Y'} //nolint:gocritic // the pair is "o" then a space.
	cmapLines := []string{
		"/CIDInit /ProcSet findresource begin",
		"12 dict begin",
		"begincmap",
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def",
		"/CMapName /Adobe-Identity-UCS def",
		"/CMapType 2 def",
		"1 begincodespacerange",
		"<0000> <FFFF>",
		"endcodespacerange",
		fmt.Sprintf("%d beginbfchar", len(codes)+len(pairs)),
	}
	for _, code := range codes {
		cmapLines = append(cmapLines, fmt.Sprintf("<%04X> <%04X>", code, code))
	}
	sortedPairs := make([]string, 0, len(pairs))
	for pair := range pairs {
		sortedPairs = append(sortedPairs, pair)
	}
	sort.Strings(sortedPairs)
	for _, pair := range sortedPairs {
		cmapLines = append(cmapLines, fmt.Sprintf("<%s> <%04X>", strings.ToUpper(hex.EncodeToString([]byte(pair))), pairs[pair]))
	}
	cmapLines = append(cmapLines,
		"endbfchar",
		"endcmap",
		"CMapName currentdict /CMap defineresource pop",
		"end",
		"end",
	)
	toUnicode := pdf.MakeIndirect(pdf.Stream([]byte(strings.Join(cmapLines, "\n") + "\n")))

	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoPair"),
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
	for range 256 {
		widths.Append(t1AdvanceWidth)
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/LithoPair"),
		"FirstChar", 0,
		"LastChar", 255,
		"Widths", widths,
		"ToUnicode", toUnicode,
		"FontDescriptor", pdf.Indirect(descriptor),
	))

	body := fmt.Sprintf("BT /F1 12 Tf 72 700 Td <%s> Tj ET\n", hex.EncodeToString([]byte(line)))
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(body))),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "U"}))
	return save(pdf, "1.7")
}
