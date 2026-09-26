package compliance

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// minimalPDFForCompliance returns a 1-page PDF sufficient to drive every
// compliance check group. The checks will flag several violations
// (missing XMP, missing OutputIntent, etc.) which is fine. The
// assertion is that validation runs to completion without returning an
// engine-level error.
func minimalPDFForCompliance() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"xref\n0 4\n" +
			"0000000000 65535 f\r\n" +
			"0000000009 00000 n\r\n" +
			"0000000058 00000 n\r\n" +
			"0000000115 00000 n\r\n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\n" +
			"startxref\n198\n%%EOF\n")
}

// TestValidate_Smoke_AllProfiles runs every PDF/A and PDF/UA profile
// against a minimal PDF. The test asserts only the behavioral contract:
//
//   - [ValidatePDFA] / [ValidatePDFUA] returns without an engine-level error
//     (the returned []error is the violation list, not a failure of the
//     validation engine itself).
//   - Every returned element is a [Violation] (the public error shape).
//
// The goal is regression coverage: any future change that crashes a
// check group, deadlocks the parallel runner, or returns the wrong
// error shape fails this test. Exact violation counts and rule
// identities are not asserted. Those vary with rule generation and
// would lock the implementation.
func TestValidate_Smoke_AllProfiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := minimalPDFForCompliance()

	// NOTE: each sub-test opens its own document.Document. The
	// validation engine mutates per-document resolver caches as it
	// walks the object graph, so a single *Document is not safe to
	// share across concurrent validators. Giving each sub-test its
	// own parse is cheap (minimal fixture) and lets the sub-tests
	// run in parallel without tripping the race detector.
	pdfaProfiles := []models.PDFAProfile{
		models.PDFA1a, models.PDFA1b,
		models.PDFA2a, models.PDFA2b, models.PDFA2u,
		models.PDFA3a, models.PDFA3b, models.PDFA3u,
		models.PDFA4, models.PDFA4e, models.PDFA4f,
	}
	for _, profile := range pdfaProfiles {
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			violations, err := ValidatePDFA(ctx, doc, profile)
			if err != nil {
				t.Fatalf("ValidatePDFA(%s): %v", profile, err)
			}
			for i, v := range violations {
				if _, ok := v.(Violation); !ok {
					t.Errorf("violation[%d]: got %T, want Violation", i, v)
				}
			}
		})
	}

	pdfuaProfiles := []models.PDFUAProfile{models.PDFUA1, models.PDFUA2}
	for _, profile := range pdfuaProfiles {
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			violations, err := ValidatePDFUA(ctx, doc, profile)
			if err != nil {
				t.Fatalf("ValidatePDFUA(%s): %v", profile, err)
			}
			for i, v := range violations {
				if _, ok := v.(Violation); !ok {
					t.Errorf("violation[%d]: got %T, want Violation", i, v)
				}
			}
		})
	}
}

// TestValidate_Deterministic asserts that running the same validation
// twice on the same document produces the same set of violation
// clauses. The engine runs check groups in parallel; a race condition
// on the shared [validationCtx] or result aggregator would show up here
// as a flaky comparison.
func TestValidate_Deterministic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := minimalPDFForCompliance()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}

	first, err := ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatal(err)
	}

	if len(first) != len(second) {
		t.Fatalf("non-deterministic: %d violations first run, %d second", len(first), len(second))
	}

	// Compare by sorted clause string, since parallel execution may
	// reorder within each run.
	firstSet := map[string]int{}
	for _, v := range first {
		if viol, ok := v.(Violation); ok {
			firstSet[viol.Rule.Clause]++
		}
	}
	secondSet := map[string]int{}
	for _, v := range second {
		if viol, ok := v.(Violation); ok {
			secondSet[viol.Rule.Clause]++
		}
	}
	for clause, count := range firstSet {
		if secondSet[clause] != count {
			t.Errorf("non-deterministic clause %q: first=%d second=%d",
				clause, count, secondSet[clause])
		}
	}
}

// TestValidate_Concurrent_SameDocument exercises the document
// validation-run mutex: multiple goroutines calling [ValidatePDFA] on
// the same [document.Document] must serialize through
// [document.Document.LockValidationRun] without tripping the race
// detector. This test is the regression guard for the contract that
// "Document is safe for concurrent use after construction."
func TestValidate_Concurrent_SameDocument(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := minimalPDFForCompliance()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	const goroutines = 8
	done := make(chan error, goroutines)
	for range goroutines {
		go func() {
			_, err := ValidatePDFA(ctx, doc, models.PDFA2b)
			done <- err
		}()
	}
	for i := range goroutines {
		if err := <-done; err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
}

// TestValidate_ContextCancellation ensures the validation engine
// honors a cancelled context: it returns promptly with the context error,
// never with a partial violation list that would read as a verdict. This
// is the observable contract callers rely on for request-scoped deadlines.
func TestValidate_ContextCancellation(t *testing.T) {
	t.Parallel()
	data := minimalPDFForCompliance()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling

	if _, err := ValidatePDFA(ctx, doc, models.PDFA2b); !errors.Is(err, context.Canceled) {
		t.Errorf("ValidatePDFA err = %v, want context.Canceled", err)
	}
	if _, err := ValidatePDFUA(ctx, doc, models.PDFUA1); !errors.Is(err, context.Canceled) {
		t.Errorf("ValidatePDFUA err = %v, want context.Canceled", err)
	}
}

// pdfWithFontsAndContent returns a 3-page PDF with a Helvetica font
// resource and content streams that draw text. This drives the font,
// content stream, graphics, and text-extraction check paths that the
// minimal 1-page fixture does not reach.
func pdfWithFontsAndContent() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 6 0 R 8 0 R] /Count 3 >>\nendobj\n" +
			"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
			"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 10 0 R >>\nendobj\n" +
			"10 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 700 Td (Page1) Tj ET\nendstream\nendobj\n" +
			"6 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 11 0 R >>\nendobj\n" +
			"11 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 700 Td (Page2) Tj ET\nendstream\nendobj\n" +
			"8 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 792 612] /Resources 5 0 R /Contents 12 0 R >>\nendobj\n" +
			"12 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 500 Td (Page3) Tj ET\nendstream\nendobj\n" +
			"9 0 obj\n<< /Title (Three Pages) >>\nendobj\n" +
			"xref\n0 13\n" +
			"0000000000 65535 f\r\n" +
			"0000000009 00000 n\r\n" +
			"0000000058 00000 n\r\n" +
			"0000000248 00000 n\r\n" +
			"0000000131 00000 n\r\n" +
			"0000000204 00000 n\r\n" +
			"0000000413 00000 n\r\n" +
			"0000000000 00000 f\r\n" +
			"0000000578 00000 n\r\n" +
			"0000000744 00000 n\r\n" +
			"0000000380 00000 n\r\n" +
			"0000000545 00000 n\r\n" +
			"0000000710 00000 n\r\n" +
			"trailer\n<< /Size 13 /Root 1 0 R /Info 9 0 R >>\n" +
			"startxref\n790\n%%EOF\n")
}

// TestValidate_Smoke_FontsAndContent exercises the font / content
// stream / graphics check paths on a document that actually has those
// features. Combined with the 1-page minimal fixture this reaches most
// of the check-group entry points.
func TestValidate_Smoke_FontsAndContent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := pdfWithFontsAndContent()

	// Same note as TestValidate_Smoke_AllProfiles: each subtest owns
	// its own *Document to avoid the validator's shared resolver cache.
	for _, profile := range []models.PDFAProfile{
		models.PDFA1b, models.PDFA2b, models.PDFA2u, models.PDFA4,
	} {
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			violations, err := ValidatePDFA(ctx, doc, profile)
			if err != nil {
				t.Fatalf("ValidatePDFA(%s): %v", profile, err)
			}
			for i, v := range violations {
				if _, ok := v.(Violation); !ok {
					t.Errorf("violation[%d]: got %T, want Violation", i, v)
				}
			}
		})
	}

	for _, profile := range []models.PDFUAProfile{models.PDFUA1, models.PDFUA2} {
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			if _, err := ValidatePDFUA(ctx, doc, profile); err != nil {
				t.Fatalf("ValidatePDFUA(%s): %v", profile, err)
			}
		})
	}
}
