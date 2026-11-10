package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("flatuntagged_ua1", flatuntaggedUa1) }

// flatuntaggedUa1 builds flatuntagged_ua1.pdf: an untagged document whose annotations paint
// with transparency, at PDF/UA-1.
//
// ISO 14289-1, 7.1 and ISO 14289-2, 8.2.2 require every piece of page content
// of a document claiming conformance to be tagged as real content or marked as
// an artifact. An annotation is not page content, so a document with no
// structure tree passes those tests while its page draws nothing of its own.
// Writer.FlattenTransparency draws each annotation that paints with
// transparency into the page, where no structure element can take it, so in a
// document claiming PDF/UA, as this one does, litho marks the drawing as an
// artifact (ISO 32000-1, 14.8.2.2), as FlattenForm marks a baked widget. The
// corpora's untagged files that claim PDF/UA hold no annotation painting with
// transparency, so this fixture gives the flatten cell the shape:
//
// | Annotation | /Rect             | Appearance                                          |
// | ---------- | ----------------- | --------------------------------------------------- |
// | Square     | [10 10 60 60]     | a blue square, /CA 0.5 and an ExtGState at ca 0.5   |
// | FreeText   | [80 20 190 80]    | a background at ca 0.6 under opaque text "DEA"      |
//
// The page's own content draws nothing. Every glyph is a Type 3 glyph with a
// /ToUnicode map. The file has no /StructTreeRoot and no /MarkInfo; the XMP
// packet claims PDF/UA-1, and an RGB OutputIntent covers the DeviceRGB the
// appearances paint.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func flatuntaggedUa1() ([]byte, error) {
	const printFlag = 4

	pdf := pdfgen.New()

	destination := pdf.Stream(iccRGBDisplayProfile())
	destination.Set("N", 3)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho RGB display"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	glyphs := []type3Proc{
		{"A", 600, "0 0 500 700 re f"},
		{"B", 600, "0 0 500 700 re 100 100 300 500 re f*"},
		{"C", 600, "0 0 m 500 0 l 250 700 l f"},
		{"D", 600, "0 300 500 150 re f 175 0 150 700 re f"},
		{"E", 600, "200 0 120 700 re f 0 580 500 120 re f"},
	}
	widths := make([]any, len(glyphs))
	mapping := map[int]int{}
	for i, g := range glyphs {
		widths[i] = 600
		mapping[65+i] = int(g.Name[0])
	}
	font := type3Font(pdf, glyphs, widths)
	font.Set("ToUnicode", toUnicodeCMap(pdf, mapping))
	font = pdf.Indirect(font)

	form := func(content string, bbox *pdfgen.Array, resources *pdfgen.Dict) *pdfgen.Stream {
		s := pdf.Stream([]byte(content))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", bbox)
		s.Set("Resources", resources)
		pdf.MakeIndirect(s)
		return s
	}

	state := func(kv ...any) *pdfgen.Dict {
		return pdf.Indirect(d(append([]any{"Type", n("ExtGState")}, kv...)...))
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"Parent", pdf.Pages,
		"MediaBox", a(0, 0, 200, 100),
		"Resources", d(),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte{})),
	))
	pdf.Pages.Set("Kids", a(page))
	pdf.Pages.Set("Count", 1)

	square := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(10, 10, 60, 60),
		"P", page,
		"F", printFlag,
		"CA", 0.5,
		"C", a(0, 0, 1),
		"Contents", text("A faded square"),
		"AP", d("N", form("/H gs 0 0 1 rg 0 0 50 50 re f\n", a(0, 0, 50, 50), d("ExtGState", d("H", state("ca", 0.5))))),
	))
	freeText := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("FreeText"),
		"Rect", a(80, 20, 190, 80),
		"P", page,
		"F", printFlag,
		"Contents", text("DEA"),
		"DA", text("0 0 1 rg /F 16 Tf"),
		"AP", d(
			"N", form(
				"q /T gs 0.8 0.9 1 rg 0 0 110 60 re f Q BT /F 16 Tf 0 0 1 rg 10 22 Td (DEA) Tj ET\n",
				a(0, 0, 110, 60),
				d("Font", d("F", font), "ExtGState", d("T", state("ca", 0.6))),
			),
		),
	))

	page.Set("Annots", a(square, freeText))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	return save(pdf, "1.7")
}
