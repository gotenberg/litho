package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("structsplit_ua1", structsplitUa1) }

// structsplitUa1 builds structsplit_ua1.pdf: structure elements a redaction cuts.
//
// A region redaction removes the glyphs it covers and keeps the BDC and EMC
// around them, so a structure element whose marked content the region crosses
// keeps tagging what survived (ISO 32000-1, 14.7.4.2). What a cut cannot divide
// is the text an element holds itself: /ActualText, /Alt and /E describe the
// element's whole content, its descendants' included (14.9.3 to 14.9.5).
//
// Page 1 places every element the centred quarter of the page crosses, and none
// of them holds text the cut divides:
//
//   - a paragraph, MCID 1, one line whose middle words the quarter takes;
//   - a table row whose second cell, MCID 3, lies inside the quarter while its
//     first, MCID 2, lies outside;
//   - a paragraph of two spans under a division, the first span, MCID 4, inside
//     the quarter and the second, MCID 5, outside;
//   - a division whose /ActualText describes one span, MCID 6, wholly inside the
//     quarter, which the redaction clears as it removes the span.
//
// Page 2 is the refusal that stays: one paragraph, MCID 0, holding its words in
// /ActualText, whose line the centred quarter cuts.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func structsplitUa1() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// run is one marked-content sequence drawing text at (x, y).
	run := func(mcid int, tag string, x, y int, s string, size int) string {
		return fmt.Sprintf("/%s <</MCID %d>> BDC BT /F1 %d Tf %d %d Td (%s) Tj ET EMC\n", tag, mcid, size, x, y, s)
	}

	page1Content := run(0, "H1", 72, 720, "Structure split", 18) +
		run(1, "P", 72, 500, "Kept before the cut, then these removed middle words, and the words kept after the cut", 12) +
		run(2, "TD", 72, 420, "Name", 12) +
		run(3, "TD", 300, 420, "Hidden value", 12) +
		run(4, "Span", 200, 340, "inner span", 12) +
		run(5, "Span", 470, 340, "outer span", 12) +
		run(6, "Span", 200, 260, "inner words", 12)
	page2Content := run(0, "P", 72, 396, "Left side words SECRET right side words", 12)

	var pages []*pdfgen.Dict
	for number, data := range []string{page1Content, page2Content} {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d("Font", d("F1", font)),
			"Contents", pdf.Stream([]byte(data)),
			"StructParents", number,
		))
		pdf.AddPage(page)
		pages = append(pages, page)
	}
	page1, page2 := pages[0], pages[1]

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))

	// elem is a structure element of page, its /K set to kids.
	elem := func(tag string, parent *pdfgen.Dict, kids any, page *pdfgen.Dict, extra ...any) *pdfgen.Dict {
		e := pdf.Indirect(d(append([]any{"Type", n("StructElem"), "S", n(tag), "P", parent, "Pg", page}, extra...)...))
		e.Set("K", kids)
		return e
	}

	heading := elem("/H1", document, 0, page1)
	para := elem("/P", document, 1, page1)
	table := elem("/Table", document, a(), page1)
	row := elem("/TR", table, a(), page1)
	cellKept := elem("/TD", row, 2, page1)
	cellCut := elem("/TD", row, 3, page1)
	row.Set("K", a(cellKept, cellCut))
	table.Set("K", a(row))

	nest := elem("/Div", document, a(), page1)
	nestPara := elem("/P", nest, a(), page1)
	spanIn := elem("/Span", nestPara, 4, page1)
	spanOut := elem("/Span", nestPara, 5, page1)
	nestPara.Set("K", a(spanIn, spanOut))
	nest.Set("K", a(nestPara))

	described := elem("/Div", document, a(), page1, "ActualText", text("inner words"))
	describedPara := elem("/P", described, a(), page1)
	spanWhole := elem("/Span", describedPara, 6, page1)
	describedPara.Set("K", a(spanWhole))
	described.Set("K", a(describedPara))

	spanning := elem(
		"/P", document, 0, page2, "ActualText", text("Left side words SECRET right side words"),
	)

	document.Set("K", a(heading, para, table, nest, described, spanning))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(
			0,
			a(heading, para, cellKept, cellCut, spanIn, spanOut, spanWhole),
			1,
			a(spanning),
		),
	)))
	structRoot.Set("ParentTreeNextKey", 2)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))

	return save(pdf, "1.7")
}
