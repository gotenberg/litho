package mutations

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// DeletePages removes pages at the given 1-based positions from the document's
// page tree. It rebuilds the /Pages tree as a flat list containing only the
// remaining pages.
func DeletePages(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, allPages []*document.Page, pagesToDelete []int) error {
	if len(pagesToDelete) == 0 {
		return nil
	}

	deleteSet := make(map[int]bool, len(pagesToDelete))
	for _, p := range pagesToDelete {
		if p < 1 || p > len(allPages) {
			return fmt.Errorf("page %d out of range [1, %d]", p, len(allPages))
		}
		deleteSet[p] = true
	}

	if len(deleteSet) >= len(allPages) {
		return fmt.Errorf("cannot delete all %d pages", len(allPages))
	}

	// Collect kept page indices (0-based), preserving original order.
	kept := make([]int, 0, len(allPages)-len(deleteSet))
	for i := range allPages {
		if !deleteSet[i+1] {
			kept = append(kept, i)
		}
	}

	sort.Ints(kept)

	return rebuildPageTree(ctx, alloc, doc, allPages, kept)
}

// rebuildPageTree replaces the document's page tree with a flat /Pages node
// containing only the pages at the given 0-based indices (in order).
func rebuildPageTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, allPages []*document.Page, keptIndices []int) error {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return fmt.Errorf("trailer missing /Root indirect reference")
	}

	catalogDict := ResolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return fmt.Errorf("resolving catalog dict")
	}

	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return fmt.Errorf("catalog missing /Pages")
	}
	pagesRef, ok := pagesObj.(ast.PdfIndirectRef)
	if !ok {
		return fmt.Errorf("/Pages is not an indirect reference")
	}

	// Build new /Kids array from kept pages.
	kids := make(ast.PdfArray, len(keptIndices))
	for i, idx := range keptIndices {
		kids[i] = allPages[idx].Ref
	}

	// Replace the existing /Pages dict with a flat tree.
	alloc.AllocAt(pagesRef.ObjectNum, ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  kids,
		"Count": ast.PdfInt(len(keptIndices)),
	})

	// A destination that still names a dropped page is both a broken GoTo and a
	// live reference to that page, so the writer carries the page and its
	// content stream into the output: deleting page 1 of govdocs
	// thread_100_100852.pdf left the page behind with all 1191 bytes of it.
	removed := removedPageSet(allPages, keptIndices)

	// Update each kept page's /Parent to point to the pages node.
	for _, idx := range keptIndices {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("rebuilding page tree canceled: %w", err)
		}
		page := allPages[idx]
		base := latestPageDict(alloc, page)
		newDict := make(ast.PdfDict, len(base)+4)
		maps.Copy(newDict, base)
		newDict["Parent"] = pagesRef
		materializeInheritedAttrs(newDict, page)
		if len(removed) > 0 {
			pruneLinkAnnots(ctx, alloc, doc, newDict, removed)
		}
		alloc.AllocAt(page.Ref.ObjectNum, newDict)
	}

	if len(removed) > 0 {
		pruneCatalogDestinations(ctx, alloc, doc, catalogRef, catalogDict, removed)

		// A field keeps the widgets that present it on each page, and every
		// widget names its page in /P, so a widget left behind keeps the page
		// too. Delete reuses the source object numbers in place, which is
		// exactly what fieldKidOverrides is keyed by.
		dropped := droppedPageAnnots(ctx, doc, allPages, removed)
		for objNum, patched := range fieldKidOverrides(ctx, doc, catalogDict, dropped) {
			alloc.AllocAt(objNum, patched)
		}

		if err := pruneStructTree(ctx, alloc, doc, catalogDict, removed); err != nil {
			return fmt.Errorf("pruning the structure tree: %w", err)
		}
		pruneOutlineDestinations(ctx, alloc, doc, catalogDict, removed)
		pruneArticleBeads(ctx, alloc, doc, catalogDict, removed)
		pruneWebCaptureSets(ctx, alloc, doc, catalogDict, removed)
	}

	return nil
}
