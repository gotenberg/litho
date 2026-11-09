package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("smaskbudget_2b", smaskbudget2b) }

// smaskbudget2b builds smaskbudget_2b.pdf: a graphics-state soft mask and a transparency
// group whose rasters pass the render pixel budget, at PDF/A-2b.
//
// A renderer that cannot afford a soft mask's group (ISO 32000-1, 11.6.5.2) or
// a transparency group (11.4) must not paint what they hide. No corpus file the
// render goldens cover reached the budget: pdfjs issue14982.pdf does, at 800 x
// 5424 pt, and has no render golden. Each page is 4000 x 4000 pt, 16 million
// pixels at 72 dpi, under litho's default budget of 50 million; a non-isolated
// group over it holds four planes and a layer, 80 million:
//
// | Page | Draws                                                                    |
// | ---- | ------------------------------------------------------------------------ |
// | 1    | a blue page, then red over it through a luminosity soft mask whose      |
// |      | group, over the whole page, is white in a central square and grey 0.5   |
// |      | in a band below it: red in the square, purple in the band, blue around  |
// | 2    | a blue page, then at ca 0.5 a non-isolated group over the whole page    |
// |      | holding a red square: purple in the square                              |
//
// litho renders page 1's mask group at half the resolution, and paints page 2's
// group content into the page at the group's alpha. Before, both painted the
// red opaque and unmasked.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func smaskbudget2b() ([]byte, error) {
	pdf := pdfgen.New()

	const size = 4000
	const bluePage = "0 0 1 rg 0 0 4000 4000 re f\n"

	form := func(content string, group, resources *pdfgen.Dict) *pdfgen.Stream {
		stream := pdf.Stream([]byte(content))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, size, size))
		stream.Set("Group", group)
		if resources != nil {
			stream.Set("Resources", resources)
		}
		pdf.MakeIndirect(stream)
		return stream
	}

	addPage := func(content string, resources *pdfgen.Dict) {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, size, size),
			"Resources", resources,
			"Contents", pdf.Stream([]byte(content)),
		)
		pdf.AddPage(pdf.Indirect(page))
	}

	state := func(entries ...any) *pdfgen.Dict {
		return d(append([]any{"Type", n("ExtGState")}, entries...)...)
	}

	maskGroup := form(
		"1 g 1000 1500 2000 2000 re f 0.5 g 1000 500 2000 800 re f\n",
		d("Type", n("Group"), "S", n("Transparency"), "CS", n("DeviceGray")),
		nil,
	)
	addPage(
		bluePage+"/M gs 1 0 0 rg 0 0 4000 4000 re f\n",
		d("ExtGState", d("M", state("SMask", d("Type", n("Mask"), "S", n("Luminosity"), "G", maskGroup)))),
	)

	group := form("1 0 0 rg 1000 1000 2000 2000 re f\n", d("Type", n("Group"), "S", n("Transparency")), nil)
	addPage(
		bluePage+"/A gs /G Do\n",
		d("ExtGState", d("A", state("ca", 0.5)), "XObject", d("G", group)),
	)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
