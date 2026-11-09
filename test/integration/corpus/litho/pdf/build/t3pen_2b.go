package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("t3pen_2b", t3pen2b) }

// t3pen2b builds t3pen_2b.pdf: Type 3 glyphs that stroke with the pen of the state showing them, at PDF/A-2b.
//
// A Type 3 glyph description runs under the graphics state of the text that
// shows it (ISO 32000-1, 9.6.5), so a stroke it paints before setting its own
// line width, cap, join or miter limit strokes with those in force where the
// glyph is shown (8.4.3.2 to 8.4.3.5). Redaction bounded each glyph once, when
// its font loaded, with the initial pen, so a glyph shown under a wide pen was
// bounded by its line alone, and a region over the pen and clear of the line
// selected nothing. Six corpus files hold such glyphs, none shown under a pen
// past the initial one.
//
// The page, 400 by 400, shows glyphs of font T3 at Tf 40, which maps glyph
// space by a thousandth to 0.04 point per unit:
//
// | Glyph | Description                        | Shown at | Under    | Pen reaches        |
// | ----- | ---------------------------------- | -------- | -------- | ------------------ |
// | `L`   | strokes a line at y 500            | 130 60   | `2000 w` | y 40 to 120        |
// | `K`   | sets `40 w`, strokes the same line | 230 60   | `2000 w` | y 79.2 to 80.8     |
// | `L`   | the same glyph                     | 130 320  | `1 w`    | y 339.98 to 340.02 |
//
// The centred quarter [100 100 300 300] meets 20 points of the first `L`'s
// pen, and neither its line nor the box its line gives it. `K` sets its own pen
// and lies clear of it, and so does the second `L`, under the initial pen.
//
// The font states /Resources of its own, and the RGB output intent covers
// DeviceGray, so the file is conforming at PDF/A-2b.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func t3pen2b() ([]byte, error) {
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

	procs := d(
		"K", pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0\n40 w 0 500 m 1000 500 l S\n"))),
		"L", pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0\n0 500 m 1000 500 l S\n"))),
	)
	type3 := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type3"),
		"FontBBox", a(0, 0, 1000, 1000),
		"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
		"CharProcs", procs,
		"Encoding", d("Type", n("Encoding"), "Differences", a(75, n("K"), n("L"))),
		"FirstChar", 75,
		"LastChar", 76,
		"Widths", a(1000, 1000),
		"Resources", d(),
		"ToUnicode", toUnicodeCMap(pdf, map[int]int{0x4B: 0x4B, 0x4C: 0x4C}),
	))

	content := "0 G 2000 w\n" +
		"BT /T3 40 Tf 1 0 0 1 130 60 Tm (L) Tj 1 0 0 1 230 60 Tm (K) Tj ET\n" +
		"1 w\n" +
		"BT /T3 40 Tf 1 0 0 1 130 320 Tm (L) Tj ET\n"
	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 400, 400),
		"Resources", d("Font", d("T3", type3)),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
	)
	pdf.AddPage(pdf.Indirect(page))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
