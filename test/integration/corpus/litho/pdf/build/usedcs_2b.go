package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("usedcs_2b", usedcs2b) }

// usedcs2b builds usedcs_2b.pdf: which resources count as colour usage at PDF/A-2b.
//
// ISO 19005-2, 6.2.4.3 is usage-driven: a colour space is constrained when content
// paints with it, not when a resource dictionary merely declares it. Three resource
// categories carry their own colour and each is reached by its own operator, so
// each needs its own usage gate:
//
// | Resource            | Painted by            | Declared-only means |
// | ------------------- | --------------------- | ------------------- |
// | `/Pattern` entry    | a name operand of `scn` | paints nothing    |
// | `/Shading` entry    | `sh`                  | paints nothing      |
// | `/Font` Type3 entry | selected by `Tf`      | draws nothing       |
//
// A fourth construct is not about usage at all. An uncoloured tiling pattern
// (`/PaintType 2`) shall not specify any colour in its content stream: the colour
// comes from the operands of the `scn` that selects it, interpreted in the
// underlying space of `[/Pattern base]` (ISO 32000-1 §8.7.3.3, Table 75). The
// initial DeviceGray of §8.6.8 therefore never applies inside the cell, so a bare
// painting operator there is not a DeviceGray use. Measured: veraPDF reports
// nothing for such a cell, not even for an explicit device colour written into it.
//
// Seven objects sit in one file. Only `Sh1` and `T3Used` may report, which is the
// masking probe: five of the seven are controls, and each is the kind of object a
// naive walk condemns.
//
// | Object   | Construct                                          | Rule        |
// | -------- | -------------------------------------------------- | ----------- |
// | `Sh1`    | shading painted by `sh`, `/ColorSpace /DeviceGray`  | `6.2.4.3/4` |
// | `T3Used` | Type3 font selected by `Tf`, CharProcs paint `0 g`   | `6.2.4.3/4` |
// | `Sh0`    | DeviceCMYK shading, declared but never painted       | silent      |
// | `T3Unused` | Type3 font declared but never selected            | silent      |
// | `PUnc`   | uncoloured pattern, explicit `0 0 0 1 k` in its cell | silent      |
// | `PUnref` | coloured pattern, DeviceCMYK, never named by `scn`  | silent      |
// | `PCol`   | coloured pattern selecting its own ICCBased space   | silent      |
//
// Every declared-only object paints DeviceCMYK rather than DeviceGray on purpose,
// and the two painted ones paint DeviceGray. Without that split the file proves
// nothing: `Sh1` and `T3Used` legitimately report test 4, so an implementation that
// also scans `Sh0`, `T3Unused`, `PUnc` and `PUnref` still reports test 4 and agrees
// with veraPDF for the wrong reasons. Measured on the first build, and fixed by
// moving every ungated source to test 3, which nothing conforming produces.
//
// The file declares no OutputIntent, which is what keeps test 4 reachable: the test
// is `gOutputCS != null`, so an OutputIntent of any destination colour space
// permits DeviceGray outright. The page paints its own text inside an ICCBased
// scope, because text drawn with no colour space selected is itself a DeviceGray
// use and would report the clause from the page, masking every object above.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func usedcs2b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccVersion21 = 0x02100000

	// iccProfile is a structurally minimal ICC profile veraPDF will parse
	// without throwing.
	iccProfile := func(colourSpace string, components int, tag string) *pdfgen.Stream {
		stream := pdf.Stream(iccMinimalProfile("mntr", colourSpace, iccVersion21, tag))
		stream.Set("N", components)
		return pdf.MakeIndirect(stream).(*pdfgen.Stream)
	}

	iccbased := pdf.MakeIndirect(a(n("ICCBased"), iccProfile("RGB ", 3, "ICCB")))

	// shading is an axial shading in the given device colour space. Python's
	// defaults are DeviceGray and 1 component.
	shading := func(colourSpace pdfgen.Name, components int) *pdfgen.Dict {
		c0, c1 := a(), a()
		for i := 0; i < components; i++ {
			c0.Append(0)
			c1.Append(1)
		}
		return pdf.Indirect(d(
			"ShadingType", 2,
			"ColorSpace", colourSpace,
			"Coords", a(0, 0, 100, 0),
			"Function", pdf.Indirect(d(
				"FunctionType", 2,
				"Domain", a(0, 1),
				"C0", c0,
				"C1", c1,
				"N", 1,
			)),
		))
	}

	// tiling's Python default paintType is 1.
	tiling := func(content string, paintType int) *pdfgen.Stream {
		pat := pdf.Stream([]byte(content))
		pat.Set("Type", n("Pattern"))
		pat.Set("PatternType", 1)
		pat.Set("PaintType", paintType)
		pat.Set("TilingType", 1)
		pat.Set("BBox", a(0, 0, 20, 20))
		pat.Set("XStep", 20)
		pat.Set("YStep", 20)
		pat.Set("Resources", d("ColorSpace", d("CS0", iccbased)))
		return pdf.MakeIndirect(pat).(*pdfgen.Stream)
	}

	// type3Font is a Type3 font whose only glyph procedure paints a device
	// colour. Python's default paint is "0 g".
	type3Font := func(paint string) *pdfgen.Dict {
		proc := pdf.Stream([]byte("10 0 0 0 10 10 d1 " + paint + " 0 0 10 10 re f\n"))
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"FontBBox", a(0, 0, 10, 10),
			"FontMatrix", a(0.1, 0, 0, 0.1, 0, 0),
			"CharProcs", d("a", pdf.MakeIndirect(proc)),
			"Encoding", d(
				"Type", n("Encoding"), "Differences", a(97, n("/a")),
			),
			"FirstChar", 97,
			"LastChar", 97,
			"Widths", a(10),
			"Resources", d(),
		))
	}

	helvetica := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// The underlying space an uncoloured pattern is selected through.
	uncolouredCS := pdf.MakeIndirect(a(n("Pattern"), n("DeviceRGB")))

	content := "q /CS0 cs 0.2 0.4 0.6 scn\n" +
		// The page text sits inside the ICCBased scope so the page adds no DeviceGray.
		"  BT /F1 12 Tf 50 740 Td (resource usage and colour) Tj ET\n" +
		"Q\n" +
		// Sh1 is painted; Sh0 is not named anywhere.
		"q 1 0 0 1 60 620 cm /Sh1 sh Q\n" +
		// T3Used is selected by Tf; T3Unused is never selected.
		"q BT /T3Used 12 Tf 60 560 Td (a) Tj ET Q\n" +
		// An uncoloured pattern, selected through [/Pattern /DeviceRGB]. Its cell
		// writes an explicit device colour, which the spec forbids and veraPDF
		// ignores: the colour comes from the scn operands, not from the cell.
		"q /PUCS cs 0.2 0.4 0.6 /PUnc scn 60 480 100 40 re f Q\n" +
		// A coloured pattern selecting its own permitted space.
		"q /Pattern cs /PCol scn 200 480 100 40 re f Q\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d(
				"F1", helvetica,
				"T3Used", type3Font("0 g"),
				// Declared but never selected by any Tf. It paints DeviceCMYK
				// so that scanning it by mistake reports test 3, not test 4.
				"T3Unused", type3Font("0 0 0 1 k"),
			),
			"ColorSpace", d("CS0", iccbased, "PUCS", uncolouredCS),
			"Shading", d(
				// Painted by the sh above.
				"Sh1", shading(n("DeviceGray"), 1),
				// Declared but never painted. DeviceCMYK so that scanning it by
				// mistake reports test 3, not test 4.
				"Sh0", shading(n("DeviceCMYK"), 4),
			),
			"Pattern", d(
				"PUnc", tiling("0 0 0 1 k 0 0 20 20 re f\n", 2),
				"PCol", tiling("/CS0 cs 0.2 0.4 0.6 scn 0 0 20 20 re f\n", 1),
				// Declared but never named by any scn. DeviceCMYK so that
				// scanning it by mistake reports test 3, not test 4.
				"PUnref", tiling("0 0 0 1 k 0 0 20 20 re f\n", 1),
			),
		),
		"Contents", pdf.Stream([]byte(content)),
	))
	pdf.AddPage(page)

	// Deliberately no /Metadata.
	return save(pdf, "1.7")
}
