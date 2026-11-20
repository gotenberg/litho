package build

import (
	"maps"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("symbolenc_2b", symbolenc2b) }

// symbolenc2b builds symbolenc_2b.pdf: symbolic TrueType fonts that state a base encoding.
//
// ISO 19005-2, 6.2.11.6 t3 forbids a symbolic TrueType font an /Encoding entry,
// and veraPDF reads the entry as the name it holds or the /BaseEncoding of its
// dictionary. Reporting tools write Wingdings and barcode fonts this way: a
// symbolic program whose only cmap subtable is the Microsoft Symbol (3,0), under
// /WinAnsiEncoding. ISO 32000-1, 9.6.6.4 reads such a code through its glyph name
// and the (3,1) or (1,0) subtable when the font states a base encoding, and
// through the code and the (3,0) or (1,0) subtable when it states none, so with
// neither (3,1) nor (1,0) in the program both readings select the same glyph, and
// with either they select the same glyph wherever the two subtables agree.
//
//   - S1: symbolic, /WinAnsiEncoding, a (3,0)-only program. Fails t3.
//   - S2: symbolic, an indirect encoding dictionary with /BaseEncoding
//     /WinAnsiEncoding and /Differences, a (3,0)-only program. Fails t3.
//   - N1: nonsymbolic, the same encoding object as S2, a (3,1) and (1,0)
//     program. Conforms, and shows that a repair of S2 leaves the shared object
//     alone.
//   - S3: symbolic, /WinAnsiEncoding, a (3,0) and (3,1) program whose (3,1)
//     subtable selects the glyphs (3,0) does. Fails t3.
//   - S4: symbolic, /WinAnsiEncoding, a (3,0) and (1,0) program, as reporting
//     tools embed Wingdings: (1,0) agrees with (3,0) on 41 and 42, and maps Mac
//     OS Roman 0xDB, which WinAnsi code 0x80 (Euro) reads through, where (3,0)
//     maps nothing. No page shows 0x80. Fails t3.
//   - S5: symbolic, /WinAnsiEncoding, a (3,0) and (1,0) program whose (1,0)
//     subtable swaps 41 and 42. Fails t3, and dropping the base encoding would
//     draw them as the other glyph.
//
// Every font shows 41 42. None carries a /ToUnicode, so extraction reads S1, S2
// and S3 through their encoding.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func symbolenc2b() ([]byte, error) {
	pdf := pdfgen.New()

	// Flags bit 3 (value 4) marks a symbolic font, bit 6 (value 32) a non-symbolic
	// one (ISO 32000-1, Table 123).
	const (
		symbolic    = 4
		nonsymbolic = 32
	)

	// A symbolic (3,0) subtable is conventionally keyed in the 0xF000 range, so it
	// maps both forms and a viewer resolves the glyph either way.
	latin := map[int]string{0x41: "A", 0x42: "B"}
	symbol := map[int]string{0x41: "A", 0x42: "B", 0xF041: "A", 0xF042: "B"}
	roman := map[int]string{0xDB: "A"}
	maps.Copy(roman, latin)

	// truetype is one simple TrueType font dictionary with an embedded program.
	truetype := func(name string, flags int, subtables []ttSubtable, encoding any) *pdfgen.Dict {
		program := ttBuild(subtables, nil)
		fontFile := pdf.Stream(program)
		fontFile.Set("Length1", len(program))
		descriptor := d(
			"Type", n("FontDescriptor"),
			"FontName", n("/"+name),
			"Flags", flags,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile2", pdf.MakeIndirect(fontFile),
		)
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("TrueType"),
			"BaseFont", n("/"+name),
			"FirstChar", 65,
			"LastChar", 66,
			"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
			"FontDescriptor", pdf.Indirect(descriptor),
			"Encoding", encoding,
		))
	}

	shared := pdf.Indirect(d(
		"Type", n("Encoding"),
		"BaseEncoding", n("WinAnsiEncoding"),
		"Differences", a(66, n("B")),
	))
	s1 := truetype("LithoSymWinAnsi", symbolic, []ttSubtable{{3, 0, symbol}}, n("WinAnsiEncoding"))
	s2 := truetype("LithoSymShared", symbolic, []ttSubtable{{3, 0, symbol}}, shared)
	n1 := truetype("LithoNonSymShared", nonsymbolic, []ttSubtable{{3, 1, latin}, {1, 0, latin}}, shared)
	s3 := truetype("LithoSymUnicode", symbolic, []ttSubtable{{3, 0, symbol}, {3, 1, latin}}, n("WinAnsiEncoding"))
	s4 := truetype("LithoSymRoman", symbolic, []ttSubtable{{3, 0, symbol}, {1, 0, roman}}, n("WinAnsiEncoding"))
	s5 := truetype("LithoSymSwapped", symbolic, []ttSubtable{{3, 0, symbol}, {1, 0, map[int]string{0x41: "B", 0x42: "A"}}}, n("WinAnsiEncoding"))

	content := "BT /S1 24 Tf 50 700 Td (AB) Tj ET\n" +
		"BT /S2 24 Tf 50 660 Td (AB) Tj ET\n" +
		"BT /N1 24 Tf 50 620 Td (AB) Tj ET\n" +
		"BT /S3 24 Tf 50 580 Td (AB) Tj ET\n" +
		"BT /S4 24 Tf 50 540 Td (AB) Tj ET\n" +
		"BT /S5 24 Tf 50 500 Td (AB) Tj ET\n"

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("S1", s1, "S2", s2, "N1", n1, "S3", s3, "S4", s4, "S5", s5)),
		"Contents", pdf.Stream([]byte(content)),
	)
	pdf.AddPage(pdf.Indirect(page))

	// Deliberately no /OutputIntents and no /Metadata.
	return save(pdf, "1.7")
}
