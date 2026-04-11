package semantics

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// pdfWithInfo is a minimal PDF with an /Info dict. Exercises the
// GetMetadata path that reads DocInfo fields.
func pdfWithInfo() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"4 0 obj\n<< /Title (Test Title) /Author (Alice) /Subject (Test Subject) /Keywords (k1, k2) /Creator (litho) /Producer (litho) >>\nendobj\n" +
			"xref\n0 5\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000115 00000 n \r\n" +
			"0000000198 00000 n \r\n" +
			"trailer\n<< /Size 5 /Root 1 0 R /Info 4 0 R >>\n" +
			"startxref\n330\n%%EOF\n")
}

func newSemanticsDoc(t *testing.T, data []byte) *document.Document {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

func TestGetMetadata_InfoDict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := newSemanticsDoc(t, pdfWithInfo())

	meta, err := GetMetadata(ctx, doc)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	// Behavior contract: every Info field that exists on disk appears
	// in the returned map with a non-empty value.
	for _, key := range []string{"Title", "Author", "Subject", "Keywords", "Creator", "Producer"} {
		if meta[key] == "" {
			t.Errorf("GetMetadata[%q] empty, want non-empty", key)
		}
	}
}

func TestGetOutline_NoOutline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := newSemanticsDoc(t, pdfWithInfo())

	// No /Outlines in catalog → empty bookmark list, no error.
	out, err := GetOutline(ctx, doc)
	if err != nil {
		t.Errorf("GetOutline: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("GetOutline: got %d bookmarks, want 0", len(out))
	}
}

func TestIsPortfolio_NotAPortfolio(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := newSemanticsDoc(t, pdfWithInfo())

	isPortfolio, err := IsPortfolio(ctx, doc)
	if err != nil {
		t.Errorf("IsPortfolio: %v", err)
	}
	if isPortfolio {
		t.Error("IsPortfolio: got true for non-portfolio PDF")
	}
}

func TestExtractPortfolioItems_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := newSemanticsDoc(t, pdfWithInfo())

	items, err := ExtractPortfolioItems(ctx, doc)
	if err != nil {
		t.Errorf("ExtractPortfolioItems: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("ExtractPortfolioItems: got %d, want 0 (no /Collection)", len(items))
	}
}

func TestExtractPronunciationLexicons_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := newSemanticsDoc(t, pdfWithInfo())

	lex, err := ExtractPronunciationLexicons(ctx, doc)
	if err != nil {
		t.Errorf("ExtractPronunciationLexicons: %v", err)
	}
	if len(lex) != 0 {
		t.Errorf("ExtractPronunciationLexicons: got %d, want 0", len(lex))
	}
}

func TestGetThreads_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := newSemanticsDoc(t, pdfWithInfo())

	threads, err := GetThreads(ctx, doc)
	if err != nil {
		t.Errorf("GetThreads: %v", err)
	}
	if len(threads) != 0 {
		t.Errorf("GetThreads: got %d, want 0", len(threads))
	}
}
