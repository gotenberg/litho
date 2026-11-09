package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("structcopy_ua2", structcopyUa2) }

// structcopyUa2 builds structcopy_ua2.pdf: a tagged document whose structure tree reaches
// every part a copy of its pages carries.
//
// Split, merge and page insertion carry the structure elements that describe the
// pages they copy, and the entries of the structure tree root that name those
// elements. No corpus file packs all of those entries into one tree over several
// pages, so this file does, each one reached by content on a page a copy keeps
// or leaves out:
//
//   - a root `/RoleMap` mapping the custom type `Para` to `P` for the elements
//     in the default namespace (ISO 32000-1, 14.7.3);
//   - a `/ClassMap` defining the `Centered` class one paragraph names (14.7.5.2);
//   - an `/IDTree` listing the identifiers of a heading, two paragraphs and the
//     two header cells whose identifiers the data cells name in `/Headers`
//     (Table 323, Table 349);
//   - two `/Namespaces`: the PDF 2.0 one the Document element names, and a
//     custom one whose `/RoleMapNS` maps `Callout` to the PDF 2.0 `Aside`
//     (ISO 32000-2, 14.7.4.2);
//   - a Link element on page 1 and one on page 3, each enclosing its link
//     annotation through an object reference (14.7.4.3), whose `/StructParent`
//     keys the `/ParentTree` after the pages' `/StructParents`;
//   - a `Sect` naming no page whose paragraphs sit on pages 1 and 2, so a copy
//     of either page keeps part of it, and a table wholly on page 2.
//
// Each page opens with content its own elements describe, and the headings are
// `H1` on pages 1 and 3, so moving any page to the end leaves a reading order
// PDF/UA accepts: `mutate-pages` moves page 1 behind page 3, and the copy carries
// its elements. The font is embedded and draws only `A` and `B`, which is all the
// text uses, so no font rule masks the structure.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func structcopyUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"
	const customNS = "http://litho.example/structcopy"
	latin := map[int]string{0x41: "A", 0x42: "B"}

	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoStructCopy"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"Encoding", n("WinAnsiEncoding"),
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/LithoStructCopy"),
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

	marked := func(tag string, mcid, y int, s string) string {
		return fmt.Sprintf("/%s <</MCID %d>> BDC\nBT /F1 12 Tf 50 %d Td (%s) Tj ET\nEMC\n", tag, mcid, y, s)
	}

	linkAnnot := func(y int, uri, alt string) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("Link"),
			"Rect", a(50, y-4, 150, y+14),
			"Border", a(0, 0, 0),
			"F", 4,
			"Contents", text(alt),
			"A", d("S", n("URI"), "URI", text(uri)),
		))
	}

	link1 := linkAnnot(600, "https://example.com/one", "first link")
	link3 := linkAnnot(600, "https://example.com/three", "third link")
	link1.Set("StructParent", 3)
	link3.Set("StructParent", 4)

	// No word of page 1 occurs anywhere else, so a redaction of the page has no
	// copy left to refuse over.
	contents := []string{
		marked("H1", 0, 740, "AAAAAB") +
			marked("P", 1, 700, "AAAABB") +
			marked("P", 2, 660, "AAABBB") +
			marked("Link", 3, 600, "AABBBB"),
		marked("P", 0, 740, "BBBA") +
			marked("TH", 1, 700, "BA") +
			marked("TH", 2, 680, "BB") +
			marked("TD", 3, 660, "BAB") +
			marked("TD", 4, 640, "BBA"),
		marked("H1", 0, 740, "BABA") +
			marked("P", 1, 700, "BAAB") +
			marked("P", 2, 660, "BBBB") +
			marked("Link", 3, 600, "BABB"),
	}
	annots := [][]any{{link1}, {}, {link3}}
	var pages []*pdfgen.Dict
	for i, content := range contents {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d("Font", d("F1", font)),
			"Contents", pdf.Stream([]byte(content)),
			"StructParents", i,
			"Tabs", n("S"),
		))
		if len(annots[i]) > 0 {
			page.Set("Annots", a(annots[i]...))
		}
		pdf.AddPage(page)
		pages = append(pages, pdf.Page(i))
	}
	link1.Set("P", pages[0])
	link3.Set("P", pages[2])

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	customNs := pdf.Indirect(d(
		"Type", n("Namespace"),
		"NS", text(customNS),
		"RoleMapNS", d("Callout", a(n("Aside"), ns20)),
	))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "NS", ns20, "P", structRoot))

	elem := func(s string, parent, page *pdfgen.Dict, k any, extra ...any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n(s), "P", parent)
		if page != nil {
			dd.Set("Pg", page)
		}
		if k != nil {
			dd.Set("K", k)
		}
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		return pdf.Indirect(dd)
	}

	// Page 1 and the start of page 2, under a Sect naming no page.
	sect1 := elem("Sect", document, nil, nil)
	title1 := elem("H1", sect1, pages[0], 0, "ID", text("title-1"))
	intro := elem("Para", sect1, pages[0], 1, "ID", text("intro"))
	centered := elem("Para", sect1, pages[0], 2, "C", n("/Centered"))
	link1Elem := elem(
		"Link",
		sect1,
		pages[0],
		a(3, d("Type", n("OBJR"), "Obj", link1)),
		"Alt", text("first link"),
	)
	cont := elem("Para", sect1, pages[1], 0, "ID", text("cont"))
	sect1.Set("K", a(title1, intro, centered, link1Elem, cont))

	// The table, wholly on page 2.
	table := elem("Table", document, pages[1], nil)
	headRow := elem("TR", table, pages[1], nil)
	bodyRow := elem("TR", table, pages[1], nil)
	th1 := elem(
		"TH",
		headRow,
		pages[1],
		1,
		"ID", text("h1"),
		"A", d("O", n("Table"), "Scope", n("Column")),
	)
	th2 := elem(
		"TH",
		headRow,
		pages[1],
		2,
		"ID", text("h2"),
		"A", d("O", n("Table"), "Scope", n("Column")),
	)
	td1 := elem(
		"TD",
		bodyRow,
		pages[1],
		3,
		"A", d("O", n("Table"), "Headers", a(text("h1"))),
	)
	td2 := elem(
		"TD",
		bodyRow,
		pages[1],
		4,
		"A", d("O", n("Table"), "Headers", a(text("h2"))),
	)
	headRow.Set("K", a(th1, th2))
	bodyRow.Set("K", a(td1, td2))
	table.Set("K", a(headRow, bodyRow))

	// Page 3: a French paragraph and a callout in the custom namespace.
	sect3 := elem("Sect", document, pages[2], nil)
	title3 := elem("H1", sect3, pages[2], 0)
	french := elem("Para", sect3, pages[2], 1, "Lang", text("fr-FR"))
	callout := elem("Callout", sect3, pages[2], nil, "NS", customNs)
	asideP := elem("P", callout, pages[2], 2)
	callout.Set("K", a(asideP))
	link3Elem := elem(
		"Link",
		sect3,
		pages[2],
		a(3, d("Type", n("OBJR"), "Obj", link3)),
		"Alt", text("third link"),
	)
	sect3.Set("K", a(title3, french, callout, link3Elem))

	document.Set("K", a(sect1, table, sect3))
	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ns20, customNs))
	structRoot.Set("RoleMap", d("Para", n("P")))
	structRoot.Set("ClassMap", d(
		"Centered", d("O", n("Layout"), "TextAlign", n("Center")),
	))
	structRoot.Set("IDTree", pdf.Indirect(d(
		"Names", a(
			text("cont"),
			cont,
			text("h1"),
			th1,
			text("h2"),
			th2,
			text("intro"),
			intro,
			text("title-1"),
			title1,
		),
	)))
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(
			0,
			a(title1, intro, centered, link1Elem),
			1,
			a(cont, th1, th2, td1, td2),
			2,
			a(title3, french, asideP, link3Elem),
			3,
			link1Elem,
			4,
			link3Elem,
		),
	)))
	structRoot.Set("ParentTreeNextKey", 5)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	return save(pdf, "2.0")
}
