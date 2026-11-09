package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("blendspace_2b", blendspace2b) }

// blendspace2b builds blendspace_2b.pdf: transparency composited in a CMYK and a grey
// blending colour space, at PDF/A-2b.
//
// ISO 32000-1, 11.3.4 composites a transparency group's elements in the group's
// blending colour space, the space its /CS names (Table 147): a blend mode in a
// CMYK group applies to the complements of the inks (11.3.5), and its
// non-separable modes take K from one side. The corpora's render goldens held no
// CMYK group whose content blends, so the gate compared none. Each page draws
// in DeviceCMYK, read through litho's built-in CMYK profile as poppler reads it:
//
// | Page | Draws                                                                    |
// | ---- | ------------------------------------------------------------------------ |
// | 1    | in a page group over DeviceCMYK: an isolated CMYK group, a cyan square   |
// |      | then a magenta one Multiply over its right half, and an isolated         |
// |      | knockout CMYK group of the same squares Multiply at ca 0.7 over yellow   |
// | 2    | a luminosity mask whose group blends in DeviceGray, cyan then magenta    |
// |      | Multiply, over a black fill                                              |
// | 3    | in a page group over DeviceCMYK: black ink and cyan ink at ca 0.5, each  |
// |      | over the other, which flattening keeps as inks                           |
//
// poppler 26.09.0 and PDFium blend every group in RGB, so they draw the overlap
// of page 1 as the RGB product of cyan and magenta where ISO 32000-1 makes it
// the colour of both inks.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func blendspace2b() ([]byte, error) {
	pdf := pdfgen.New()

	form := func(content string, bbox *pdfgen.Array, group, resources *pdfgen.Dict) *pdfgen.Stream {
		stream := pdf.Stream([]byte(content))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", bbox)
		stream.Set("Group", group)
		stream.Set("Resources", resources)
		pdf.MakeIndirect(stream)
		return stream
	}

	transparency := func(attrs ...any) *pdfgen.Dict {
		return d(append([]any{"Type", n("Group"), "S", n("Transparency")}, attrs...)...)
	}

	addPage := func(content string, resources, group *pdfgen.Dict) {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 100, 100),
			"Resources", resources,
			"Contents", pdf.Stream([]byte(content)),
		)
		if group != nil {
			page.Set("Group", group)
		}
		pdf.AddPage(pdf.Indirect(page))
	}

	state := func(entries ...any) *pdfgen.Dict {
		return d(append([]any{"Type", n("ExtGState")}, entries...)...)
	}

	multiplyRes := d("ExtGState", d("M", state("BM", n("Multiply"))))

	multiply := form(
		"1 0 0 0 k 0 0 60 50 re f /M gs 0 1 0 0 k 40 0 60 50 re f\n",
		a(0, 0, 100, 50),
		transparency("I", true, "CS", n("DeviceCMYK")),
		multiplyRes,
	)
	knockout := form(
		"/K gs 1 0 0 0 k 5 5 55 40 re f 0 1 0 0 k 40 5 55 40 re f\n",
		a(0, 0, 100, 50),
		transparency("I", true, "K", true, "CS", n("DeviceCMYK")),
		d("ExtGState", d("K", state("BM", n("Multiply"), "ca", 0.7))),
	)
	addPage(
		"q 1 0 0 1 0 50 cm /C Do Q 0 0 1 0 k 0 0 100 50 re f /K Do\n",
		d("XObject", d("C", multiply, "K", knockout)),
		transparency("CS", n("DeviceCMYK")),
	)

	maskGroup := form(
		"1 0 0 0 k 0 0 100 100 re f /M gs 0 1 0 0 k 0 0 100 100 re f\n",
		a(0, 0, 100, 100),
		transparency("CS", n("DeviceGray")),
		multiplyRes,
	)
	addPage(
		"/L gs 0 0 0 1 k 0 0 100 100 re f\n",
		d("ExtGState", d("L", state("SMask", d("Type", n("Mask"), "S", n("Luminosity"), "G", maskGroup)))),
		transparency("CS", n("DeviceCMYK")),
	)

	addPage(
		"/A gs 0 0 0 1 k 10 10 50 80 re f 1 0 0 0 k 40 10 50 80 re f\n",
		d("ExtGState", d("A", state("ca", 0.5))),
		transparency("CS", n("DeviceCMYK")),
	)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
