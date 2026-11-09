package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("flattentext_2b", flattentext2b) }

// flattentext2b builds flattentext_2b.pdf: transparency over, under and on text, which
// transparency flattening keeps as text, at PDF/A-2b.
//
// Writer.FlattenTransparency composites each region of transparent content into
// an opaque image and keeps every text object as text, drawn above the image
// (the R4 decision). The corpora hold transparency, but no blend mode drawn over
// text, no transparent text over content, no transparency group showing text,
// and no text under a constant alpha of 0, so the flatten cell never saw what
// flattening does with text. Every glyph is a Type 3 glyph whose description
// paints in the text's colour, with a /ToUnicode map, so the text extracts and
// draws the same in every renderer:
//
// | Page | Draws                                                              |
// | ---- | ------------------------------------------------------------------ |
// | 1    | text at ca 0.5 and Multiply text on the bare page, and a square    |
// |      | at ca 0.5 beside them                                              |
// | 2    | a knockout group of two squares at ca 0.6 with text between them,  |
// |      | and a non-isolated group holding an isolated one that shows text   |
// |      | and a square under a luminosity soft mask                          |
// | 3    | text under ca 0 over a grey page, text stroked under CA 0 and      |
// |      | filled, and a square at ca 0.5                                     |
// | 4    | a line of text, then a Multiply rectangle and a Screen rectangle   |
// |      | over parts of it                                                   |
// | 5    | text at ca 0.5 over a blue band, and text under a luminosity soft  |
// |      | mask                                                               |
//
// What flattening makes of each, by ISO 32000-2, 11: the text on page 1's bare
// page is drawn exactly in its colour composited over white (Normal and Multiply
// give the source colour over white, 11.3.5); page 2's groups are composited and
// their text drawn above, kept hidden where the source hid it; page 3's
// invisible text is drawn in rendering mode 3 and the stroked
// text in mode 0, both exact. Pages 4 and 5 hold what flattening approximates and
// reports: the text the Screen rectangle tinted draws above it untinted (the
// Multiply one leaves black text black, which stays exact), the text over the
// band and the soft-masked text draw opaque. The flatten cell renders the first three
// pages; its text and transparency checks read all five.
//
// The file declares no OutputIntent and its pages no /Group, so every page that
// paints DeviceRGB fails ISO 19005-2, 6.2.4.3 t2 and every page that holds
// transparency fails 6.2.10 t2.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func flattentext2b() ([]byte, error) {
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
	for i := range glyphs {
		widths[i] = 600
	}
	font := type3Font(pdf, glyphs, widths, 65)
	for i, g := range glyphs {
		toUnicode[65+i] = int(g.Name[0])
	}
	font.Set("ToUnicode", toUnicodeCMap(pdf, toUnicode))
	pdf.Indirect(font)

	form := func(content string, bbox []any, group, resources *pdfgen.Dict) pdfgen.Object {
		stream := pdf.Stream([]byte(content))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(bbox...))
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

	state := func(entries ...any) *pdfgen.Dict {
		return pdf.Indirect(d(append([]any{"Type", n("ExtGState")}, entries...)...))
	}

	addPage := func(content string, resources *pdfgen.Dict) {
		resources.Set("Font", d("F", font))
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 100),
			"Resources", resources,
			"Contents", pdf.Stream([]byte(content)),
		)
		pdf.AddPage(pdf.Indirect(page))
	}

	ramp := d(
		"ShadingType", 2,
		"ColorSpace", n("DeviceGray"),
		"Coords", a(0, 0, 200, 0),
		"Function", d("FunctionType", 2, "Domain", a(0, 1), "C0", a(0.2), "C1", a(1), "N", 1),
	)
	luminosity := form("/Sh sh\n", []any{0, 0, 200, 100}, transparency("CS", n("DeviceGray")), d("Shading", d("Sh", ramp)))
	soft := state("SMask", d("Type", n("Mask"), "S", n("Luminosity"), "G", luminosity))
	half := state("ca", 0.5, "CA", 0.5)

	addPage(
		"q /Half gs 1 0 0 rg BT /F 20 Tf 10 60 Td (ABCDE) Tj ET Q\n"+
			"q /MulT gs 0 0.6 0 rg BT /F 20 Tf 10 20 Td (ABC) Tj ET Q\n"+
			"q /Half gs 0 0 1 rg 140 20 40 60 re f Q\n",
		d("ExtGState", d("Half", half, "MulT", state("BM", n("Multiply"), "ca", 0.7))),
	)

	textResources := d("Font", d("F", font), "ExtGState", d("G", state("ca", 0.6), "Soft", soft))
	knockout := form(
		"/G gs 1 0 0 rg 5 5 40 40 re f BT /F 16 Tf 0 g 8 20 Td (ABC) Tj ET /G gs 0 0 1 rg 25 25 40 40 re f\n",
		[]any{0, 0, 80, 80},
		transparency("K", true),
		textResources,
	)
	isolated := form(
		"BT /F 16 Tf 0 0 0.5 rg 5 50 Td (DEA) Tj ET q /Soft gs 0 0.6 0 rg 0 0 80 45 re f Q\n",
		[]any{0, 0, 80, 80},
		transparency("I", true),
		textResources,
	)
	outer := form("0.9 0.9 0.5 rg 0 0 80 20 re f /I Do\n", []any{0, 0, 80, 80}, transparency(), d("XObject", d("I", isolated)))
	addPage(
		"q 1 0 0 1 10 10 cm /K Do Q q 1 0 0 1 110 10 cm /O Do Q\n",
		d("XObject", d("K", knockout, "O", outer)),
	)

	addPage(
		"0.8 g 0 0 200 100 re f\n"+
			"q /Clear gs 0 g BT /F 20 Tf 10 60 Td (ABCDE) Tj ET Q\n"+
			"q /StrokeClear gs 2 Tr 1 0 0 rg 0 0 1 RG BT /F 20 Tf 10 20 Td (ABC) Tj ET Q\n"+
			"q /Half gs 0 0 1 rg 120 10 60 80 re f Q\n",
		d("ExtGState", d("Clear", state("ca", 0), "StrokeClear", state("CA", 0), "Half", half)),
	)

	addPage(
		"BT /F 20 Tf 0 g 10 40 Td (ABCDEABCDE) Tj ET\n"+
			"q /Mul gs 1 1 0 rg 5 35 60 30 re f Q q /Scr gs 0 0 1 rg 70 35 60 30 re f Q\n",
		d("ExtGState", d("Mul", state("BM", n("Multiply")), "Scr", state("BM", n("Screen"), "ca", 0.8))),
	)

	addPage(
		"0 0 1 rg 0 0 200 35 re f q /Half gs 1 0 0 rg BT /F 20 Tf 10 10 Td (ABCDE) Tj ET Q\n"+
			"q /Soft gs 0 g BT /F 20 Tf 10 60 Td (DEABC) Tj ET Q\n",
		d("ExtGState", d("Half", half, "Soft", soft)),
	)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
