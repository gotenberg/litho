package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("redactrefs_ua1", redactrefsUa1) }

// redactrefsUa1 builds redactrefs_ua1.pdf: copies of page text behind objects litho cannot read.
//
// Redaction clears the copies of removed text the file holds elsewhere, and two
// of those copies sit behind an object the scrub cannot read:
//
//   - The structure element describing page 1's paragraph holds its words in
//     /ActualText. The page's /ParentTree entry names no element for the
//     paragraph's MCID, its slot being null, but the element is in the tree,
//     reached from the root's /K with /Pg naming page 1 (ISO 32000-1, 14.7.4).
//   - The outline's /First is the integer 7 rather than a reference to an outline
//     item (ISO 32000-1, Table 153), so the chain from /First ends at once, while
//     its /Last names the item that quotes the paragraph, whose /Prev is that
//     integer again.
//
// A rewrite copies both copies, so redacting page 1 has to clear them. Before,
// the redaction refused: the null slot read as an element written directly into
// the array, and a number in /First as an outline item it could not rewrite.
//
// Page 2 draws text no redaction of page 1 touches, a control for the pages the
// redaction leaves alone.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func redactrefsUa1() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))
	resources := d("Font", d("F1", font))

	const words = "the secret plan"

	page1 := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", resources,
		"Contents", pdf.Stream(
			[]byte("/P <</MCID 0>> BDC\nBT /F1 12 Tf 72 700 Td ("+words+") Tj ET\nEMC\n"),
		),
		"StructParents", 0,
	))
	page2 := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", resources,
		"Contents", pdf.Stream([]byte("/P <</MCID 0>> BDC\nBT /F1 12 Tf 72 700 Td (page two) Tj ET\nEMC\n")),
		"StructParents", 1,
	))
	pdf.AddPage(page1)
	pdf.AddPage(page2)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("Document"), "P", structRoot, "Lang", text("en-US")),
	)
	secret := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("P"),
		"P", document,
		"Pg", page1,
		"K", 0,
		"ActualText", text(words),
	))
	other := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page2, "K", 0),
	)
	document.Set("K", a(secret, other))
	structRoot.Set("K", a(document))
	// The null slot: the /ParentTree names no element for page 1's MCID 0.
	structRoot.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(nil), 1, a(other))),
	))
	structRoot.Set("ParentTreeNextKey", 2)

	// /First is a number, which is no outline item.
	const notAnItem = 7
	outlines := pdf.Indirect(d("Type", n("Outlines"), "Count", 2))
	quoting := pdf.Indirect(d(
		"Title", text(words),
		"Parent", outlines,
		"Prev", notAnItem,
		"Dest", a(page1, n("Fit")),
	))
	outlines.Set("First", notAnItem)
	outlines.Set("Last", quoting)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("Outlines", outlines)

	return save(pdf, "1.7")
}
