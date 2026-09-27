package mutations

import (
	"fmt"
)

// OrphanHandling controls what happens to bookmarks whose target page is
// no longer in the output document.
type OrphanHandling int

const (
	// OrphanDrop removes the bookmark entirely.
	OrphanDrop OrphanHandling = iota
	// OrphanRemap retargets the bookmark to the nearest kept page.
	OrphanRemap
	// OrphanKeep emits the bookmark with a zero PageNumber so the title
	// remains visible but the destination is dropped by [SetBookmarks].
	OrphanKeep
)

// FilterAndRemap walks a bookmark tree and applies an orphan policy. The
// newPageNum function returns the new 1-based page number for an old one,
// or 0 if the page is not in the kept set. Returns the rewritten tree.
//
// When promoteOrphanChildren is true, the children of a dropped bookmark
// are promoted to the dropped bookmark's level (recursively) so that
// surviving descendants are not lost.
func FilterAndRemap(
	bookmarks []OutlineItem,
	newPageNum func(int) int,
	policy OrphanHandling,
	promoteOrphanChildren bool,
	keptPages []int,
) []OutlineItem {
	if len(bookmarks) == 0 {
		return nil
	}
	out := make([]OutlineItem, 0, len(bookmarks))
	for _, bm := range bookmarks {
		newPage := newPageNum(bm.PageNumber)
		isOrphan := newPage == 0

		// Recurse into children first.
		processedChildren := FilterAndRemap(bm.Children, newPageNum, policy, promoteOrphanChildren, keptPages)

		if !isOrphan {
			bm.PageNumber = newPage
			bm.Children = processedChildren
			out = append(out, bm)
			continue
		}

		// Orphan handling.
		switch policy {
		case OrphanDrop:
			if promoteOrphanChildren && len(processedChildren) > 0 {
				out = append(out, processedChildren...)
			}
			// else: drop everything
		case OrphanRemap:
			remapped := nearestKeptPage(bm.PageNumber, keptPages)
			if remapped > 0 {
				bm.PageNumber = remapped
				bm.Children = processedChildren
				out = append(out, bm)
			} else if promoteOrphanChildren && len(processedChildren) > 0 {
				// No kept page at all (shouldn't happen), promote children.
				out = append(out, processedChildren...)
			}
		case OrphanKeep:
			bm.PageNumber = 0 // SetBookmarks emits no Dest when page is missing
			bm.Children = processedChildren
			out = append(out, bm)
		}
	}
	return out
}

// nearestKeptPage returns the kept page (1-based source numbering) closest
// to oldPage. Returns the new page number after remapping. keptPages is the
// sorted list of source page numbers that survived. Returns 0 if empty.
func nearestKeptPage(oldPage int, keptPages []int) int {
	if len(keptPages) == 0 {
		return 0
	}
	bestIdx := 0
	bestDelta := abs(keptPages[0] - oldPage)
	for i := 1; i < len(keptPages); i++ {
		d := abs(keptPages[i] - oldPage)
		if d < bestDelta {
			bestDelta = d
			bestIdx = i
		}
	}
	// keptPages[bestIdx] is the source page; the new 1-based number is bestIdx+1.
	return bestIdx + 1
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// OffsetPages adds offset to every PageNumber in the tree. Used by [Merge]
// to shift each source's bookmarks into the concatenated page space.
//
// Page number zero means the bookmark names no page of its document, which is
// what an [OutlineItem] carries for a destination that is null, points outside
// the file, or does not resolve. Shifting it produced a page number in the
// preceding source. The entry is kept without a destination instead, which is
// what the writer emits for zero.
//
// # Corpus evidence
//
//   - govdocs thread_400_400931.pdf, thread_400_400934.pdf: merging them sent
//     the latter's "cr2002073.pdf" bookmark, whose /Dest is [null 0 0 1], to
//     page 5, the last page of the other document.
func OffsetPages(bookmarks []OutlineItem, offset int) []OutlineItem {
	if len(bookmarks) == 0 || offset == 0 {
		return bookmarks
	}
	out := make([]OutlineItem, len(bookmarks))
	for i, bm := range bookmarks {
		if bm.PageNumber > 0 {
			bm.PageNumber += offset
		}
		bm.Children = OffsetPages(bm.Children, offset)
		out[i] = bm
	}
	return out
}

// WrapInRoot wraps a flat slice of bookmarks under a single root container
// with the given title pointing to firstPage. Returns the new root bookmark.
func WrapInRoot(bookmarks []OutlineItem, title string, firstPage int) OutlineItem {
	return OutlineItem{
		Title:      title,
		PageNumber: firstPage,
		Children:   bookmarks,
	}
}

// DefaultSourceLabel returns the default label for a wrapped source when
// no explicit SourceLabels entry was provided.
func DefaultSourceLabel(idx int) string {
	return fmt.Sprintf("Document %d", idx+1)
}
