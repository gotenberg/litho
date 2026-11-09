package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("flattenannots_2b", flattenannots2b) }

// flattenannots2b builds flattenannots_2b.pdf: annotations that paint with transparency, which
// transparency flattening draws into the page, at PDF/A-2b.
//
// Writer.FlattenTransparency draws into the page content the appearance of each
// annotation that paints with transparency, leaves the annotation with an
// appearance that paints nothing, and flattens the page. The corpora hold such
// annotations, but the flatten cell's render sample reaches few kinds of them,
// none synthesised from its dictionary, no annotation drawn under a transparent
// one, and no stroked straight line, whose path box has no area. Every glyph is
// a Type 3 glyph painting in the text's colour, with a /ToUnicode map, so the
// text extracts and draws the same in every renderer:
//
// | Page | /Annots, in order                                                     |
// | ---- | --------------------------------------------------------------------- |
// | 1    | an opaque Circle the Highlight overlaps, a Multiply Highlight over    |
// |      | text with a closed Popup, a Square at /CA 0.5 with an opaque          |
// |      | appearance, a Square at /CA 0.5 with no appearance, an opaque Ink     |
// |      | apart from the rest, and a hidden Square at /CA 0.5                   |
// | 2    | a FreeText whose background paints at ca 0.6 under opaque text        |
// | 3    | an Ink annotation stroking a straight line at ca 0.7 below text       |
// | 4    | the same Ink line through the text, and a FreeText whose text paints  |
// |      | at ca 0.6 too                                                         |
//
// What flattening makes of each: the Highlight, both Squares, the Ink line and
// the FreeText paint with transparency and are drawn into the page, the Square
// with no appearance as litho synthesises it; the Circle is drawn too, before
// the Highlight, because a viewer drew it under the Highlight; the Popup stays,
// and so does the opaque Ink no drawn annotation overlaps; the hidden Square
// loses its appearance with nothing drawn. The FreeText's text joins the page's.
// The flatten cell renders the first three pages, which flattening draws
// exactly. Page 4 holds what it approximates and reports: the text the Ink line
// crosses draws above it untinted (TextUnderTransparency), and the FreeText's
// transparent text draws opaque, composited over white (TransparentText).
//
// The file declares no OutputIntent and its pages no /Group, so DeviceRGB fails
// ISO 19005-2, 6.2.4.3 t2 and every page holding transparency 6.2.10 t2; the
// appearance-less Square fails 6.3.3 t1 and the hidden one 6.3.2 t2.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func flattenannots2b() ([]byte, error) {
	pdf := pdfgen.New()

	glyphs := []type3Proc{
		{"A", 600, "0 0 500 700 re f"},
		{"B", 600, "0 0 500 700 re 100 100 300 500 re f*"},
		{"C", 600, "0 0 m 500 0 l 250 700 l f"},
		{"D", 600, "0 300 500 150 re f 175 0 150 700 re f"},
		{"E", 600, "200 0 120 700 re f 0 580 500 120 re f"},
	}
	widths := make([]any, len(glyphs))
	toUnicode := map[int]int{}
	for i, g := range glyphs {
		widths[i] = 600
		toUnicode[65+i] = int(g.Name[0])
	}
	font := type3Font(pdf, glyphs, widths, 65)
	font.Set("ToUnicode", toUnicodeCMap(pdf, toUnicode))
	font = pdf.Indirect(font)

	form := func(content string, bbox *pdfgen.Array, resources *pdfgen.Dict, matrix *pdfgen.Array) *pdfgen.Stream {
		stream := pdf.Stream([]byte(content))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", bbox)
		if matrix != nil {
			stream.Set("Matrix", matrix)
		}
		if resources != nil {
			stream.Set("Resources", resources)
		} else {
			stream.Set("Resources", d())
		}
		return pdf.MakeIndirect(stream).(*pdfgen.Stream)
	}

	state := func(entries ...any) *pdfgen.Dict {
		return pdf.Indirect(d(append([]any{"Type", n("ExtGState")}, entries...)...))
	}

	annot := func(subtype pdfgen.Name, rect *pdfgen.Array, entries ...any) *pdfgen.Dict {
		return pdf.Indirect(d(append([]any{"Type", n("Annot"), "Subtype", subtype, "Rect", rect}, entries...)...))
	}

	addPage := func(content string, annots []*pdfgen.Dict) {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 100),
			"Resources", d("Font", d("F", font)),
			"Contents", pdf.Stream([]byte(content)),
		))
		arr := a()
		for _, an := range annots {
			an.Set("P", page)
			arr.Append(an)
		}
		page.Set("Annots", arr)
		pdf.AddPage(page)
	}

	multiply := state("BM", n("Multiply"))
	circle := annot(
		n("Circle"),
		a(5, 30, 45, 70),
		"F", 4,
		"C", a(0, 0.6, 0),
		"AP", d("N", form("0 0.6 0 RG 2 w 20 2 m 38 20 l 20 38 l 2 20 l h S\n", a(0, 0, 40, 40), nil, nil)),
	)
	highlight := annot(
		n("Highlight"),
		a(8, 36, 112, 64),
		"F", 4,
		"C", a(1, 1, 0),
		"QuadPoints", a(8, 64, 112, 64, 8, 36, 112, 36),
		"Contents", text("Check this"),
		"AP", d("N", form("/R0 gs 1 1 0 rg 0 0 104 28 re f\n", a(8, 36, 112, 64), d("ExtGState", d("R0", multiply)), a(1, 0, 0, 1, -8, -36))),
	)
	popup := annot(n("Popup"), a(120, 60, 190, 95), "Parent", highlight, "Open", false)
	highlight.Set("Popup", popup)
	faded := annot(
		n("Square"),
		a(130, 10, 160, 40),
		"F", 4,
		"CA", 0.5,
		"C", a(0, 0, 1),
		"AP", d("N", form("0 0 1 rg 0 0 30 30 re f\n", a(0, 0, 30, 30), nil, nil)),
	)
	drawn := annot(n("Square"), a(165, 10, 195, 40), "F", 4, "CA", 0.5, "C", a(1, 0, 0), "IC", a(1, 0.8, 0.8), "Border", a(0, 0, 2))
	ink := annot(
		n("Ink"),
		a(170, 60, 195, 95),
		"F", 4,
		"C", a(0.5, 0, 0.5),
		"InkList", a(a(172, 62, 193, 93)),
		"AP", d("N", form("0.5 0 0.5 RG 2 w 2 2 m 23 33 l S\n", a(0, 0, 25, 35), nil, nil)),
	)
	hidden := annot(
		n("Square"),
		a(60, 5, 90, 30),
		"F", 6,
		"CA", 0.5,
		"C", a(1, 0, 0),
		"AP", d("N", form("1 0 0 rg 0 0 30 25 re f\n", a(0, 0, 30, 25), nil, nil)),
	)
	addPage(
		"0.85 g 0 0 200 100 re f BT /F 20 Tf 0 g 10 40 Td (ABCDE) Tj ET\n",
		[]*pdfgen.Dict{circle, highlight, popup, faded, drawn, ink, hidden},
	)

	inkLine := func(y int, rect *pdfgen.Array) *pdfgen.Dict {
		return annot(
			n("Ink"),
			rect,
			"F", 4,
			"CA", 0.7,
			"C", a(1, 0, 0),
			"InkList", a(a(10, y, 190, y)),
			"AP", d("N", form("/X gs 1 0 0 RG 4 w 1 J 5 5 m 185 5 l S\n", a(0, 0, 190, 10), d("ExtGState", d("X", state("CA", 0.7, "ca", 0.7))), nil)),
		)
	}

	freeText := func(content string) *pdfgen.Dict {
		return annot(
			n("FreeText"),
			a(120, 60, 190, 90),
			"F", 4,
			"Contents", text("DEA"),
			"DA", text("0 0 1 rg /F 12 Tf"),
			"AP", d("N", form(content, a(0, 0, 70, 30), d("Font", d("F", font), "ExtGState", d("T", state("ca", 0.6))), nil)),
		)
	}

	addPage(
		"BT /F 20 Tf 0 g 10 38 Td (ABCDEABCDE) Tj ET\n",
		[]*pdfgen.Dict{freeText("q /T gs 0.8 0.9 1 rg 0 0 70 30 re f Q BT /F 16 Tf 0 0 1 rg 8 9 Td (DEA) Tj ET\n")},
	)
	addPage("BT /F 20 Tf 0 g 10 38 Td (ABCDEABCDE) Tj ET\n", []*pdfgen.Dict{inkLine(15, a(5, 10, 195, 20))})
	addPage(
		"BT /F 20 Tf 0 g 10 38 Td (ABCDEABCDE) Tj ET\n",
		[]*pdfgen.Dict{inkLine(45, a(5, 40, 195, 50)), freeText("/T gs 0.8 0.9 1 rg 0 0 70 30 re f BT /F 16 Tf 0 0 1 rg 8 9 Td (DEA) Tj ET\n")},
	)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
