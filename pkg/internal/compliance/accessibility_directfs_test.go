package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestPDFUAFileSpecFUFSeesDirectFileSpec pins that ISO 14289-1 7.11 reaches a
// file specification written as a direct dictionary.
//
// checkPDFUAFileSpecFUF documented itself as covering FileAttachment
// annotations, but found file specifications by sweeping the cross-reference
// map, which lists only indirect objects. pdfjs issue14203.pdf attaches
// example.tex through an annotation whose /FS is inline and carries /F without
// /UF: veraPDF 1.30.2 reports one failed check for 7.11 t1, litho reported none.
func TestPDFUAFileSpecFUFSeesDirectFileSpec(t *testing.T) {
	t.Parallel()

	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 20 20] "+
			"/FS << /Type /Filespec /F (a.tex) /EF << /F 5 0 R >> >> >>",
		"<< /Length 5 >>\nstream\nlitho\nendstream",
	)

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}

	got := 0
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == "7.11" {
			got++
		}
	}
	if got != 1 {
		t.Errorf("clause 7.11 violations = %d, want 1 (/UF is missing)", got)
	}
}
