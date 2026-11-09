package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("sizetie_2b", sizetie2b) }

// sizetie2b builds sizetie_2b.pdf: two font sizes of equal weight, under bookmark
// generation, at PDF/A-2b.
//
// Bookmark generation takes the body font size to be the size that sets the
// most characters, and calls a line a heading when it is set larger. Two sizes
// setting the same number of characters tie, and the tie used to break in the
// iteration order of a Go map, which changes from run to run: the outline came
// out with the heading or without it. No litho fixture held such a tie, so the
// gate's determinism cell, which runs each bookmark generation several times,
// never saw it.
//
// One page shows `AABB` at 48 points over `ABAB` at 12, four characters each.
// The tie breaks to the smaller size, so the body is 12 points and `AABB` is the
// one heading.
//
// The file conforms to PDF/A-2b: the text is in the conforming embedded
// TrueType of ttcmap_2b.pdf, in the initial DeviceGray a CMYK OutputIntent
// covers, as in pagelabels_2b.pdf, so no colour or font clause fires.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func sizetie2b() ([]byte, error) {
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

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoTieSans"),
		"Flags", 32, // non-symbolic, ISO 32000-1, Table 123
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontBBox", a(0, -200, 550, 700),
		"FontFile2", pdf.MakeIndirect(fontFile),
	)
	const first, last = 0x41, 0x42
	widths := a()
	for range last - first + 1 {
		widths.Append(ttAdvanceWidth)
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoTieSans"),
		"FirstChar", first,
		"LastChar", last,
		"Widths", widths,
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))

	// Four characters at each size: the weights tie.
	content := pdf.Stream([]byte(
		"BT /F1 48 Tf 72 600 Td (AABB) Tj ET\n" +
			"BT /F1 12 Tf 72 500 Td (ABAB) Tj ET\n",
	))
	pdf.AddPage(pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.MakeIndirect(content),
	)))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
