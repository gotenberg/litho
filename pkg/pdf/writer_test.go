package pdf

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// writerTestPDF builds a minimal valid PDF with a Catalog, one page, and an
// Info dictionary.
func writerTestPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"4 0 obj\n<< /Title (Original Title) /Author (Jane) >>\nendobj\n" +
			"xref\n0 5\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000115 00000 n \r\n" +
			"0000000198 00000 n \r\n" +
			"trailer\n<< /Size 5 /Root 1 0 R /Info 4 0 R >>\n" +
			"startxref\n256\n%%EOF\n")
}

func TestWriter_SetMetadata(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.SetMetadata(ctx, Metadata{
		Title:  "New Title",
		Author: "John",
	})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open the written PDF and verify metadata.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open modified: %v", err)
	}

	meta, err := r2.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}

	if meta["Title"] != "New Title" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "New Title")
	}
	if meta["Author"] != "John" {
		t.Errorf("Author: got %q, want %q", meta["Author"], "John")
	}
}

func TestWriter_RotatePages(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	if err := w.RotatePages(ctx, 90, 1); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify rotation.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open modified: %v", err)
	}

	page, err := r2.GetPage(ctx, 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	if page.Rotate != 90 {
		t.Errorf("Rotate: got %d, want 90", page.Rotate)
	}
	if !page.IsLandscape {
		t.Error("expected landscape after 90° rotation")
	}
}

func TestWriter_RotatePages_InvalidAngle(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	w := NewWriter(r)
	if err := w.RotatePages(ctx, 45, 1); err == nil {
		t.Error("expected error for non-90° rotation")
	}
}

func TestWriter_SetBookmarks(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.SetBookmarks(ctx, []models.Bookmark{
		{Title: "Chapter 1", PageNumber: 1},
		{Title: "Chapter 2", PageNumber: 1, Children: []models.Bookmark{
			{Title: "Section 2.1", PageNumber: 1},
		}},
	})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify bookmarks.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open modified: %v", err)
	}

	bms, err := r2.GetBookmarks(ctx)
	if err != nil {
		t.Fatalf("GetBookmarks: %v", err)
	}

	if len(bms) != 2 {
		t.Fatalf("bookmark count: got %d, want 2", len(bms))
	}
	if bms[0].Title != "Chapter 1" {
		t.Errorf("bookmark 0 title: got %q, want %q", bms[0].Title, "Chapter 1")
	}
	if bms[1].Title != "Chapter 2" {
		t.Errorf("bookmark 1 title: got %q, want %q", bms[1].Title, "Chapter 2")
	}
	if len(bms[1].Children) != 1 {
		t.Fatalf("bookmark 1 children: got %d, want 1", len(bms[1].Children))
	}
	if bms[1].Children[0].Title != "Section 2.1" {
		t.Errorf("child title: got %q, want %q", bms[1].Children[0].Title, "Section 2.1")
	}
}

func TestWriter_NoMutations(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Output should be identical to input (just a copy).
	if !bytes.Equal(out.Bytes(), data) {
		t.Error("expected output to be identical to input when no mutations applied")
	}
}

func TestWriter_MultipleMutations(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.SetMetadata(ctx, Metadata{Title: "Combined"})
	if err := w.RotatePages(ctx, 180, 1); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify both mutations.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open modified: %v", err)
	}

	meta, err := r2.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta["Title"] != "Combined" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Combined")
	}

	page, err := r2.GetPage(ctx, 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if page.Rotate != 180 {
		t.Errorf("Rotate: got %d, want 180", page.Rotate)
	}
}
