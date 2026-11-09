package build

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("fontcopies_2b", fontcopies2b) }

// fontcopies2b builds fontcopies_2b.pdf: fonts written directly, and CIDs past a program.
//
// A font dictionary written directly in a /Font resource dictionary has no
// object number, and veraPDF tells its copies apart by the dictionary that
// writes each one: every dictionary is a font of its own, judged once however
// many pages or forms reach it. Twelve corpus files write a font directly, and
// judging each copy rather than the first page that names it moves no count, so
// no fixture pinned the reading.
//
// Three fonts are written directly, each a distinct defect:
//
//	U  an unembedded Helvetica, ISO 19005-2, 6.2.11.4.1 t1 once per copy;
//	W  a Type 1 subset over a bare CFF whose /Widths say 100 where the program
//	   says 600, 6.2.11.5 t1 once per glyph shown, two per copy;
//	C  a Type0 over a shared CIDFontType2 of three glyphs, showing CID 5, which
//	   names no glyph: 6.2.11.4.1 t2 and 6.2.11.8 t1 once per copy, and, since
//	   /DW 1000 disagrees with the 600 glyph 0 advances, 6.2.11.5 t1 too.
//
// They sit in nine layouts, each on pages of its own, 13 copies in all:
//
// | Layout                                             | Copies |
// | -------------------------------------------------- | ------ |
// | two pages, a copy in each page's resources         | 2      |
// | two pages sharing one indirect resource dictionary | 1      |
// | two pages sharing one indirect /Font dictionary    | 1      |
// | two pages inheriting resources from their node     | 1      |
// | an indirect font on one page, a copy of it on the next, one name | 2 |
// | two names on one page, a copy each                 | 2      |
// | one form two pages paint                           | 1      |
// | two forms on one page, a copy in each              | 2      |
// | two forms sharing one indirect resource dictionary | 1      |
//
// Every rule above counts 13 at PDF/A-2b, W's width rule 26.
//
// A CID a TrueType CIDFont's program holds no glyph for is weighed at the
// advance of glyph 0 (veraPDF-validation 1.30, PDFontFile), so it fails the width
// rule wherever the declared width disagrees with that advance, beside the
// presence and .notdef rules. fontleft_2b.pdf shows such CIDs with /DW set to the
// advance, where the width rule stays silent, and pdfjs Brotli-Prototype-FileA.pdf
// shows one veraPDF cannot open. Seven more Type0 fonts, indirect, one CID each,
// each over the three-glyph program:
//
// | Font | Descendant                                            | Shows | Answers at PDF/A-2b     |
// | ---- | ----------------------------------------------------- | ----- | ----------------------- |
// | M1   | `/DW 1000 /W [5 [600]]`, the width of glyph 0         | 5     | presence, .notdef       |
// | M2   | `/DW 600 /W [5 [500]]`                                | 5     | presence, .notdef, width |
// | M3   | no `/DW`, so 1000                                     | 5     | presence, .notdef, width |
// | M4   | no `/CIDToGIDMap`, so Identity                        | 5     | presence, .notdef, width |
// | M5   | `/DW 1000`, the first CID past the program            | 3     | presence, .notdef, width |
// | M6   | `/CIDToGIDMap` stream sending CID 1 to glyph 100      | 1     | presence, .notdef, width |
// | M7   | `/CIDToGIDMap` stream of two entries, ending before it | 5    | presence, .notdef, width |
//
// And the rules veraPDF states on a descendant CIDFont count descendant objects,
// while the /CIDSystemInfo rule counts composite fonts. Two unembedded Type0
// fonts under UniJIS-UCS2-H whose descendant declares Adobe-Korea1 sit on one
// page and share one descendant object (6.2.11.4.1 t1 once, 6.2.11.3.1 t1
// twice); two pages write the same Type0 font directly, each with its descendant
// written in it (t1 twice, 6.2.11.3.1 t1 twice).
//
// Three rules answer beside these: the Korean descendants carry no /Type, which
// 6.2.11.2 t1 fails 7 times, once per descendant and once more per composite font
// read through it; M4 embeds a program with no /CIDToGIDMap, 6.2.11.3.2 t1; and
// the two pages inheriting their resources resolve every font through them,
// 6.2.2 t2.
//
// Every count was confirmed with live veraPDF 1.30.2. See ../FIXTURES.md for the
// authoritative expected firing set.
func fontcopies2b() ([]byte, error) {
	pdf := pdfgen.New()

	const nonsymbolic = 32
	ttProgram := ttBuild([]ttSubtable{{3, 1, map[int]string{0x41: "A", 0x42: "B"}}}, nil)
	cffProgram := cffBuild()

	program := func(data []byte, keys ...any) *pdfgen.Stream {
		stream := pdf.Stream(data)
		for i := 0; i < len(keys); i += 2 {
			stream.Set(keys[i].(string), keys[i+1])
		}
		return pdf.MakeIndirect(stream).(*pdfgen.Stream)
	}

	descriptor := func(name string, fontFile ...any) *pdfgen.Dict {
		return pdf.Indirect(d(append([]any{
			"Type", n("FontDescriptor"),
			"FontName", n("/" + name),
			"Flags", nonsymbolic,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
		}, fontFile...)...))
	}

	cidInfo := func(ordering string) *pdfgen.Dict {
		return d("Registry", text("Adobe"), "Ordering", text(ordering), "Supplement", 0)
	}

	helvetica := descriptor("Helvetica")
	subset := descriptor("ABCDEF+LithoCFF", "FontFile3", program(cffProgram, "Subtype", n("Type1C")))
	ttFile := program(ttProgram, "Length1", len(ttProgram))
	sharedCID := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("CIDFontType2"),
		"BaseFont", n("/ABCDEF+LithoCID"),
		"CIDSystemInfo", cidInfo("Identity"),
		"FontDescriptor", descriptor("ABCDEF+LithoCID", "FontFile2", ttFile),
		"DW", 1000,
		"CIDToGIDMap", n("Identity"),
	))

	fontU := func() *pdfgen.Dict {
		return d("Type", n("Font"), "Subtype", n("Type1"), "BaseFont", n("Helvetica"), "FirstChar", 65, "LastChar", 66,
			"Widths", a(600, 600), "Encoding", n("WinAnsiEncoding"), "FontDescriptor", helvetica)
	}

	fontW := func() *pdfgen.Dict {
		return d("Type", n("Font"), "Subtype", n("Type1"), "BaseFont", n("/ABCDEF+LithoCFF"), "FirstChar", 65,
			"LastChar", 66, "Widths", a(100, 100), "Encoding", n("WinAnsiEncoding"), "FontDescriptor", subset)
	}

	fontC := func() *pdfgen.Dict {
		return d("Type", n("Font"), "Subtype", n("Type0"), "BaseFont", n("/ABCDEF+LithoCID"),
			"Encoding", n("/Identity-H"), "DescendantFonts", a(sharedCID))
	}

	// fonts is a /Font dictionary writing the three fonts directly.
	fonts := func() *pdfgen.Dict {
		return d("U", fontU(), "W", fontW(), "C", fontC())
	}

	const textOps = "BT /U 12 Tf 10 50 Td (AB) Tj ET BT /W 12 Tf 10 30 Td (AB) Tj ET BT /C 12 Tf 10 10 Td <0005> Tj ET\n"

	root := pdf.Root.Get("Pages").(*pdfgen.Dict)
	root.Set("MediaBox", a(0, 0, 200, 100))

	attach := func(parent, kid *pdfgen.Dict, pages int) {
		parent.Get("Kids").(*pdfgen.Array).Append(kid)
		node := parent
		for {
			node.Set("Count", int(node.Get("Count").(pdfgen.Int))+pages)
			if !node.Has("Parent") {
				break
			}
			node = node.Get("Parent").(*pdfgen.Dict)
		}
	}

	// page's Python defaults are resources None and parent root.
	page := func(content string, resources, parent *pdfgen.Dict) *pdfgen.Dict {
		dd := d("Type", n("Page"), "Parent", parent, "Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))))
		if resources != nil {
			dd.Set("Resources", resources)
		}
		ref := pdf.Indirect(dd)
		attach(parent, ref, 1)
		return ref
	}

	// form's Python default body is TEXT.
	form := func(resources *pdfgen.Dict, body string) *pdfgen.Stream {
		stream := pdf.Stream([]byte(body))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 200, 100))
		stream.Set("Resources", resources)
		return pdf.MakeIndirect(stream).(*pdfgen.Stream)
	}

	// Two pages, a copy each.
	page(textOps, d("Font", fonts()), root)
	page(textOps, d("Font", fonts()), root)
	// Two pages sharing one indirect resource dictionary.
	sharedRes := pdf.Indirect(d("Font", fonts()))
	page(textOps, sharedRes, root)
	page(textOps, sharedRes, root)
	// Two pages sharing one indirect /Font dictionary.
	sharedFonts := pdf.Indirect(fonts())
	page(textOps, d("Font", sharedFonts), root)
	page(textOps, d("Font", sharedFonts), root)
	// Two pages inheriting their resources from their node.
	node := pdf.Indirect(d("Type", n("Pages"), "Kids", a(), "Count", 0, "Parent", root,
		"Resources", d("Font", fonts())))
	attach(root, node, 0)
	page(textOps, nil, node)
	page(textOps, nil, node)
	// An indirect font, then a copy of it under the same name.
	indirect := d("U", pdf.Indirect(fontU()), "W", pdf.Indirect(fontW()), "C", pdf.Indirect(fontC()))
	page(textOps, d("Font", indirect), root)
	page(textOps, d("Font", fonts()), root)
	// Two names on one page.
	renamed := strings.NewReplacer("/U ", "/U2 ", "/W ", "/W2 ", "/C ", "/C2 ").Replace(textOps)
	page(textOps+renamed,
		d("Font", d("U", fontU(), "W", fontW(), "C", fontC(), "U2", fontU(), "W2", fontW(), "C2", fontC())), root)
	// One form two pages paint.
	oneForm := form(d("Font", fonts()), textOps)
	page("/X1 Do\n", d("XObject", d("X1", oneForm)), root)
	page("/X1 Do\n", d("XObject", d("X1", oneForm)), root)
	// Two forms, a copy each.
	page("/X1 Do /X2 Do\n", d("XObject", d("X1", form(d("Font", fonts()), textOps),
		"X2", form(d("Font", fonts()), textOps))), root)
	// Two forms sharing one indirect resource dictionary.
	formsRes := pdf.Indirect(d("Font", fonts()))
	page("/X1 Do /X2 Do\n", d("XObject", d("X1", form(formsRes, textOps), "X2", form(formsRes, textOps))), root)

	type missingFont struct {
		font *pdfgen.Dict
		show int
	}

	// missing is a Type0 font over its own CIDFontType2 on the three-glyph
	// program.
	missing := func(name string, show int, cidKeys ...any) missingFont {
		descendant := d(
			"Type", n("Font"),
			"Subtype", n("CIDFontType2"),
			"BaseFont", n("/"+name),
			"CIDSystemInfo", cidInfo("Identity"),
			"FontDescriptor", descriptor(name, "FontFile2", ttFile),
		)
		for i := 0; i < len(cidKeys); i += 2 {
			descendant.Set(cidKeys[i].(string), cidKeys[i+1])
		}
		return missingFont{pdf.Indirect(d("Type", n("Font"), "Subtype", n("Type0"), "BaseFont", n("/"+name),
			"Encoding", n("/Identity-H"),
			"DescendantFonts", a(pdf.Indirect(descendant)))), show}
	}

	gidMap := func(gids ...int) *pdfgen.Stream {
		var data []byte
		for _, g := range gids {
			data = binary.BigEndian.AppendUint16(data, uint16(g)) //nolint:gosec // glyph indexes below 2^16.
		}
		return program(data)
	}

	missingFonts := []missingFont{
		missing("LithoMissingW", 5, "DW", 1000, "W", a(5, a(ttAdvanceWidth)), "CIDToGIDMap", n("Identity")),
		missing("LithoMissingWOff", 5, "DW", ttAdvanceWidth, "W", a(5, a(500)), "CIDToGIDMap", n("Identity")),
		missing("LithoMissingNoDW", 5, "CIDToGIDMap", n("Identity")),
		missing("LithoMissingNoMap", 5, "DW", 1000),
		missing("LithoMissingFirst", 3, "DW", 1000, "CIDToGIDMap", n("Identity")),
		missing("LithoMissingMapped", 1, "DW", 1000, "CIDToGIDMap", gidMap(0, 100)),
		missing("LithoMissingShortMap", 5, "DW", 1000, "CIDToGIDMap", gidMap(0, 1)),
	}
	missingRes := d()
	missingText := ""
	for index, m := range missingFonts {
		missingRes.Set(fmt.Sprintf("/M%d", index+1), m.font)
		missingText += fmt.Sprintf("BT /M%d 12 Tf 10 %d Td <%04X> Tj ET\n", index+1, 90-10*index, m.show)
	}
	page(missingText, d("Font", missingRes), root)

	// Descendant rules: two Type0 fonts sharing one unembedded descendant object,
	// then one Type0 font written directly on two pages with its descendant in it.
	koreaDescriptor := descriptor("LithoKorea")

	koreaDescendant := func() *pdfgen.Dict {
		return d("Subtype", n("CIDFontType2"), "BaseFont", n("/LithoKorea"), "DW", 1000,
			"FontDescriptor", koreaDescriptor, "CIDSystemInfo", cidInfo("Korea1"))
	}

	koreaType0 := func(descendant *pdfgen.Dict) *pdfgen.Dict {
		return d("Type", n("Font"), "Subtype", n("Type0"), "BaseFont", n("/LithoKorea"),
			"Encoding", n("/UniJIS-UCS2-H"), "DescendantFonts", a(descendant))
	}

	const koreaText = "BT /K1 12 Tf 10 50 Td <00410042> Tj ET BT /K2 12 Tf 10 30 Td <00410042> Tj ET\n"
	sharedDesc := pdf.Indirect(koreaDescendant())
	page(koreaText, d("Font", d("K1", pdf.Indirect(koreaType0(sharedDesc)),
		"K2", pdf.Indirect(koreaType0(sharedDesc)))), root)
	page("BT /K1 12 Tf 10 50 Td <00410042> Tj ET\n", d("Font", d("K1", koreaType0(koreaDescendant()))), root)
	page("BT /K1 12 Tf 10 50 Td <00410042> Tj ET\n", d("Font", d("K1", koreaType0(koreaDescendant()))), root)

	// Deliberately no /Metadata and no output intent.
	return save(pdf, "1.7")
}
