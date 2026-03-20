package pdf

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// minimalPDF builds a valid PDF with a catalog and one empty page.
func minimalPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n200\n%%EOF\n")
}

func TestWithLogger_DebugOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	data := minimalPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), WithLogger(logger))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	_, err = r.NumPages(context.Background())
	if err != nil {
		t.Fatalf("NumPages failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "document opened") {
		t.Errorf("expected 'document opened' in debug output, got:\n%s", output)
	}
	if !strings.Contains(output, "xref parsed") {
		t.Errorf("expected 'xref parsed' in debug output, got:\n%s", output)
	}
	if !strings.Contains(output, "page tree traversed") {
		t.Errorf("expected 'page tree traversed' in debug output, got:\n%s", output)
	}
}

func TestWithoutLogger_NoOutput(t *testing.T) {
	data := minimalPDF()

	// Open without WithLogger — should produce no output and no errors.
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	_, err = r.NumPages(context.Background())
	if err != nil {
		t.Fatalf("NumPages failed: %v", err)
	}
}

func TestVersion(t *testing.T) {
	data := minimalPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if v := r.Version(); v != "1.4" {
		t.Errorf("Version() = %q, want %q", v, "1.4")
	}
}

func TestExtractAllText(t *testing.T) {
	data := minimalPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Minimal PDF has no text content, so ExtractAllText should return empty.
	text, err := r.ExtractAllText(context.Background())
	if err != nil {
		t.Fatalf("ExtractAllText failed: %v", err)
	}
	if text != "" {
		t.Errorf("expected empty text, got %q", text)
	}
}

func TestClose_Noop(t *testing.T) {
	data := minimalPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Close on a Reader opened via Open should be a no-op.
	if err := r.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}
