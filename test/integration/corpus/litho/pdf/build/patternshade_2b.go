package build

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("patternshade_2b", patternshade2b) }

// patternshade2b builds patternshade_2b.pdf: shadings a pattern or a Type 3 glyph reaches.
//
// A shading reaches the page through a shading pattern an scn selects (ISO
// 32000-1, 8.7.4.1) or through the sh operator (8.7.4.2), and either can sit in
// the cell of a tiling pattern (8.7.3.1) or in a Type 3 glyph description
// (9.6.5). veraPDF 1.30.2 judges the colour space of every such shading, the
// tint transform of a Separation or DeviceN space included, and judges neither
// the shading's /Function nor a shading pattern's own /ExtGState. No corpus file
// reaches a shading through a cell or a glyph, so this fixture plants one per
// path, each answering a rule nothing else in the file fires:
//
// | Page | Path                                 | Shading colour space               | Answers at PDF/A-2b     |
// | ---- | ------------------------------------ | ---------------------------------- | ----------------------- |
// | 1    | tiling cell, `/SH1 scn`              | DeviceRGB                          | `6.2.4.3/2`             |
// | 2    | Type 3 glyph on the page, `/SH2 scn` | Separation, type 4 tint 2147483648 | `6.1.13/1`              |
// | 3    | tiling cell, Type 3 glyph, `/Sh3 sh` | DeviceN of 33 colorants            | `6.1.13/9`, `6.2.4.4/1` |
//
// Every shading's /Function is a type 4 function whose program holds 2147483649
// and 40000.5, and SH1 carries an /ExtGState setting /TR /Identity and /BM
// /Foo: veraPDF judges none of them, so they are must-not-fire controls for
// 6.2.5 and 6.2.10, keep 6.1.13 t1 at the tint's single count, and keep 6.1.12
// t2 silent at PDF/A-1b.
//
// A CMYK OutputIntent covers DeviceCMYK and the DeviceGray a glyph shows in.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func patternshade2b() ([]byte, error) {
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

	// type4 is a PostScript calculator function of the given arity.
	type4 := func(program string, inputs, outputs int) pdfgen.Object {
		fn := pdf.Stream([]byte(program))
		fn.Set("FunctionType", 4)
		domain, rng := a(), a()
		for range inputs {
			domain.Append(0, 1)
		}
		for range outputs {
			rng.Append(0, 1)
		}
		fn.Set("Domain", domain)
		fn.Set("Range", rng)
		return pdf.MakeIndirect(fn)
	}

	// shading is an axial shading whose /Function only veraPDF's model leaves unread.
	shading := func(colourSpace any) *pdfgen.Dict {
		return pdf.Indirect(d(
			"ShadingType", 2,
			"ColorSpace", colourSpace,
			"Coords", a(0, 0, 20, 0),
			"Function", type4("{ 2147483649 pop 40000.5 pop }", 1, 1),
			"Extend", a(true, true),
		))
	}

	// tiling is a coloured tiling pattern carrying its own /Resources.
	tiling := func(content string, resources *pdfgen.Dict) pdfgen.Object {
		pat := pdf.Stream([]byte(content))
		pat.Set("Type", n("Pattern"))
		pat.Set("PatternType", 1)
		pat.Set("PaintType", 1)
		pat.Set("TilingType", 1)
		pat.Set("BBox", a(0, 0, 20, 20))
		pat.Set("XStep", 20)
		pat.Set("YStep", 20)
		pat.Set("Resources", resources)
		return pdf.MakeIndirect(pat)
	}

	// type3 is a one-glyph Type 3 font; the glyph sets its colour (d0).
	type3 := func(glyph string, resources *pdfgen.Dict) *pdfgen.Dict {
		proc := pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0\n" + glyph)))
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"FontBBox", a(0, 0, 1000, 1000),
			"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
			"CharProcs", d("A", proc),
			"Encoding", d("Type", n("Encoding"), "Differences", a(65, n("A"))),
			"FirstChar", 65,
			"LastChar", 65,
			"Widths", a(1000),
			"Resources", resources,
		))
	}

	sh1 := pdf.Indirect(d(
		"Type", n("Pattern"),
		"PatternType", 2,
		"Shading", shading(n("DeviceRGB")),
		"ExtGState", d("Type", n("ExtGState"), "TR", n("Identity"), "BM", n("/Foo")),
	))
	cell1 := tiling("q /Pattern cs /SH1 scn 0 0 20 20 re f Q\n", d("Pattern", d("SH1", sh1)))

	sh2 := pdf.Indirect(d(
		"Type", n("Pattern"),
		"PatternType", 2,
		"Shading", shading(
			a(
				n("Separation"),
				n("/Spot2"),
				n("DeviceCMYK"),
				type4("{ 2147483648 pop dup dup dup }", 1, 4),
			),
		),
	))
	glyph2 := type3("q /Pattern cs /SH2 scn 0 0 1000 1000 re f Q\n", d("Pattern", d("SH2", sh2)))

	colorants := a()
	for i := range 33 {
		colorants.Append(n(fmt.Sprintf("/C%02d", i)))
	}
	sh3 := shading(
		a(
			n("DeviceN"),
			colorants,
			n("DeviceCMYK"),
			type4("{ "+strings.Repeat("pop ", 33)+"0 0 0 0 }", 33, 4),
		),
	)
	glyph3 := type3("q /Sh3 sh Q\n", d("Shading", d("Sh3", sh3)))
	cell3 := tiling("q 0 g BT /T3 10 Tf 1 1 Td (A) Tj ET Q\n", d("Font", d("T3", glyph3)))

	addPage := func(content string, resources *pdfgen.Dict) {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", resources,
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
		))
		pdf.AddPage(page)
	}

	addPage("q /Pattern cs /P1 scn 0 0 100 100 re f Q\n", d("Pattern", d("P1", cell1)))
	addPage("q 0 g BT /T2 50 Tf 10 10 Td (A) Tj ET Q\n", d("Font", d("T2", glyph2)))
	addPage("q /Pattern cs /P3 scn 0 0 100 100 re f Q\n", d("Pattern", d("P3", cell3)))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
