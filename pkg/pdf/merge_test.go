package pdf

import (
	"bytes"
	"context"
	"testing"
)

func TestMerge_TwoDocuments(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r1, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open r1: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open r2: %v", err)
	}

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{r1, r2}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Re-open merged PDF.
	r3, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open merged: %v", err)
	}

	n, err := r3.NumPages(ctx)
	if err != nil {
		t.Fatalf("NumPages: %v", err)
	}
	if n != 6 {
		t.Errorf("page count: got %d, want 6", n)
	}

	// Verify text from original pages.
	for i, want := range []string{"Page1", "Page2", "Page3", "Page1", "Page2", "Page3"} {
		text, err := r3.ExtractText(ctx, i+1)
		if err != nil {
			t.Fatalf("ExtractText page %d: %v", i+1, err)
		}
		if text != want {
			t.Errorf("page %d text: got %q, want %q", i+1, text, want)
		}
	}
}

func TestMerge_PreservesMetadata(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r1, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	r2, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{r1, r2}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	r3, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open merged: %v", err)
	}

	meta, err := r3.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta["Title"] != "Three Pages" {
		t.Errorf("title: got %q, want %q", meta["Title"], "Three Pages")
	}
}

func TestMerge_SingleDocument(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{r}, &out); err != nil {
		t.Fatalf("Merge single: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	n, err := r2.NumPages(ctx)
	if err != nil {
		t.Fatalf("NumPages: %v", err)
	}
	if n != 3 {
		t.Errorf("page count: got %d, want 3", n)
	}
}

func TestMerge_Empty(t *testing.T) {
	ctx := context.Background()
	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{}, &out); err == nil {
		t.Error("expected error for empty sources")
	}
}
