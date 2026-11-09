package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("xfatable_ua1", xfatableUa1) }

// xfatableUa1 builds xfatable_ua1.pdf: a dynamic XFA form and row spans past the last row.
//
// Three ISO 14289-1 rules the coverage scan showed unreached, on unrelated parts
// of the document:
//
// | Construct                                          | Rule      |
// | -------------------------------------------------- | --------- |
// | an XFA configuration asking for dynamic rendering  | `7.15/1`  |
// | a `/RowSpan` past the last row, in a later column  | `7.2/41`  |
// | a `/RowSpan` past the last row, in the first column | `7.2/43` |
//
// `7.15/1` reads `dynamicRender != 'required'`, which is not a PDF key at all: it
// lives inside the XFA packet the `/AcroForm` carries, so reaching the rule means
// writing the XFA XML rather than setting a dictionary entry.
//
// The two table rules are one defect placed a column apart. A `/RowSpan` reaching
// past the last declared row puts cells in rows no `TR` declares, and which rule
// answers for it turns on where that span starts: the first column answers
// `7.2/43`, any other column answers `7.2/41`. That split follows veraPDF's cell
// placement rather than anything the clauses say, and was measured across two,
// three and four column tables before being relied on.
//
// Neither is a span in an EARLIER row, which is a different defect: such a span
// SHIFTS the following row's cells rather than colliding with them, so it reads as
// a column count mismatch and answers `7.2/42`, which `tablespan_ua2.pdf` covers.
//
// A third table conforms and exists to stay silent, its span covering exactly the
// rows the table declares. It is worth carrying because the geometry checks are the
// ones most likely to condemn a legitimate span, and because a document whose only
// defect sat in the XFA packet would let a check that stops at the first structural
// complaint look correct.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func xfatableUa1() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// One marked-content sequence per table cell, plus the leading paragraph.
	const cellCount = 11
	content := []byte("/P <</MCID 0>> BDC\nBT /F1 12 Tf 50 750 Td (tables and XFA) Tj ET\nEMC\n")
	for mcid := 1; mcid <= cellCount; mcid++ {
		y := 720 - mcid*20
		content = fmt.Appendf(content,
			"/TD <</MCID %d>> BDC\nBT /F1 10 Tf 50 %d Td (cell %d) Tj ET\nEMC\n",
			mcid, y, mcid)
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream(content),
		"StructParents", 0,
		"Tabs", n("S"),
	))
	pdf.AddPage(page)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d(
		"Type", n("StructElem"), "S", n("Document"), "P", structRoot, "Lang", text("en-US"),
	))
	paragraph := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page, "K", 0),
	)

	kids := []any{paragraph}

	// cell is one TD element over a marked-content sequence, optionally
	// spanning rows (rowSpan 0 for none).
	cell := func(parent *pdfgen.Dict, mcid, rowSpan int) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n("TD"), "P", parent, "Pg", page, "K", mcid)
		if rowSpan != 0 {
			dd.Set("A", d("O", n("Table"), "RowSpan", rowSpan))
		}
		made := pdf.Indirect(dd)
		kids = append(kids, made)
		return made
	}

	// table is a Table element over TR rows. rowsSpec is a list of cell
	// descriptions, (mcid, row span) pairs.
	table := func(rowsSpec [][][2]int) *pdfgen.Dict {
		tbl := pdf.Indirect(
			d("Type", n("StructElem"), "S", n("Table"), "P", document, "Pg", page),
		)
		rows := a()
		for _, spec := range rowsSpec {
			tr := pdf.Indirect(
				d("Type", n("StructElem"), "S", n("TR"), "P", tbl, "Pg", page),
			)
			cells := a()
			for _, c := range spec {
				cells.Append(cell(tr, c[0], c[1]))
			}
			tr.Set("K", cells)
			rows.Append(tr)
			kids = append(kids, tr)
		}
		tbl.Set("K", rows)
		kids = append(kids, tbl)
		return tbl
	}

	// Both defective tables carry a /RowSpan reaching past the last row they
	// declare, and they differ only in which column it starts in. That is what
	// selects the rule: a span starting in the first column answers the column-gap
	// rule (t43), one starting anywhere else answers the row-span rule (t41). The
	// split follows veraPDF's cell placement rather than anything the clauses say,
	// and was measured across two, three and four column tables.
	tableT41 := table([][][2]int{{{1, 0}, {2, 0}}, {{3, 0}, {4, 2}}})
	tableT43 := table([][][2]int{{{5, 0}, {6, 0}}, {{7, 2}, {8, 0}}})

	// The control: a real /RowSpan covering exactly the rows the table has, so a
	// firing against it is over-suppression rather than a missing case. The geometry
	// checks are the ones most likely to condemn a legitimate span.
	tableOK := table([][][2]int{{{9, 2}, {10, 0}}, {{11, 0}}})

	document.Set("K", a(paragraph, tableT41, tableT43, tableOK))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, a(kids...)))))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	// 7.15 t1: the XFA configuration packet asks for dynamic rendering. XFA rides as
	// an array of alternating packet name and stream, so the configuration can be
	// addressed on its own.
	const xfaConfig = `<config xmlns="http://www.xfa.org/schema/xci/3.0/">
 <acrobat>
  <acrobat7>
   <dynamicRender>required</dynamicRender>
  </acrobat7>
 </acrobat>
</config>`
	const xfaTemplate = `<template xmlns="http://www.xfa.org/schema/xfa-template/3.0/">
 <subform name="form1"/>
</template>`

	pdf.Root.Set("AcroForm", d(
		"Fields", a(),
		"XFA", a(
			text("config"),
			pdf.MakeIndirect(pdf.Stream([]byte(xfaConfig))),
			text("template"),
			pdf.MakeIndirect(pdf.Stream([]byte(xfaTemplate))),
		),
	))

	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">dynamic XFA and table spans</rdf:li></rdf:Alt></dc:title>
   <pdfuaid:part>1</pdfuaid:part>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`
	metadata := pdf.Stream([]byte(xmp))
	metadata.Set("Type", n("Metadata"))
	metadata.Set("Subtype", n("XML"))
	pdf.Root.Set("Metadata", pdf.MakeIndirect(metadata))

	return save(pdf, "1.7")
}
