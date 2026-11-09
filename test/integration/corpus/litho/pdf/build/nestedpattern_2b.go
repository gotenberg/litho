package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("nestedpattern_2b", nestedpattern2b) }

// nestedpattern2b builds nestedpattern_2b.pdf: tiling pattern names only the page defines.
//
// ISO 19005-2, 6.2.2 requires every content stream to have an explicitly
// associated resource dictionary, and test 2 fails a stream that resolves a name
// only through the page's. A tiling pattern's cell is a content stream with a
// /Resources entry of its own (ISO 32000-1, 8.7.3.1, Table 75), and so is a form
// XObject. veraPDF 1.30.2 fails test 2 on a name a cell finds only on the page,
// with the pattern's /Resources empty or absent alike, and on a form XObject whose
// `scn` selects a pattern only the page defines. No corpus file reaches either,
// and litho's remediation gave form XObjects and Type 3 fonts the names they
// borrow, never a pattern's cell nor a pattern a form paints.
//
// | Object | Construct                                            | Answers   |
// | ------ | ---------------------------------------------------- | --------- |
// | P0     | tiling pattern, `/Resources << >>`, `/GS0 gs`         | `6.2.2/2` |
// | P1     | tiling pattern, no `/Resources`, `/GS1 gs`            | `6.2.2/2` |
// | P2     | tiling pattern holding its own `/GS0`                | silent    |
// | X0     | form XObject, `/Resources << >>`, `/P3 scn`           | `6.2.2/2` |
// | X1     | form XObject holding its own `/P3`                   | silent    |
// | P3     | tiling pattern the two forms paint, naming nothing   | silent    |
//
// P2 and X1 are the over-repair controls: each already holds the name it uses,
// so a repair that rewrites either rewrites a conforming object. The page itself
// defines every name, so its own content fails nothing, and test 2 counts one
// failure for each of P0, P1 and X0.
//
// A CMYK OutputIntent covers the initial DeviceGray every cell paints in, and
// the file draws no text, so no colour or font clause fires beside test 2.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func nestedpattern2b() ([]byte, error) {
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

	gs0 := pdf.Indirect(d("Type", n("ExtGState"), "CA", 1.0))
	gs1 := pdf.Indirect(d("Type", n("ExtGState"), "ca", 1.0))

	// tilingPattern is one coloured tiling pattern; resources nil states no
	// /Resources.
	tilingPattern := func(content string, resources *pdfgen.Dict) *pdfgen.Stream {
		pat := pdf.Stream([]byte(content))
		pat.Set("Type", n("Pattern"))
		pat.Set("PatternType", 1)
		pat.Set("PaintType", 1)
		pat.Set("TilingType", 1)
		pat.Set("BBox", a(0, 0, 10, 10))
		pat.Set("XStep", 10)
		pat.Set("YStep", 10)
		if resources != nil {
			pat.Set("Resources", resources)
		}
		pdf.MakeIndirect(pat)
		return pat
	}

	// form is one form XObject carrying the resources given.
	form := func(content string, resources *pdfgen.Dict) *pdfgen.Stream {
		xo := pdf.Stream([]byte(content))
		xo.Set("Type", n("XObject"))
		xo.Set("Subtype", n("Form"))
		xo.Set("BBox", a(0, 0, 50, 50))
		xo.Set("Resources", resources)
		pdf.MakeIndirect(xo)
		return xo
	}

	p3 := tilingPattern("0 0 5 5 re f\n", d())
	patterns := d(
		"P0", tilingPattern("/GS0 gs 0 0 5 5 re f\n", d()),
		"P1", tilingPattern("/GS1 gs 0 0 5 5 re f\n", nil),
		"P2", tilingPattern("/GS0 gs 0 0 5 5 re f\n", d("ExtGState", d("GS0", gs0))),
		"P3", p3,
	)
	xobjects := d(
		"X0", form("/Pattern cs /P3 scn 0 0 50 50 re f\n", d()),
		"X1", form("/Pattern cs /P3 scn 0 0 50 50 re f\n", d("Pattern", d("P3", p3))),
	)

	content := pdf.Stream([]byte(
		"q /Pattern cs /P0 scn 0 700 50 50 re f\n" +
			"/P1 scn 60 700 50 50 re f\n" +
			"/P2 scn 120 700 50 50 re f Q\n" +
			"q 1 0 0 1 0 600 cm /X0 Do Q\n" +
			"q 1 0 0 1 60 600 cm /X1 Do Q\n",
	))
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"ExtGState", d("GS0", gs0, "GS1", gs1),
			"Pattern", patterns,
			"XObject", xobjects,
		),
		"Contents", pdf.MakeIndirect(content),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
