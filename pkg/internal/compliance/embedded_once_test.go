package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestEmbeddedFilesReportedOncePerObject covers the count ISO 19005-1 6.1.11
// carries. Its object is a file specification, so a file specification holding
// /EF is one violation, and the /Names /EmbeddedFiles key is one more. litho
// used to state the whole clause twice, in checkEmbeddedGroup and in
// checkEmbeddedFiles, and reported every offending object twice.
func TestEmbeddedFilesReportedOncePerObject(t *testing.T) {
	t.Parallel()

	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 5 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Names [(a.txt) 6 0 R (b.txt) 7 0 R] >>",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 8 0 R >> >>",
		"<< /Type /Filespec /F (b.txt) /UF (b.txt) /EF << /F 8 0 R >> >>",
		"<< /Length 5 >>\nstream\nlitho\nendstream",
	)

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}

	counts := map[int]int{}
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == "6.1.11" {
			counts[v.Rule.TestNumber]++
		}
	}

	// Two file specifications carry /EF, and /Names carries /EmbeddedFiles once.
	if counts[1] != 2 {
		t.Errorf("6.1.11 t1 violations = %d, want 2 (one per file specification)", counts[1])
	}
	if counts[2] != 1 {
		t.Errorf("6.1.11 t2 violations = %d, want 1", counts[2])
	}
}
