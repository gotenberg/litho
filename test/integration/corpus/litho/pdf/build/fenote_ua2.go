package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("fenote_ua2", fenoteUa2) }

// fenoteUa2 builds fenote_ua2.pdf: footnote and endnote structure under PDF/UA-2.
//
// The coverage scan showed ISO 14289-2, 8.2.5.14 entirely unreached, and PDF/UA-2
// as the weakest profile in the corpus. The clause governs how a document marks
// up notes: PDF 2.0 replaces the PDF 1.7 `Note` type with `FENote`, requires the
// referring content and the note to point at each other through `/Ref`, and
// constrains the note's `/NoteType`.
//
// All four tests are independent and are planted together, which is the masking
// probe: a check that reports the first offending note and stops reports one rule
// where four are due.
//
// The two `/Ref` directions were measured on both tools rather than read out of
// the clause, which names neither:
//
// | construct                                  | fires |
// | ------------------------------------------ | ----- |
// | referrer points at the note, note silent   | t2    |
// | note points at the referrer, referrer silent | t3  |
// | both point at each other                   | none  |
// | neither points at anything                 | none  |
//
// So a bare `FENote` with no `/Ref` at all conforms, and only a half-built
// association fails. Both of those are planted as must-not-fire controls.
//
// Test 1 turns on the namespace, not the type name: a `Note` bound to the PDF 1.7
// namespace fires, while the same element claimed in the PDF 2.0 namespace
// resolves elsewhere and reports 8.2.4 t1 instead. The legacy note here is
// therefore bound to the 1.7 namespace deliberately.
//
// Beyond the note cluster the file carries two independent defects, no XMP packet
// and a non-embedded font, so a structure verdict is visibly separate from the
// metadata and font clauses.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func fenoteUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf17NS = "http://iso.org/pdf/ssn"
	const pdf20NS = "http://iso.org/pdf2/ssn"

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// One marked content sequence per structure element: six referring paragraphs
	// and the six notes they pair with.
	var parts []byte
	for i := range 12 {
		parts = fmt.Appendf(parts, "/P <</MCID %d>> BDC\nBT /F1 10 Tf 50 %d Td (item %d) Tj ET\nEMC\n",
			i, 740-i*24, i)
	}
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream(parts),
		"StructParents", 0,
	))
	pdf.AddPage(page)

	ns17 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf17NS)))
	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot"), "Namespaces", a(ns20, ns17)))
	document := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Document"),
		"P", structRoot,
		"NS", ns20,
		"Lang", text("en-US"),
	))

	// para is one referring paragraph, in the PDF 2.0 namespace.
	para := func(mcid int) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("StructElem"), "S", n("P"), "P", document, "NS", ns20, "Pg", page, "K", mcid,
		))
	}

	// note is one note element. kind FENote is the PDF 2.0 replacement for
	// Note. A nil namespace is ns20, an empty noteType leaves /A out.
	note := func(mcid int, kind string, namespace *pdfgen.Dict, noteType string) *pdfgen.Dict {
		if namespace == nil {
			namespace = ns20
		}
		dd := d(
			"Type", n("StructElem"),
			"S", n("/"+kind),
			"P", document,
			"NS", namespace,
			"Pg", page,
			"K", mcid,
		)
		if noteType != "" {
			dd.Set("A", d("O", n("/FENote"), "NoteType", n("/"+noteType)))
		}
		return pdf.Indirect(dd)
	}

	// N1: the legacy Note type, bound to the PDF 1.7 namespace, which PDF/UA-2
	// forbids outright (t1).
	p1, n1 := para(0), note(1, "Note", ns17, "")

	// N2: the paragraph points at the note and the note stays silent (t2).
	p2, n2 := para(2), note(3, "FENote", nil, "Footnote")
	p2.Set("Ref", a(n2))

	// N3: the note points at the paragraph and the paragraph stays silent (t3).
	p3, n3 := para(4), note(5, "FENote", nil, "Endnote")
	n3.Set("Ref", a(p3))

	// N4: a fully wired association whose /NoteType is none of Footnote, Endnote
	// or None (t4). Its /Ref pair is complete, so t2 and t3 stay silent on it and
	// the NoteType verdict stands alone.
	p4, n4 := para(6), note(7, "FENote", nil, "Sidenote")
	p4.Set("Ref", a(n4))
	n4.Set("Ref", a(p4))

	// N5 conforms in every respect, so a firing against it is over-suppression.
	p5, n5 := para(8), note(9, "FENote", nil, "Footnote")
	p5.Set("Ref", a(n5))
	n5.Set("Ref", a(p5))

	// N6 refers to nothing and nothing refers to it, which conforms: the clause
	// constrains half-built associations, not absent ones.
	p6, n6 := para(10), note(11, "FENote", nil, "None")

	kids := []any{p1, n1, p2, n2, p3, n3, p4, n4, p5, n5, p6, n6}
	document.Set("K", a(kids...))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, a(kids...)))))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	// Deliberately no /Metadata: the XMP clauses stay reachable beside the notes.
	return save(pdf, "2.0")
}
