package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("fontclaims_2b", fontclaims2b) }

// fontclaims2b builds fontclaims_2b.pdf: glyphs a name-keyed CFF program lacks, where a
// painted pattern and a Type 3 glyph read a font's program, and a control code an
// unembedded standard font names .notdef.
//
// Each construct rides on a font of its own, so each count has one source:
//
//	NK  a Type0 font over a CIDFontType0 whose CFF program is name-keyed: it has
//	    no ROS operator, so a CID is the glyph index itself (ISO 32000-1,
//	    9.7.4.2), and the three-glyph program lacks CID 3. The font is a subset,
//	    so veraPDF weighs the missing glyph at the /DW of 1000, and CID 3 fails
//	    the presence, .notdef and width rules against its /W of 500. CID 1 is
//	    present and 600 wide, as /W declares.
//	NF  the same program under a name with no subset tag, CID 3 declared 600
//	    wide. veraPDF weighs a missing glyph of such a font at the program's
//	    defaultWidthX, 600, so the width rule passes where the presence and
//	    .notdef rules fail.
//	PT  a composite TrueType font a tiling pattern's cell shows CID 5 through,
//	    then the page text CID 6, both past the three-glyph program. veraPDF
//	    parses the cell where the page fills with the pattern, before the text,
//	    and judges neither glyph by the presence rule; both fail .notdef.
//	TC  a composite TrueType font a Type 3 glyph procedure shows CID 5 through,
//	    then the page text CIDs 6 and 7. veraPDF parses the procedure where the
//	    page shows the Type 3 glyph, so the procedure's glyph is the one the
//	    presence rule judges; all three fail .notdef.
//	TF  a composite TrueType font the procedure of a Type 3 glyph a form shows
//	    draws CID 5 through: reached through the form, it fails the presence and
//	    .notdef rules.
//	HV  an unembedded Helvetica stating no /Encoding and no /FontDescriptor,
//	    showing code 1. Its built-in encoding, StandardEncoding, names nothing
//	    there, so the code references .notdef. A /ToUnicode maps the code, so
//	    the Unicode rules have a value to judge rather than the .notdef name.
//
// Every count was confirmed with live veraPDF 1.30. See ../FIXTURES.md for the
// authoritative expected firing set.
func fontclaims2b() ([]byte, error) {
	pdf := pdfgen.New()

	const nonsymbolic = 32
	ttProgram := ttBuild([]ttSubtable{{3, 1, map[int]string{0x41: "A", 0x42: "B"}}}, nil)
	cffProgram := cffBuild()

	program := func(data []byte, keys ...any) *pdfgen.Stream {
		stream := pdf.Stream(data)
		for i := 0; i < len(keys); i += 2 {
			stream.Set(keys[i].(string), keys[i+1])
		}
		pdf.MakeIndirect(stream)
		return stream
	}

	cffStream := program(cffProgram, "Subtype", n("CIDFontType0C"))

	descriptor := func(name string, fontFile ...any) *pdfgen.Dict {
		dd := d(
			"Type", n("FontDescriptor"),
			"FontName", n(name),
			"Flags", nonsymbolic,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
		)
		for i := 0; i < len(fontFile); i += 2 {
			dd.Set(fontFile[i].(string), fontFile[i+1])
		}
		return pdf.Indirect(dd)
	}

	type0 := func(name string, descendant *pdfgen.Dict) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type0"),
			"BaseFont", n(name),
			"Encoding", n("/Identity-H"),
			"DescendantFonts", a(pdf.Indirect(descendant)),
		))
	}

	cidInfo := func() *pdfgen.Dict {
		return d("Registry", text("Adobe"), "Ordering", text("Identity"), "Supplement", 0)
	}

	// nameKeyedFont is a Type0 font over a CIDFontType0 embedding the
	// name-keyed CFF.
	nameKeyedFont := func(name string, widths *pdfgen.Array) *pdfgen.Dict {
		descendant := d(
			"Type", n("Font"),
			"Subtype", n("CIDFontType0"),
			"BaseFont", n(name),
			"CIDSystemInfo", cidInfo(),
			"FontDescriptor", descriptor(name, "FontFile3", cffStream),
			"DW", 1000,
			"W", widths,
		)
		return type0(name, descendant)
	}

	// ttCIDFont is a Type0 font over a CIDFontType2 embedding the three-glyph
	// TrueType.
	ttCIDFont := func(name string) *pdfgen.Dict {
		descendant := d(
			"Type", n("Font"),
			"Subtype", n("CIDFontType2"),
			"BaseFont", n(name),
			"CIDSystemInfo", cidInfo(),
			"FontDescriptor", descriptor(name, "FontFile2", program(ttProgram, "Length1", len(ttProgram))),
			"DW", ttAdvanceWidth,
			"CIDToGIDMap", n("Identity"),
		)
		return type0(name, descendant)
	}

	// type3 is a Type 3 font whose glyph a shows shows through font, named
	// fontName.
	type3 := func(name, fontName string, font *pdfgen.Dict, shows string) *pdfgen.Dict {
		proc := "500 0 d0 BT /" + fontName + " 12 Tf 0 0 Td " + shows + " Tj ET"
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"Name", n(name),
			"FontBBox", a(0, 0, 1000, 1000),
			"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
			"CharProcs", d("a", program([]byte(proc))),
			"Encoding", d("Type", n("Encoding"), "Differences", a(97, n("a"))),
			"FirstChar", 97,
			"LastChar", 97,
			"Widths", a(500),
			"Resources", d("Font", d(fontName, font)),
		))
	}

	// Maps HV's code 1 to U+0041, so the Unicode rules have a value for it and the
	// .notdef rule alone judges its name.
	toUnicode := `/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def
/CMapName /Adobe-Identity-UCS def
/CMapType 2 def
1 begincodespacerange
<00> <FF>
endcodespacerange
1 beginbfchar
<01> <0041>
endbfchar
endcmap
CMapName currentdict /CMap defineresource pop
end
end
`

	pt := ttCIDFont("LithoCIDPattern")
	tc := ttCIDFont("LithoCIDType3")
	tf := ttCIDFont("LithoCIDType3Form")

	fonts := d(
		"NK", nameKeyedFont("ABCDEF+LithoNameKeyed", a(1, a(600), 3, a(500))),
		"NF", nameKeyedFont("LithoNameKeyedFull", a(1, a(600), 3, a(600))),
		"PT", pt,
		"TC", tc,
		"T3C", type3("LithoT3Claim", "TC", tc, "<0005>"),
		"HV", pdf.Indirect(
			d("Type", n("Font"), "Subtype", n("Type1"), "BaseFont", n("Helvetica"), "ToUnicode", program([]byte(toUnicode))),
		),
	)

	cell := pdf.Stream([]byte("BT /PT 12 Tf 2 2 Td <0005> Tj ET"))
	cell.Set("Type", n("Pattern"))
	cell.Set("PatternType", 1)
	cell.Set("PaintType", 1)
	cell.Set("TilingType", 1)
	cell.Set("BBox", a(0, 0, 20, 20))
	cell.Set("XStep", 20)
	cell.Set("YStep", 20)
	cell.Set("Resources", d("Font", d("PT", pt)))

	t3Form := pdf.Stream([]byte("BT /T3F 12 Tf 0 0 Td (a) Tj ET"))
	t3Form.Set("Type", n("XObject"))
	t3Form.Set("Subtype", n("Form"))
	t3Form.Set("BBox", a(0, 0, 612, 792))
	t3Form.Set("Resources", d("Font", d("T3F", type3("LithoT3Form", "TF", tf, "<0005>"))))

	content := "BT /NK 12 Tf 50 740 Td <00010003> Tj ET\n" +
		"BT /NF 12 Tf 50 710 Td <0003> Tj ET\n" +
		// The fill parses the cell before the text shows PT.
		"q /Pattern cs /P1 scn 50 600 60 60 re f Q\n" +
		"BT /PT 12 Tf 50 560 Td <0006> Tj ET\n" +
		// The Type 3 glyph parses its procedure before the text shows TC.
		"BT /T3C 12 Tf 50 530 Td (a) Tj ET\n" +
		"BT /TC 12 Tf 50 500 Td <00060007> Tj ET\n" +
		"q /FmT3 Do Q\n" +
		"BT /HV 12 Tf 50 440 Td (\\001) Tj ET\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", fonts,
			"Pattern", d("P1", pdf.MakeIndirect(cell)),
			"XObject", d("FmT3", pdf.MakeIndirect(t3Form)),
		),
		"Contents", pdf.Stream([]byte(content)),
	))
	pdf.AddPage(page)

	// Deliberately no /OutputIntents and no /Metadata, both independent of the font
	// clauses above so a font verdict stays visibly separate from them.
	return save(pdf, "1.7")
}
