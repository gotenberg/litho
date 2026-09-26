package mutations

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestDeletePages_RootCountMatchesAFlattenedTree pins that the root /Count
// [DeletePages] writes is the number of pages left.
//
// A /Pages node's /Count is the number of page objects descended from it at
// every level (ISO 32000-1, 7.7.3.2), not the length of its /Kids. The two are
// equal only while the tree is flat, which is what [DeletePages] rebuilds it
// as, so len() is the right count there and nothing in the file says so.
//
// Worth pinning because nothing else can catch it. litho's own reader never
// consults page-tree /Count: pdf.Reader.PageCount in pkg/pdf resolves the
// pages and returns len(pages), and pkg/internal/core/document contains no
// reference to the key at all. So a wrong /Count is invisible to litho and
// visible only to other readers, which is exactly how the sibling bug in the
// OUTLINE /Count survived until 2026-09-19.
func TestDeletePages_RootCountMatchesAFlattenedTree(t *testing.T) {
	ctx := context.Background()
	data := nestedPageTreePDF(t, false)

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	allPages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	if len(allPages) != 4 {
		t.Fatalf("fixture has %d pages, want 4: it must be the nested tree", len(allPages))
	}

	// The fixture nests: the root's only kid is an intermediate /Pages node.
	// Deleting through that shape is the case where a count taken from /Kids
	// could disagree with the number of pages actually descended from the root.
	pagesRef, ok := catalog.Dict["Pages"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("catalog /Pages = %T, want an indirect reference", catalog.Dict["Pages"])
	}

	alloc := writer.NewObjAllocator(20)
	if err := DeletePages(ctx, alloc, doc, allPages, []int{2}); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}

	root := pendingDictAt(t, alloc, pagesRef.ObjectNum)
	kids, ok := root["Kids"].(ast.PdfArray)
	if !ok {
		t.Fatalf("root /Kids = %T, want an array", root["Kids"])
	}
	count, ok := root["Count"].(ast.PdfInt)
	if !ok {
		t.Fatalf("root /Count = %T, want an integer", root["Count"])
	}

	if int(count) != len(kids) {
		t.Errorf("root /Count = %d but /Kids holds %d entries", count, len(kids))
	}
	if int(count) != 3 {
		t.Errorf("root /Count = %d, want 3: four pages less the one deleted", count)
	}

	// The invariant, rather than the arithmetic: /Count equals len(/Kids) only
	// because every kid is a leaf. An intermediate /Pages node surviving here
	// would make the two disagree without either value changing.
	for i, kid := range kids {
		ref, ok := kid.(ast.PdfIndirectRef)
		if !ok {
			t.Errorf("kid %d = %T, want an indirect reference", i, kid)
			continue
		}
		d := ResolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if d == nil {
			t.Errorf("kid %d (object %d) does not resolve", i, ref.ObjectNum)
			continue
		}
		if name, _ := d["Type"].(ast.PdfName); name != "Page" {
			t.Errorf("kid %d /Type = %v, want Page: the tree was not flattened, so /Count is no longer len(/Kids)",
				i, d["Type"])
		}
	}
}

// TestInsertPages_RootCountMatchesAFlattenedTree pins the same for
// [InsertPages].
//
// [InsertPages] derives its /Count from a different expression than its /Kids:
// totalCount is len(targetPages)+len(copiedRefs) while the array is assembled
// from three slices in insert_pages.go. They can drift apart here in a way
// they cannot in [DeletePages], where both come from one slice, so the equality
// is worth asserting rather than assuming.
func TestInsertPages_RootCountMatchesAFlattenedTree(t *testing.T) {
	ctx := context.Background()

	targetData := nestedPageTreePDF(t, false)
	target, err := document.NewDocument(ctx, bytes.NewReader(targetData), int64(len(targetData)), nil)
	if err != nil {
		t.Fatalf("NewDocument (target): %v", err)
	}
	targetCatalog, err := document.ResolveCatalog(ctx, target)
	if err != nil {
		t.Fatalf("ResolveCatalog (target): %v", err)
	}
	targetPages, err := document.ResolvePages(ctx, target, targetCatalog)
	if err != nil {
		t.Fatalf("ResolvePages (target): %v", err)
	}
	if len(targetPages) != 4 {
		t.Fatalf("target has %d pages, want the four-page nested fixture", len(targetPages))
	}

	srcData := onePagePDF(t)
	src, err := document.NewDocument(ctx, bytes.NewReader(srcData), int64(len(srcData)), nil)
	if err != nil {
		t.Fatalf("NewDocument (source): %v", err)
	}
	srcCatalog, err := document.ResolveCatalog(ctx, src)
	if err != nil {
		t.Fatalf("ResolveCatalog (source): %v", err)
	}
	srcPages, err := document.ResolvePages(ctx, src, srcCatalog)
	if err != nil {
		t.Fatalf("ResolvePages (source): %v", err)
	}

	pagesRef, ok := targetCatalog.Dict["Pages"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("catalog /Pages = %T, want an indirect reference", targetCatalog.Dict["Pages"])
	}

	alloc := writer.NewObjAllocator(target.HighestObjectNumber(ctx))
	if err := InsertPages(ctx, alloc, target, targetPages, 0, src, srcPages, []int{1}); err != nil {
		t.Fatalf("InsertPages: %v", err)
	}

	root := pendingDictAt(t, alloc, pagesRef.ObjectNum)
	kids, ok := root["Kids"].(ast.PdfArray)
	if !ok {
		t.Fatalf("root /Kids = %T, want an array", root["Kids"])
	}
	count, ok := root["Count"].(ast.PdfInt)
	if !ok {
		t.Fatalf("root /Count = %T, want an integer", root["Count"])
	}

	if int(count) != len(kids) {
		t.Errorf("root /Count = %d but /Kids holds %d entries: the count is computed separately from the array",
			count, len(kids))
	}
	if int(count) != 5 {
		t.Errorf("root /Count = %d, want 5: four target pages plus the one inserted", count)
	}

	for i, kid := range kids {
		ref, ok := kid.(ast.PdfIndirectRef)
		if !ok {
			t.Errorf("kid %d = %T, want an indirect reference", i, kid)
			continue
		}
		d := ResolveFromAllocOrDoc(ctx, alloc, target, ref)
		if d == nil {
			t.Errorf("kid %d (object %d) does not resolve", i, ref.ObjectNum)
			continue
		}
		if name, _ := d["Type"].(ast.PdfName); name != "Page" {
			t.Errorf("kid %d /Type = %v, want Page: an intermediate node survived the rebuild", i, d["Type"])
		}
	}
}
