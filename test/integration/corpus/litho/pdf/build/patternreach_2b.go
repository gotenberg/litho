package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("patternreach_2b", patternreach2b) }

// patternreach2b builds patternreach_2b.pdf: DeviceRGB a pattern reaches, one path per page.
//
// A pattern reaches a page through an scn or SCN that selects it (ISO 32000-1,
// 8.7.3.1 and 8.7.4.1), from the page's content, a form XObject, an annotation
// appearance, a Type 3 glyph or another pattern's cell, and a shading reaches it
// through a shading pattern or through sh (8.7.4.2). veraPDF 1.30.2 judges the
// colour space of each such shading and of each colour operator in a cell
// (PDShadingPattern/shading/colorSpace, PDTilingPattern/contentStream), and
// judges a stream once however many paths reach it. No corpus file isolates
// these paths: the veraPDF corpus reaches a cell only from a page's content and
// reaches no shading pattern, and the litho fixtures before this one reach a
// shading pattern only from a page, a cell or a glyph. This fixture plants one path
// per page, each painting DeviceRGB, which ISO 19005-2, 6.2.4.3 t2 answers under
// the CMYK OutputIntent:
//
// | Page | Path                                                         | `6.2.4.3/2` |
// | ---- | ------------------------------------------------------------ | ----------- |
// | 1    | `SCN` strokes with a shading pattern                         | 1           |
// | 2    | a form XObject selects a shading pattern                     | 1           |
// | 3    | an annotation appearance selects a shading pattern           | 1           |
// | 4    | text in a `d1` Type 3 font filled with a shading pattern     | 1           |
// | 5    | a tiling cell paints a form XObject that runs `sh`           | 1           |
// | 6    | a shading pattern over a type 4 mesh shading                 | 1           |
// | 7    | a shading pattern over an `/Indexed` space on DeviceRGB      | 1           |
// | 8    | a direct shading pattern holding a direct shading            | 2           |
// | 9    | a tiling cell that selects itself                            | 2           |
// | 10   | two tiling cells that select each other                      | 4           |
// | 11   | a Type 3 glyph whose pattern cell shows the same font        | 2           |
// | 12   | one shading pattern on the page and in a cell it paints      | 1           |
// | 13   | one /Pattern dictionary two cells share                      | 1           |
//
// A cycle ends where it reaches a stream already judged (pages 9 to 11), and a
// shading pattern or a /Pattern dictionary reached twice counts once (pages 12
// and 13). Page 8's pattern and shading are direct objects, so the `scn` and the
// fill that follows it are each a link to the shading's colour space.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func patternreach2b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccVersion21 = 0x02100000

	// iccProfile is a structurally minimal ICC profile, as
	// nestedpattern_2b.go builds it.
	iccProfile := func(deviceClass, colourSpace, tag string) []byte {
		return iccMinimalProfile(deviceClass, colourSpace, iccVersion21, tag)
	}

	destination := pdf.Stream(iccProfile("prtr", "CMYK", "OUTI"))
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho destination"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	rgb := n("DeviceRGB")
	const sel = "q /Pattern cs /S scn 0 0 20 20 re f Q\n"
	const selectCell = "q /Pattern cs /P scn 0 0 100 100 re f Q\n"

	// exponential is a type 2 function from one input to components outputs.
	exponential := func(components int) *pdfgen.Dict {
		c0, c1 := a(), a()
		for i := 0; i < components; i++ {
			c0.Append(0)
			c1.Append(1)
		}
		return d(
			"FunctionType", 2,
			"Domain", a(0, 1),
			"C0", c0,
			"C1", c1,
			"N", 1,
		)
	}

	// axial is an axial shading over colourSpace. Python's default
	// components is 3.
	axial := func(colourSpace pdfgen.Object, components int) *pdfgen.Dict {
		return pdf.Indirect(d(
			"ShadingType", 2,
			"ColorSpace", colourSpace,
			"Coords", a(0, 0, 20, 0),
			"Function", exponential(components),
		))
	}

	shadingPattern := func(shading pdfgen.Object) *pdfgen.Dict {
		return pdf.Indirect(d("Type", n("Pattern"), "PatternType", 2, "Shading", shading))
	}

	// rgbPatterns is resources naming /S, a shading pattern over a DeviceRGB
	// shading.
	rgbPatterns := func() *pdfgen.Dict {
		return d("Pattern", d("S", shadingPattern(axial(rgb, 3))))
	}

	form := func(content string, resources *pdfgen.Dict) *pdfgen.Stream {
		xobj := pdf.Stream([]byte(content))
		xobj.Set("Type", n("XObject"))
		xobj.Set("Subtype", n("Form"))
		xobj.Set("BBox", a(0, 0, 100, 100))
		xobj.Set("Resources", resources)
		return pdf.MakeIndirect(xobj).(*pdfgen.Stream)
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
		return pdf.MakeIndirect(pat).(*pdfgen.Stream)
	}

	// type3 is a one-glyph Type 3 font. Python's default metrics is
	// "1000 0 d0\n".
	type3 := func(glyph string, resources *pdfgen.Dict, metrics string) *pdfgen.Dict {
		proc := pdf.MakeIndirect(pdf.Stream([]byte(metrics + glyph)))
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

	annotation := func(appearance *pdfgen.Stream) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("Square"),
			"Rect", a(0, 0, 100, 100),
			"F", 4,
			"AP", d("N", appearance),
		))
	}

	addPage := func(content string, resources *pdfgen.Dict, annots *pdfgen.Array) {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", resources,
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
		)
		if annots != nil {
			page.Set("Annots", annots)
		}
		pdf.AddPage(pdf.Indirect(page))
	}

	// 1: a stroke.
	addPage("q /Pattern CS /S SCN 0 0 50 50 re S Q\n", rgbPatterns(), nil)

	// 2: a form XObject.
	addPage("q /X Do Q\n", d("XObject", d("X", form(sel, rgbPatterns()))), nil)

	// 3: an annotation appearance.
	addPage("q 0 g 0 0 1 1 re f Q\n", d(), a(annotation(form(sel, rgbPatterns()))))

	// 4: text filled with the pattern, in a font whose glyph takes its colour (d1).
	page4 := rgbPatterns()
	page4.Set("Font", d("F", type3("0 0 1000 1000 re f\n", d(), "1000 0 0 0 1000 1000 d1\n")))
	addPage("q /Pattern cs /S scn BT /F 50 Tf 10 10 Td (A) Tj ET Q\n", page4, nil)

	// 5: sh in a form a cell paints.
	shForm := form("q /Sh sh Q\n", d("Shading", d("Sh", axial(rgb, 3))))
	addPage(selectCell, d("Pattern", d("P", tiling("q /X Do Q\n", d("XObject", d("X", shForm))))), nil)

	// 6: a free-form triangle mesh, three vertices of flag, x, y and r g b.
	mesh := pdf.Stream(
		[]byte{0, 0, 0, 255, 0, 0, 0, 255, 0, 0, 255, 0, 0, 0, 255, 0, 0, 255, 255, 0, 0},
	)
	mesh.Set("ShadingType", 4)
	mesh.Set("ColorSpace", rgb)
	mesh.Set("BitsPerCoordinate", 8)
	mesh.Set("BitsPerComponent", 8)
	mesh.Set("BitsPerFlag", 8)
	mesh.Set("Decode", a(0, 255, 0, 255, 0, 1, 0, 1, 0, 1))
	addPage(sel, d("Pattern", d("S", shadingPattern(pdf.MakeIndirect(mesh)))), nil)

	// 7: /Indexed over DeviceRGB.
	indexed := a(n("Indexed"), rgb, 1, str("\x00\x00\x00\xff\xff\xff"))
	addPage(sel, d("Pattern", d("S", shadingPattern(axial(indexed, 1)))), nil)

	// 8: a direct shading pattern over a direct shading.
	direct := d(
		"Type", n("Pattern"),
		"PatternType", 2,
		"Shading", d("ShadingType", 2, "ColorSpace", rgb, "Coords", a(0, 0, 20, 0), "Function", exponential(3)),
	)
	addPage(sel, d("Pattern", d("S", direct)), nil)

	// 9: a cell that selects itself.
	selfCell := tiling("q 1 0 0 rg 0 0 5 5 re f /Pattern cs /P scn 0 0 20 20 re f Q\n", d())
	selfCell.Set("Resources", d("Pattern", d("P", selfCell)))
	addPage(selectCell, d("Pattern", d("P", selfCell)), nil)

	// 10: two cells that select each other.
	cellA := tiling("q 1 0 0 rg 0 0 5 5 re f /Pattern cs /Q scn 0 0 20 20 re f Q\n", d())
	cellB := tiling("q 0 1 0 rg 0 0 5 5 re f /Pattern cs /P scn 0 0 20 20 re f Q\n", d())
	cellA.Set("Resources", d("Pattern", d("Q", cellB)))
	cellB.Set("Resources", d("Pattern", d("P", cellA)))
	addPage(selectCell, d("Pattern", d("P", cellA)), nil)

	// 11: a glyph whose cell shows the glyph's own font.
	cycleFont := type3("q 1 0 0 rg 0 0 100 100 re f /Pattern cs /P scn 0 0 1000 1000 re f Q\n", d(), "1000 0 d0\n")
	cycleCell := tiling("q 0 g BT /T 10 Tf 1 1 Td (A) Tj ET Q\n", d("Font", d("T", cycleFont)))
	cycleFont.Set("Resources", d("Pattern", d("P", cycleCell)))
	addPage("q 0 g BT /T 50 Tf 10 10 Td (A) Tj ET Q\n", d("Font", d("T", cycleFont)), nil)

	// 12: one shading pattern on the page and in a cell the page paints.
	sharedPattern := shadingPattern(axial(rgb, 3))
	sharedCell := tiling(sel, d("Pattern", d("S", sharedPattern)))
	addPage(
		"q /Pattern cs /S scn 0 0 20 20 re f /P scn 0 0 100 100 re f Q\n",
		d("Pattern", d("S", sharedPattern, "P", sharedCell)),
		nil,
	)

	// 13: one /Pattern dictionary two cells share.
	sharedDict := pdf.Indirect(d("S", shadingPattern(axial(rgb, 3))))
	addPage(
		"q /Pattern cs /A scn 0 0 50 50 re f /B scn 50 50 50 50 re f Q\n",
		d(
			"Pattern", d(
				"A", tiling(sel, d("Pattern", sharedDict)),
				"B", tiling(sel, d("Pattern", sharedDict)),
			),
		),
		nil,
	)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
