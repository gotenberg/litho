package pdf

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// endingContext is a context whose Err reports it cancelled from its
// (after+1)th call on, which ends it at a chosen point of a reading.
type endingContext struct {
	context.Context
	calls atomic.Int32
	after int32
}

func (c *endingContext) Err() error {
	if c.calls.Add(1) > c.after {
		return context.Canceled
	}
	return nil
}

// TestReaderForgetsPagesReadUnderAnEndedContext pins what a Reader keeps of
// its first reading of the page tree when the context of that reading ends,
// at each point where the reading asks it: nothing, so the next caller, with
// a live context, counts the three pages. The first reading was kept whatever
// its context, so one cancelled caller left every later one a document
// without pages.
func TestReaderForgetsPagesReadUnderAnEndedContext(t *testing.T) {
	t.Parallel()

	ended := 0
	for after := int32(0); ; after++ {
		r := openThreePage(t)
		n, err := r.PageCount(&endingContext{Context: context.Background(), after: after})
		switch {
		case err == nil && n != 3:
			t.Fatalf("context ended after %d checks: %d pages and no error, want 3 pages or the context's error", after, n)
		case err != nil && !errors.Is(err, context.Canceled):
			t.Fatalf("context ended after %d checks: err = %v, want the cancellation", after, err)
		}
		if got, liveErr := r.PageCount(context.Background()); liveErr != nil || got != 3 {
			t.Fatalf("context ended after %d checks, then a live one: %d pages, err = %v, want 3", after, got, liveErr)
		}
		if closeErr := r.Close(); closeErr != nil {
			t.Fatalf("Close: %v", closeErr)
		}
		if err == nil {
			// The reading ended before the context did.
			break
		}
		ended++
	}
	if ended == 0 {
		t.Fatal("the reading never asked its context")
	}
}

// TestReaderKeepsAMalformedPageTree pins the other half: an error that is the
// document's, read under a live context, is kept as the pages are, until
// ClearCache drops it.
func TestReaderKeepsAMalformedPageTree(t *testing.T) {
	t.Parallel()

	data := []byte("%PDF-1.4\n" +
		"1 0 obj\n<< /Type /Catalog >>\nendobj\n" +
		"trailer\n<< /Size 2 /Root 1 0 R >>\n%%EOF\n")
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	if _, err := r.PageCount(context.Background()); !errors.Is(err, ErrMalformedPDF) {
		t.Fatalf("PageCount = %v, want ErrMalformedPDF", err)
	}
	kept := r.pages.Load()
	if kept == nil || !errors.Is(kept.err, ErrMalformedPDF) {
		t.Fatalf("kept %+v, want the document's error", kept)
	}
	r.ClearCache()
	if r.pages.Load() != nil {
		t.Fatal("ClearCache kept the reading of the page tree")
	}
}
