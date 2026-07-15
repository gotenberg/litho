package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// firesUA1 reports whether validating data as PDF/UA-1 fires clause/test.
func firesUA1(t *testing.T, data []byte, clause string, test int) bool {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == clause && v.Rule.TestNumber == test {
			return true
		}
	}
	return false
}

// TestUAAnnotContentsLang covers ISO 14289-1 §7.2 t24: an annotation /Contents
// entry needs a determinable natural language. With no catalog /Lang, the
// language comes from the annotation's own /Lang or the /Lang of the structure
// element it is nested in (directly, not an inherited ancestor value).
func TestUAAnnotContentsLang(t *testing.T) {
	t.Parallel()

	// The /Link structure element (obj 8) has no /Lang; its parent /Document
	// carries /Lang, but that must not make the annotation determinable.
	firing := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [9 0 R] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 7 0 R >>",
		"<< /Type /StructElem /S /Document /Lang (en-US) /K [8 0 R] >>",
		"<< /Nums [1 8 0 R] >>",
		"<< /Type /StructElem /S /Link /P 6 0 R /K << /Type /OBJR /Obj 9 0 R >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /Contents (a link) /StructParent 1 >>",
	)
	if !firesUA1(t, firing, "7.2", 24) {
		t.Error("annotation /Contents with no determinable language must fire §7.2 t24")
	}

	// Same tree but the /Link element carries /Lang directly: determinable.
	passing := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [9 0 R] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 7 0 R >>",
		"<< /Type /StructElem /S /Document /K [8 0 R] >>",
		"<< /Nums [1 8 0 R] >>",
		"<< /Type /StructElem /S /Link /Lang (en-US) /P 6 0 R /K << /Type /OBJR /Obj 9 0 R >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /Contents (a link) /StructParent 1 >>",
	)
	if firesUA1(t, passing, "7.2", 24) {
		t.Error("annotation whose structure element carries /Lang must not fire §7.2 t24")
	}
}
