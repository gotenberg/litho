package build

import (
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("qnestreach_2b", qnestreach2b) }

// qnestreach2b builds qnestreach_2b.pdf: q/Q nesting past the limit in streams a page draws.
//
// ISO 19005-1, 6.1.12 and ISO 19005-2, 6.1.13 cap q/Q nesting at 28 levels
// (ISO 32000-1, Annex C, Table C.1). veraPDF 1.30.2 counts the levels of every
// content stream it parses on their own, starting at zero, and fails the test
// once for each q past the 28th: in the page's content, and in each form
// XObject, tiling pattern cell, Type 3 glyph description and annotation
// appearance the page draws. No corpus file nests that deep outside a page's own
// content, so this fixture plants one stream per page, each at its own depth so
// the count names the stream:
//
// | Page | Stream                                  | Levels | Answers at PDF/A-2b |
// | ---- | --------------------------------------- | ------ | ------------------- |
// | 1    | a form XObject                          | 29     | `6.1.13/8` once     |
// | 2    | a tiling pattern cell                   | 30     | twice               |
// | 3    | a Type 3 glyph description              | 31     | three times         |
// | 4    | an annotation's normal appearance       | 32     | four times          |
// | 5    | page 1's form, painted again            | 29     | nothing             |
// | 6    | a page 20 deep painting a form 15 deep  | 20, 15 | nothing             |
//
// Pages 5 and 6 are must-not-fire controls: a stream is judged once however
// often it is drawn, and a form's levels do not add to its painter's.
//
// A CMYK OutputIntent covers the DeviceGray every stream paints in.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func qnestreach2b() ([]byte, error) {
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

	// nested is a grey square painted inside levels of q/Q.
	nested := func(levels int) string {
		return strings.Repeat("q ", levels) + "0 g 0 0 10 10 re f " + strings.Repeat("Q ", levels) + "\n"
	}

	// form is a form XObject with empty /Resources.
	form := func(content string) pdfgen.Object {
		xobj := pdf.Stream([]byte(content))
		xobj.Set("Type", n("XObject"))
		xobj.Set("Subtype", n("Form"))
		xobj.Set("BBox", a(0, 0, 100, 100))
		xobj.Set("Resources", d())
		return pdf.MakeIndirect(xobj)
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

	form1 := form(nested(29))
	addPage("q /X1 Do Q\n", d("XObject", d("X1", form1)), nil)

	cell := pdf.Stream([]byte(nested(30)))
	cell.Set("Type", n("Pattern"))
	cell.Set("PatternType", 1)
	cell.Set("PaintType", 1)
	cell.Set("TilingType", 1)
	cell.Set("BBox", a(0, 0, 20, 20))
	cell.Set("XStep", 20)
	cell.Set("YStep", 20)
	cell.Set("Resources", d())
	addPage(
		"q /Pattern cs /P2 scn 0 0 100 100 re f Q\n",
		d("Pattern", d("P2", pdf.MakeIndirect(cell))),
		nil,
	)

	glyph := pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0\n" + nested(31))))
	type3 := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type3"),
		"FontBBox", a(0, 0, 1000, 1000),
		"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
		"CharProcs", d("A", glyph),
		"Encoding", d("Type", n("Encoding"), "Differences", a(65, n("A"))),
		"FirstChar", 65,
		"LastChar", 65,
		"Widths", a(1000),
		"Resources", d(),
	))
	addPage("q 0 g BT /T3 50 Tf 10 10 Td (A) Tj ET Q\n", d("Font", d("T3", type3)), nil)

	annot := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(0, 0, 100, 100),
		"F", 4,
		"AP", d("N", form(nested(32))),
	))
	addPage("q 0 g 0 0 1 1 re f Q\n", d(), a(annot))

	addPage("q /X1 Do Q\n", d("XObject", d("X1", form1)), nil)

	addPage(
		strings.Repeat("q ", 20)+"/X6 Do "+strings.Repeat("Q ", 20)+"\n",
		d("XObject", d("X6", form(nested(15)))),
		nil,
	)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
