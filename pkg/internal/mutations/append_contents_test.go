package mutations

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestAppendPageContents_FlattensIndirectArray pins that a /Contents array
// reached through a reference is flattened, not nested.
//
// ISO 32000-1, 7.7.3.3 allows a stream or an array of streams. Wrapping an
// existing array as one element produces an array of arrays, which a
// conforming reader treats as a non-stream entry and drops: pdfjs
// issue12963.pdf holds /Contents as a reference to a two-stream array and lost
// all 1241 characters of its only text page when stamped.
func TestAppendPageContents_FlattensIndirectArray(t *testing.T) {
	ctx := context.Background()

	// obj 4 is an array of two content streams; the page reaches it by
	// reference, which is what a direct type assertion misses.
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n[5 0 R 6 0 R]\nendobj\n" +
		"5 0 obj\n<< /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"6 0 obj\n<< /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}

	stampRef := ast.PdfIndirectRef{ObjectNum: 9}
	got := appendPageContents(ctx, doc, pages[0].Dict, stampRef)

	arr, ok := got.(ast.PdfArray)
	if !ok {
		t.Fatalf("/Contents = %T, want an array", got)
	}
	if len(arr) != 3 {
		t.Fatalf("/Contents has %d entries (%v), want 3: the two originals plus the stamp", len(arr), arr)
	}
	for i, elem := range arr {
		if _, nested := elem.(ast.PdfArray); nested {
			t.Errorf("entry %d is a nested array; a reader drops it and every stream it holds", i)
		}
		if _, isRef := elem.(ast.PdfIndirectRef); !isRef {
			t.Errorf("entry %d = %T, want a stream reference", i, elem)
		}
	}
	if arr[len(arr)-1] != ast.PdfObject(stampRef) {
		t.Errorf("last entry = %v, want the stamp %v", arr[len(arr)-1], stampRef)
	}
}
