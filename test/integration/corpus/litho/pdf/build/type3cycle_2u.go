package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("type3cycle_2u", type3cycle2u) }

// type3cycle2u builds type3cycle_2u.pdf: a Type 3 font shown again in a cell its own glyph selects.
//
// A Type 3 glyph procedure is a content stream (ISO 32000-1, 9.6.5) and can
// select a tiling pattern whose cell (8.7.3.1) shows text in the same font.
// veraPDF 1.30.2 judges the glyphs the cell shows as any other, and tells them
// apart from the page's by the text rendering mode the cell starts in, the mode
// the selecting procedure starts in. No corpus file the gate validates reaches
// the shape, so this fixture plants it with the cell's glyph as the only one
// without a Unicode value:
//
// | Stream                         | Shows                                   | Unicode value      |
// | ------------------------------ | --------------------------------------- | ------------------ |
// | page                           | code A, glyph `A`, rendering mode 0     | `A`, from the AGL  |
// | procedure of `A`               | selects the cell's pattern and fills    |                    |
// | cell                           | code B, glyph `r1`, of the same font    | none               |
//
// ISO 19005-2, 6.2.11.7.2 t1 (6.3.8 t1 at PDF/A-1a, ISO 14289-1, 7.21.7 t1 and
// ISO 14289-2, 8.4.5.8 t1 at PDF/UA) fails once, for `r1`; litho once judged
// no glyph of a Type 3 font in a stream its own procedures reach, which left the
// rule silent outside PDF/A-1a.
//
// A CMYK OutputIntent covers the DeviceGray every stream paints in.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func type3cycle2u() ([]byte, error) {
	pdf := pdfgen.New()

	destination := pdf.Stream(iccMinimalProfile("prtr", "CMYK", iccVersion21, "OUTI"))
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho destination"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	glyphA := pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0\n/Pattern cs /P scn 0 0 1000 1000 re f\n")))
	glyphR1 := pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0\n0 0 1000 1000 re f\n")))
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type3"),
		"FontBBox", a(0, 0, 1000, 1000),
		"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
		"CharProcs", d("A", glyphA, "r1", glyphR1),
		"Encoding", d("Type", n("Encoding"), "Differences", a(65, n("A"), n("/r1"))),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(1000, 1000),
	))

	cell := pdf.Stream([]byte("BT /T 4 Tf (B) Tj ET\n"))
	cell.Set("Type", n("Pattern"))
	cell.Set("PatternType", 1)
	cell.Set("PaintType", 1)
	cell.Set("TilingType", 1)
	cell.Set("BBox", a(0, 0, 20, 20))
	cell.Set("XStep", 20)
	cell.Set("YStep", 20)
	cell.Set("Resources", d("Font", d("T", font)))
	font.Set("Resources", d("Pattern", d("P", pdf.MakeIndirect(cell))))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d("Font", d("T", font)),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte("BT /T 10 Tf 10 10 Td (A) Tj ET\n"))),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "U"}))
	return save(pdf, "1.7")
}
