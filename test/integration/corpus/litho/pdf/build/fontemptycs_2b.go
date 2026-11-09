package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("fontemptycs_2b", fontemptycs2b) }

// fontemptycs2b builds fontemptycs_2b.pdf: CFF programs whose CharStrings INDEX is empty.
//
// Such a program still reads: its header leads to a Top DICT and on to a
// CharStrings INDEX, which holds no glyph. ISO 19005-2, 6.2.11.4.1 t1 then counts
// the font as embedded, 6.2.11.4.1 t2 finds no glyph for any code it shows, and
// 6.2.11.5 still weighs the width each glyph is drawn with against the one the
// font dictionary declares. veraPDF takes that width from the font: the
// defaultWidthX of the Private DICT, or, for a subset or a CID-keyed program,
// the width the font dictionary gives a missing glyph. Each simple font shows
// codes 41 and 42 through /WinAnsiEncoding, each Type 0 font CIDs 1 and 2 under
// Identity-H:
//
//	E1  Type 1, defaultWidthX 700, /Widths 700.
//	E2  Type 1, defaultWidthX 700, /Widths 0.
//	E3  Type 1, a subset (ABCDEF+E3), defaultWidthX 700, /Widths 700, no
//	    /MissingWidth.
//	E4  Type 1, a subset (ABCDEF+E4), /Widths 300, /MissingWidth 300.
//	E5  Type 1, defaultWidthX 700, /Widths 700, a /Differences naming code 41
//	    .notdef.
//	C1  CIDFontType0, CID-keyed, /W 1000 for both CIDs, no /DW.
//	C2  CIDFontType0, CID-keyed, /W 500.
//	K1  CIDFontType0, name-keyed, defaultWidthX 700, /W 700.
//	K2  CIDFontType0, name-keyed, defaultWidthX 700, /W 0.
//
// Controls: EK the whole program E1 to E5 are cut from, and CK the whole
// CID-keyed program C1 and C2 are cut from.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func fontemptycs2b() ([]byte, error) {
	pdf := pdfgen.New()
	const nonsymbolic = 32
	const symbolic = 4
	const defaultWidth = 700

	empty := cffBuildCharstrings(nil, defaultWidth)

	descriptor := func(tag string, program []byte, subtype string, flags int, extra []any) *pdfgen.Dict {
		stream := pdf.Stream(program)
		stream.Set("Subtype", n(subtype))
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
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		dd.Set("FontFile3", pdf.MakeIndirect(stream))
		return pdf.Indirect(dd)
	}

	type taggedFont struct {
		tag  string
		font *pdfgen.Dict
	}

	simple := func(tag string, program []byte, widths *pdfgen.Array, base string, encoding obj, extra []any) taggedFont {
		name := base
		if name == "" {
			name = tag
		}
		if encoding == nil {
			encoding = n("WinAnsiEncoding")
		}
		return taggedFont{tag, pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type1"),
			"BaseFont", n(name),
			"FirstChar", 65,
			"LastChar", 66,
			"Widths", widths,
			"Encoding", encoding,
			"FontDescriptor", descriptor(name, program, "Type1C", nonsymbolic, extra),
		))}
	}

	type0 := func(tag string, program []byte, width int) taggedFont {
		cid := d(
			"Type", n("Font"),
			"Subtype", n("CIDFontType0"),
			"BaseFont", n(tag),
			"CIDSystemInfo", d("Registry", text("Adobe"), "Ordering", text("Identity"), "Supplement", 0),
			"W", a(1, a(width, width)),
			"FontDescriptor", descriptor(tag, program, "CIDFontType0C", symbolic, nil),
		)
		return taggedFont{tag, pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type0"),
			"BaseFont", n(tag),
			"Encoding", n("/Identity-H"),
			"DescendantFonts", a(pdf.Indirect(cid)),
		))}
	}

	notdef41 := d("Type", n("Encoding"), "BaseEncoding", n("WinAnsiEncoding"),
		"Differences", a(65, n("/.notdef")))

	fonts := []taggedFont{
		simple("E1", empty, a(700, 700), "", nil, nil),
		simple("E2", empty, a(0, 0), "", nil, nil),
		simple("E3", empty, a(700, 700), "ABCDEF+E3", nil, nil),
		simple("E4", empty, a(300, 300), "ABCDEF+E4", nil, []any{"MissingWidth", 300}),
		simple("E5", empty, a(700, 700), "", notdef41, nil),
		type0("C1", cffBuildCIDCharstrings(0, defaultWidth), 1000),
		type0("C2", cffBuildCIDCharstrings(0, defaultWidth), 500),
		type0("K1", empty, 700),
		type0("K2", empty, 0),
		simple("EK", cffBuild(), a(600, 600), "", nil, nil),
		type0("CK", cffBuildCIDCharstrings(3, cffAdvanceWidth), 600),
	}

	resources := d()
	var body []byte
	y := 750
	for _, f := range fonts {
		resources.Set(f.tag, f.font)
		codes := "<4142>"
		if f.tag[0] == 'C' || f.tag[0] == 'K' {
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
