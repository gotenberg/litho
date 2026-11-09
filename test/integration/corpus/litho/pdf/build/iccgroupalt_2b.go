package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("iccgroupalt_2b", iccgroupalt2b) }

// iccgroupalt2b builds iccgroupalt_2b.pdf: transparency groups blending in, and transparent
// text drawn in, ICCBased spaces whose profiles no colour engine can use, each
// with an /Alternate of another family than its /N names, at PDF/A-2b.
//
// ISO 32000-1, 8.6.5.5, Table 66 has a reader that cannot use an ICCBased
// space's profile use the stream's /Alternate, with the colour values as they
// are. A transparency group blends in the space its /CS names (11.3.4, Table
// 147), which admits no special space, and flattening composites transparent
// text over white paper in the text's own space (11.3.7.2), where the paper is
// the alternate's. No corpus file reached either: the only unusable profiles
// in a group's /CS or under transparent text name no /Alternate, or the device
// space of /N.
//
// Each profile is a version 2.1 header with an empty tag table, as
// iccalternate_2b.pdf's are: veraPDF parses it, but it holds no transform, so
// every reader falls back to its /Alternate. Every glyph is a Type 3 glyph
// painting in the text's colour, with a /ToUnicode map:
//
// | Page | Draws                                                                    |
// | ---- | ------------------------------------------------------------------------ |
// | 1    | an isolated group whose /CS has a CMYK header and /Alternate DeviceCMYK, |
// |      | blue at ca 0.6 over yellow, and an isolated group whose /CS has a GRAY   |
// |      | header and a Separation /Alternate, red then green Multiply              |
// | 2    | text at ca 0.5 in the GRAY-header space, tint 0.6, over the bare page    |
//
// See ../FIXTURES.md for the authoritative expected firing set.
func iccgroupalt2b() ([]byte, error) {
	pdf := pdfgen.New()

	iccbased := func(colourSpace string, components int, alternate any) *pdfgen.Array {
		profile := pdf.Stream(iccMinimalProfile("mntr", colourSpace, iccVersion21, "\x00\x00\x00\x00"))
		profile.Set("N", components)
		profile.Set("Alternate", alternate)
		return a(n("ICCBased"), pdf.MakeIndirect(profile))
	}

	magentaTint := d("FunctionType", 2, "Domain", a(0, 1), "C0", a(0, 0, 0, 0), "C1", a(0, 1, 0, 0), "N", 1)
	cmykLike := iccbased("CMYK", 4, n("DeviceCMYK"))
	spotLike := iccbased("GRAY", 1, a(n("Separation"), n("Spot"), n("DeviceCMYK"), magentaTint))

	glyphs := []type3Proc{
		{"A", 600, "0 0 500 700 re f"},
		{"B", 600, "0 0 500 700 re 100 100 300 500 re f*"},
		{"C", 600, "0 0 m 500 0 l 250 700 l f"},
	}
	widths := make([]any, len(glyphs))
	for i := range glyphs {
		widths[i] = 600
	}
	font := type3Font(pdf, glyphs, widths, 65)
	toUnicode := map[int]int{}
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
		stream.Set("Group", group)
		stream.Set("Resources", resources)
		return pdf.MakeIndirect(stream)
	}

	transparency := func(attrs ...any) *pdfgen.Dict {
		return d(append([]any{"Type", n("Group"), "S", n("Transparency")}, attrs...)...)
	}

	state := func(entries ...any) *pdfgen.Dict {
		return pdf.Indirect(d(append([]any{"Type", n("ExtGState")}, entries...)...))
	}

	addPage := func(content string, resources *pdfgen.Dict) {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 100),
			"Resources", resources,
			"Contents", pdf.Stream([]byte(content)),
		)
		pdf.AddPage(pdf.Indirect(page))
	}

	overYellow := form(
		"0 0 1 0 k 0 0 80 80 re f /H gs 0 0 1 rg 20 20 60 60 re f\n",
		[]any{0, 0, 80, 80},
		transparency("I", true, "CS", cmykLike),
		d("ExtGState", d("H", state("ca", 0.6))),
	)
	multiply := form(
		"1 0 0 rg 0 0 60 80 re f /M gs 0 1 0 rg 20 0 60 80 re f\n",
		[]any{0, 0, 80, 80},
		transparency("I", true, "CS", spotLike),
		d("ExtGState", d("M", state("BM", n("Multiply")))),
	)
	addPage(
		"q 1 0 0 1 10 10 cm /Y Do Q q 1 0 0 1 110 10 cm /M Do Q\n",
		d("XObject", d("Y", overYellow, "M", multiply)),
	)
	addPage(
		"q /Half gs /S cs 0.6 sc BT /F 30 Tf 10 40 Td (ABCABC) Tj ET Q\n",
		d(
			"Font", d("F", font),
			"ColorSpace", d("S", spotLike),
			"ExtGState", d("Half", state("ca", 0.5, "CA", 0.5)),
		),
	)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
