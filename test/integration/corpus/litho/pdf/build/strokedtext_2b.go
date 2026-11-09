package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("strokedtext_2b", strokedtext2b) }

// strokedtext2b builds strokedtext_2b.pdf: text whose pen paints past its outline, at PDF/A-2b.
//
// A stroking text rendering mode (ISO 32000-1, 9.3.6, Table 106: modes 1, 2,
// 5 and 6) strokes each glyph's outline with the graphics state's line width,
// join and miter limit (8.4.3.2 to 8.4.3.5), so a wide pen paints past the box
// the glyph's outline is bounded by. Redaction once bounded a glyph by that
// box alone: a region over the stroke and clear of the outline selected
// nothing, and the stroke stayed under it. No corpus file the redact-render
// cell samples strokes text wide enough to reach a region the outline does
// not.
//
// The page, 400 by 400, shows one embedded CFF font (`cff.go`: A and B are
// rectangles 400 units wide, 400 and 500 high, advancing 600):
//
// | Run    | At       | Tf | Mode | Pen                        | Reaches past its outline |
// | ------ | -------- | -- | ---- | -------------------------- | ------------------------ |
// | `A`    | 55 170   | 50 | 1    | 60 wide, round joins       | 30 points                |
// | `B`    | 318 170  | 50 | 2    | 4 wide, miter joins, M 10  | 2.8 points at its corners|
// | `ABAB` | 130 195  | 20 | 2    | 0.5 wide, the fake bold    | 0.35 at its corners      |
//
// The centred quarter [100 100 300 300] meets the stroke of `A`, 7.5 points
// of it, and neither its outline nor its box without the stroke. A glyph's box
// grows by half its pen, so `B`'s grows by 2 points, to 16 points clear of
// the quarter, which keeps it: its square corners reach 2.8 points past its
// outline, inside the 2.5 points of side bearing its box holds. `ABAB` lies
// inside the quarter and under the band.
//
// The RGB output intent covers DeviceGray, and the font is embedded, so the
// file is conforming at PDF/A-2b.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func strokedtext2b() ([]byte, error) {
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

	program := pdf.Stream(cffBuild())
	program.Set("Subtype", n("/Type1C"))
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/LithoCFF"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(600, 600),
		"Encoding", n("WinAnsiEncoding"),
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/LithoCFF"),
			"Flags", 32,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile3", pdf.MakeIndirect(program),
		)),
	))

	content := "0 g 0 G\n" +
		"q 60 w 1 j BT /F1 50 Tf 1 Tr 1 0 0 1 55 170 Tm (A) Tj ET Q\n" +
		"q 4 w 0 j 10 M BT /F1 50 Tf 2 Tr 1 0 0 1 318 170 Tm (B) Tj ET Q\n" +
		"q 0.5 w BT /F1 20 Tf 2 Tr 1 0 0 1 130 195 Tm (ABAB) Tj ET Q\n"
	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 400, 400),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
	)
	pdf.AddPage(pdf.Indirect(page))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
