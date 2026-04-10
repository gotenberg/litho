package mutations

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

func TestFilterAndRemap_DropOrphans(t *testing.T) {
	bookmarks := []models.Bookmark{
		{Title: "A", PageNumber: 1},
		{Title: "B", PageNumber: 3}, // dropped
		{Title: "C", PageNumber: 5},
	}
	keptOldPages := []int{1, 5}
	oldToNew := map[int]int{1: 1, 5: 2}
	newPageNum := func(old int) int { return oldToNew[old] }

	got := FilterAndRemap(bookmarks, newPageNum, OrphanDrop, true, keptOldPages)

	if len(got) != 2 {
		t.Fatalf("got %d bookmarks, want 2", len(got))
	}
	if got[0].Title != "A" || got[0].PageNumber != 1 {
		t.Errorf("first: got %+v", got[0])
	}
	if got[1].Title != "C" || got[1].PageNumber != 2 {
		t.Errorf("second: got %+v", got[1])
	}
}

func TestFilterAndRemap_PromoteOrphanChildren(t *testing.T) {
	bookmarks := []models.Bookmark{
		{Title: "Parent", PageNumber: 3, Children: []models.Bookmark{ // dropped
			{Title: "Child A", PageNumber: 1}, // kept
			{Title: "Child B", PageNumber: 5}, // kept
		}},
	}
	keptOldPages := []int{1, 5}
	oldToNew := map[int]int{1: 1, 5: 2}
	newPageNum := func(old int) int { return oldToNew[old] }

	got := FilterAndRemap(bookmarks, newPageNum, OrphanDrop, true, keptOldPages)

	if len(got) != 2 {
		t.Fatalf("expected 2 promoted children, got %d", len(got))
	}
	if got[0].Title != "Child A" || got[0].PageNumber != 1 {
		t.Errorf("child A: %+v", got[0])
	}
	if got[1].Title != "Child B" || got[1].PageNumber != 2 {
		t.Errorf("child B: %+v", got[1])
	}
}

func TestFilterAndRemap_NoPromote(t *testing.T) {
	bookmarks := []models.Bookmark{
		{Title: "Parent", PageNumber: 3, Children: []models.Bookmark{
			{Title: "Child", PageNumber: 1},
		}},
	}
	got := FilterAndRemap(bookmarks, func(old int) int {
		if old == 1 {
			return 1
		}
		return 0
	}, OrphanDrop, false, []int{1})

	if len(got) != 0 {
		t.Errorf("expected children to be dropped with parent, got %d", len(got))
	}
}

func TestFilterAndRemap_OrphanRemap(t *testing.T) {
	bookmarks := []models.Bookmark{
		{Title: "Orphan", PageNumber: 4}, // not kept; nearest is 5 (idx 1 → new 2)
	}
	keptOldPages := []int{1, 5, 10}
	oldToNew := map[int]int{1: 1, 5: 2, 10: 3}
	newPageNum := func(old int) int { return oldToNew[old] }

	got := FilterAndRemap(bookmarks, newPageNum, OrphanRemap, false, keptOldPages)
	if len(got) != 1 {
		t.Fatalf("expected 1 remapped bookmark, got %d", len(got))
	}
	if got[0].PageNumber != 2 {
		t.Errorf("expected remap to new page 2 (source page 5), got %d", got[0].PageNumber)
	}
}

func TestFilterAndRemap_OrphanKeep(t *testing.T) {
	bookmarks := []models.Bookmark{
		{Title: "Dropped", PageNumber: 3},
	}
	got := FilterAndRemap(bookmarks, func(int) int { return 0 }, OrphanKeep, false, []int{1})
	if len(got) != 1 {
		t.Fatalf("expected 1 kept bookmark, got %d", len(got))
	}
	if got[0].PageNumber != 0 {
		t.Errorf("expected zero page number, got %d", got[0].PageNumber)
	}
	if got[0].Title != "Dropped" {
		t.Errorf("expected title preserved, got %q", got[0].Title)
	}
}

func TestOffsetPages(t *testing.T) {
	bookmarks := []models.Bookmark{
		{Title: "A", PageNumber: 1, Children: []models.Bookmark{
			{Title: "A.1", PageNumber: 2},
		}},
		{Title: "B", PageNumber: 5},
	}
	got := OffsetPages(bookmarks, 10)
	if got[0].PageNumber != 11 {
		t.Errorf("A: got %d, want 11", got[0].PageNumber)
	}
	if got[0].Children[0].PageNumber != 12 {
		t.Errorf("A.1: got %d, want 12", got[0].Children[0].PageNumber)
	}
	if got[1].PageNumber != 15 {
		t.Errorf("B: got %d, want 15", got[1].PageNumber)
	}
}

func TestWrapInRoot(t *testing.T) {
	children := []models.Bookmark{
		{Title: "Section A", PageNumber: 1},
	}
	root := WrapInRoot(children, "Document 1", 1)
	if root.Title != "Document 1" {
		t.Errorf("title: %q", root.Title)
	}
	if root.PageNumber != 1 {
		t.Errorf("page: %d", root.PageNumber)
	}
	if len(root.Children) != 1 {
		t.Errorf("children: %d", len(root.Children))
	}
}

func TestNearestKeptPage(t *testing.T) {
	keptPages := []int{1, 5, 10}
	tests := []struct {
		oldPage int
		want    int
	}{
		{1, 1},  // exact match → idx 0 → new 1
		{4, 2},  // closest to 5 → idx 1 → new 2
		{6, 2},  // closest to 5 → idx 1 → new 2
		{8, 3},  // closest to 10 → idx 2 → new 3
		{15, 3}, // closest to 10 → new 3
	}
	for _, tc := range tests {
		got := nearestKeptPage(tc.oldPage, keptPages)
		if got != tc.want {
			t.Errorf("nearestKeptPage(%d, %v): got %d, want %d", tc.oldPage, keptPages, got, tc.want)
		}
	}
}
