package build

import (
	"encoding/binary"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("fontnohead_2b", fontnohead2b) }

// fontnohead2b builds fontnohead_2b.pdf: TrueType programs with no 'head' table.
//
// ISO 19005-2, 6.2.11.5 asks the widths of the font dictionary and of the
// embedded program to be consistent, which takes the program's advances in
// units of its em. 'head' states that em (unitsPerEm), and a program without the
// table states none. veraPDF still reads such a program, and scales its 'hmtx'
// advances as if the em were 2048 units. Each font shows codes 41 and 42 through
// /WinAnsiEncoding, or CIDs 1 and 2 under Identity-H:
//
//	H1  TrueType, advances 1229 with no 'head', /Widths 600: consistent at 2048,
//	    where litho read 1000 units and failed it.
//	H2  CIDFontType2, advances 600 with no 'head', /W 293: consistent at 2048.
//
// The converse, advances 600 under /Widths 600, fails at 2048 and passed at 1000.
// It stays out of the fixture: a font of each kind leaves both tools reporting
// the same width rule, for different glyphs, and the corpus gate compares rules.
//
// Controls: HK the program H1 is cut from, its 'head' stating 2048 units, and
// HO an OpenType program with CFF outlines and no 'head', whose widths a Type 1
// font reads from the CFF and not at 2048.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func fontnohead2b() ([]byte, error) {
	pdf := pdfgen.New()
	const nonsymbolic = 32
	const symbolic = 4

	full := ttBuild([]ttSubtable{{3, 1, map[int]string{0x41: "A", 0x42: "B"}}}, nil)
	tables := ttTablesOf(full)

	// withAdvance is the fixture's TrueType tables with every glyph advancing
	// advance units, 'head' stating upem when given, and no 'head' at all when
	// head is false.
	withAdvance := func(advance, upem int, head bool) []byte {
		t := maps.Clone(tables)
		var hmtx []byte
		for range ttGlyphNames {
			hmtx = binary.BigEndian.AppendUint16(hmtx, uint16(advance)) //nolint:gosec // struct's ">H".
			hmtx = binary.BigEndian.AppendUint16(hmtx, 50)
		}
		t["hmtx"] = hmtx
		hhea := append([]byte(nil), t["hhea"]...)
		binary.BigEndian.PutUint16(hhea[10:], uint16(advance)) //nolint:gosec // advanceWidthMax
		t["hhea"] = hhea
		if upem != 0 {
			h := append([]byte(nil), t["head"]...)
			binary.BigEndian.PutUint16(h[18:], uint16(upem)) //nolint:gosec // struct's ">H".
			t["head"] = h
		}
		if !head {
			delete(t, "head")
		}
		return ttAssemble([]byte("\x00\x01\x00\x00"), t)
	}

	descriptor := func(tag, key string, program []byte, flags int, streamExtra []any) *pdfgen.Dict {
		stream := pdf.Stream(program)
		for i := 0; i < len(streamExtra); i += 2 {
			stream.Set(streamExtra[i].(string), streamExtra[i+1])
		}
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
		dd.Set(key, pdf.MakeIndirect(stream))
		return pdf.Indirect(dd)
	}

	type taggedFont struct {
		tag  string
		font *pdfgen.Dict
	}

	simple := func(tag, subtype, key string, program []byte, widths *pdfgen.Array, streamExtra []any) taggedFont {
		return taggedFont{tag, pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n(subtype),
			"BaseFont", n(tag),
			"FirstChar", 65,
			"LastChar", 66,
			"Widths", widths,
			"Encoding", n("WinAnsiEncoding"),
			"FontDescriptor", descriptor(tag, key, program, nonsymbolic, streamExtra),
		))}
	}

	type2 := func(tag string, program []byte, width int) taggedFont {
		cid := d(
			"Type", n("Font"),
			"Subtype", n("CIDFontType2"),
			"BaseFont", n(tag),
			"CIDSystemInfo", d("Registry", text("Adobe"), "Ordering", text("Identity"), "Supplement", 0),
			"CIDToGIDMap", n("Identity"),
			"W", a(1, a(width, width)),
			"FontDescriptor", descriptor(tag, "FontFile2", program, symbolic, nil),
		)
		return taggedFont{tag, pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type0"),
			"BaseFont", n(tag),
			"Encoding", n("/Identity-H"),
			"DescendantFonts", a(pdf.Indirect(cid)),
		))}
	}

	ottoTables := map[string][]byte{
		"CFF ": cffBuild(), //nolint:gocritic // the table tag ends in a space.
		"cmap": tables["cmap"],
		"hhea": tables["hhea"],
		"hmtx": tables["hmtx"],
		"maxp": ttU16(ttU32(nil, 0x00005000), len(ttGlyphNames)),
		"name": tables["name"],
		"post": tables["post"],
	}

	fonts := []taggedFont{
		simple("H1", "TrueType", "FontFile2", withAdvance(1229, 0, false), a(600, 600), nil),
		type2("H2", withAdvance(600, 0, false), 293),
		simple("HK", "TrueType", "FontFile2", withAdvance(1229, 2048, true), a(600, 600), nil),
		simple("HO", "Type1", "FontFile3", ttAssemble([]byte("OTTO"), ottoTables), a(600, 600),
			[]any{"Subtype", n("OpenType")}),
	}

	resources := d()
	var body []byte
	y := 750
	for _, f := range fonts {
		resources.Set(f.tag, f.font)
		codes := "<4142>"
		if f.tag == "H2" {
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
