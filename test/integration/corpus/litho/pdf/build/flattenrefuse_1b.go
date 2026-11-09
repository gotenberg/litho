package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("flattenrefuse_1b", flattenrefuse1b) }

// flattenrefuse1b builds flattenrefuse_1b.pdf: transparency on two pages, one of which
// transparency flattening refuses, at PDF/A-1b.
//
// A PDF/A-1 remediation flattens the transparency ISO 19005-1, 6.4 forbids, page
// by page when a page refuses, and sets opaque what it leaves. The corpora hold
// no page flattening refuses: every page they flatten fits the render pixel
// budget at some resolution. This file holds one beside one that flattens:
//
// | Page | Size (pt)   | Draws                                                      |
// | ---- | ----------- | ---------------------------------------------------------- |
// | 1    | 612 x 792   | a red square at ca 0.5 over a blue band                    |
// | 2    | 2000 x 2000 | the same, the square inside eight nested isolated knockout |
// |      |             | groups                                                     |
//
// Flattening composites page 2 with two page-sized planes for each open
// isolated knockout group, which no renderer can elide. Eight of them pass the
// 50 million pixel render budget at every resolution flattening tries, down to
// 72 dpi, so it refuses the page (`flatten.ErrTooComplex`), and the remediation
// flattens page 1 alone and sets page 2's transparency opaque. A renderer
// drawing page 2 without the groups draws the same page: each group holds one
// element, composited over the group's transparent initial backdrop and the
// result over the page beneath, as painting it directly does (ISO 32000-1,
// 11.4.6 and 11.4.7).
//
// The file declares no OutputIntent, so the DeviceRGB paint fails 6.2.3.3 t1 at
// PDF/A-1b; the groups fail 6.4 t3 and the constant alpha 6.4 t6.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func flattenrefuse1b() ([]byte, error) {
	pdf := pdfgen.New()

	const (
		nestedGroups = 8
		big          = 2000
	)

	half := pdf.Indirect(d("Type", n("ExtGState"), "ca", 0.5))
	const (
		band   = "0 0 1 rg 50 300 400 100 re f\n"
		square = "q /Half gs 1 0 0 rg 150 250 200 200 re f Q\n"
	)

	groupForm := func(content string, resources *pdfgen.Dict, size int) *pdfgen.Stream {
		stream := pdf.Stream([]byte(content))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, size, size))
		stream.Set("Group", d("Type", n("Group"), "S", n("Transparency"), "I", true, "K", true))
		stream.Set("Resources", resources)
		pdf.MakeIndirect(stream)
		return stream
	}

	page := func(width, height int, content string, resources *pdfgen.Dict) {
		pdf.AddPage(
			pdf.Indirect(
				d(
					"Type", n("Page"),
					"MediaBox", a(0, 0, width, height),
					"Resources", resources,
					"Contents", pdf.Stream([]byte(content)),
				),
			),
		)
	}

	page(612, 792, band+square, d("ExtGState", d("Half", half)))

	inner := groupForm(square, d("ExtGState", d("Half", half)), big)
	for range nestedGroups - 1 {
		inner = groupForm("/G Do\n", d("XObject", d("G", inner)), big)
	}
	page(big, big, band+"/G Do\n", d("XObject", d("G", inner)))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 1, Conformance: "B"}))

	return save(pdf, "1.4")
}
