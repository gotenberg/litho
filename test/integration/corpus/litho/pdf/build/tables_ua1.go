package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("tables_ua1", tablesUa1) }

// tablesUa1 builds tables_ua1.pdf: a PDF/UA-1 fixture built around tables and lists.
//
// No other fixture in this corpus contains a /Table or an /L, so the table
// rules of ISO 14289-1, 7.5 and the deeply nested table and list rows of the
// ISO/TS 32005 Table 5 matrix are unreached by the rest of the corpus. Both are
// nested structures, which is where a check that stops at the first defect, or
// one that never descends past the outer element, hides a second finding.
//
// The file holds two tables. The first is defective and the second conforms, so
// a check that reports per document rather than per element is visible as a
// count, and a check that latches on the first table is visible as a missing
// rule on the second.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func tablesUa1() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(unembeddedTrueType())

	// Marked content: two table cells, two list items, one paragraph. Each shows
	// text so every element is a text item for the language rules.
	content := `/TH <</MCID 0>> BDC
BT /F1 12 Tf 50 720 Td (Header) Tj ET
EMC
/TD <</MCID 1>> BDC
BT /F1 12 Tf 150 720 Td (Cell) Tj ET
EMC
/TH <</MCID 2>> BDC
BT /F1 12 Tf 50 680 Td (Header2) Tj ET
EMC
/TD <</MCID 3>> BDC
BT /F1 12 Tf 150 680 Td (Cell2) Tj ET
EMC
/LBody <</MCID 4>> BDC
BT /F1 12 Tf 50 640 Td (First item) Tj ET
EMC
/LBody <</MCID 5>> BDC
BT /F1 12 Tf 50 620 Td (Second item) Tj ET
EMC
/P <</MCID 6>> BDC
BT /F1 12 Tf 50 580 Td (Trailing paragraph) Tj ET
EMC
`

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte(content)),
		"StructParents", 0,
	))
	pdf.AddPage(page)

	elem := func(stype string, parent *pdfgen.Dict, kids any, extra ...any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n(stype), "P", parent, "Pg", page)
		if kids != nil {
			dd.Set("K", kids)
		}
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		return pdf.Indirect(dd)
	}

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("Document"), "P", structRoot, "Lang", text("en-US")),
	)

	// Table one is defective: the TH carries no /Scope and no /ID, and the TD
	// carries no /Headers, so ISO 14289-1, 7.5 cannot determine the association
	// algorithmically.
	tableBad := elem("Table", document, nil)
	trBad := elem("TR", tableBad, nil)
	thBad := elem("TH", trBad, 0)
	tdBad := elem("TD", trBad, 1)
	trBad.Set("K", a(thBad, tdBad))
	tableBad.Set("K", a(trBad))

	// Table two conforms: the TH declares /Scope and an /ID, and the TD points at
	// it through /Headers. A firing on this table would be a false positive, and
	// its silence proves the check descends past the first table rather than
	// latching on it.
	tableOK := elem("Table", document, nil)
	trOK := elem("TR", tableOK, nil)
	thOK := elem(
		"TH",
		trOK,
		2,
		"ID", text("h2"), "A", d("O", n("/Table"), "Scope", n("Column")),
	)
	tdOK := elem(
		"TD",
		trOK,
		3,
		"A", d("O", n("/Table"), "Headers", a(text("h2"))),
	)
	trOK.Set("K", a(thOK, tdOK))
	tableOK.Set("K", a(trOK))

	// A list whose second item omits the /Lbl its sibling carries. The nesting
	// L > LI > LBody is what the ISO/TS 32005 Table 5 rows govern under PDF/UA-2.
	lst := elem("L", document, nil)
	liOne := elem("LI", lst, nil)
	lblOne := elem("Lbl", liOne, nil)
	lbodyOne := elem("LBody", liOne, 4)
	liOne.Set("K", a(lblOne, lbodyOne))
	liTwo := elem("LI", lst, nil)
	lbodyTwo := elem("LBody", liTwo, 5)
	liTwo.Set("K", a(lbodyTwo))
	lst.Set("K", a(liOne, liTwo))

	para := elem("P", document, 6)

	document.Set("K", a(tableBad, tableOK, lst, para))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(
			0,
			a(thBad, tdBad, thOK, tdOK, lbodyOne, lbodyTwo, para),
		),
	)))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: "1"}))
	return save(pdf, "1.7")
}
