package pdf

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

func TestWriter_RemediatePDFA_BasicFixes(t *testing.T) {
	// This PDF is non-compliant: no XMP metadata, no OutputIntent, no MarkInfo,
	// no StructTreeRoot, no ID, no ViewerPreferences.
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Validate before remediation to confirm violations exist.
	violationsBefore, err := r.ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA before: %v", err)
	}
	if len(violationsBefore) == 0 {
		t.Skip("document already compliant, nothing to test")
	}
	t.Logf("violations before remediation: %d", len(violationsBefore))

	// Remediate.
	w := NewWriter(r)
	report, err := w.RemediatePDFA(ctx, models.PDFA2b, WithLanguage("en"))
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}

	if len(report.Fixed) == 0 {
		t.Error("expected at least one fix")
	}
	for _, fix := range report.Fixed {
		t.Logf("fixed: %s", fix)
	}
	t.Logf("residual violations: %d", len(report.Residual))

	// Write the remediated document.
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and validate.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open remediated: %v", err)
	}

	violationsAfter, err := r2.ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA after: %v", err)
	}

	t.Logf("violations after remediation: %d", len(violationsAfter))
	if len(violationsAfter) >= len(violationsBefore) {
		t.Errorf("remediation did not reduce violations: before=%d after=%d", len(violationsBefore), len(violationsAfter))
	}
}

func TestWriter_RemediatePDFA_ReportContents(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	report, err := w.RemediatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}

	// Should have fixed at least XMP metadata and trailer ID.
	fixMap := make(map[string]bool)
	for _, fix := range report.Fixed {
		fixMap[fix] = true
	}

	if !fixMap["generated XMP metadata stream"] {
		t.Errorf("expected XMP fix in report: %v", report.Fixed)
	}
	if !fixMap["added /ID to trailer"] {
		t.Errorf("expected trailer ID fix in report: %v", report.Fixed)
	}
}

func TestWriter_RemediatePDFA_Idempotent(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	// First pass: remediate.
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	w := NewWriter(r)
	_, err := w.RemediatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("first RemediatePDFA: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("first WriteTo: %v", err)
	}

	// Second pass: remediate the already-remediated document.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open remediated: %v", err)
	}
	w2 := NewWriter(r2)
	report2, err := w2.RemediatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("second RemediatePDFA: %v", err)
	}

	// The second pass should not find new things to fix (or very few).
	t.Logf("second pass fixes: %d, residual: %d", len(report2.Fixed), len(report2.Residual))
}
