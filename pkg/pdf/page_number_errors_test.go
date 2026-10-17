package pdf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

// TestPageOpsReturnErrInvalidPageNumber holds the page operations to the
// contract [ErrInvalidPageNumber] states. The Writer's page operations and
// [Split] once built bare errors for an out-of-range page, so errors.Is was
// false for the sentinel the package documents for that case.
func TestPageOpsReturnErrInvalidPageNumber(t *testing.T) {
	t.Parallel()

	data := multiPagePDF()
	open := func(t *testing.T) *Reader {
		t.Helper()
		r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		t.Cleanup(func() {
			if err := r.Close(); err != nil {
				t.Errorf("Close failed: %v", err)
			}
		})
		return r
	}

	writerCases := []struct {
		name  string
		queue func(w *Writer, src *Reader) error
	}{
		{"RotatePages", func(w *Writer, _ *Reader) error { return w.RotatePages(90, 4) }},
		{"DeletePages", func(w *Writer, _ *Reader) error { return w.DeletePages(0) }},
		{"InsertPages position", func(w *Writer, src *Reader) error { return w.InsertPages(4, src) }},
		{"InsertPages source page", func(w *Writer, src *Reader) error { return w.InsertPages(0, src, 9) }},
	}
	for _, tc := range writerCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := NewWriter(open(t))
			if err := tc.queue(w, open(t)); err != nil {
				t.Fatalf("queueing: %v", err)
			}
			err := w.WriteTo(context.Background(), io.Discard)
			if !errors.Is(err, ErrInvalidPageNumber) {
				t.Fatalf("WriteTo error = %v, want ErrInvalidPageNumber", err)
			}
		})
	}

	t.Run("Split", func(t *testing.T) {
		t.Parallel()
		err := Split(context.Background(), open(t), []int{1, 4}, io.Discard)
		if !errors.Is(err, ErrInvalidPageNumber) {
			t.Fatalf("Split error = %v, want ErrInvalidPageNumber", err)
		}
	})
}
