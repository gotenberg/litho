package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("structnest_ua1", structnestUa1) }

// structnestUa1 builds structnest_ua1.pdf: a PDF/UA-1 fixture for ISO 14289-1, 7.2.
//
// Despite its title, 7.2 is a mixed clause. It governs three unrelated things:
// where a table, list and table-of-contents element may sit in the structure
// tree, where a Caption may sit among its siblings, and whose natural language
// covers a piece of text that is not page content. No other fixture reaches any
// of it beyond the two document-wide language tests.
//
// Every defect is a separate element, and nine of them are siblings under one
// Div, so a check that reports the first misplaced element and stops reports one
// rule where nine are due. Six controls conform in the same respects the defects
// break.
//
// The page carries no text, only artifact and tagged vector content, so the
// document-wide language tests stay out of the way of the per-element ones.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func structnestUa1() ([]byte, error) {
	pdf := pdfgen.New()

	// Four /Span marked-content sequences, three carrying text in a property whose
	// language nothing determines. The fourth declares /Lang inline and is the
	// control. Each wraps a stroked path rather than a glyph run, so the sequences
	// are real tagged content without pulling a font into the fixture.
	content := `/Artifact BMC
q 0.6 w 40 752 m 560 752 l S Q
EMC
/Span <</MCID 0 /ActualText (Marked content actual text)>> BDC
q 40 720 m 200 720 l S Q
EMC
/Span <</MCID 1 /Alt (Marked content alternate description)>> BDC
q 40 700 m 200 700 l S Q
EMC
/Span <</MCID 2 /E (MC)>> BDC
q 40 680 m 200 680 l S Q
EMC
/Span <</MCID 3 /Lang (en-US) /ActualText (Language declared inline)>> BDC
q 40 660 m 200 660 l S Q
EMC
`

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(),
		"Contents", pdf.Stream([]byte(content)),
		"StructParents", 0,
	))
	pdf.AddPage(page)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))

	// elem creates one structure element, wired to parent and to the page.
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

	// scopedTH creates a TH carrying /Scope, so 7.5 stays out of the 7.2 rule
	// set.
	scopedTH := func(parent *pdfgen.Dict) *pdfgen.Dict {
		return elem("TH", parent, nil, "A", d("O", n("/Table"), "Scope", n("Column")))
	}

	// row creates a TR holding the given number of cells.
	row := func(parent *pdfgen.Dict, cells int, cell func(*pdfgen.Dict) *pdfgen.Dict) *pdfgen.Dict {
		if cell == nil {
			cell = scopedTH
		}
		tr := elem("TR", parent, nil)
		k := a()
		for range cells {
			k.Append(cell(tr))
		}
		tr.Set("K", k)
		return tr
	}

	// section creates a THead/TBody/TFoot holding one TR.
	section := func(stype string, parent *pdfgen.Dict) *pdfgen.Dict {
		sec := elem(stype, parent, nil)
		sec.Set("K", a(row(sec, 1, nil)))
		return sec
	}

	// Nine misplaced siblings under one Div. Each breaks a different containment
	// test, and none of them is inside a Table, an L or a TOC, so no enclosing
	// element can absorb the report.
	orphans := elem("Div", document, nil)
	orphanTR := row(orphans, 1, nil)         // t4: TR outside Table/THead/TBody/TFoot
	orphanTHead := section("THead", orphans) // t5: THead outside Table
	orphanTBody := section("TBody", orphans) // t6: TBody outside Table
	orphanTFoot := section("TFoot", orphans) // t7: TFoot outside Table
	orphanTH := scopedTH(orphans)            // t8: TH outside TR
	orphanTD := elem("TD", orphans, nil)     // t9: TD outside TR
	orphanLI := elem("LI", orphans, nil)     // t17: LI outside L
	orphanLI.Set("K", a(elem("LBody", orphanLI, nil)))
	orphanLBody := elem("LBody", orphans, nil) // t18: LBody outside LI
	orphanTOCI := elem("TOCI", orphans, nil)   // t26: TOCI outside TOC
	orphans.Set("K", a(
		orphanTR,
		orphanTHead,
		orphanTBody,
		orphanTFoot,
		orphanTH,
		orphanTD,
		orphanLI,
		orphanLBody,
		orphanTOCI,
	))

	// t3: a Table kid that is none of TR, THead, TBody, TFoot, Caption.
	tblKid := elem("Table", document, nil)
	tblKid.Set("K", a(row(tblKid, 1, nil), elem("P", tblKid, nil)))

	// t10: a TR kid that is neither TH nor TD.
	tblTRKid := elem("Table", document, nil)
	trTRKid := elem("TR", tblTRKid, nil)
	trTRKid.Set("K", a(scopedTH(trTRKid), elem("P", trTRKid, nil)))
	tblTRKid.Set("K", a(trTRKid))

	// t11: two THead kids. The TBody keeps t14 out of this table.
	tbl2THead := elem("Table", document, nil)
	tbl2THead.Set("K", a(
		section("THead", tbl2THead),
		section("THead", tbl2THead),
		section("TBody", tbl2THead),
	))

	// t12: two TFoot kids. The TBody keeps t13 out of this table.
	tbl2TFoot := elem("Table", document, nil)
	tbl2TFoot.Set("K", a(
		section("TBody", tbl2TFoot),
		section("TFoot", tbl2TFoot),
		section("TFoot", tbl2TFoot),
	))

	// t13: a TFoot with no TBody beside it.
	tblTFoot := elem("Table", document, nil)
	tblTFoot.Set("K", a(section("TFoot", tblTFoot)))

	// t14: a THead with no TBody beside it.
	tblTHead := elem("Table", document, nil)
	tblTHead.Set("K", a(section("THead", tblTHead)))

	// t16: a Caption that is neither the first nor the last Table kid.
	tblCapMid := elem("Table", document, nil)
	tblCapMid.Set("K", a(
		row(tblCapMid, 1, nil), elem("Caption", tblCapMid, nil), row(tblCapMid, 1, nil),
	))

	// t39: two Captions. Both sit in a position t16 allows, so only the count
	// fails.
	tbl2Cap := elem("Table", document, nil)
	tbl2Cap.Set("K", a(
		elem("Caption", tbl2Cap, nil), row(tbl2Cap, 1, nil), elem("Caption", tbl2Cap, nil),
	))

	// t36, t37, t38: a section holding something other than a TR. Each is a
	// separate table so the three tests cannot mask one another.
	tblHKid := elem("Table", document, nil)
	theadHKid := elem("THead", tblHKid, nil)
	theadHKid.Set("K", a(elem("P", theadHKid, nil)))
	tblHKid.Set("K", a(theadHKid, section("TBody", tblHKid)))

	tblBKid := elem("Table", document, nil)
	tbodyBKid := elem("TBody", tblBKid, nil)
	tbodyBKid.Set("K", a(elem("P", tbodyBKid, nil)))
	tblBKid.Set("K", a(tbodyBKid))

	tblFKid := elem("Table", document, nil)
	tfootFKid := elem("TFoot", tblFKid, nil)
	tfootFKid.Set("K", a(elem("P", tfootFKid, nil)))
	tblFKid.Set("K", a(section("TBody", tblFKid), tfootFKid))

	// t42 and t43 split the same irregularity by direction. veraPDF sizes the grid
	// from the first row and the row count, then walks the cells: a cell that
	// overflows the last column is t42, and a row that ends before the last column
	// is t43.
	tblWide := elem("Table", document, nil)
	tblWide.Set("K", a(row(tblWide, 1, nil), row(tblWide, 2, nil)))

	tblShort := elem("Table", document, nil)
	tblShort.Set("K", a(row(tblShort, 2, nil), row(tblShort, 1, nil)))

	// Control table. Caption first, one THead, one TBody, one TFoot, every TR
	// holding one cell, the TH scoped over its column so both data cells resolve a
	// header. It conforms in every respect the thirteen tables above break.
	td := func(p *pdfgen.Dict) *pdfgen.Dict { return elem("TD", p, nil) }
	tblOK := elem("Table", document, nil)
	theadOK := section("THead", tblOK)
	tbodyOK := elem("TBody", tblOK, nil)
	tbodyOK.Set("K", a(row(tbodyOK, 1, td)))
	tfootOK := elem("TFoot", tblOK, nil)
	tfootOK.Set("K", a(row(tfootOK, 1, td)))
	tblOK.Set("K", a(elem("Caption", tblOK, nil), theadOK, tbodyOK, tfootOK))

	// listItem creates an LI holding an LBody, optionally preceded by an Lbl.
	listItem := func(parent *pdfgen.Dict, labelled bool) *pdfgen.Dict {
		li := elem("LI", parent, nil)
		kids := a()
		if labelled {
			kids.Append(elem("Lbl", li, nil))
		}
		kids.Append(elem("LBody", li, nil))
		li.Set("K", kids)
		return li
	}

	// t19: an L kid that is none of L, LI, Caption.
	lstKid := elem("L", document, nil)
	lstKid.Set("K", a(listItem(lstKid, false), elem("P", lstKid, nil)))

	// t20: an LI kid that is neither Lbl nor LBody.
	lstLIKid := elem("L", document, nil)
	liLIKid := elem("LI", lstLIKid, nil)
	liLIKid.Set("K", a(elem("P", liLIKid, nil)))
	lstLIKid.Set("K", a(liLIKid))

	// t40: a Caption that is not the first L kid. t16 allows a last-position
	// Caption in a Table and t40 does not allow one in an L, which is the whole
	// difference between the two tests.
	lstCap := elem("L", document, nil)
	lstCap.Set("K", a(listItem(lstCap, false), elem("Caption", lstCap, nil)))

	// Control list: L > LI > Lbl + LBody, the nesting the three lists above break.
	lstOK := elem("L", document, nil)
	lstOK.Set("K", a(listItem(lstOK, true)))

	// t27: a TOC kid that is none of TOC, TOCI, Caption.
	tocKid := elem("TOC", document, nil)
	tocKid.Set("K", a(elem("TOCI", tocKid, nil), elem("P", tocKid, nil)))

	// t28: a Caption that is not the first TOC kid.
	tocCap := elem("TOC", document, nil)
	tocCap.Set("K", a(elem("TOCI", tocCap, nil), elem("Caption", tocCap, nil)))

	// Control table of contents: Caption first, then a TOCI.
	tocOK := elem("TOC", document, nil)
	tocOK.Set("K", a(elem("Caption", tocOK, nil), elem("TOCI", tocOK, nil)))

	// t21, t22, t23: text carried by a structure attribute rather than by page
	// content. Nothing in the /P chain and nothing in the catalog declares a
	// language, so none of the three is determinable.
	attrActualText := elem("Span", document, nil, "ActualText", text("Structure actual text"))
	attrAlt := elem("Span", document, nil, "Alt", text("Structure alternate description"))
	attrExpansion := elem("Span", document, nil, "E", text("SE"))

	// Control: the same construct carrying its own /Lang. Its silence is what
	// proves t21 reports per element rather than once per document.
	attrOK := elem(
		"Span", document, nil, "ActualText", text("Covered actual text"), "Lang", text("en-US"),
	)

	// The four /Span marked-content sequences hang off structure elements that
	// declare no language of their own, so only the inline /Lang on the fourth
	// sequence can cover it.
	mcParent := elem("P", document, nil)
	mcActualText := elem("Span", mcParent, 0)
	mcAlt := elem("Span", mcParent, 1)
	mcExpansion := elem("Span", mcParent, 2)
	mcOK := elem("Span", mcParent, 3)
	mcParent.Set("K", a(mcActualText, mcAlt, mcExpansion, mcOK))

	document.Set("K", a(
		orphans,
		tblKid,
		tblTRKid,
		tbl2THead,
		tbl2TFoot,
		tblTFoot,
		tblTHead,
		tblCapMid,
		tbl2Cap,
		tblHKid,
		tblBKid,
		tblFKid,
		tblWide,
		tblShort,
		tblOK,
		lstKid,
		lstLIKid,
		lstCap,
		lstOK,
		tocKid,
		tocCap,
		tocOK,
		attrActualText,
		attrAlt,
		attrExpansion,
		attrOK,
		mcParent,
	))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(mcActualText, mcAlt, mcExpansion, mcOK))),
	))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	// t2: an outline entry draws its natural language from the catalog alone, and
	// this catalog declares none.
	outlineItem := pdf.Indirect(d("Title", text("Nested structures")))
	outlines := pdf.Indirect(
		d("Type", n("Outlines"), "First", outlineItem, "Last", outlineItem, "Count", 1),
	)
	outlineItem.Set("Parent", outlines)
	pdf.Root.Set("Outlines", outlines)

	// t25: a form field whose /TU tooltip has no determinable language, beside one
	// that declares /Lang itself.
	fieldTU := pdf.Indirect(
		d("FT", n("Tx"), "T", text("undetermined"), "TU", text("Tooltip text")),
	)
	fieldOK := pdf.Indirect(d(
		"FT", n("Tx"),
		"T", text("determined"),
		"TU", text("Tooltip text"),
		"Lang", text("en-US"),
	))
	pdf.Root.Set("AcroForm", d("Fields", a(fieldTU, fieldOK)))

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: "1"}))
	return save(pdf, "1.7")
}
