package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestWriter_StampText(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.StampText(ctx, TextStamp{
		Text:     "CONFIDENTIAL",
		X:        200,
		Y:        400,
		FontSize: 36,
	}, 1)

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify the stamp text is extractable.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open stamped: %v", err)
	}

	text, err := r2.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}

	if !strings.Contains(text, "CONFIDENTIAL") {
		t.Errorf("expected stamp text in output, got: %q", text)
	}
}

func TestWriter_StampText_AllPages(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.StampText(ctx, TextStamp{
		Text:     "DRAFT",
		X:        100,
		Y:        100,
		FontSize: 24,
	}) // no pages = all pages

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open stamped: %v", err)
	}

	for i := 1; i <= 3; i++ {
		text, err := r2.ExtractText(ctx, i)
		if err != nil {
			t.Fatalf("ExtractText page %d: %v", i, err)
		}
		if !strings.Contains(text, "DRAFT") {
			t.Errorf("page %d: expected DRAFT in text, got: %q", i, text)
		}
	}
}
