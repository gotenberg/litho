package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("pagelabels_2b", pagelabels2b) }

// pagelabels2b builds pagelabels_2b.pdf: page labels in every numbering style, under the
// page edits, at PDF/A-2b.
//
// ISO 32000-1, 12.4.2 makes /PageLabels a number tree keyed by page index, each
// range giving its pages a style, a prefix and a start (Table 159), so an edit
// that moves, removes or adds a page changes what every later page displays. A
// split and a merge used to drop the tree, and page insertion and deletion kept
// it as it was, keyed to pages that had moved. No litho fixture carried labels.
//
// Eight pages, each showing its own text, carry one range per style:
//
// | Page | Shows | Label   | Range                                                 |
// | ---- | ----- | ------- | ----------------------------------------------------- |
// | 1    | `A`   | `i`     | lowercase roman                                       |
// | 2    | `B`   | `ii`    |                                                       |
// | 3    | `AA`  | `Cover` | a prefix with no numbering style                      |
// | 4    | `AB`  | `A-7`   | decimal, prefix `A-`, start 7, an object of its own   |
// | 5    | `BA`  | `A-8`   |                                                       |
// | 6    | `BB`  | `a`     | lowercase letters                                     |
// | 7    | `AAA` | `b`     |                                                       |
// | 8    | `AAB` | `Ü-IV`  | uppercase roman, a UTF-16BE prefix `Ü-`, start 4      |
//
// The tree is a root with /Kids over two leaves, each carrying its /Limits (ISO
// 32000-1, 7.9.7, Table 37), and the range of page 4 is an indirect page label
// dictionary with its optional /Type. mutate-pages moves page 1 to the end, and
// each page has to keep its label there.
//
// The file conforms to PDF/A-2b: the text is in the conforming embedded
// TrueType of ttcmap_2b.pdf, in the initial DeviceGray a CMYK OutputIntent
// covers, as in baretree_2b.pdf, so no colour or font clause fires.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func pagelabels2b() ([]byte, error) {
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
		"FontName", n("/LithoLabelSans"),
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
		"BaseFont", n("/LithoLabelSans"),
		"FirstChar", first,
		"LastChar", last,
		"Widths", widths,
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))
	fontRes := d("Font", d("F1", font))

	for _, word := range []string{"A", "B", "AA", "AB", "BA", "BB", "AAA", "AAB"} {
		content := pdf.Stream([]byte("BT /F1 24 Tf 20 100 Td (" + word + ") Tj ET\n"))
		pdf.AddPage(pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", fontRes,
			"Contents", pdf.MakeIndirect(content),
		)))
	}

	// ISO 32000-1, 7.9.2.2: a text string may be UTF-16BE behind its byte order
	// mark, here U+00DC and a hyphen.
	annexPrefix := str("\xfe\xff\x00\xdc\x00\x2d")
	decimalRange := pdf.Indirect(d("Type", n("PageLabel"), "S", n("D"), "P", text("A-"), "St", 7))
	firstLeaf := pdf.Indirect(d(
		"Limits", a(0, 3),
		"Nums", a(
			0,
			d("S", n("r")),
			2,
			d("P", text("Cover")),
			3,
			decimalRange,
		),
	))
	secondLeaf := pdf.Indirect(d(
		"Limits", a(5, 7),
		"Nums", a(5, d("S", n("a")), 7, d("S", n("R"), "P", annexPrefix, "St", 4)),
	))
	pdf.Root.Set("PageLabels", pdf.Indirect(d("Kids", a(firstLeaf, secondLeaf))))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
