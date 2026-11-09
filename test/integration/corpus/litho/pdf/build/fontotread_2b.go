package build

import (
	"encoding/binary"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("fontotread_2b", fontotread2b) }

// fontotread2b builds fontotread_2b.pdf: glyph rules read an OpenType program by its font.
//
// veraPDF reads an embedded sfnt by the font that embeds it, not by the
// outlines it carries, for every rule, not only for the embedding verdict that
// fontcff2_2b.pdf plants. A Type 1 or CIDFontType0 font reads its 'CFF ' table
// and nothing else of the sfnt; a TrueType or CIDFontType2 font reads any sfnt,
// "OTTO" included, as a TrueType program through 'hmtx', 'maxp' and 'head'.
// Every program here is a /FontFile3 /OpenType. Its CFF charstrings advance 600,
// and its 'hmtx' states 500, so the two readings disagree on every width. Each
// simple font shows codes 41 and 42 through /WinAnsiEncoding, each Type 0 font
// CIDs 1 and 2 under Identity-H:
//
//	W1  Type 1 over an "OTTO", /Widths 600, as the 'CFF ' table states.
//	W2  Type 1 over the same program, /Widths 500, as 'hmtx' states.
//	W3  Type 1 over a version-1.0 sfnt carrying 'glyf' and a 'CFF ' table,
//	    /Widths 600.
//	W4  CIDFontType0 over a version-1.0 sfnt carrying 'glyf' and a CID-keyed
//	    'CFF ' table, /W 600.
//	W5  CIDFontType2 over an "OTTO" with a CID-keyed 'CFF ' table, /W 500, as
//	    'hmtx' states.
//	W6  CIDFontType2 over the same program, /W 600.
//	H1  TrueType over an "OTTO" with no 'head', whose 'hmtx' advances 1000:
//	    a TrueType program with no 'head' weighs its advances against 2048
//	    units, so /Widths 488.
//	P1  CIDFontType2 over an "OTTO" whose 'maxp' counts three glyphs and whose
//	    'CFF ' charset holds CIDs 1 to 3, showing CIDs 1 and 3.
//
// W2 and W6 fail the width rule, W1, W3, W4, W5 and H1 pass it.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func fontotread2b() ([]byte, error) {
	pdf := pdfgen.New()
	const nonsymbolic = 32
	const symbolic = 4

	tt := ttBuild([]ttSubtable{{3, 1, map[int]string{0x41: "A", 0x42: "B"}}}, nil)
	tables := ttTablesOf(tt)
	glyphs := len(ttGlyphNames)
	maxp05 := ttU16(ttU32(nil, 0x00005000), glyphs)

	hmtx := func(advance int) []byte {
		var out []byte
		for range glyphs {
			out = binary.BigEndian.AppendUint16(out, uint16(advance)) //nolint:gosec // struct's ">H".
			out = binary.BigEndian.AppendUint16(out, 50)
		}
		return out
	}

	// sfnt is an sfnt under version over the fixture's cmap, hhea, name and
	// post tables, with the 'CFF ' table given and 'hmtx' advancing advance.
	// glyf adds the TrueType outlines and a version-1.0 'maxp'.
	sfnt := func(version string, cff []byte, advance int, head, glyf bool) []byte {
		t := map[string][]byte{
			"cmap": tables["cmap"], "hhea": tables["hhea"], "hmtx": hmtx(advance),
			"name": tables["name"], "post": tables["post"], "CFF ": cff, //nolint:gocritic // the table tag ends in a space.
		}
		if head {
			t["head"] = tables["head"]
		}
		if glyf {
			t["glyf"] = tables["glyf"]
			t["loca"] = tables["loca"]
			t["maxp"] = tables["maxp"]
		} else {
			t["maxp"] = maxp05
		}
		return ttAssemble([]byte(version), t)
	}

	nameKeyed := sfnt("OTTO", cffBuild(), 500, true, false)
	nameKeyedTT := sfnt("\x00\x01\x00\x00", cffBuild(), 500, true, true)
	cidKeyed := sfnt("OTTO", cffBuildCID([]int{1, 2}), 500, true, false)
	cidKeyedTT := sfnt("\x00\x01\x00\x00", cffBuildCID([]int{1, 2}), 500, true, true)
	headless := sfnt("OTTO", cffBuild(), 1000, false, false)
	threeCIDs := sfnt("OTTO", cffBuildCID([]int{1, 2, 3}), 500, true, false)

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

	simple := func(tag, subtype string, program []byte, width int) taggedFont {
		return taggedFont{tag, pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n(subtype),
			"BaseFont", n(tag),
			"FirstChar", 65,
			"LastChar", 66,
			"Widths", a(width, width),
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
			"W", a(1, a(width, width, width)),
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
		simple("W1", "Type1", nameKeyed, 600),
		simple("W2", "Type1", nameKeyed, 500),
		simple("W3", "Type1", nameKeyedTT, 600),
		type0("W4", "CIDFontType0", cidKeyedTT, 600),
		type0("W5", "CIDFontType2", cidKeyed, 500),
		type0("W6", "CIDFontType2", cidKeyed, 600),
		simple("H1", "TrueType", headless, 488),
		type0("P1", "CIDFontType2", threeCIDs, 500),
	}
	shown := map[string]string{"W4": "<00010002>", "W5": "<00010002>", "W6": "<00010002>", "P1": "<00010003>"}

	resources := d()
	var body []byte
	y := 750
	for _, f := range fonts {
		resources.Set(f.tag, f.font)
		codes, ok := shown[f.tag]
		if !ok {
			codes = "<4142>"
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
