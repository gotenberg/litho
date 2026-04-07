package pdf

import (
	"bytes"
	"context"
	"testing"
)

// threePagePDF builds a valid PDF with 3 pages, each containing text.
func threePagePDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			// Catalog
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			// Pages
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 6 0 R 8 0 R] /Count 3 >>\nendobj\n" +
			// Font
			"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
			// Resources
			"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
			// Page 1
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 10 0 R >>\nendobj\n" +
			// Page 1 content
			"10 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 700 Td (Page1) Tj ET\nendstream\nendobj\n" +
			// Page 2
			"6 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 11 0 R >>\nendobj\n" +
			// Page 2 content
			"11 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 700 Td (Page2) Tj ET\nendstream\nendobj\n" +
			// Page 3
			"8 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 792 612] /Resources 5 0 R /Contents 12 0 R >>\nendobj\n" +
			// Page 3 content (landscape)
			"12 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 500 Td (Page3) Tj ET\nendstream\nendobj\n" +
			// Info
			"9 0 obj\n<< /Title (Three Pages) >>\nendobj\n" +
			// xref
			"xref\n0 13\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000248 00000 n \r\n" +
			"0000000131 00000 n \r\n" +
			"0000000204 00000 n \r\n" +
			"0000000413 00000 n \r\n" +
			"0000000000 00000 f \r\n" +
			"0000000578 00000 n \r\n" +
			"0000000744 00000 n \r\n" +
			"0000000380 00000 n \r\n" +
			"0000000545 00000 n \r\n" +
			"0000000710 00000 n \r\n" +
			"trailer\n<< /Size 13 /Root 1 0 R /Info 9 0 R >>\n" +
			"startxref\n790\n%%EOF\n")
}

func TestSplit_SinglePage(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var out bytes.Buffer
	if err := Split(ctx, r, []int{2}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}

	// Re-open the split PDF.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open split: %v", err)
	}

	n, err := r2.NumPages(ctx)
	if err != nil {
		t.Fatalf("NumPages: %v", err)
	}
	if n != 1 {
		t.Errorf("page count: got %d, want 1", n)
	}

	text, err := r2.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if text != "Page2" {
		t.Errorf("text: got %q, want %q", text, "Page2")
	}
}

func TestSplit_MultiplePages(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var out bytes.Buffer
	if err := Split(ctx, r, []int{3, 1}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open split: %v", err)
	}

	n, err := r2.NumPages(ctx)
	if err != nil {
		t.Fatalf("NumPages: %v", err)
	}
	if n != 2 {
		t.Errorf("page count: got %d, want 2", n)
	}

	// Page 1 of split should be original page 3 (landscape).
	page, err := r2.GetPage(ctx, 1)
	if err != nil {
		t.Fatalf("GetPage 1: %v", err)
	}
	if !page.IsLandscape {
		t.Error("page 1: expected landscape (from original page 3)")
	}

	// Page 2 of split should be original page 1.
	text, err := r2.ExtractText(ctx, 2)
	if err != nil {
		t.Fatalf("ExtractText page 2: %v", err)
	}
	if text != "Page1" {
		t.Errorf("page 2 text: got %q, want %q", text, "Page1")
	}
}

func TestSplit_PreservesMetadata(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var out bytes.Buffer
	if err := Split(ctx, r, []int{1}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open split: %v", err)
	}

	meta, err := r2.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta["Title"] != "Three Pages" {
		t.Errorf("title: got %q, want %q", meta["Title"], "Three Pages")
	}
}

func TestSplit_InvalidPage(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))

	var out bytes.Buffer
	if err := Split(ctx, r, []int{5}, &out); err == nil {
		t.Error("expected error for out-of-bounds page")
	}
}

func TestSplit_EmptyPages(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))

	var out bytes.Buffer
	if err := Split(ctx, r, []int{}, &out); err == nil {
		t.Error("expected error for empty pages list")
	}
}
