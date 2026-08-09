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

	saveRef := ast.PdfIndirectRef{ObjectNum: 7}
	restoreRef := ast.PdfIndirectRef{ObjectNum: 8}
	stampRef := ast.PdfIndirectRef{ObjectNum: 9}
	brackets := func() (ast.PdfIndirectRef, ast.PdfIndirectRef) { return saveRef, restoreRef }
	got := appendPageContents(ctx, doc, pages[0].Dict, brackets, stampRef)

	arr, ok := got.(ast.PdfArray)
	if !ok {
		t.Fatalf("/Contents = %T, want an array", got)
	}
	if len(arr) != 5 {
		t.Fatalf("/Contents has %d entries (%v), want 5: q, the two originals, Q, and the stamp", len(arr), arr)
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
	if arr[0] != ast.PdfObject(saveRef) {
		t.Errorf("first entry = %v, want the q that brackets the original content %v", arr[0], saveRef)
	}
	if arr[len(arr)-2] != ast.PdfObject(restoreRef) {
		t.Errorf("entry before the stamp = %v, want the matching Q %v", arr[len(arr)-2], restoreRef)
	}
}

// TestStampText_UnbalancedPageCTMDoesNotScaleStamp pins that a page leaving a
// scale in force does not carry it into an appended stamp.
//
// Operators run to the end of the content stream, so a bare "cm" with no
// enclosing q is still in force when the stamp is appended (ISO 32000-1,
// 8.4.4). pdfjs bug946506 opens with "16 0 0 16 0 0 cm", which drew a 36pt
// stamp at 576pt: it ran off the page and extraction recovered only "ZZQ" of
// "ZZQQ".
func TestStampText_UnbalancedPageCTMDoesNotScaleStamp(t *testing.T) {
	ctx := context.Background()

	const content = "16 0 0 16 0 0 cm\n"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length 17 >>\nstream\n" + content + "endstream\nendobj\n" +
		"trailer\n<< /Size 5 /Root 1 0 R >>\n%%EOF\n")

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

	saveRef := ast.PdfIndirectRef{ObjectNum: 7}
	restoreRef := ast.PdfIndirectRef{ObjectNum: 8}
	stampRef := ast.PdfIndirectRef{ObjectNum: 9}
	brackets := func() (ast.PdfIndirectRef, ast.PdfIndirectRef) { return saveRef, restoreRef }
	got := appendPageContents(ctx, doc, pages[0].Dict, brackets, stampRef)

	arr, ok := got.(ast.PdfArray)
	if !ok {
		t.Fatalf("/Contents = %T, want an array bracketing the original content", got)
	}
	if len(arr) != 4 {
		t.Fatalf("/Contents has %d entries (%v), want 4: q, the original, Q, and the stamp", len(arr), arr)
	}
	if arr[0] != ast.PdfObject(saveRef) {
		t.Errorf("the original content is not preceded by q, so its CTM reaches the stamp: %v", arr)
	}
	if arr[2] != ast.PdfObject(restoreRef) {
		t.Errorf("the original content is not followed by Q, so its CTM reaches the stamp: %v", arr)
	}
	if arr[3] != ast.PdfObject(stampRef) {
		t.Errorf("last entry = %v, want the stamp %v", arr[3], stampRef)
	}
}

// TestAppendPageContents_NoContentsAllocatesNoBrackets pins that a page with no
// /Contents does not draw on the q/Q pair.
//
// There is nothing to bracket, and the writer does not prune unreachable
// objects, so allocating them eagerly left two orphan streams in the output.
func TestAppendPageContents_NoContentsAllocatesNoBrackets(t *testing.T) {
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
		"trailer\n<< /Size 4 /Root 1 0 R >>\n%%EOF\n")

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

	asked := false
	brackets := func() (ast.PdfIndirectRef, ast.PdfIndirectRef) {
		asked = true
		return ast.PdfIndirectRef{ObjectNum: 7}, ast.PdfIndirectRef{ObjectNum: 8}
	}
	stampRef := ast.PdfIndirectRef{ObjectNum: 9}
	got := appendPageContents(ctx, doc, pages[0].Dict, brackets, stampRef)

	if asked {
		t.Error("a page with no /Contents allocated the q/Q pair, leaving orphan streams in the output")
	}
	if got != ast.PdfObject(stampRef) {
		t.Errorf("/Contents = %v, want the stamp alone %v", got, stampRef)
	}
}
