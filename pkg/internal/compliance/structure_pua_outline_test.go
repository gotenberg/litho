package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// puaOutlineNoStructPDF builds a document whose outline item title carries the
// given string and whose catalog names no /StructTreeRoot.
//
// It is [puaPDF] with that one catalog key removed. The structure objects stay
// in the file but nothing reaches them, which is the shape litho's own merge
// leaves behind: merge deliberately drops /StructTreeRoot because a structure
// tree is keyed to pages the output no longer has (see
// pkg/internal/mutations/split.go) while carrying the outline across.
func puaOutlineNoStructPDF(title string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> "+
			"/Lang (en-US) /ViewerPreferences << /DisplayDocTitle true >> /Outlines 8 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 /Tabs /S >>",
		"<< /Type /StructTreeRoot /Namespaces [6 0 R] /K 5 0 R /ParentTree 9 0 R >>",
		"<< /Type /StructElem /S /Document /NS 6 0 R /P 4 0 R /Lang (en-US) /K 7 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		"<< /Type /StructElem /S /P /NS 6 0 R /P 5 0 R /Pg 3 0 R /K 0 >>",
		"<< /Type /Outlines /First 10 0 R /Last 10 0 R /Count 1 >>",
		"<< /Nums [0 [7 0 R]] >>",
		"<< /Title "+title+" /Parent 8 0 R >>",
	)
}

// TestOutlineTitlePUAWithoutStructureTree pins ISO 14289-2 §8.6 t1 for a
// document that carries no structure tree.
//
// The clause judges the text strings a consumer reads out, and veraPDF scopes
// it to CosTextString: an outline item's /Title reaches that whether or not the
// document is tagged, since the outline hangs off the catalog rather than off
// the structure tree. Judging it inside the structure-tree branch left litho
// silent on exactly the files that have an outline and no /StructTreeRoot.
//
// Measured against veraPDF 1.30.2 on the corpus fixture puatext_ua2.pdf with
// its /StructTreeRoot entry removed and nothing else changed: veraPDF reports
// §8.6 t1 once, litho reported it not at all.
func TestOutlineTitlePUAWithoutStructureTree(t *testing.T) {
	t.Parallel()

	if !firesRuleUA(t, puaOutlineNoStructPDF(puaHex), models.PDFUA2, "8.6", 1) {
		t.Error("an outline title with PUA must fire ISO 14289-2 §8.6 t1 when the document has no structure tree")
	}
	if firesRuleUA(t, puaOutlineNoStructPDF(plainHex), models.PDFUA2, "8.6", 1) {
		t.Error("an outline title without PUA must stay silent when the document has no structure tree")
	}
}

// TestOutlineTitlePUAStillReportedOnce pins that moving the check out of the
// structure-tree branch did not make a tagged document report the clause twice.
func TestOutlineTitlePUAStillReportedOnce(t *testing.T) {
	t.Parallel()

	if got := countRuleUAOccurrences(t, puaPDF("", "", puaHex), models.PDFUA2, "8.6", 1); got != 1 {
		t.Errorf("ISO 14289-2 §8.6 t1 fired %d time(s) on a tagged document, want exactly 1", got)
	}
}

// countRuleUAOccurrences reports how many times a PDF/UA validation run emits
// the given clause and test. [firesRuleUA] answers whether a rule fired at all;
// catching a double report needs the count.
func countRuleUAOccurrences(t *testing.T, data []byte, profile models.PDFUAProfile, clause string, test int) int {
	t.Helper()

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFUA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}

	n := 0
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == clause && v.Rule.TestNumber == test {
			n++
		}
	}
	return n
}
