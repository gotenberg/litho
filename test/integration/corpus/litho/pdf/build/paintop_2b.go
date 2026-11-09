package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("paintop_2b", paintop2b) }

// paintop2b builds paintop_2b.pdf: transparency nothing paints, and halftones judged once.
//
// ISO 19005-2, 6.2.10 t2 asks every page that contains transparency, in a
// document without a PDF/A OutputIntent, for a blending colour space. veraPDF
// decides that a page contains transparency at the operators that paint, under
// the graphics state in force there: a fill tests /ca, a stroke /CA, both a soft
// mask and a blend mode, and an alpha counts only below 1. The file declares no
// OutputIntent and no page group, and every page paints in an ICCBased space,
// so no page uses a device colour space of its own. Each of pages 1 to 7 sets a
// transparent graphics state that no operator paints under:
//
// | Page | Construct                                                 |
// | ---- | --------------------------------------------------------- |
// | 1    | `/ca 0.5` set inside `q ... Q`, the fill after `Q`         |
// | 2    | `/ca 0.5` before a stroke alone                            |
// | 3    | `/CA 0.5` before a fill alone                              |
// | 4    | `/ca 1.5` before a fill                                    |
// | 5    | a soft mask cleared by a graphics state without one        |
// | 6    | a tiling pattern filling at `/ca 0.5`, selected, not used |
// | 7    | an annotation appearance setting `/ca 0.5`, painting none  |
//
// None of them contains transparency, so 6.2.10 t2 fires nowhere. litho counted
// every graphics state a gs sets, and every tiling pattern the resources list,
// and failed all seven.
//
// Page 8 sets four graphics states whose halftones ISO 19005-2, 6.2.5 judges:
//
// | State | /HT                                                         | Rule        |
// | ----- | ----------------------------------------------------------- | ----------- |
// | H1    | type 5, /Red and /Cyan naming one component, no transfer fn | silent      |
// | H2    | type 5, /Blue and /Yellow naming one component              | silent      |
// | H3    | the name /Foo                                               | `6.2.5/4`,5 |
// | H4    | a type 6 halftone, a stream                                 | `6.2.5/4`   |
//
// veraPDF judges a component written as an indirect object once, under the
// colorant that reaches it first: it lists a type 5 halftone's components in the
// order of the Java HashMap its dictionary is and walks them from the last, so
// /Cyan and /Yellow, process colorants that need no transfer function, reach the
// shared components before /Red and /Blue, which would. litho judged every
// colorant, and failed t6 for /Red and /Blue. A halftone named by a name other
// than /Default has no type and names itself, and a type 6 halftone is a
// stream; litho read neither.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func paintop2b() ([]byte, error) {
	pdf := pdfgen.New()

	// iccProfile is a structurally minimal ICC profile veraPDF will parse.
	iccProfile := func(colourSpace string, components int, tag string) pdfgen.Object {
		stream := pdf.Stream(iccMinimalProfile("mntr", colourSpace, iccVersion21, tag))
		stream.Set("N", components)
		return pdf.MakeIndirect(stream)
	}

	iccbased := pdf.MakeIndirect(a(n("ICCBased"), iccProfile("RGB ", 3, "ICCB")))

	state := func(entries ...any) *pdfgen.Dict {
		dd := d("Type", n("ExtGState"))
		for i := 0; i < len(entries); i += 2 {
			dd.Set(entries[i].(string), entries[i+1])
		}
		return pdf.Indirect(dd)
	}

	softMaskGroup := pdf.Stream([]byte("/CS0 cs 1 1 1 scn 0 0 612 792 re f\n"))
	softMaskGroup.Set("Type", n("XObject"))
	softMaskGroup.Set("Subtype", n("Form"))
	softMaskGroup.Set("BBox", a(0, 0, 612, 792))
	softMaskGroup.Set("Resources", d("ColorSpace", d("CS0", iccbased)))
	softMaskGroup.Set("Group", d("Type", n("Group"), "S", n("Transparency"), "CS", iccbased))

	states := d(
		"GSca", state("ca", 0.5),
		"GSCA", state("CA", 0.5),
		"GSca15", state("ca", 1.5),
		"GSsm", state("SMask", d("Type", n("Mask"), "S", n("Luminosity"), "G", pdf.MakeIndirect(softMaskGroup))),
		"GSlw", state("LW", 2),
	)

	patternStream := pdf.Stream([]byte("/GSca gs /CS0 cs 0.2 0.4 0.6 scn 0 0 5 5 re f\n"))
	patternStream.Set("Type", n("Pattern"))
	patternStream.Set("PatternType", 1)
	patternStream.Set("PaintType", 1)
	patternStream.Set("TilingType", 1)
	patternStream.Set("BBox", a(0, 0, 10, 10))
	patternStream.Set("XStep", 10)
	patternStream.Set("YStep", 10)
	patternStream.Set("Resources", d("ExtGState", d("GSca", states.Get("GSca")), "ColorSpace", d("CS0", iccbased)))
	pattern := pdf.MakeIndirect(patternStream)

	const (
		fill   = "/CS0 cs 0.2 0.4 0.6 scn 60 60 200 200 re f\n"
		stroke = "/CS0 CS 0.2 0.4 0.6 SCN 60 60 200 200 re S\n"
	)

	addPage := func(content string, annots *pdfgen.Array, extgstate *pdfgen.Dict) {
		if extgstate == nil {
			extgstate = states
		}
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d(
				"ExtGState", extgstate,
				"ColorSpace", d("CS0", iccbased),
				"Pattern", d("P1", pattern),
			),
			"Contents", pdf.Stream([]byte(content)),
		)
		if annots != nil {
			page.Set("Annots", annots)
		}
		pdf.AddPage(pdf.Indirect(page))
	}

	addPage("q /GSca gs Q "+fill, nil, nil)
	addPage("/GSca gs "+stroke, nil, nil)
	addPage("/GSCA gs "+fill, nil, nil)
	addPage("/GSca15 gs "+fill, nil, nil)
	addPage("/GSsm gs /GSlw gs "+fill, nil, nil)
	addPage("/Pattern cs /P1 scn "+fill, nil, nil)

	appearance := pdf.Stream([]byte("/GSca gs\n"))
	appearance.Set("Type", n("XObject"))
	appearance.Set("Subtype", n("Form"))
	appearance.Set("BBox", a(0, 0, 100, 100))
	appearance.Set("Resources", d("ExtGState", d("GSca", states.Get("GSca"))))
	square := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(100, 100, 200, 200),
		"F", 4,
		"Contents", text("a square"),
		"AP", d("N", pdf.MakeIndirect(appearance)),
	))
	addPage(fill, a(square), nil)

	t1 := []any{"Type", n("Halftone"), "HalftoneType", 1, "Frequency", 60, "Angle", 45, "SpotFunction", n("Round")}

	halftone := func(entries ...any) *pdfgen.Dict {
		return pdf.Indirect(d(entries...))
	}

	redCyan := halftone(t1...)
	blueYellow := halftone(t1...)
	dflt := halftone(t1...)
	type6 := pdf.Stream([]byte("\x00\x40\x80\xc0"))
	type6.Set("Type", n("Halftone"))
	type6.Set("HalftoneType", 6)
	type6.Set("Width", 2)
	type6.Set("Height", 2)

	halftones := d(
		"H1", state("HT", halftone("Type", n("Halftone"), "HalftoneType", 5, "Red", redCyan, "Cyan", redCyan, "Default", dflt)),
		"H2", state("HT", halftone("Type", n("Halftone"), "HalftoneType", 5, "Blue", blueYellow, "Yellow", blueYellow, "Default", dflt)),
		"H3", state("HT", n("Foo")),
		"H4", state("HT", pdf.MakeIndirect(type6)),
	)
	addPage("/H1 gs /H2 gs /H3 gs /H4 gs "+fill, nil, halftones)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
