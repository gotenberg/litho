package build

import (
	"maps"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("ttnoenc_2b", ttnoenc2b) }

// ttnoenc2b builds ttnoenc_2b.pdf: embedded simple TrueType fonts that carry no /Encoding.
//
// ISO 32000-1, 9.6.6.4 maps the codes of a non-symbolic TrueType font through
// /Encoding, and a font dictionary that omits the key leaves no base encoding to
// name the codes by. veraPDF 1.30.2 then reads such a font as it reads a symbolic
// one (`TrueTypeFontProgram`: every branch tests `isSymbolic ||
// encoding.getDirectBase() == null`): a code reaches a glyph only through a
// Microsoft Symbol (3,0) or a Macintosh Roman (1,0) subtable, the glyph has no
// name, and so no Unicode value comes from the encoding either. The (3,1)
// subtable, which a non-symbolic font is otherwise looked up through, is skipped.
//
// The fixture draws five fonts, the same two codes each, and differs only in the
// 'cmap' subtables the programs carry:
//
//   - N31: non-symbolic, (3,1) only. No (3,0) and no (1,0), so the drawn glyphs
//     are not present for glyph presence.
//   - N3110: non-symbolic, (3,1) and (1,0). The (1,0) finds both glyphs.
//   - N10: non-symbolic, (1,0) only.
//   - N30: non-symbolic, (3,0) only, keyed in the 0xF000 range.
//   - S31: symbolic, (3,1) only, the symbolic twin of N31.
//
// Each font carries /Widths matching its program, so the width clause stays
// silent wherever the glyph is found. No font carries /ToUnicode, so the Unicode
// clauses of parts 2u, 2a, 3u, 3a and PDF/UA read whatever the encoding names.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func ttnoenc2b() ([]byte, error) {
	pdf := pdfgen.New()

	// Flags bit 3 (value 4) marks a symbolic font, bit 6 (value 32) a non-symbolic
	// one (ISO 32000-1, Table 123).
	const (
		symbolic    = 4
		nonsymbolic = 32
	)

	latin := map[int]string{0x41: "A", 0x42: "B"}
	symbolRange := map[int]string{0xF041: "A", 0xF042: "B"}
	latinAndSymbols := map[int]string{}
	maps.Copy(latinAndSymbols, latin)
	maps.Copy(latinAndSymbols, symbolRange)

	// truetype is one simple TrueType font dictionary with an embedded program and no /Encoding.
	truetype := func(name string, flags int, subtables []ttSubtable) *pdfgen.Dict {
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
		font := d(
			"Type", n("Font"),
			"Subtype", n("TrueType"),
			"BaseFont", n("/"+name),
			"FirstChar", 65,
			"LastChar", 66,
			"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
			"FontDescriptor", pdf.Indirect(descriptor),
		)
		return pdf.Indirect(font)
	}

	fonts := d(
		"N31", truetype("LithoNoEncUni", nonsymbolic, []ttSubtable{{3, 1, latin}}),
		"N3110", truetype("LithoNoEncUniMac", nonsymbolic, []ttSubtable{{3, 1, latin}, {1, 0, latin}}),
		"N10", truetype("LithoNoEncMac", nonsymbolic, []ttSubtable{{1, 0, latin}}),
		"N30", truetype("LithoNoEncSym30", nonsymbolic, []ttSubtable{{3, 0, latinAndSymbols}}),
		"S31", truetype("LithoNoEncSymUni", symbolic, []ttSubtable{{3, 1, latin}}),
	)

	content := "BT /N31 24 Tf 50 700 Td (AB) Tj ET\n" +
		"BT /N3110 24 Tf 50 660 Td (AB) Tj ET\n" +
		"BT /N10 24 Tf 50 620 Td (AB) Tj ET\n" +
		"BT /N30 24 Tf 50 580 Td (AB) Tj ET\n" +
		"BT /S31 24 Tf 50 540 Td (AB) Tj ET\n"

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", fonts),
		"Contents", pdf.Stream([]byte(content)),
	)
	pdf.AddPage(pdf.Indirect(page))

	// Deliberately no /Metadata and no /OutputIntents, as every sibling fixture.
	return save(pdf, "1.7")
}
