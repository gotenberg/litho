package document

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// TestResolvePages_AContextThatEndsNeverShortensTheList ends the context at
// each point where the walk of a nested page tree asks it, one walk per
// point: every walk returns the four pages or the context's error. A kid the
// context cut off read as a damaged branch pruned, so the walk returned the
// pages gathered until then, or none, and no error.
func TestResolvePages_AContextThatEndsNeverShortensTheList(t *testing.T) {
	t.Parallel()

	data := buildDestPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 4 /MediaBox [0 0 612 792] >>",
		"<< /Type /Pages /Parent 2 0 R /Kids [5 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Pages /Parent 2 0 R /Kids [7 0 R 8 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 3 0 R >>",
		"<< /Type /Page /Parent 3 0 R >>",
		"<< /Type /Page /Parent 4 0 R >>",
		"<< /Type /Page /Parent 4 0 R >>",
	)
	const want = 4
	ended := 0
	for after := int32(0); ; after++ {
		doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		catalog, err := ResolveCatalog(context.Background(), doc)
		if err != nil {
			t.Fatalf("ResolveCatalog: %v", err)
		}
		pages, err := ResolvePages(&cancelAfterCtx{Context: context.Background(), after: after}, doc, catalog)
		if err == nil {
			if len(pages) != want {
				t.Fatalf("context ended after %d checks: %d pages and no error, want %d pages or the context's error", after, len(pages), want)
			}
			// The walk ended before the context did.
			break
		}
		if !errors.Is(err, context.Canceled) || pages != nil {
			t.Fatalf("context ended after %d checks: %d pages, err = %v, want no page and the cancellation", after, len(pages), err)
		}
		ended++
	}
	if ended == 0 {
		t.Fatal("the walk never asked its context")
	}
}
