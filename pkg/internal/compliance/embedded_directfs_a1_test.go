package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// a1EFViolations counts ISO 19005-1 §6.1.11 violations reported for data.
func a1EFViolations(t *testing.T, data []byte) int {
	t.Helper()

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}

	got := 0
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == "6.1.11" {
			got++
		}
	}
	return got
}

// TestPDFA1EmbeddedFileSeesDirectFileSpec pins that ISO 19005-1 §6.1.11 reaches
// a file specification written as a direct dictionary.
//
// PDF/A-1 forbids embedded files outright, but checkEmbeddedFiles found file
// specifications by sweeping the cross-reference map, which lists only indirect
// objects, while ISO 32000-1 §7.11.3 does not require a file specification to be
// one. Moving the same /EF-carrying filespec from its own object into an
// annotation's /FS therefore turned a fatal violation into a silent pass, which
// is an evasion of the ban rather than a missed corner case. Parts 2, 3 and 4
// already discovered both forms through collectFileSpecs.
func TestPDFA1EmbeddedFileSeesDirectFileSpec(t *testing.T) {
	t.Parallel()

	direct := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 20 20] "+
			"/FS << /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 5 0 R >> >> >>",
		"<< /Type /EmbeddedFile /Length 5 >>\nstream\nlitho\nendstream",
	)
	if got := a1EFViolations(t, direct); got != 1 {
		t.Errorf("direct /FS: clause 6.1.11 violations = %d, want 1", got)
	}
}

// TestPDFA1EmbeddedFileStillSeesIndirectFileSpec keeps the pre-existing half of
// the clause covered, so the switch to a structural walk cannot trade one blind
// spot for another.
func TestPDFA1EmbeddedFileStillSeesIndirectFileSpec(t *testing.T) {
	t.Parallel()

	indirect := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 20 20] /FS 6 0 R >>",
		"<< /Type /EmbeddedFile /Length 5 >>\nstream\nlitho\nendstream",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 5 0 R >> >>",
	)
	if got := a1EFViolations(t, indirect); got != 1 {
		t.Errorf("indirect /FS: clause 6.1.11 violations = %d, want 1", got)
	}
}
