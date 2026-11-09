package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("shadedefault_2b", shadedefault2b) }

// shadedefault2b builds shadedefault_2b.pdf: shading patterns over DeviceCMYK beside DeviceRGB.
//
// A Default colour space replaces a device family the content stream selects
// through its own resources (ISO 32000-1, 8.6.5.6), and a shading pattern has no
// resources of its own (8.7.4.1, Table 76). veraPDF 1.30.2 therefore judges the
// device family of a shading pattern's shading against the output intent alone.
// The sole intent here is a CMYK one, which covers every DeviceCMYK, and page 1
// paints DeviceRGB, which it does not. Remediation covers DeviceRGB by replacing
// the intent with an sRGB one and gives DeviceCMYK a /DefaultCMYK, which reaches
// none of these shadings:
//
// | Page | Path                                     | Shading colour space                                |
// | ---- | ---------------------------------------- | --------------------------------------------------- |
// | 1    | `rg` fill, `/P1 scn`                     | Separation `/Spot` over DeviceCMYK, both indirect   |
// | 2    | `/P2 scn`, pattern and shading direct    | DeviceCMYK                                          |
// | 3    | `/P3 scn`                                | DeviceN `/Spot2` over DeviceCMYK, `/Colorants` too  |
// | 4    | `/CS0 cs`, `k` fill, `/P1 scn`           | the page's `/CS0` is page 1's Separation `/Spot`    |
// | 5    | a form whose own resources hold `/P5`    | Separation `/Spot` over DeviceCMYK                  |
//
// Page 4 selects the colorant of page 1's shading through a /ColorSpace entry,
// which a /DefaultCMYK reaches, so the remediated output must give both the same
// alternate (ISO 19005-2, 6.2.4.4 t2).
//
// See ../FIXTURES.md for the authoritative expected firing set.
func shadedefault2b() ([]byte, error) {
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

	tint := pdf.Indirect(d("FunctionType", 2, "Domain", a(0, 1), "C0", a(0, 0, 0, 0), "C1", a(0, 1, 1, 0), "N", 1))
	spot := pdf.MakeIndirect(a(n("Separation"), n("/Spot"), n("DeviceCMYK"), tint))

	// ramp is a one-in exponential function giving components outputs.
	ramp := func(components int) *pdfgen.Dict {
		c0, c1 := a(), a()
		for range components {
			c0.Append(0)
			c1.Append(1)
		}
		return d("FunctionType", 2, "Domain", a(0, 1), "C0", c0, "C1", c1, "N", 1)
	}

	// axial is an axial shading over colourSpace whose function gives components.
	axial := func(colourSpace any, components int) *pdfgen.Dict {
		return d(
			"ShadingType", 2,
			"ColorSpace", colourSpace,
			"Coords", a(0, 0, 100, 0),
			"Function", ramp(components),
		)
	}

	// shadingPattern is a shading pattern, which carries no /Resources (ISO 32000-1, Table 76).
	shadingPattern := func(shading *pdfgen.Dict) *pdfgen.Dict {
		return d("Type", n("Pattern"), "PatternType", 2, "Shading", shading)
	}

	p1 := pdf.Indirect(shadingPattern(pdf.Indirect(axial(spot, 1))))
	p2 := shadingPattern(axial(n("DeviceCMYK"), 4))
	spot2 := a(n("Separation"), n("/Spot2"), n("DeviceCMYK"), tint)
	devicen := a(
		n("DeviceN"),
		a(n("/Spot2")),
		n("DeviceCMYK"),
		tint,
		d("Colorants", d("Spot2", spot2)),
	)
	p3 := pdf.Indirect(shadingPattern(axial(devicen, 1)))
	spotDirect := a(n("Separation"), n("/Spot"), n("DeviceCMYK"), tint)
	p5 := pdf.Indirect(shadingPattern(axial(spotDirect, 1)))

	formStream := pdf.Stream([]byte("q /Pattern cs /P5 scn 0 0 100 100 re f Q\n"))
	formStream.Set("Type", n("XObject"))
	formStream.Set("Subtype", n("Form"))
	formStream.Set("BBox", a(0, 0, 100, 100))
	formStream.Set("Resources", d("Pattern", d("P5", p5)))
	form := pdf.MakeIndirect(formStream)

	addPage := func(content string, resources *pdfgen.Dict) {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", resources,
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
		))
		pdf.AddPage(page)
	}

	addPage(
		"q 1 0 0 rg 0 150 20 20 re f /Pattern cs /P1 scn 0 0 100 100 re f Q\n",
		d("Pattern", d("P1", p1)),
	)
	addPage("q /Pattern cs /P2 scn 0 0 100 100 re f Q\n", d("Pattern", d("P2", p2)))
	addPage("q /Pattern cs /P3 scn 0 0 100 100 re f Q\n", d("Pattern", d("P3", p3)))
	addPage(
		"q /CS0 cs 1 scn 0 150 20 20 re f 0 0 0 1 k 30 150 20 20 re f /Pattern cs /P1 scn 0 0 100 100 re f Q\n",
		d("ColorSpace", d("CS0", spot), "Pattern", d("P1", p1)),
	)
	addPage("q /F5 Do Q\n", d("XObject", d("F5", form)))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
