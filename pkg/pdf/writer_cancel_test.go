package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// cancelledCtx returns a context that is already done.
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// openRich opens the shared fixture as a Reader.
func openRich(t *testing.T) *Reader {
	t.Helper()

	data := richReaderPDF(t)
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})
	return r
}

// TestWriteToHonoursCancellationWithNothingQueued pins the pass-through path.
//
// A write with no queued mutations copies the source verbatim. It used to do so
// with a single io.Copy that no cancellation could interrupt, while every other
// path through writeToInternal stopped on a cancelled context: a caller could
// not rely on the contract, and a multi-gigabyte pass-through ran to completion
// regardless of the caller giving up.
func TestWriteToHonoursCancellationWithNothingQueued(t *testing.T) {
	t.Parallel()

	err := NewWriter(openRich(t)).WriteTo(cancelledCtx(), &bytes.Buffer{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("WriteTo returned %v, want context.Canceled", err)
	}
}

// TestPassThroughStaysByteIdentical is the control: chunking the copy to make
// it cancellable must not change what an uncancelled write produces, which the
// writer documents as a byte-for-byte pass-through.
func TestPassThroughStaysByteIdentical(t *testing.T) {
	t.Parallel()

	data := richReaderPDF(t)
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	}()

	var out bytes.Buffer
	if err := NewWriter(r).WriteTo(context.Background(), &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Errorf("pass-through wrote %d bytes for a %d byte source", out.Len(), len(data))
	}
}

// TestPublicReadersHonourCancellation pins the contract across the readers that
// walk document-wide tables, so a new one cannot quietly skip its ctx checks.
func TestPublicReadersHonourCancellation(t *testing.T) {
	t.Parallel()

	r := openRich(t)
	ctx := cancelledCtx()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"ExtractAllText", func() error { _, err := r.ExtractAllText(ctx); return err }},
		{"ExtractText", func() error { _, err := r.ExtractText(ctx, 1); return err }},
		{"Bookmarks", func() error { _, err := r.Bookmarks(ctx); return err }},
		{"FormFields", func() error { _, err := r.FormFields(ctx); return err }},
		{"FormData", func() error { _, err := r.FormData(ctx); return err }},
		{"ExtractAnnotations", func() error { _, err := r.ExtractAnnotations(ctx, 1); return err }},
		{"PageCount", func() error { _, err := r.PageCount(ctx); return err }},
		{"ValidatePDFA", func() error { _, err := r.ValidatePDFA(ctx, models.PDFA2b); return err }},
	} {
		if err := tc.call(); !errors.Is(err, context.Canceled) {
			t.Errorf("%s returned %v, want context.Canceled", tc.name, err)
		}
	}
}
