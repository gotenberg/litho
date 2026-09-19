package mutations

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// countFixtureTree holds five visible items over three levels, with siblings at
// two of them. The shape matters: a single-child tree never reaches the /Prev
// and /Next branches, and a two-level tree cannot tell "immediate children"
// from "all descendants" for the item count.
//
//	A            (children A1, A2)
//	  A1         (child A1a)
//	    A1a
//	  A2
//	B
func countFixtureTree() []models.Bookmark {
	return []models.Bookmark{
		{Title: "A", PageNumber: 1, Children: []models.Bookmark{
			{Title: "A1", PageNumber: 1, Children: []models.Bookmark{
				{Title: "A1a", PageNumber: 1},
			}},
			{Title: "A2", PageNumber: 1},
		}},
		{Title: "B", PageNumber: 1},
	}
}

func countFixturePageRefs() map[int]ast.PdfIndirectRef {
	return map[int]ast.PdfIndirectRef{1: {ObjectNum: 3}}
}

func pdfIntAt(t *testing.T, d ast.PdfDict, key string) int {
	t.Helper()

	v, ok := d[ast.PdfName(key)]
	if !ok {
		t.Fatalf("/%s is absent from %v", key, d)
	}
	n, ok := v.(ast.PdfInt)
	if !ok {
		t.Fatalf("/%s = %T, want an integer", key, v)
	}
	return int(n)
}

func TestBuildOutline_RootCountCountsEveryVisibleItem(t *testing.T) {
	alloc := writer.NewObjAllocator(20)

	ref := BuildOutline(context.Background(), alloc, countFixturePageRefs(), countFixtureTree())
	if ref.ObjectNum == 0 {
		t.Fatal("BuildOutline returned a zero ref for a non-empty tree")
	}

	root := pendingDictAt(t, alloc, ref.ObjectNum)
	if got := pdfIntAt(t, root, "Count"); got != 5 {
		t.Errorf("root /Count = %d, want 5: every visible item at all levels, not the 2 top-level entries", got)
	}
}

func TestBuildOutline_ItemCountCountsEveryDescendant(t *testing.T) {
	alloc := writer.NewObjAllocator(20)

	ref := BuildOutline(context.Background(), alloc, countFixturePageRefs(), countFixtureTree())
	if ref.ObjectNum == 0 {
		t.Fatal("BuildOutline returned a zero ref for a non-empty tree")
	}
	root := pendingDictAt(t, alloc, ref.ObjectNum)

	firstRef, ok := root["First"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("root /First = %T, want an indirect reference", root["First"])
	}
	// The first item is A, whose descendants are A1, A1a and A2.
	if got := pdfIntAt(t, pendingDictAt(t, alloc, firstRef.ObjectNum), "Count"); got != 3 {
		t.Errorf("item /Count = %d, want 3: A1, A1a and A2, not the 2 immediate children", got)
	}
}

func TestWriteOutline_RootCountCountsEveryVisibleItem(t *testing.T) {
	doc := metadataFixture(t, true, false)
	alloc := writer.NewObjAllocator(20)

	if err := WriteOutline(context.Background(), alloc, doc, countFixturePageRefs(), countFixtureTree()); err != nil {
		t.Fatalf("WriteOutline: %v", err)
	}

	catalogRef, ok := CatalogRef(context.Background(), doc)
	if !ok {
		t.Fatal("the fixture has no catalog reference")
	}
	catalog := pendingDictAt(t, alloc, catalogRef.ObjectNum)
	outlineRef, ok := catalog["Outlines"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("catalog /Outlines = %T, want an indirect reference", catalog["Outlines"])
	}

	root := pendingDictAt(t, alloc, outlineRef.ObjectNum)
	if got := pdfIntAt(t, root, "Count"); got != 5 {
		t.Errorf("root /Count = %d, want 5: every visible item at all levels", got)
	}
}

// A flat list must keep counting as its own length: the recursive count has to
// generalise the old behaviour, not replace it.
func TestVisibleOutlineCount_FlatListCountsItsLength(t *testing.T) {
	flat := []models.Bookmark{
		{Title: "one", PageNumber: 1},
		{Title: "two", PageNumber: 1},
		{Title: "three", PageNumber: 1},
	}
	if got := visibleOutlineCount(flat); got != len(flat) {
		t.Errorf("visibleOutlineCount(flat) = %d, want %d", got, len(flat))
	}
	if got := visibleOutlineCount(nil); got != 0 {
		t.Errorf("visibleOutlineCount(nil) = %d, want 0", got)
	}
}

func TestVisibleOutlineCount_CountsEveryLevel(t *testing.T) {
	if got := visibleOutlineCount(countFixtureTree()); got != 5 {
		t.Errorf("visibleOutlineCount = %d, want 5 (A, A1, A1a, A2, B)", got)
	}
}
