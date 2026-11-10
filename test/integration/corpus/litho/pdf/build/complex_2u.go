package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("complex_2u", complex2u) }

// complex2u builds complex_2u.pdf: PDF/A-2u, the Unicode angle on top of part-2 rules.
//
// Conformance level U adds one demand to level B: every glyph rendered in the
// file has to map to Unicode, and the mapping has to be a legal one. ISO
// 19005-2 6.2.11.7.2 splits that into an absent mapping and a mapping onto a
// forbidden code point, and the level-A rule 6.2.11.7.3 adds Private Use Area
// text with no /ActualText. All three sit here alongside the part-2 defects.
//
// Three must-not-fire controls guard against over-suppression:
//   - a font whose /ToUnicode maps its rendered code to a plain Latin letter,
//   - an unrendered code whose glyph name maps to nothing, which the
//     usage-scoped glyph rules must skip,
//   - a Private Use Area glyph wrapped in a marked-content /ActualText, which
//     6.2.11.7.3 exempts.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func complex2u() ([]byte, error) {
	pdf := pdfgen.New()

	// simpleFont builds a non-embedded Type1 font whose codes start at 65
	// ("A"), optionally with a /ToUnicode CMap. A nil encoding is
	// /WinAnsiEncoding, a nil tounicode leaves the key out.
	simpleFont := func(encoding, tounicode pdfgen.Object, last int, widths []any, base string) *pdfgen.Dict {
		const first = 65
		dd := d(
			"Type", n("Font"),
			"Subtype", n("Type1"),
			"BaseFont", n(base),
			"FirstChar", first,
			"LastChar", last,
			"Widths", a(widths...),
		)
		if encoding != nil {
			dd.Set("Encoding", encoding)
		} else {
			dd.Set("Encoding", n("WinAnsiEncoding"))
		}
		if tounicode != nil {
			dd.Set("ToUnicode", tounicode)
		}
		return pdf.Indirect(dd)
	}

	// FNoUni: code 65 is renamed to a glyph name that appears in no standard
	// encoding and in no Adobe Glyph List entry, and the font carries no
	// /ToUnicode. Its rendered glyph therefore maps to nothing (6.2.11.7.2 t1).
	// Code 66 gets the same treatment but is never rendered, so the usage-scoped
	// glyph rules must skip it: a firing on it would be a false positive.
	// The base font is deliberately not one of the standard 14: a standard-14
	// name is taken as proof that every glyph name resolves through the Adobe
	// Glyph List, which would make the /Differences array unreachable.
	fNoUni := simpleFont(
		d(
			"Type", n("Encoding"),
			"Differences", a(65, n("/lithoglyphone"), 66, n("/lithoglyphtwo")),
		),
		nil, 66, []any{722, 722}, "/LithoCustom",
	)

	// FZero maps its rendered code onto U+0000, which 6.2.11.7.2 t2 forbids
	// along with U+FEFF and U+FFFE.
	fZero := simpleFont(nil, toUnicodeCMap(pdf, map[int]int{65: 0x0000}), 65, []any{722}, "/Helvetica")

	// FPua maps its rendered code onto a Private Use Area value. Rendered bare it
	// fails the level-A rule 6.2.11.7.3; rendered inside a marked-content
	// /ActualText it is exempt, and the content stream does both.
	fPua := simpleFont(nil, toUnicodeCMap(pdf, map[int]int{65: 0xE000}), 65, []any{722}, "/Helvetica")

	// Control: a well-formed /ToUnicode onto a plain Latin letter. Must stay
	// silent while the three above fire.
	fOk := simpleFont(nil, toUnicodeCMap(pdf, map[int]int{65: 0x0041}), 65, []any{722}, "/Helvetica")

	tt := unembeddedTrueType()
	img := rgbImage(pdf, true)

	lzwForm := pdf.Stream(nil)
	lzwForm.Data = lzwEncode([]byte("0 0 1 rg 5 5 20 20 re f\n"), 1)
	lzwForm.Set("Filter", n("LZWDecode"))
	lzwForm.Set("Type", n("XObject"))
	lzwForm.Set("Subtype", n("Form"))
	lzwForm.Set("BBox", a(0, 0, 30, 30))
	lzwForm.Set("Resources", d())

	groupForm := pdf.Stream([]byte("0 0 1 rg 0 0 50 50 re f\n"),
		"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 50, 50),
		"Group", d("Type", n("Group"), "S", n("Transparency")), "Resources", d())

	psxobj := pdf.Stream([]byte("%!PS\n"), "Type", n("XObject"), "Subtype", n("PS"))

	content := `q
/BadIntent ri
1 0 0 rg
50 700 100 50 re f
0 0 0 1 k
50 660 100 30 re f
0.5 g
50 640 100 10 re f
BT /FNoUni 12 Tf 50 610 Td (A) Tj ET
BT /FZero 12 Tf 50 590 Td (A) Tj ET
BT /FPua 12 Tf 50 570 Td (A) Tj ET
/P <</ActualText (guarded)>> BDC
BT /FPua 12 Tf 150 570 Td (A) Tj ET
EMC
BT /FOk 12 Tf 50 550 Td (A) Tj ET
BT /TT1 12 Tf 50 530 Td (AB) Tj ET
q 100 0 0 60 50 440 cm /Im1 Do Q
q 1 0 0 1 50 360 cm /Fm1 Do Q
q 1 0 0 1 150 360 cm /FmLZW Do Q
q 1 0 0 1 250 360 cm /PSX Do Q
Q
`

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d(
				"FNoUni", fNoUni,
				"FZero", fZero,
				"FPua", fPua,
				"FOk", fOk,
				"TT1", pdf.Indirect(tt),
			),
			"XObject", d(
				"Im1", img,
				"Fm1", groupForm,
				"FmLZW", lzwForm,
				"PSX", pdf.MakeIndirect(psxobj),
			),
		),
		"Contents", pdf.Stream([]byte(content)),
	)
	pdf.AddPage(pdf.Indirect(page))

	movie := d(
		"Type", n("Annot"),
		"Subtype", n("Movie"),
		"Rect", a(400, 700, 500, 750),
		"F", 4,
		"T", text("clip"),
		"Movie", d("F", text("clip.mov")),
	)
	hidden := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 600, 500, 650),
		"F", 2,
		"C", a(0, 1, 0),
	)
	pdf.Page(0).Set("Annots", a(pdf.Indirect(movie), pdf.Indirect(hidden)))

	pdf.Root.Set("OpenAction", d(
		"Type", n("Action"), "S", n("JavaScript"), "JS", text("app.alert('litho');")))
	pdf.Root.Set("NeedsRendering", true)
	ocg := pdf.Indirect(d("Type", n("OCG"), "Name", text("layer")))
	pdf.Root.Set("OCProperties", d(
		"OCGs", a(ocg), "D", d("Order", a(), "AS", a())))

	// Deliberately no /Metadata on the catalog and no /OutputIntents.
	return save(pdf, "1.7")
}
