package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("patternglyph_2b", patternglyph2b) }

// patternglyph2b builds patternglyph_2b.pdf: Type 3 glyphs a tiling pattern's cell shows.
//
// A tiling pattern's cell is a content stream (ISO 32000-1, 8.7.3.1), so it can
// show text in a Type 3 font, whose glyph descriptions are content streams in
// turn (9.6.5) and can paint images, show text in other fonts, select colours and
// select patterns of their own. veraPDF 1.30.2 judges what such a glyph draws
// once an scn selects the pattern and text is shown with the font. No corpus file
// shows Type 3 text in a pattern cell, so this fixture plants one path per page,
// each answering a rule nothing else in the file fires:
//
// | Page | Path                                                    | Glyph draws                     | Answers at PDF/A-2b |
// | ---- | ------------------------------------------------------- | ------------------------------- | ------------------- |
// | 1    | tiling cell, Type 3 glyph                               | an image with `/Interpolate true` | `6.2.8/3`           |
// | 2    | tiling cell, Type 3 glyph                               | text in an unembedded TrueType  | `6.2.11.4.1/1`      |
// | 3    | tiling cell, Type 3 glyph, tiling cell, Type 3 glyph    | a DeviceRGB fill                | `6.2.4.3/2`         |
// | 4    | tiling cell selecting a Type 3 font with Tf, no text    | an image with `/Intent /Bogus`  | silent              |
//
// Page 4 is the must-not-fire control: veraPDF reaches a Type 3 glyph through
// the text that shows it, never through a Tf alone, so `6.2.6/1` stays silent.
//
// A CMYK OutputIntent covers the DeviceGray every cell and glyph shows text in.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func patternglyph2b() ([]byte, error) {
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

	// grayImage is a 2x2 DeviceGray image, which the CMYK OutputIntent covers.
	grayImage := func(extra ...any) *pdfgen.Stream {
		img := pdf.Stream(zlibCompress([]byte{0, 85, 170, 255}))
		img.Set("Type", n("XObject"))
		img.Set("Subtype", n("Image"))
		img.Set("Width", 2)
		img.Set("Height", 2)
		img.Set("ColorSpace", n("DeviceGray"))
		img.Set("BitsPerComponent", 8)
		img.Set("Filter", n("FlateDecode"))
		for i := 0; i < len(extra); i += 2 {
			img.Set(extra[i].(string), extra[i+1])
		}
		pdf.MakeIndirect(img)
		return img
	}

	// tiling is a coloured tiling pattern carrying its own /Resources.
	tiling := func(content string, resources *pdfgen.Dict) *pdfgen.Stream {
		pat := pdf.Stream([]byte(content))
		pat.Set("Type", n("Pattern"))
		pat.Set("PatternType", 1)
		pat.Set("PaintType", 1)
		pat.Set("TilingType", 1)
		pat.Set("BBox", a(0, 0, 20, 20))
		pat.Set("XStep", 20)
		pat.Set("YStep", 20)
		pat.Set("Resources", resources)
		pdf.MakeIndirect(pat)
		return pat
	}

	// type3 is a one-glyph Type 3 font; the glyph sets its colour (d0).
	type3 := func(glyph string, resources *pdfgen.Dict) *pdfgen.Dict {
		proc := pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0\n" + glyph)))
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"FontBBox", a(0, 0, 1000, 1000),
			"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
			"CharProcs", d("A", proc),
			"Encoding", d("Type", n("Encoding"), "Differences", a(65, n("A"))),
			"FirstChar", 65,
			"LastChar", 65,
			"Widths", a(1000),
			"Resources", resources,
		))
	}

	const show = "q 0 g BT /T3 10 Tf 1 1 Td (A) Tj ET Q\n"

	glyph1 := type3(
		"q 1000 0 0 1000 0 0 cm /Im1 Do Q\n",
		d("XObject", d("Im1", grayImage("Interpolate", true))),
	)
	cell1 := tiling(show, d("Font", d("T3", glyph1)))

	glyph2 := type3(
		"BT /TT 12 Tf 0 0 Td (AB) Tj ET\n",
		d("Font", d("TT", pdf.Indirect(unembeddedTrueType("/Helvetica")))),
	)
	cell2 := tiling(show, d("Font", d("T3", glyph2)))

	innerGlyph := type3("1 0 0 rg 0 0 1000 1000 re f\n", d())
	innerCell := tiling(show, d("Font", d("T3", innerGlyph)))
	outerGlyph := type3(
		"q /Pattern cs /P0 scn 0 0 1000 1000 re f Q\n",
		d("Pattern", d("P0", innerCell)),
	)
	cell3 := tiling(show, d("Font", d("T3", outerGlyph)))

	glyph4 := type3(
		"q 1000 0 0 1000 0 0 cm /Im4 Do Q\n",
		d("XObject", d("Im4", grayImage("Intent", n("/Bogus")))),
	)
	cell4 := tiling("BT /T3 10 Tf ET 0 0 20 20 re f\n", d("Font", d("T3", glyph4)))

	addPage := func(cell *pdfgen.Stream) {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", d("Pattern", d("P1", cell)),
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte("q /Pattern cs /P1 scn 0 0 100 100 re f Q\n"))),
		))
		pdf.AddPage(page)
	}

	for _, cell := range []*pdfgen.Stream{cell1, cell2, cell3, cell4} {
		addPage(cell)
	}

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
