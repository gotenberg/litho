package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("outlinedest_ua2", outlinedestUa2) }

// outlinedestUa2 builds outlinedest_ua2.pdf: a tagged document whose outline opens structure
// destinations in every form a reader resolves, over pages drawing optional
// content.
//
// Split and merge rebuild the outline of the pages they copy, and a page
// insertion copies a page's optional content into another document. No corpus
// file packs the forms of a structure destination an outline item can name
// (ISO 32000-2, 12.3.2.3) into one conforming file, and no corpus file names one
// through a named destination's dictionary, so this file does:
//
//   - a GoTo action naming page 1 under /D and its heading under /SD, both with
//     the same /XYZ view (ISO 32000-2, 12.6.4.2, Table 202);
//   - a child item whose /Dest is a structure destination array naming the
//     first paragraph of page 2, and no page;
//   - an item whose /Dest is a string naming a /Dests name tree entry whose
//     value is a dictionary carrying /D, page 3, and /SD, its heading
//     (ISO 32000-2, 12.3.2);
//   - an item whose GoTo action's /D is that same string.
//
// Page 1 draws a paragraph in a layer the default configuration shows and one in
// a layer it hides, and /OCProperties carries a second configuration in /Configs,
// each named and each ordering both groups (ISO 32000-1, 8.11.4.3; ISO 14289-2,
// 8.7; ISO 19005-2, 6.9), so `mutate-pages`, which copies page 1 to the end,
// joins the copied groups to both configurations.
//
// The font is embedded and draws only `A` and `B`, which is all the text uses.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func outlinedestUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"
	latin := map[int]string{0x41: "A", 0x42: "B"}

	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoOutlineDest"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"Encoding", n("WinAnsiEncoding"),
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/LithoOutlineDest"),
			"Flags", 32,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile2", pdf.MakeIndirect(fontFile),
		)),
	))

	marked := func(tag string, mcid, y int, s, layer string) string {
		ops := fmt.Sprintf("/%s <</MCID %d>> BDC\nBT /F1 12 Tf 50 %d Td (%s) Tj ET\nEMC\n", tag, mcid, y, s)
		if layer == "" {
			return ops
		}
		return fmt.Sprintf("/OC /%s BDC\n%sEMC\n", layer, ops)
	}

	shown := pdf.Indirect(d("Type", n("OCG"), "Name", text("Shown layer")))
	hidden := pdf.Indirect(d("Type", n("OCG"), "Name", text("Hidden layer")))

	contents := []string{
		marked("H1", 0, 740, "AAAB", "") +
			marked("P", 1, 700, "AABA", "Shown") +
			marked("P", 2, 660, "ABAA", "Hidden"),
		marked("P", 0, 740, "BAAA", "") + marked("P", 1, 700, "BBAA", ""),
		marked("H1", 0, 740, "BABA", "") + marked("P", 1, 700, "BBBA", ""),
	}
	pages := make([]*pdfgen.Dict, 0, len(contents))
	for i, content := range contents {
		resources := d("Font", d("F1", font))
		if i == 0 {
			resources.Set("Properties", d("Shown", shown, "Hidden", hidden))
		}
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", resources,
			"Contents", pdf.Stream([]byte(content)),
			"StructParents", i,
			"Tabs", n("S"),
		))
		pdf.AddPage(page)
		pages = append(pages, pdf.Page(i))
	}

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "NS", ns20, "P", structRoot))

	elem := func(s string, parent, page *pdfgen.Dict, k any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n(s), "P", parent, "NS", ns20)
		if page != nil {
			dd.Set("Pg", page)
		}
		if k != nil {
			dd.Set("K", k)
		}
		return pdf.Indirect(dd)
	}

	sect1 := elem("Sect", document, nil, nil)
	title1 := elem("H1", sect1, pages[0], 0)
	shownP := elem("P", sect1, pages[0], 1)
	hiddenP := elem("P", sect1, pages[0], 2)
	sect1.Set("K", a(title1, shownP, hiddenP))

	sect2 := elem("Sect", document, nil, nil)
	cont := elem("P", sect2, pages[1], 0)
	more := elem("P", sect2, pages[1], 1)
	sect2.Set("K", a(cont, more))

	sect3 := elem("Sect", document, nil, nil)
	title3 := elem("H1", sect3, pages[2], 0)
	last := elem("P", sect3, pages[2], 1)
	sect3.Set("K", a(title3, last))

	document.Set("K", a(sect1, sect2, sect3))
	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ns20))
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(
			0,
			a(title1, shownP, hiddenP),
			1,
			a(cont, more),
			2,
			a(title3, last),
		),
	)))
	structRoot.Set("ParentTreeNextKey", 3)

	// The named destination the third and fourth items name: a dictionary carrying
	// the page destination and the structure destination side by side.
	pdf.Root.Set("Names", d(
		"Dests", pdf.Indirect(d(
			"Names", a(
				text("third"),
				d(
					"D", a(pages[2], n("Fit")),
					"SD", a(title3, n("Fit")),
				),
			),
		)),
	))

	outlines := pdf.Indirect(d("Type", n("Outlines")))
	first := pdf.Indirect(d(
		"Title", text("Opening heading"),
		"Parent", outlines,
		"A", d(
			"S", n("GoTo"),
			"D", a(pages[0], n("XYZ"), 50, 752, nil),
			"SD", a(title1, n("XYZ"), 50, 752, nil),
		),
	))
	child := pdf.Indirect(d(
		"Title", text("Second page"),
		"Parent", first,
		"Dest", a(cont, n("Fit")),
	))
	third := pdf.Indirect(d("Title", text("Third page"), "Parent", outlines, "Dest", text("third")))
	fourth := pdf.Indirect(d(
		"Title", text("Third page again"),
		"Parent", outlines,
		"A", d("S", n("GoTo"), "D", text("third")),
	))
	first.Set("First", child)
	first.Set("Last", child)
	first.Set("Count", 1)
	first.Set("Next", third)
	third.Set("Prev", first)
	third.Set("Next", fourth)
	fourth.Set("Prev", third)
	outlines.Set("First", first)
	outlines.Set("Last", fourth)
	outlines.Set("Count", 4)

	pdf.Root.Set("Outlines", outlines)
	pdf.Root.Set("OCProperties", d(
		"OCGs", a(shown, hidden),
		"D", d(
			"Name", text("Default layers"),
			"Order", a(shown, hidden),
			"OFF", a(hidden),
		),
		"Configs", a(d("Name", text("All layers"), "Order", a(shown, hidden))),
	))
	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	return save(pdf, "2.0")
}
