package build

import (
	"fmt"
	"slices"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("fontcff2_2b", fontcff22b) }

// fontcff22b builds fontcff2_2b.pdf: OpenType programs read by the font that embeds them.
//
// ISO 32000-1, Table 126 lets /FontFile3 /OpenType hold an OpenType program
// (PDF 1.6). veraPDF reads it by the font that embeds it, not by the outlines it
// carries: a Type 1 or CIDFontType0 font as a CFF font through its 'CFF ' table,
// a TrueType or CIDFontType2 font as a TrueType font through its 'hhea' and
// 'hmtx' tables. A program lacking the tables its reading needs is not embedded
// (ISO 19005-2, 6.2.11.4.1 t1), and a 'CFF2' table (OpenType 1.8) is not a
// 'CFF ' table. Each simple font shows codes 41 and 42 through
// /WinAnsiEncoding, each Type 0 font CIDs 1 and 2 under Identity-H:
//
//	F1  Type 1, an "OTTO" whose outlines are 'CFF2'.
//	F2  CIDFontType0, the same program.
//	F3  TrueType, the same program, /Widths 600 as 'hmtx' states.
//	F4  CIDFontType2, the same program, /W 600.
//	F5  TrueType, the same program, /Widths 500.
//	M1  Type 1, a TrueType-outline sfnt (version 1.0), no 'CFF ' table.
//	M2  CIDFontType0, the same sfnt.
//	M3  TrueType, an "OTTO" with a 'CFF ' table and no 'hhea' or 'hmtx'.
//	M4  CIDFontType2, the same program.
//
// F1, F2, M1 to M4 are not embedded. F3 and F4 are, and F5 fails the width rule
// alone. OK, a Type 1 font over a whole "OTTO" with 'CFF ' outlines, is the
// control.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func fontcff22b() ([]byte, error) {
	pdf := pdfgen.New()
	const nonsymbolic = 32
	const symbolic = 4

	tt := ttBuild([]ttSubtable{{3, 1, map[int]string{0x41: "A", 0x42: "B"}}}, nil)
	tables := ttTablesOf(tt)
	maxp05 := ttU16(ttU32(nil, 0x00005000), len(ttGlyphNames))

	// otto is an "OTTO" program over the fixture's cmap, name and post tables,
	// with the outline table given and, when metrics is true, 'hhea' and
	// 'hmtx'.
	otto := func(outlines map[string][]byte, metrics bool) []byte {
		t := map[string][]byte{
			"cmap": tables["cmap"], "head": tables["head"], "maxp": maxp05,
			"name": tables["name"], "post": tables["post"],
		}
		for k, v := range outlines {
			t[k] = v
		}
		if metrics {
			t["hhea"] = tables["hhea"]
			t["hmtx"] = tables["hmtx"]
		}
		return ttAssemble([]byte("OTTO"), t)
	}

	cff2 := otto(map[string][]byte{"CFF2": cffBuildCFF2()}, true)
	cffNoMetrics := otto(map[string][]byte{"CFF ": cffBuild()}, false)
	whole := otto(map[string][]byte{"CFF ": cffBuild()}, true)

	descriptor := func(tag string, program []byte, flags int) *pdfgen.Dict {
		stream := pdf.Stream(program)
		stream.Set("Subtype", n("OpenType"))
		dd := d(
			"Type", n("FontDescriptor"),
			"FontName", n(tag),
			"Flags", flags,
			"FontBBox", a(0, -200, 550, 700),
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
		)
		dd.Set("FontFile3", pdf.MakeIndirect(stream))
		return pdf.Indirect(dd)
	}

	type taggedFont struct {
		tag  string
		font *pdfgen.Dict
	}

	simple := func(tag, subtype string, program []byte, widths *pdfgen.Array) taggedFont {
		return taggedFont{tag, pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n(subtype),
			"BaseFont", n(tag),
			"FirstChar", 65,
			"LastChar", 66,
			"Widths", widths,
			"Encoding", n("WinAnsiEncoding"),
			"FontDescriptor", descriptor(tag, program, nonsymbolic),
		))}
	}

	type0 := func(tag, subtype string, program []byte, width int) taggedFont {
		cid := d(
			"Type", n("Font"),
			"Subtype", n(subtype),
			"BaseFont", n(tag),
			"CIDSystemInfo", d("Registry", text("Adobe"), "Ordering", text("Identity"), "Supplement", 0),
			"W", a(1, a(width, width)),
			"FontDescriptor", descriptor(tag, program, symbolic),
		)
		if subtype == "CIDFontType2" {
			cid.Set("CIDToGIDMap", n("Identity"))
		}
		return taggedFont{tag, pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type0"),
			"BaseFont", n(tag),
			"Encoding", n("/Identity-H"),
			"DescendantFonts", a(pdf.Indirect(cid)),
		))}
	}

	fonts := []taggedFont{
		simple("F1", "Type1", cff2, a(600, 600)),
		type0("F2", "CIDFontType0", cff2, 600),
		simple("F3", "TrueType", cff2, a(600, 600)),
		type0("F4", "CIDFontType2", cff2, 600),
		simple("F5", "TrueType", cff2, a(500, 500)),
		simple("M1", "Type1", tt, a(600, 600)),
		type0("M2", "CIDFontType0", tt, 600),
		simple("M3", "TrueType", cffNoMetrics, a(600, 600)),
		type0("M4", "CIDFontType2", cffNoMetrics, 600),
		simple("OK", "Type1", whole, a(600, 600)),
	}

	resources := d()
	var body []byte
	y := 750
	for _, f := range fonts {
		resources.Set(f.tag, f.font)
		codes := "<4142>"
		if slices.Contains([]string{"F2", "F4", "M2", "M4"}, f.tag) {
			codes = "<00010002>"
		}
		body = fmt.Appendf(body, "BT 10 %d Td /%s 12 Tf %s Tj ET\n", y, f.tag, codes)
		y -= 20
	}
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", resources),
		"Contents", pdf.MakeIndirect(pdf.Stream(body)),
	))
	pdf.AddPage(page)

	// Deliberately no /OutputIntents and no /Metadata.
	return save(pdf, "1.7")
}
