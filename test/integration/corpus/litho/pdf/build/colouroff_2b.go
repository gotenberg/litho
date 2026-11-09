package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("colouroff_2b", colouroff2b) }

// colouroff2b builds colouroff_2b.pdf: resource names a stream with its colour off never uses.
//
// ISO 19005-2, 6.2.2 test 2 fails a content stream that resolves a resource name
// only through the page's resources. A Type 3 glyph description past its `d1`
// ignores its colour operators (ISO 32000-1, 9.6.5, Table 113), and so does the
// cell of an uncoloured tiling pattern (8.7.3.3, Table 75). veraPDF processes
// none of `cs`, `CS`, `sh`, `scn` or `SCN` there, so it resolves none of their
// names and the stream uses no inherited name through them. No file of any
// corpus selects a colour resource the page alone defines from such a stream.
//
// Every stream below has no `/ColorSpace`, `/Shading` or `/Pattern` of its own,
// so each name falls back to the page's:
//
// | Stream                      | Names through the page                  | 6.2.2/2 |
// | --------------------------- | --------------------------------------- | ------- |
// | glyph of `T1`, past `d1`    | `/CS0 cs`                               | silent  |
// | glyph of `T2`, past `d1`    | `/Sh0 sh`                               | silent  |
// | glyph of `T3`, past `d1`    | `/Pattern cs /P0 scn`                   | silent  |
// | glyph of `T4`, after `d0`   | `/CS0 cs`                               | fires   |
// | cell of uncoloured `PUnc`   | `/CS0 cs`                               | silent  |
// | cell of coloured `PCol`     | `/CS0 cs`                               | fires   |
//
// Test 2 judges the stream, not the name, so each stream fails it once at most,
// and the two controls make it fire twice. Every glyph sits in a font of its own:
// veraPDF gives the glyphs of one Type 3 font a single set of inherited names.
//
// A form paints `PUnc`, not the page. Every tiling cell a stream paints begins in
// one state, which an uncoloured pattern turns off for all of them, so the page
// painting `PUnc` beside `PCol` would silence the coloured control too. The form
// carries its own resources, so it uses no inherited name itself.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func colouroff2b() ([]byte, error) {
	pdf := pdfgen.New()

	const fill = "0 0 5 5 re f\n"
	calRGB := a(n("CalRGB"), d("WhitePoint", a(0.9505, 1, 1.089)))

	// tiling is a tiling pattern with empty resources, so its names fall back to the page.
	tiling := func(paintType int, content string) pdfgen.Object {
		pat := pdf.Stream([]byte(content))
		pat.Set("Type", n("Pattern"))
		pat.Set("PatternType", 1)
		pat.Set("PaintType", paintType)
		pat.Set("TilingType", 1)
		pat.Set("BBox", a(0, 0, 10, 10))
		pat.Set("XStep", 10)
		pat.Set("YStep", 10)
		pat.Set("Resources", d())
		return pdf.MakeIndirect(pat)
	}

	// type3 is a Type 3 font of one glyph, with empty resources.
	type3 := func(glyph string) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"FontBBox", a(0, 0, 10, 10),
			"FontMatrix", a(0.1, 0, 0, 0.1, 0, 0),
			"CharProcs", d("a", pdf.MakeIndirect(pdf.Stream([]byte(glyph)))),
			"Encoding", d("Type", n("Encoding"), "Differences", a(97, n("/a"))),
			"FirstChar", 97,
			"LastChar", 97,
			"Widths", a(10),
			"Resources", d(),
		))
	}

	const d1 = "10 0 0 0 10 10 d1 "
	shading := pdf.Indirect(d(
		"ShadingType", 2,
		"ColorSpace", calRGB,
		"Coords", a(0, 0, 1, 0),
		"Function", d("FunctionType", 2, "Domain", a(0, 1), "C0", a(0, 0, 0), "C1", a(1, 1, 1), "N", 1),
	))
	uncoloured := tiling(2, "/CS0 cs 1 0 0 sc "+fill)

	form := pdf.Stream([]byte("/PS cs 0.5 /PUnc scn 0 0 10 10 re f\n"))
	form.Set("Type", n("XObject"))
	form.Set("Subtype", n("Form"))
	form.Set("BBox", a(0, 0, 10, 10))
	form.Set("Resources", d(
		"ColorSpace", d("PS", a(n("Pattern"), n("DeviceGray"))),
		"Pattern", d("PUnc", uncoloured),
	))

	resources := d(
		"Font", d(
			"T1", type3(d1+"/CS0 cs 1 0 0 sc "+fill),
			"T2", type3(d1+"/Sh0 sh\n"),
			"T3", type3(d1+"/Pattern cs /P0 scn "+fill),
			"T4", type3("10 0 d0 /CS0 cs 1 0 0 sc "+fill),
		),
		"ColorSpace", d("CS0", calRGB),
		"Shading", d("Sh0", shading),
		"Pattern", d("P0", tiling(1, "/CS0 cs 0 1 0 sc "+fill), "PCol", tiling(1, "/CS0 cs 0 0 1 sc "+fill)),
		"XObject", d("Fm", pdf.MakeIndirect(form)),
	)

	content := "q /CS0 cs 0 0 0 sc\n" +
		"BT /T1 10 Tf 10 150 Td (a) Tj /T2 10 Tf (a) Tj /T3 10 Tf (a) Tj /T4 10 Tf (a) Tj ET\n" +
		"Q\n" +
		"q /Pattern cs /PCol scn 10 100 20 20 re f Q\n" +
		"q 1 0 0 1 50 100 cm /Fm Do Q\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", resources,
		"Contents", pdf.Stream([]byte(content)),
	))
	pdf.AddPage(page)

	// Deliberately no /Metadata and no output intent.
	return save(pdf, "1.7")
}
