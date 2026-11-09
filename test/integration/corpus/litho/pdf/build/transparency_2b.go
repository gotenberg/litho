package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("transparency_2b", transparency2b) }

// transparency2b builds transparency_2b.pdf: the transparency the renderer composites, at
// PDF/A-2b.
//
// ISO 32000-1, 11 composites every painting into the transparency group being
// painted. The render goldens of the corpora held soft masks and blend modes,
// but no knockout group, no luminosity mask with a /BC or a /TR, and no blend
// mode drawn over the bare page, so the render cell never compared them. Each
// page draws in DeviceRGB or DeviceGray:
//
// | Page | Draws                                                               |
// | ---- | ------------------------------------------------------------------- |
// | 1    | four groups over a yellow band at ca 0.8: non-isolated, isolated,   |
// |      | knockout, isolated knockout, each two squares Multiply at ca 0.6,   |
// |      | and an isolated group holding a knockout group, composited Screen   |
// | 2    | a luminosity mask over a gray ramp with /BC 0.5 outside its box,    |
// |      | the same mask through an inverting /TR, and an alpha mask whose     |
// |      | group holds an opaque square and one at ca 0.4                      |
// | 3    | the sixteen blend modes, one per cell, over two backdrop colours    |
// |      | and the bare page                                                   |
//
// poppler 26.09.0 draws all three as ISO 32000-1 asks. It ignores the alpha
// source flag (AIS), text knockout (TK) and a graphics-state soft mask over an
// image that has its own /SMask, so the file holds none of those: litho's unit
// tests check them against the specification.
//
// The file declares no OutputIntent and its pages no /Group, so every page that
// paints DeviceRGB fails ISO 19005-2, 6.2.4.3 t2 and every page that holds
// transparency fails 6.2.10 t2.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func transparency2b() ([]byte, error) {
	pdf := pdfgen.New()

	form := func(content string, bbox []int, group, resources *pdfgen.Dict) obj {
		stream := pdf.Stream([]byte(content))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", bbox)
		if group != nil {
			stream.Set("Group", group)
		}
		if resources != nil {
			stream.Set("Resources", resources)
		}
		return pdf.MakeIndirect(stream)
	}

	transparency := func(attrs ...any) *pdfgen.Dict {
		return d(append([]any{"Type", n("Group"), "S", n("Transparency")}, attrs...)...)
	}

	addPage := func(width, height int, content string, resources *pdfgen.Dict) {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, width, height),
			"Resources", resources,
			"Contents", pdf.Stream([]byte(content)),
		)
		pdf.AddPage(pdf.Indirect(page))
	}

	const squares = "/G gs 1 0 0 rg 5 5 30 30 re f 0 0 1 rg 15 15 30 30 re f\n"
	squaresResources := d("ExtGState", d("G", d("Type", n("ExtGState"), "ca", 0.6, "BM", n("Multiply"))))
	groups := make([]any, 0, 10)
	groups = append(groups,
		"N", form(squares, []int{0, 0, 50, 50}, transparency(), squaresResources),
		"I", form(squares, []int{0, 0, 50, 50}, transparency("I", true), squaresResources),
		"K", form(squares, []int{0, 0, 50, 50}, transparency("K", true), squaresResources),
		"IK", form(squares, []int{0, 0, 50, 50}, transparency("I", true, "K", true), squaresResources),
	)
	knockout := form(
		"/D gs 1 0 0 rg 5 5 20 30 re f 0 1 1 rg 15 10 20 30 re f\n",
		[]int{0, 0, 50, 50},
		transparency("K", true),
		d("ExtGState", d("D", d("Type", n("ExtGState"), "ca", 0.7, "BM", n("Difference")))),
	)
	groups = append(groups, "O", form(
		"0.5 g 2 2 46 15 re f /K Do\n",
		[]int{0, 0, 50, 50},
		transparency("I", true),
		d("XObject", d("K", knockout)),
	))
	addPage(
		250,
		60,
		"1 1 0 rg 0 0 250 25 re f /A gs\n"+
			"q 1 0 0 1 0 5 cm /N Do Q q 1 0 0 1 50 5 cm /I Do Q q 1 0 0 1 100 5 cm /K Do Q\n"+
			"q 1 0 0 1 150 5 cm /IK Do Q /S gs q 1 0 0 1 200 5 cm /O Do Q\n",
		d(
			"XObject", d(groups...),
			"ExtGState", d(
				"A", d("Type", n("ExtGState"), "ca", 0.8),
				"S", d("Type", n("ExtGState"), "BM", n("Screen"), "ca", 0.9),
			),
		),
	)

	ramp := d(
		"ShadingType", 2,
		"ColorSpace", n("DeviceGray"),
		"Coords", a(20, 0, 100, 0),
		"Function", d("FunctionType", 2, "Domain", a(0, 1), "C0", a(0), "C1", a(1), "N", 1),
	)
	luminosity := form(
		"/Sh sh\n",
		[]int{20, 0, 100, 100},
		transparency("CS", n("DeviceGray")),
		d("Shading", d("Sh", ramp)),
	)
	invert := pdf.Indirect(d("FunctionType", 2, "Domain", a(0, 1), "C0", a(1), "C1", a(0), "N", 1))
	alpha := form(
		"0 g 110 10 40 80 re f /H gs 150 10 40 80 re f\n",
		[]int{100, 0, 200, 100},
		transparency(),
		d("ExtGState", d("H", d("Type", n("ExtGState"), "ca", 0.4))),
	)
	addPage(
		200,
		100,
		"0 0 1 rg 0 0 100 100 re f q /L gs 1 0 0 rg 0 50 100 50 re f Q q /T gs 0 1 0 rg 0 0 100 50 re f Q\n"+
			"1 1 0 rg 100 0 100 100 re f q /M gs 0 0 1 rg 100 0 100 100 re f Q\n",
		d(
			"ExtGState", d(
				"L", d("Type", n("ExtGState"), "SMask", d("Type", n("Mask"), "S", n("Luminosity"), "G", luminosity, "BC", a(0.5))),
				"T", d(
					"Type", n("ExtGState"),
					"SMask", d("Type", n("Mask"), "S", n("Luminosity"), "G", luminosity, "BC", a(0.5), "TR", invert),
				),
				"M", d("Type", n("ExtGState"), "SMask", d("Type", n("Mask"), "S", n("Alpha"), "G", alpha)),
			),
		),
	)

	modes := []string{
		"Normal", "Multiply", "Screen", "Overlay", "Darken", "Lighten", "ColorDodge", "ColorBurn",
		"HardLight", "SoftLight", "Difference", "Exclusion", "Hue", "Saturation", "Color", "Luminosity",
	}
	states := d()
	content := ""
	for i, mode := range modes {
		states.Set(fmt.Sprintf("B%d", i), d("Type", n("ExtGState"), "BM", n("/"+mode)))
		x, y := (i%4)*40, (3-i/4)*40
		content += fmt.Sprintf("0.9 0.5 0.1 rg %d %d 20 26 re f 0.2 0.4 0.8 rg %d %d 20 26 re f\n", x, y, x+20, y)
		content += fmt.Sprintf("q /B%d gs 0.3 0.8 0.5 rg %d %d 30 32 re f Q\n", i, x+5, y+5)
	}
	addPage(160, 160, content, d("ExtGState", states))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
