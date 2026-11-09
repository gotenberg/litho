package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("ttmaccopy_2b", ttmaccopy2b) }

// ttmaccopy2b builds ttmaccopy_2b.pdf: symbolic TrueType fonts whose (1,0) subtable copies (3,1).
//
// ISO 32000-1, 9.6.6.4 has a symbolic TrueType font with no /Encoding and no
// (3,0) subtable select its glyphs through the (1,0) Macintosh subtable, whose
// codes are Mac OS Roman characters. Some producers write the (3,1) mapping under
// the (1,0) record instead: the "Mac" table then carries no meaning of its own,
// and reading its codes as Mac OS Roman names the wrong characters. litho reads
// such a copy through the glyphs its codes select, and keeps the Mac OS Roman
// reading of a genuine (1,0) subtable. pdfjs issue15910.pdf is the one corpus
// file that reaches the construct.
//
// | Font | (1,0) subtable             | (3,1) subtable             | Shows | Text |
// | ---- | -------------------------- | -------------------------- | ----- | ---- |
// | M1   | C4 Adieresis, E4 adieresis | C4 Adieresis, E4 adieresis | C4 E4 | Ää   |
// | M2   | 80 Adieresis, 8A adieresis | C4 Adieresis, E4 adieresis | 80 8A | Ää   |
//
// M1 is the copy: read as Mac OS Roman, C4 and E4 are "ƒ‰". M2 is the control, a
// genuine Mac OS Roman table: read as anything else, 80 and 8A are "€Š".
//
// Both fonts are symbolic, embedded and drawn, and declare two subtables with no
// (3,0), so 6.2.11.6 t4 fires once per program; neither declares /Encoding, so
// 6.2.11.6 t3 stays silent. Every count was confirmed with live veraPDF 1.30.2.
// See ../FIXTURES.md for the authoritative expected firing set.
func ttmaccopy2b() ([]byte, error) {
	pdf := pdfgen.New()

	const symbolic = 4
	names := []string{".notdef", "Adieresis", "adieresis"}
	unicode := map[int]string{0xC4: "Adieresis", 0xE4: "adieresis"}
	macRoman := map[int]string{0x80: "Adieresis", 0x8A: "adieresis"}

	// truetype is one symbolic TrueType font dictionary with no /Encoding.
	truetype := func(name string, subtables []ttSubtable, first, last int) *pdfgen.Dict {
		program := ttBuild(subtables, names)
		fontFile := pdf.Stream(program)
		fontFile.Set("Length1", len(program))
		descriptor := d(
			"Type", n("FontDescriptor"),
			"FontName", n("/"+name),
			"Flags", symbolic,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile2", pdf.MakeIndirect(fontFile),
		)
		widths := a()
		for i := first; i <= last; i++ {
			widths.Append(ttAdvanceWidth)
		}
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("TrueType"),
			"BaseFont", n("/"+name),
			"FirstChar", first,
			"LastChar", last,
			"Widths", widths,
			"FontDescriptor", pdf.Indirect(descriptor),
		))
	}

	m1 := truetype("LithoMacCopy", []ttSubtable{{1, 0, unicode}, {3, 1, unicode}}, 0xC4, 0xE4)
	m2 := truetype("LithoMacGenuine", []ttSubtable{{1, 0, macRoman}, {3, 1, unicode}}, 0x80, 0x8A)

	content := []byte("BT /M1 24 Tf 50 700 Td <C4E4> Tj ET\n" +
		"BT /M2 24 Tf 50 660 Td <808A> Tj ET\n")

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("M1", m1, "M2", m2)),
		"Contents", pdf.Stream(content),
	)
	pdf.AddPage(pdf.Indirect(page))

	// Deliberately no /Metadata and no /OutputIntents.
	return save(pdf, "1.7")
}
