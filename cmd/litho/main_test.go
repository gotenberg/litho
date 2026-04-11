package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalPDF returns a handcrafted minimal 1-page PDF with an Info
// dict. Used so the CLI tests don't need a binary test fixture checked
// into the repo.
func minimalPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"4 0 obj\n<< /Title (CLI Test Doc) /Author (litho) >>\nendobj\n" +
			"xref\n0 5\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000115 00000 n \r\n" +
			"0000000198 00000 n \r\n" +
			"trailer\n<< /Size 5 /Root 1 0 R /Info 4 0 R >>\n" +
			"startxref\n256\n%%EOF\n")
}

func writeTempPDF(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pdf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write temp PDF: %v", err)
	}
	return path
}

func TestRun_NoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), nil, &stdout, &stderr)
	if rc != 1 {
		t.Errorf("exit code: got %d, want 1", rc)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("expected usage message on stderr, got: %q", stderr.String())
	}
}

func TestRun_MissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"/nonexistent/file.pdf"}, &stdout, &stderr)
	if rc != 1 {
		t.Errorf("exit code: got %d, want 1", rc)
	}
	if !strings.Contains(stderr.String(), "Failed to open PDF") {
		t.Errorf("expected open error on stderr, got: %q", stderr.String())
	}
}

func TestRun_MinimalPDF(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{path}, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("exit code: got %d, want 0. stderr: %s", rc, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "=== Metadata ===") {
		t.Error("expected metadata section header")
	}
	if !strings.Contains(out, "Title: CLI Test Doc") {
		t.Errorf("expected title in metadata, got: %s", out)
	}
	if !strings.Contains(out, "=== Pages: 1 ===") {
		t.Errorf("expected page count, got: %s", out)
	}
	if !strings.Contains(out, "=== Page 1 Text ===") {
		t.Error("expected page 1 text section header")
	}
}
