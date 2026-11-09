package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("artifact_ua1", artifactUa1) }

// artifactUa1 builds artifact_ua1.pdf: artifact and real-content marking under PDF/UA-1.
//
// The coverage scan showed ISO 14289-1, clause 7.1 as the largest unreached
// cluster in PDF/UA-1. It governs the boundary between real content and
// artifacts: an artifact may not sit inside tagged content, tagged content may
// not sit inside an artifact, and the catalog may not declare its tagging
// suspect.
//
// Three defects sit in one file, and they are independent: two are nesting
// errors in the content stream, judged per marked-content sequence, and the
// third is a catalog key. A check that reports the first marking error and stops
// reports one rule where two are due, and a check that condemns the catalog on
// seeing a content error reports three where the file earns them separately.
//
// Two conforming sequences sit beside them: a plain tagged paragraph and a
// top-level artifact. Both are must-not-fire controls, because the rule is about
// nesting rather than about artifacts or tags as such.
//
// Test 12 of the clause, a structure element missing its `/P`, is deliberately
// absent, and litho and veraPDF disagree about it. See ../FIXTURES.md.
//
// Beyond the clause the file carries two independent defects, no XMP packet and
// a non-embedded font, so a marking verdict is visibly separate from the
// metadata and font clauses.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func artifactUa1() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	show := func(y int, s string) string {
		return fmt.Sprintf("BT /F1 12 Tf 50 %d Td (%s) Tj ET\n", y, s)
	}

	// Sequence 1 is a conforming tagged paragraph, and sequence 2 a conforming
	// top-level artifact: the controls.
	//
	// The third block nests an artifact inside tagged content, which 7.1 test 1
	// forbids, and the fourth nests tagged content inside an artifact, which test 2
	// forbids. The two are separate blocks so neither defect can be read as a
	// consequence of the other.
	content := "/P <</MCID 0>> BDC\n" + show(730, "a conforming paragraph") + "EMC\n" +
		"/Artifact BMC\n" + show(710, "a conforming artifact") + "EMC\n" +
		"/P <</MCID 1>> BDC\n" +
		show(690, "tagged, and below it an artifact") +
		"/Artifact BMC\n" +
		show(670, "artifact inside tagged content") +
		"EMC\n" +
		"EMC\n" +
		"/Artifact BMC\n" +
		show(650, "artifact, and below it tagged content") +
		"/P <</MCID 2>> BDC\n" +
		show(630, "tagged content inside an artifact") +
		"EMC\n" +
		"EMC\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte(content)),
		"StructParents", 0,
	))
	pdf.AddPage(page)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Document"),
		"P", structRoot,
		"Lang", text("en-US"),
	))
	var paragraphs []any
	for mcid := 0; mcid < 3; mcid++ {
		paragraphs = append(paragraphs, pdf.Indirect(d(
			"Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page, "K", mcid,
		)))
	}
	document.Set("K", a(paragraphs...))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, a(paragraphs...)))))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	// /Suspects declares the tagging unreliable, which ISO 14289-1, 7.1 test 4
	// forbids outright. It belongs to no content stream, so it shows a marking
	// verdict does not depend on the catalog and vice versa.
	pdf.Root.Set("MarkInfo", d("Marked", true, "Suspects", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	// Deliberately no /Metadata, and the font is not embedded.
	return save(pdf, "1.7")
}
