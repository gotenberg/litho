package compliance

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// runWithinDeadline builds a document from data and runs fn against it, failing
// if it does not finish within 15 seconds (a fan-out or cyclic-chain bomb would
// otherwise hang or exhaust the stack).
func runWithinDeadline(t *testing.T, data []byte, fn func(doc *document.Document)) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err == nil {
			fn(doc)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("validation did not complete: chain fan-out/cycle not bounded")
	}
}

// TestOutlineLangCycleBomb pins that an outline whose /Next entries form a cycle
// cannot loop forever in the PDF/UA-1 outline-language check. The /Next recursion
// ran at constant depth, so a 700-byte file exhausted the goroutine stack (fatal,
// unrecoverable in Go). The catalog omits /Lang so the check walks the outline.
func TestOutlineLangCycleBomb(t *testing.T) {
	t.Parallel()
	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /Outlines 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /Outlines /First 6 0 R /Last 7 0 R /Count 2 >>",
		"<< /Title (A) /Parent 5 0 R /Next 7 0 R >>",
		"<< /Title (B) /Parent 5 0 R /Next 6 0 R >>",
	)
	runWithinDeadline(t, data, func(doc *document.Document) {
		_, _ = ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	})
}

// TestActionChainFanoutBomb pins that an action whose /Next array lists the same
// action twice at each level cannot fan out 2^depth in the PDF/A action check.
func TestActionChainFanoutBomb(t *testing.T) {
	t.Parallel()
	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A 6 0 R >>",
		"<< /S /GoTo /Next [7 0 R 7 0 R] >>",
		"<< /S /GoTo /Next [6 0 R 6 0 R] >>",
	)
	runWithinDeadline(t, data, func(doc *document.Document) {
		_, _ = ValidatePDFA(context.Background(), doc, models.PDFA2b)
	})
}
