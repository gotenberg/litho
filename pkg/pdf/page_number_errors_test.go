package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

// TestStampsRefuseOutOfRangePages holds the three stamps to the contract
// [ErrInvalidPageNumber] states. A stamp used to skip a page outside the
// document with a debug log, so the caller got an unstamped output and no
// error. The refusal comes before a byte is written, and stamping every page,
// the empty list, still succeeds.
func TestStampsRefuseOutOfRangePages(t *testing.T) {
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
	jpeg := minimalJPEG(t)

	stamps := []struct {
		name  string
		queue func(w *Writer, src *Reader, pages ...int) error
	}{
		{"StampText", func(w *Writer, _ *Reader, pages ...int) error {
			return w.StampText(TextStamp{Text: "DRAFT"}, pages...)
		}},
		{"StampImage", func(w *Writer, _ *Reader, pages ...int) error {
			return w.StampImage(ImageStamp{Data: jpeg, MimeType: "image/jpeg", Width: 10, Height: 10}, pages...)
		}},
		{"StampPDF", func(w *Writer, src *Reader, pages ...int) error {
			return w.StampPDF(PDFStamp{Source: src}, pages...)
		}},
	}
	cases := []struct {
		pages   []int
		refused bool
	}{
		{[]int{4}, true},
		{[]int{0}, true},
		{[]int{-1}, true},
		{[]int{1, 4}, true},
		{nil, false},
		{[]int{1, 3}, false},
	}
	for _, st := range stamps {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/%v", st.name, tc.pages), func(t *testing.T) {
				t.Parallel()
				w := NewWriter(open(t))
				if err := st.queue(w, open(t), tc.pages...); err != nil {
					t.Fatalf("queueing: %v", err)
				}
				var out bytes.Buffer
				err := w.WriteTo(context.Background(), &out)
				if !tc.refused {
					if err != nil {
						t.Fatalf("WriteTo error = %v, want nil", err)
					}
					return
				}
				if !errors.Is(err, ErrInvalidPageNumber) {
					t.Fatalf("WriteTo error = %v, want ErrInvalidPageNumber", err)
				}
				if out.Len() != 0 {
					t.Errorf("WriteTo wrote %d bytes before refusing", out.Len())
				}
			})
		}
	}

	t.Run("StampPDF source page", func(t *testing.T) {
		t.Parallel()
		w := NewWriter(open(t))
		if err := w.StampPDF(PDFStamp{Source: open(t), Page: 9}); err != nil {
			t.Fatalf("queueing: %v", err)
		}
		var out bytes.Buffer
		err := w.WriteTo(context.Background(), &out)
		if !errors.Is(err, ErrInvalidPageNumber) {
			t.Fatalf("WriteTo error = %v, want ErrInvalidPageNumber", err)
		}
		if out.Len() != 0 {
			t.Errorf("WriteTo wrote %d bytes before refusing", out.Len())
		}
	})
}
