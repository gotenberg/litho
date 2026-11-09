package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("symbolname_2u", symbolname2u) }

// symbolname2u builds symbolname_2u.pdf: a font's name plays no part in the Unicode value
// of its glyphs.
//
// ISO 19005-2, 6.2.11.7.2 asks a Unicode value of every shown glyph, which ISO
// 32000-1, 9.10.2 derives from the glyph name the encoding gives the code. A
// /BaseFont that contains "Symbol" says nothing about the glyph a code selects.
//
//	MS  MdSymbolA-Regular, a nonsymbolic TrueType embedding the three-glyph
//	    program with its glyphs named A and H20850, under a /Differences array
//	    naming codes 41 A and 42 H20850 and no /BaseEncoding. Its descriptor
//	    carries a /CharSet of /alpha/beta, two Symbol-font names, which ISO
//	    32000-1, Table 122 makes meaningful for Type 1 fonts alone. It shows 41
//	    and 42: 41 has a Unicode value, H20850 has none in the Adobe Glyph List,
//	    the Symbol font or ZapfDingbats, so 42 fails t1 once.
//
// litho used to exempt the font: its name holds "Symbol" and its /CharSet lists
// only Symbol-font names. veraPDF reads neither.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func symbolname2u() ([]byte, error) {
	pdf := pdfgen.New()

	const nonsymbolic = 32
	glyphs := []string{".notdef", "A", "H20850"}
	programData := ttBuild([]ttSubtable{{3, 1, map[int]string{0x41: "A", 0x42: "H20850"}}}, glyphs)

	program := pdf.Stream(programData)
	program.Set("Length1", len(programData))
	descriptor := pdf.Indirect(d(
		"Type", n("FontDescriptor"),
		"FontName", n("/MdSymbolA-Regular"),
		"Flags", nonsymbolic,
		"FontBBox", a(0, -200, 550, 700),
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontFile2", pdf.MakeIndirect(program),
		"CharSet", text("/alpha/beta"),
	))
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/MdSymbolA-Regular"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(600, 600),
		"FontDescriptor", descriptor,
		"Encoding", d("Type", n("Encoding"), "Differences", a(65, n("A"), n("H20850"))),
	))

	body := "BT 10 700 Td /MS 12 Tf <4142> Tj ET\n"
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("MS", font)),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(body))),
	))
	pdf.AddPage(page)

	// Deliberately no /OutputIntents and no /Metadata.
	return save(pdf, "1.7")
}
