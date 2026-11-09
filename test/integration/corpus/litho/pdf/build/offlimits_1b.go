package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("offlimits_1b", offlimits1b) }

// offlimits1b builds offlimits_1b.pdf: implementation limits in streams drawn colour-off.
//
// ISO 32000-1, 8.7.3.3 has an uncoloured tiling pattern's cell ignore its colour
// operators, and 9.6.5 a Type 3 glyph past its d1, and veraPDF reads every
// stream such a cell or glyph draws the same way: it builds no colour space and
// no pattern an operator there selects, so it judges neither the DeviceN
// colourant limit, ISO 19005-1, 6.1.12 t9, on a space selected there, nor any
// number given to a colour operator there against 6.1.12 t1. An image such a
// stream paints is still built. A census found 17 files that draw a stream with
// its colour operators off and none with a number past a limit in one, so no
// count reached the reading, and no remediation had to leave such a number as
// written.
//
// Every construct sits on a page of its own. `DN` is a DeviceN of 33 colourants,
// past the 8 ISO 19005-1, 6.1.12 allows and the 32 of ISO 19005-2, 6.1.13:
//
// | Page | Construct                                                         | 6.1.12 at PDF/A-1b |
// | ---- | ----------------------------------------------------------------- | ------------------ |
// | 1    | an uncoloured cell paints an image in `DN`                        | t9 x1              |
// | 2    | a coloured cell selects `DN`, beside an uncoloured cell           | nothing            |
// | 3    | an uncoloured cell paints a form selecting `DN` and an image in it | t9 x1             |
// | 4    | an `/Indexed` whose base names the `DN` entry, selected and filled | t9 x2             |
// | 5    | an image whose `/Indexed` base names the `DN` entry               | t9 x1              |
// | 6    | a glyph selects a pattern past its `d1`                           | nothing            |
// | 7    | a glyph selects a pattern past the `Q` closing its `d1`           | t1 x1              |
// | 8    | an uncoloured cell selects a pattern                              | nothing            |
// | 9    | a form an uncoloured cell paints selects a pattern                | nothing            |
// | 10   | a form painted in the Pattern space gives `scn` a pattern alone   | t1 x1              |
// | 11   | `99999999999 g` in a form an uncoloured cell paints               | nothing            |
// | 12   | `99999999999 g` in a glyph an uncoloured cell shows               | nothing            |
// | 13   | `99999999999 g` in a coloured cell, beside an uncoloured one      | nothing            |
// | 14   | `99999999999 g` in a form the page paints                         | t1 x1              |
//
// On pages 6 to 10 the pattern selected is a coloured cell of its own giving
// `w` the integer 2147483648, past 6.1.12 t1, which counts only where veraPDF
// builds the pattern. Page 2 is the uncoloured pattern turning the colour
// operators off for every tiling cell the same stream paints, the coloured one
// beside it included; page 3 is an image being built where the space a colour
// operator selects is not. Pages 11 to 14 are the numbers a content-limits
// repair reads: it clamps the one compliance judges, on page 14, and leaves the
// three it does not, since rewriting them changes the file for no rule. It does
// clamp the `w` of the cells on pages 6, 8 and 9, which compliance never builds.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func offlimits1b() ([]byte, error) {
	pdf := pdfgen.New()

	const (
		fill     = "0 0 10 10 re f\n"
		cellFill = "0 0 5 5 re f\n"
		bigW     = "2147483648 w "
		bigG     = "99999999999 g "
	)
	index := str("\x00\x00\x00\xff\xff\xff")

	// devicen is a DeviceN of 33 spot colourants over DeviceCMYK.
	devicen := func() *pdfgen.Array {
		const count = 33
		names, domain := a(), a()
		for i := range count {
			names.Append(n(fmt.Sprintf("/S%d", i)))
			domain.Append(0, 1)
		}
		return a(
			n("DeviceN"),
			names,
			n("DeviceCMYK"),
			d("FunctionType", 4, "Domain", domain, "Range", a(0, 1, 0, 1, 0, 1, 0, 1), "Length", 1),
		)
	}

	image := func(colourSpace any) pdfgen.Object {
		img := pdf.Stream([]byte("\x00"))
		img.Set("Type", n("XObject"))
		img.Set("Subtype", n("Image"))
		img.Set("Width", 1)
		img.Set("Height", 1)
		img.Set("BitsPerComponent", 8)
		img.Set("ColorSpace", colourSpace)
		return pdf.MakeIndirect(img)
	}

	form := func(body string, resources *pdfgen.Dict) pdfgen.Object {
		stream := pdf.Stream([]byte(body))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 10, 10))
		stream.Set("Resources", resources)
		return pdf.MakeIndirect(stream)
	}

	tiling := func(paint int, body string, resources *pdfgen.Dict) pdfgen.Object {
		stream := pdf.Stream([]byte(body))
		stream.Set("Type", n("Pattern"))
		stream.Set("PatternType", 1)
		stream.Set("PaintType", paint)
		stream.Set("TilingType", 1)
		stream.Set("BBox", a(0, 0, 10, 10))
		stream.Set("XStep", 10)
		stream.Set("YStep", 10)
		if resources != nil {
			stream.Set("Resources", resources)
		} else {
			stream.Set("Resources", d())
		}
		return pdf.MakeIndirect(stream)
	}

	// bigCell is a coloured cell of its own whose w operand is past 6.1.12 t1.
	bigCell := func() pdfgen.Object {
		return tiling(1, bigW+cellFill, nil)
	}

	type3 := func(glyph string, resources *pdfgen.Dict) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"FontBBox", a(0, 0, 1, 1),
			"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
			"CharProcs", d("a", pdf.MakeIndirect(pdf.Stream([]byte(glyph)))),
			"Encoding", d("Type", n("Encoding"), "Differences", a(97, n("/a"))),
			"FirstChar", 97,
			"LastChar", 97,
			"Widths", a(1000),
			"Resources", resources,
		))
	}

	page := func(content string, resources ...any) {
		dd := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", d(resources...),
			"Contents", pdf.Stream([]byte(content)),
		)
		pdf.AddPage(pdf.Indirect(dd))
	}

	offSpace := d("CSp", a(n("Pattern"), n("DeviceGray")))
	const offFill = "/CSp cs 0.5 /P2 scn " + fill
	const textOps = "BT /T3 12 Tf 10 10 Td (a) Tj ET\n"

	page(offFill, "ColorSpace", offSpace, "Pattern", d(
		"P2", tiling(2, "/Im0 Do\n", d("XObject", d("Im0", image(devicen()))))))
	page("/Pattern cs /P1 scn "+fill+offFill, "ColorSpace", offSpace, "Pattern", d(
		"P1", tiling(1, "/CSn cs 1 scn "+cellFill, d("ColorSpace", d("CSn", devicen()))),
		"P2", tiling(2, cellFill, nil)))
	page(offFill, "ColorSpace", offSpace, "Pattern", d("P2", tiling(2, "/Fm0 Do\n", d("XObject", d(
		"Fm0", form("/CSn cs 1 scn "+fill+"/Im0 Do\n",
			d("ColorSpace", d("CSn", devicen()), "XObject", d("Im0", image(devicen())))))))))
	page("/CS1 cs 0 sc "+fill, "ColorSpace", d("CS0", devicen(), "CS1", a(n("Indexed"), n("/CS0"), 1, index)))
	page("/Im0 Do\n", "ColorSpace", d("CS0", devicen()),
		"XObject", d("Im0", image(a(n("Indexed"), n("/CS0"), 1, index))))

	page(textOps, "Font", d("T3", type3("1000 0 0 0 1 1 d1 /Pattern cs /P1 scn 0 0 1 1 re f\n",
		d("Pattern", d("P1", bigCell())))))
	page(textOps, "Font", d("T3", type3("q 1000 0 0 0 1 1 d1 Q /Pattern cs /P1 scn 0 0 1 1 re f\n",
		d("Pattern", d("P1", bigCell())))))
	page(offFill, "ColorSpace", offSpace, "Pattern", d(
		"P2", tiling(2, "/Pattern cs /P1 scn "+cellFill, d("Pattern", d("P1", bigCell())))))
	page(offFill, "ColorSpace", offSpace, "Pattern", d("P2", tiling(2, "/Fm0 Do\n", d("XObject", d(
		"Fm0", form("/Pattern cs /P1 scn "+fill, d("Pattern", d("P1", bigCell()))))))))
	page("/Pattern cs /Fm0 Do\n", "XObject", d(
		"Fm0", form("/P1 scn "+fill, d("Pattern", d("P1", bigCell())))))

	page(offFill, "ColorSpace", offSpace, "Pattern", d("P2", tiling(2, "/Fm0 Do\n", d("XObject", d(
		"Fm0", form(bigG+fill, d()))))))
	page(offFill, "ColorSpace", offSpace, "Pattern", d("P2", tiling(2, textOps, d("Font", d(
		"T3", type3("1000 0 d0 "+bigG+"0 0 1 1 re f\n", d()))))))
	page("/Pattern cs /P1 scn "+fill+offFill, "ColorSpace", offSpace, "Pattern", d(
		"P1", tiling(1, bigG+cellFill, nil), "P2", tiling(2, cellFill, nil)))
	page("/Fm0 Do\n", "XObject", d("Fm0", form(bigG+fill, d())))

	// Deliberately no /Metadata and no output intent.
	return save(pdf, "1.4")
}
