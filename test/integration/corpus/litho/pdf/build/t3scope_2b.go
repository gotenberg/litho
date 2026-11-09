package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("t3scope_2b", t3scope2b) }

// t3scope2b builds t3scope_2b.pdf: Type 3 glyphs that paint through the resources of the stream showing them, at PDF/A-2b.
//
// A Type 3 font's /Resources is optional (ISO 32000-1, 9.6.5, Table 112): a
// glyph description without them takes the resources of the stream that shows
// the glyph, a PDF 1.1 practice viewers follow. Redaction bounded such a glyph
// once, when its font loaded, without those resources, so a glyph that shows
// text or paints a form named only by the page was bounded by the whole page,
// and any region on the page removed every glyph of the font. pdfjs pr4922 is
// the one corpus file that reaches it.
//
// The page, 400 by 400, shows nine glyphs of font T3, which states no
// /Resources, at Tf 40 from 20 200, and again from 20 20 inside form Fm:
//
// | Glyph  | Description              | Through the page                    | Through form Fm                    |
// | ------ | ------------------------ | ----------------------------------- | ---------------------------------- |
// | T3 `A` | shows `A` in font F1     | an embedded TrueType rectangle      | the same font                      |
// | T3 `B` | paints the form `Bar`    | a bar 32 by 28 at its origin        | a bar 32 by 60, Fm's own `Bar`     |
//
// The centred quarter [100 100 300 300] meets the page's glyphs at 100 to 300;
// the page's glyphs at 20, 60 and 340 and every glyph Fm shows, its bars
// reaching 80, lie clear of it. Bounded by the page, every glyph met it, and
// the glyphs Fm draws refused the redaction.
//
// The RGB output intent covers DeviceGray, so the file is conforming at
// PDF/A-2b.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func t3scope2b() ([]byte, error) {
	pdf := pdfgen.New()

	destination := pdf.Stream(iccRGBDisplayProfile())
	destination.Set("N", 3)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho RGB display"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	const symbolic = 4 // ISO 32000-1, Table 123
	program := ttBuild([]ttSubtable{{3, 0, map[int]string{0xF041: "A", 0xF042: "B"}}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	truetype := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoRect"),
		"FirstChar", 0x41,
		"LastChar", 0x42,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/LithoRect"),
			"Flags", symbolic,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile2", pdf.MakeIndirect(fontFile),
		)),
		"Encoding", d("Type", n("Encoding"), "Differences", a(0x41, n("A"), n("B"))),
		"ToUnicode", toUnicodeCMap(pdf, map[int]int{0x41: 0x41, 0x42: 0x42}),
	))

	// bar is a form filling a bar 800 wide and height high in glyph space.
	bar := func(height int) obj {
		form := pdf.Stream([]byte(fmt.Sprintf("0 g 100 0 800 %d re f\n", height)))
		form.Set("Type", n("XObject"))
		form.Set("Subtype", n("Form"))
		form.Set("BBox", a(100, 0, 900, height))
		form.Set("Resources", d())
		return pdf.MakeIndirect(form)
	}

	procs := d(
		"A", pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0\nBT /F1 1000 Tf (A) Tj ET\n"))),
		"B", pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0\n/Bar Do\n"))),
	)
	type3 := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type3"),
		"FontBBox", a(0, 0, 1000, 1000),
		"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
		"CharProcs", procs,
		"Encoding", d("Type", n("Encoding"), "Differences", a(65, n("A"), n("B"))),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(1000, 1000),
		"ToUnicode", toUnicodeCMap(pdf, map[int]int{0x41: 0x41, 0x42: 0x42}),
	))

	// The form shows other text: the page text a region removes must not survive in it.
	form := pdf.Stream([]byte("0 g BT /T3 40 Tf 1 0 0 1 20 20 Tm (BBBAAABBB) Tj ET\n"))
	form.Set("Type", n("XObject"))
	form.Set("Subtype", n("Form"))
	form.Set("BBox", a(0, 0, 400, 400))
	form.Set("Resources", d("Font", d("T3", type3, "F1", truetype), "XObject", d("Bar", bar(1500))))

	content := "0 g BT /T3 40 Tf 1 0 0 1 20 200 Tm (ABABABABA) Tj ET\n/Fm Do\n"
	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 400, 400),
		"Resources", d(
			"Font", d("T3", type3, "F1", truetype),
			"XObject", d("Bar", bar(700), "Fm", pdf.MakeIndirect(form)),
		),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
	)
	pdf.AddPage(pdf.Indirect(page))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
