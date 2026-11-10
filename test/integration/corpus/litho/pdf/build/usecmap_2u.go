package build

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen/zdeflate"
)

func init() { register("usecmap_2u", usecmap2u) }

// usecmap2u builds usecmap_2u.pdf: /ToUnicode CMaps that build on other CMaps.
//
// ISO 32000-1, 9.10.3 gives a /ToUnicode CMap the syntax of 9.7.5, so it can
// build on another CMap two ways: the usecmap operator in its program names a
// predefined CMap, and a /UseCMap entry in its stream dictionary (Table 120)
// names a predefined CMap or an embedded CMap stream, which may name another in
// turn. veraPDF merges the single-code mappings of a usecmap base into the
// program, a base replacing the mappings defined before the operator and
// yielding to those after it, and supplies every mapping of a /UseCMap base to
// the codes the program leaves out. ISO 32000-1, 9.10.2 names the Adobe
// Registry-Ordering-UCS2 CMaps as the source of a CIDFont's Unicode values, and
// veraPDF reads Adobe's current resources, which map codes the 2009 copies
// PDF.js ships leave out and drop others; PDF.js ships no Adobe-KR-UCS2 at all.
// pdfjs issue5010.pdf is the one corpus file whose /ToUnicode builds on another
// CMap, through usecmap and Adobe-Korea1-UCS2, where the two sources agree, and
// no file names a base with /UseCMap, so no count reached any of this.
//
// ISO 19005-2, 6.2.11.7.2 t1 fails a glyph whose code the /ToUnicode maps to no
// value, and t2 one it maps to U+0000. Five Type 3 fonts each show codes 41, 60,
// 61 and 65 hexadecimal through glyphs whose names carry no Unicode value:
//
// | Font | /ToUnicode                                                     | t1 | t2 |
// | ---- | -------------------------------------------------------------- | -- | -- |
// | T1   | runs `/No-Such-CMap usecmap`, a CMap nobody ships              | 4  | 0  |
// | T2   | `/UseCMap /Adobe-Korea1-UCS2`, a predefined base               | 0  | 0  |
// | T3   | `/UseCMap` a stream mapping 41 and 65                          | 2  | 0  |
// | T4   | `/UseCMap` a stream mapping 65, whose `/UseCMap` maps 41       | 2  | 0  |
// | T5   | maps 60 to U+0000, runs `/Adobe-Korea1-UCS2 usecmap`, maps 61 to U+0000 | 2 | 1 |
//
// Adobe-Korea1-UCS2 maps CIDs 60 and 61 through bfchar and 41 and 65 through
// bfrange, which the usecmap operator does not merge, so T5's base supplies 60,
// replacing its U+0000, and nothing for 41 and 65, while its 61 keeps U+0000.
//
// Six Type0 fonts then map no code themselves and build on an Adobe UCS2 CMap,
// showing CIDs where Adobe's current resources and the PDF.js copies disagree.
// Each is a CIDFontType2 over the three-glyph TrueType program whose
// /CIDToGIDMap stream sends every CID it shows but 0 to a glyph, so no presence
// or width rule answers:
//
// | Font | Base                          | Shows                                                  | t1 |
// | ---- | ----------------------------- | ------------------------------------------------------ | -- |
// | C1   | `/Adobe-GB1-UCS2 usecmap`     | 0254 0255 0258 0259 5610 5617 0000 0100                | 1  |
// | C2   | `/Adobe-CNS1-UCS2 usecmap`    | 3550 44DF 44E0 4A90 4A91 4A92 0000 0106                | 1  |
// | C3   | `/Adobe-Japan1-UCS2 usecmap`  | 00E7 0143 0144 0F1B 0F1C 15FC 018A 018B 23BF 0000 0100 | 4  |
// | C4   | `/Adobe-KR-UCS2 usecmap`      | 0000 0064 0065 006A 006B 0084 0104                     | 1  |
// | C5   | `/UseCMap /Adobe-CNS1-UCS2`   | 4A90 4A91 4A92 4A93 4A94 4A95 0000 4AEB                | 1  |
// | C6   | `/UseCMap /Adobe-Japan1-UCS2` | 5A12 5A13 0000 5A14                                    | 1  |
//
// Adobe's CNS1 resource maps C2's CID 44DF to U+F538, a Private Use value, which
// ISO 19005-2, 6.2.11.7.3 t1 and ISO 14289-2, 8.4.3 t1 fail with no /ActualText
// around it. Both tools report each once, the value coming from the base alone.
//
// Each shows CID 0 as well, the .notdef glyph, which 6.2.11.8 t1 fails once per
// font. Two readings stay out. A /UseCMap chain that loops makes veraPDF raise
// "Loop inside CMap" and abandon the validation. And CIDs 5A0D and 5A0E, which
// Adobe's Japan1 resource maps through the last bfrange of a block it counts as
// 42 entries, veraPDF's bundled copy counts as 41, so its count-bound reader
// drops them and fails both: an artifact of its copy, not of the rule, which C6
// showing them would turn into a disagreeing cell.
//
// Every count was confirmed with live veraPDF 1.30.2. See ../FIXTURES.md for the
// authoritative expected firing set.
func usecmap2u() ([]byte, error) {
	pdf := pdfgen.New()
	ttProgram := ttBuild([]ttSubtable{{3, 1, map[int]string{0x41: "A", 0x42: "B"}}}, nil)

	// cmapProgram is a /ToUnicode CMap program carrying body between its code
	// space and end.
	cmapProgram := func(body, codespace string) []byte {
		return []byte("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n" +
			"/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n" +
			"1 begincodespacerange\n" + codespace + "\nendcodespacerange\n" + body +
			"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	}
	const byteSpace = "<00> <FF>"

	bfchars := func(pairs ...string) string {
		var out strings.Builder
		fmt.Fprintf(&out, "%d beginbfchar\n", len(pairs)/2)
		for i := 0; i+1 < len(pairs); i += 2 {
			fmt.Fprintf(&out, "<%s> <%s>\n", pairs[i], pairs[i+1])
		}
		return out.String() + "endbfchar\n"
	}

	cmapStream := func(program []byte, useCMap any) *pdfgen.Stream {
		stream := pdf.Stream(program)
		if useCMap != nil {
			stream.Set("UseCMap", useCMap)
		}
		pdf.MakeIndirect(stream)
		return stream
	}

	ttFile := pdf.MakeIndirect(pdf.Stream(ttProgram, "Length1", len(ttProgram)))
	glyph := pdf.MakeIndirect(pdf.Stream([]byte("500 0 0 0 1000 1000 d1 0 0 1000 1000 re f\n")))

	// type3 is a Type 3 font naming codes 41, 60, 61 and 65 with names of no
	// Unicode value.
	type3 := func(toUnicode *pdfgen.Stream) *pdfgen.Dict {
		widths := a()
		for range 37 {
			widths.Append(500)
		}
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"FontBBox", a(0, 0, 1000, 1000),
			"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
			"CharProcs", d("zzq", glyph, "zzr", glyph, "zzs", glyph, "zzt", glyph),
			"Encoding", d("Type", n("Encoding"), "Differences", a(
				65, n("/zzq"), 96, n("/zzr"), n("/zzs"), 101, n("/zzt"))),
			"FirstChar", 65,
			"LastChar", 101,
			"Widths", widths,
			"Resources", d(),
			"ToUnicode", toUnicode,
		))
	}

	type3Fonts := []*pdfgen.Dict{
		type3(cmapStream(cmapProgram("/No-Such-CMap usecmap\n", byteSpace), nil)),
		type3(cmapStream(cmapProgram("", byteSpace), n("/Adobe-Korea1-UCS2"))),
		type3(cmapStream(cmapProgram("", byteSpace), cmapStream(cmapProgram(bfchars("41", "0041", "65", "0065"), byteSpace), nil))),
		type3(cmapStream(cmapProgram("", byteSpace), cmapStream(cmapProgram(bfchars("65", "0065"), byteSpace),
			cmapStream(cmapProgram(bfchars("41", "0041"), byteSpace), nil)))),
		type3(cmapStream(cmapProgram(bfchars("60", "0000")+"/Adobe-Korea1-UCS2 usecmap\n"+
			bfchars("61", "0000"), byteSpace), nil)),
	}

	type cidFontResult struct {
		font  *pdfgen.Dict
		shows string
	}

	// cidFont is a Type0 font whose CIDToGIDMap sends every CID it shows but 0
	// to a glyph.
	//
	// Its /ToUnicode maps nothing itself and builds on the predefined CMap base,
	// through a /UseCMap entry when entry is set and the usecmap operator
	// otherwise.
	cidFont := func(name string, cids []int, base string, entry bool) cidFontResult {
		gids := make([]byte, 2*(slices.Max(cids)+1))
		for _, cid := range cids {
			if cid != 0 {
				gids[2*cid+1] = 1
			}
		}
		gidMap := pdf.Stream(zdeflate.Compress(gids, 9))
		gidMap.Set("Filter", n("FlateDecode"))
		descendant := pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("CIDFontType2"),
			"BaseFont", n("/"+name),
			"CIDSystemInfo", d("Registry", text("Adobe"), "Ordering", text("Identity"), "Supplement", 0),
			"DW", ttAdvanceWidth,
			"CIDToGIDMap", pdf.MakeIndirect(gidMap),
			"FontDescriptor", pdf.Indirect(d(
				"Type", n("FontDescriptor"), "FontName", n("/"+name), "Flags", 32, "ItalicAngle", 0, "Ascent", 800,
				"Descent", -200, "CapHeight", 700, "StemV", 80, "FontBBox", a(0, -200, 550, 700),
				"FontFile2", ttFile)),
		))
		var toUnicode *pdfgen.Stream
		if entry {
			toUnicode = cmapStream(cmapProgram("", "<0000> <FFFF>"), n("/"+base))
		} else {
			toUnicode = cmapStream(cmapProgram(fmt.Sprintf("/%s usecmap\n", base), "<0000> <FFFF>"), nil)
		}
		font := pdf.Indirect(d("Type", n("Font"), "Subtype", n("Type0"), "BaseFont", n("/"+name),
			"Encoding", n("/Identity-H"), "DescendantFonts", a(descendant),
			"ToUnicode", toUnicode))
		var shows strings.Builder
		shows.WriteString("<")
		for _, c := range cids {
			fmt.Fprintf(&shows, "%04X", c)
		}
		return cidFontResult{font, shows.String() + ">"}
	}

	cidFonts := []cidFontResult{
		cidFont("LithoGB1", []int{0x0254, 0x0255, 0x0258, 0x0259, 0x5610, 0x5617, 0x0000, 0x0100},
			"Adobe-GB1-UCS2", false),
		cidFont("LithoCNS1", []int{0x3550, 0x44DF, 0x44E0, 0x4A90, 0x4A91, 0x4A92, 0x0000, 0x0106},
			"Adobe-CNS1-UCS2", false),
		cidFont("LithoJapan1", []int{0x00E7, 0x0143, 0x0144, 0x0F1B, 0x0F1C, 0x15FC, 0x018A, 0x018B, 0x23BF, 0x0000, 0x0100},
			"Adobe-Japan1-UCS2", false),
		cidFont("LithoKR", []int{0x0000, 0x0064, 0x0065, 0x006A, 0x006B, 0x0084, 0x0104}, "Adobe-KR-UCS2", false),
		cidFont("LithoCNS1Entry", []int{0x4A90, 0x4A91, 0x4A92, 0x4A93, 0x4A94, 0x4A95, 0x0000, 0x4AEB}, "Adobe-CNS1-UCS2", true),
		cidFont("LithoJapan1Entry", []int{0x5A12, 0x5A13, 0x0000, 0x5A14}, "Adobe-Japan1-UCS2", true),
	}

	fonts := d()
	var content strings.Builder
	for index, font := range type3Fonts {
		fonts.Set(fmt.Sprintf("/T%d", index+1), font)
		fmt.Fprintf(&content, "BT /T%d 12 Tf 10 %d Td <41606165> Tj ET\n", index+1, 780-20*index)
	}
	for index, cf := range cidFonts {
		fonts.Set(fmt.Sprintf("/C%d", index+1), cf.font)
		fmt.Fprintf(&content, "BT /C%d 12 Tf 10 %d Td %s Tj ET\n", index+1, 660-20*index, cf.shows)
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", fonts),
		"Contents", pdf.Stream([]byte(content.String())),
	))
	pdf.AddPage(page)

	// Deliberately no /Metadata and no output intent.
	return save(pdf, "1.7")
}
