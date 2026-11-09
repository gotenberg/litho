package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("defaultbox_2b", defaultbox2b) }

// defaultbox2b builds defaultbox_2b.pdf: an A4 document whose pages miss their media box, at
// PDF/A-2b.
//
// ISO 32000-1, 7.7.3.3, Table 30 makes /MediaBox required and inheritable, and
// gives it no default. A viewer draws a page that neither states nor inherits a
// box it uses on US Letter, and litho reads it so. A write that gives such a page
// a media box gives it the size most pages of the document have instead, here
// A4, 595.28 by 841.89, at origin 0 0: US Letter only when no page has a usable
// box. No corpus file holds a page missing its media box among pages of another
// size a majority shares but pdfjs boundingBox_invalid.pdf, whose sizes tie.
//
// | Page | Its box, or its node's         | A viewer shows | A write gives it         |
// | ---- | ------------------------------ | -------------- | ------------------------ |
// | 1    | A4                             | A4             | A4                       |
// | 2    | A4                             | A4             | A4                       |
// | 3    | none                           | US Letter      | A4                       |
// | 4    | US Letter                      | US Letter      | US Letter, the control   |
// | 5    | none, /CropBox [0 0 590 830]   | 590 by 792     | A4, the crop box 590x830 |
// | 6    | its node's /MediaBox [0 0 0 0] | US Letter      | A4, on the node          |
// | 7    | A4                             | A4             | A4                       |
//
// Page 5's crop box reaches past US Letter, which a viewer clips it to, and fits
// A4, which the write clips it against. Page 6's node states a box enclosing no
// area, which ISO 19005-2, 6.1.13 t11 fails and the PDF/A remediation repairs. Each
// page draws its own word at (20, 100), inside every size, in the conforming
// embedded TrueType of ttcmap_2b.pdf, in DeviceGray under a CMYK OutputIntent,
// and states its own /Resources, so no colour, font or resource clause fires.
//
// The tree is built by hand: pikepdf's page list flattens it, which would move
// the node's box onto its page.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func defaultbox2b() ([]byte, error) {
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
		"FontName", n("/LithoBoxSans"),
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
	widths := make([]any, last-first+1)
	for i := range widths {
		widths[i] = ttAdvanceWidth
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoBoxSans"),
		"FirstChar", first,
		"LastChar", last,
		"Widths", a(widths...),
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))

	a4 := []any{0, 0, 595.28, 841.89}
	letter := []any{0, 0, 612, 792}

	root := pdf.Indirect(d("Type", n("Pages")))

	page := func(parent *pdfgen.Dict, word string, boxes ...any) *pdfgen.Dict {
		ops := []byte("BT /F1 24 Tf 20 100 Td (" + word + ") Tj ET\n")
		dd := d(
			"Type", n("Page"),
			"Parent", parent,
			"Contents", pdf.MakeIndirect(pdf.Stream(ops)),
			// A page's own, since ISO 19005-2, 6.2.2 t2 asks for resources
			// the content stream names explicitly.
			"Resources", d("Font", d("F1", font)),
		)
		for i := 0; i < len(boxes); i += 2 {
			dd.Set(boxes[i].(string), boxes[i+1])
		}
		return pdf.Indirect(dd)
	}

	node := pdf.Indirect(d("Type", n("Pages"), "Parent", root, "MediaBox", a(0, 0, 0, 0)))
	page6 := page(node, "BAB")
	node.Set("Kids", a(page6))
	node.Set("Count", 1)

	root.Set("Kids", a(
		page(root, "A", "MediaBox", a(a4...)),
		page(root, "B", "MediaBox", a(a4...)),
		page(root, "AB"),
		page(root, "BA", "MediaBox", a(letter...)),
		page(root, "AA", "CropBox", a(0, 0, 590, 830)),
		node,
		page(root, "BB", "MediaBox", a(a4...)),
	))
	root.Set("Count", 7)
	pdf.Root.Set("Pages", root)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
