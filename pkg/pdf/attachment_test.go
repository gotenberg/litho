package pdf

import (
	"bytes"
	"context"
	"testing"
)

func TestWriter_AddAttachment(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.AddAttachment(ctx, "test.txt", []byte("Hello from attachment"), "text/plain")

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify the attachment exists.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	files, err := r2.GetEmbeddedFiles(ctx)
	if err != nil {
		t.Fatalf("ExtractEmbeddedFiles: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("attachment count: got %d, want 1", len(files))
	}
	if files[0].Name != "test.txt" {
		t.Errorf("name: got %q, want %q", files[0].Name, "test.txt")
	}
}

func TestWriter_AddMultipleAttachments(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.AddAttachment(ctx, "a.txt", []byte("File A"), "text/plain")
	w.AddAttachment(ctx, "b.csv", []byte("col1,col2"), "text/csv")

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	files, err := r2.GetEmbeddedFiles(ctx)
	if err != nil {
		t.Fatalf("ExtractEmbeddedFiles: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("attachment count: got %d, want 2", len(files))
	}
}
