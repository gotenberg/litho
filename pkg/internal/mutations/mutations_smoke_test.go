package mutations

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// minimalPDFMutations is a 1-page fixture used by the mutation smoke
// tests. Deliberately redefined here (rather than pulled from pkg/pdf)
// to keep the test file self-contained with no external fixtures.
func minimalPDFMutations() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"4 0 obj\n<< /Title (Original) >>\nendobj\n" +
			"xref\n0 5\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000115 00000 n \r\n" +
			"0000000198 00000 n \r\n" +
			"trailer\n<< /Size 5 /Root 1 0 R /Info 4 0 R >>\n" +
			"startxref\n240\n%%EOF\n")
}

// newMutationDoc returns an open document and a sized allocator ready
// for mutation testing.
func newMutationDoc(t *testing.T, data []byte) (*document.Document, *writer.ObjAllocator) {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	maxObj := 0
	for objNum := range doc.Index.Entries {
		if objNum > maxObj {
			maxObj = objNum
		}
	}
	return doc, writer.NewObjAllocator(maxObj)
}

func TestSetMetadata_AddsAllFields(t *testing.T) {
	t.Parallel()
	doc, alloc := newMutationDoc(t, minimalPDFMutations())
	err := SetMetadata(context.Background(), alloc, doc, Metadata{
		Title:    "New Title",
		Author:   "Alice",
		Subject:  "Sub",
		Keywords: "k1",
		Creator:  "C",
		Producer: "P",
	})
	if err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}
	if len(alloc.Pending()) == 0 {
		t.Error("SetMetadata: no pending allocations")
	}
}

func TestSetMetadata_EmptyOverrides(t *testing.T) {
	t.Parallel()
	doc, alloc := newMutationDoc(t, minimalPDFMutations())
	// Empty fields should not overwrite existing values (contract).
	err := SetMetadata(context.Background(), alloc, doc, Metadata{})
	if err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}
}

func TestAddAttachment_AddsPending(t *testing.T) {
	t.Parallel()
	doc, alloc := newMutationDoc(t, minimalPDFMutations())
	err := AddAttachment(context.Background(), alloc, doc, "note.txt", []byte("hello"), "text/plain")
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}
	if len(alloc.Pending()) == 0 {
		t.Error("AddAttachment: no pending allocations")
	}
}

func TestSplit_SinglePage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, _ := newMutationDoc(t, minimalPDFMutations())
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}

	var out bytes.Buffer
	err = Split(ctx, doc, pages, []int{1}, &out, "", false, false, DefaultSplitBookmarkConfig(), nil)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if out.Len() == 0 {
		t.Error("Split: empty output")
	}
	// Reopen the split result to verify it parses.
	doc2, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopen split: %v", err)
	}
	catalog2, _ := document.ResolveCatalog(ctx, doc2)
	pages2, _ := document.ResolvePages(ctx, doc2, catalog2)
	if len(pages2) != 1 {
		t.Errorf("split result: got %d pages, want 1", len(pages2))
	}
}

func TestMerge_TwoSources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := minimalPDFMutations()

	doc1, _ := newMutationDoc(t, data)
	doc2, _ := newMutationDoc(t, data)

	c1, _ := document.ResolveCatalog(ctx, doc1)
	p1, _ := document.ResolvePages(ctx, doc1, c1)
	c2, _ := document.ResolveCatalog(ctx, doc2)
	p2, _ := document.ResolvePages(ctx, doc2, c2)

	var out bytes.Buffer
	err := Merge(ctx, []MergeSource{
		{Doc: doc1, Pages: p1},
		{Doc: doc2, Pages: p2},
	}, &out, "", false, false, DefaultMergeBookmarkConfig(), nil)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if out.Len() == 0 {
		t.Error("Merge: empty output")
	}
	// Reopen → must have 2 pages.
	merged, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopen merged: %v", err)
	}
	mc, _ := document.ResolveCatalog(ctx, merged)
	mpages, _ := document.ResolvePages(ctx, merged, mc)
	if len(mpages) != 2 {
		t.Errorf("merged pages: got %d, want 2", len(mpages))
	}
}

func TestSetBookmarks_AddsOutline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newMutationDoc(t, minimalPDFMutations())
	catalog, _ := document.ResolveCatalog(ctx, doc)
	pages, _ := document.ResolvePages(ctx, doc, catalog)

	err := SetBookmarks(ctx, alloc, doc, pages, []models.Bookmark{
		{Title: "First", PageNumber: 1},
	})
	if err != nil {
		t.Fatalf("SetBookmarks: %v", err)
	}
	if len(alloc.Pending()) == 0 {
		t.Error("SetBookmarks: no pending allocations")
	}
}

func TestRotatePages_90(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newMutationDoc(t, minimalPDFMutations())
	catalog, _ := document.ResolveCatalog(ctx, doc)
	pages, _ := document.ResolvePages(ctx, doc, catalog)

	err := RotatePages(ctx, alloc, pages, 90, []int{1})
	if err != nil {
		t.Fatalf("RotatePages: %v", err)
	}
	// After mutation, the page dict should be in the allocator with
	// /Rotate 90.
	page1 := pages[0]
	pending := alloc.Pending()
	newDict, ok := pending[page1.Ref.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("page dict not in pending: %v", pending)
	}
	rot, ok := newDict["Rotate"].(ast.PdfInt)
	if !ok {
		t.Fatalf("/Rotate: got %T, want PdfInt", newDict["Rotate"])
	}
	if int(rot) != 90 {
		t.Errorf("/Rotate: got %d, want 90", rot)
	}
}

func TestStampText_Basic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newMutationDoc(t, minimalPDFMutations())
	catalog, _ := document.ResolveCatalog(ctx, doc)
	pages, _ := document.ResolvePages(ctx, doc, catalog)

	err := StampText(ctx, alloc, doc, pages, TextStampOptions{
		Text:         "STAMP",
		X:            100,
		Y:            500,
		FontSize:     24,
		StandardFont: "Helvetica",
	}, []int{1})
	if err != nil {
		t.Fatalf("StampText: %v", err)
	}
	if len(alloc.Pending()) == 0 {
		t.Error("StampText: no pending allocations")
	}
}

func TestValidateTTFForStamp_RejectsShort(t *testing.T) {
	t.Parallel()
	if err := ValidateTTFForStamp([]byte{0, 1}); err == nil {
		t.Error("expected error for short TTF")
	}
	if err := ValidateTTFForStamp(nil); err == nil {
		t.Error("expected error for nil TTF")
	}
}

func TestFormatFloat_TrimsTrailingZeros(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{1.5, "1.5"},
		{1.50, "1.5"},
		{0.1, "0.1"},
		{-1.25, "-1.25"},
	}
	for _, tc := range tests {
		got := FormatFloat(tc.in)
		if got != tc.want {
			t.Errorf("FormatFloat(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGenerateDocumentID_NonEmpty(t *testing.T) {
	t.Parallel()
	id := GenerateDocumentID()
	if len(id) == 0 {
		t.Error("GenerateDocumentID: empty")
	}
}

func TestDefaultBookmarkConfigs(t *testing.T) {
	t.Parallel()
	split := DefaultSplitBookmarkConfig()
	if split.Handling != BookmarkUpdate {
		t.Errorf("split default handling: got %v, want BookmarkUpdate", split.Handling)
	}
	merge := DefaultMergeBookmarkConfig()
	if merge.Handling != BookmarkUpdate {
		t.Errorf("merge default handling: got %v, want BookmarkUpdate", merge.Handling)
	}
}
