package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("baretree_2b", baretree2b) }

// baretree2b builds baretree_2b.pdf: a page tree short of the entries ISO 32000-1 requires
// of it, which every rewrite has to write, at PDF/A-2b.
//
// ISO 32000-1, 7.7.2, Table 28 makes the catalog's /Pages an indirect reference;
// 7.7.3.2, Table 29 makes a page tree node's /Type /Pages and a /Count of the
// pages under it required; 7.7.3.3, Table 30 makes a page's /Parent, /Resources
// and /MediaBox required, the last two inheritable, the box a rectangle (7.9.5).
// A rewrite used to copy each of these as the source had it, and page insertion
// and deletion refused a root with no object number. The corpus files that lack
// /Type on the root or state a wrong /Count are damaged past what veraPDF parses
// (pdfjs issue9105_other.pdf, Pages-tree-refs.pdf, the poppler fuzz files), and
// none writes the root directly in the catalog.
//
// The catalog writes the root directly, so no page can name it in /Parent. The
// root has no /Type, lends no /MediaBox and no /Resources, and counts four of its
// five pages:
//
// | Page | Its own box              | Its own /Resources | Shows   |
// | ---- | ------------------------ | ------------------ | ------- |
// | 1    | 200 by 200               | the font           | `A`     |
// | 2    | 200 by 200, by reference | the font           | `B`     |
// | 3    | 200 by 200               | none               | nothing |
// | 4    | 200 by 200               | `5`, no dictionary | nothing |
// | 5    | none                     | the font           | `AB`    |
//
// Page 2 is the control: its box is a rectangle written by reference, and stays
// as it is. Pages 3 and 4 have no /Contents: veraPDF reports a page with no
// /Resources whose content paints anything, DeviceGray included, under 6.2.2 t3
// at PDF/A-4, which litho does not. The text is in the conforming embedded
// TrueType of ttcmap_2b.pdf, in the initial DeviceGray a CMYK OutputIntent
// covers, so no colour or font clause fires.
//
// veraPDF 1.30.2 refuses a file holding a page tree node or a page with no
// /Type, a root with no /Count or with a /Count larger than its pages, so those
// shapes have no fixture. Two more are left out because veraPDF reads them in a
// way litho does not, which would hide the constructs behind a divergence: with
// no /Type on the catalog veraPDF reads no XMP metadata from it, and a /MediaBox
// of three numbers it reads with the missing coordinate as 0, reporting 6.1.13
// t11, which litho does not judge on a box short of four numbers. pdfjs
// issue5334.pdf reaches the catalog's /Type in the corpus, and pdfjs
// issue15150.pdf a /Resources naming no dictionary.
//
// The tree is built by hand: pikepdf's page list flattens it, which would delete
// the constructs the fixture exists for.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func baretree2b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccVersion21 = 0x02100000

	// iccProfile is a structurally minimal ICC profile, as iccbased_4.go
	// builds it.
	iccProfile := func(deviceClass, colourSpace, tag string) []byte {
		return iccMinimalProfile(deviceClass, colourSpace, iccVersion21, tag)
	}

	destination := pdf.Stream(iccProfile("prtr", "CMYK", "OUTI"))
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(
			d(
				"Type", n("OutputIntent"),
				"S", n("GTS_PDFA1"),
				"OutputConditionIdentifier", text("litho destination"),
				"DestOutputProfile", pdf.MakeIndirect(destination),
			),
		),
	))

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoBareSans"),
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
	for i := first; i <= last; i++ {
		widths.Append(ttAdvanceWidth)
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoBareSans"),
		"FirstChar", first,
		"LastChar", last,
		"Widths", widths,
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))
	box := a(0, 0, 200, 200)

	// page is a page naming no /Parent, since the root it sits under has no
	// number. One with no ops is blank and has no /Contents.
	page := func(ops string, entries ...any) *pdfgen.Dict {
		dd := d("Type", n("Page"))
		for i := 0; i < len(entries); i += 2 {
			dd.Set(entries[i].(string), entries[i+1])
		}
		if ops != "" {
			dd.Set("Contents", pdf.MakeIndirect(pdf.Stream([]byte(ops))))
		}
		return pdf.Indirect(dd)
	}

	show := func(word string) string {
		return "BT /F1 24 Tf 20 100 Td (" + word + ") Tj ET\n"
	}

	fontRes := d("Font", d("F1", font))

	pages := []any{
		page(show("A"), "MediaBox", box, "Resources", fontRes),
		// ISO 32000-1, 7.3.10: any object may be indirect, the box included, and it
		// is still a rectangle.
		page(show("B"), "MediaBox", pdf.MakeIndirect(a(0, 0, 200, 200)), "Resources", fontRes),
		page("", "MediaBox", box),
		page("", "MediaBox", box, "Resources", 5),
		page(show("AB"), "Resources", fontRes),
	}

	// The root, written directly in the catalog, with no /Type and a /Count one
	// short.
	pdf.Root.Set("Pages", d("Kids", a(pages...), "Count", len(pages)-1))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
