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

	// The pages an earlier deletion or insertion staged are the ones this
	// deletion removes pages from. The numbers still name the Reader's pages.
	if staged, ok := stagedPageKids(ctx, alloc, doc); ok {
		deleted := make(map[int]bool, len(deleteSet))
		for p := range deleteSet {
			deleted[allPages[p-1].Ref.ObjectNum] = true
		}
		kept := make([]ast.PdfIndirectRef, 0, len(staged))
		for _, ref := range staged {
			if !deleted[ref.ObjectNum] {
				kept = append(kept, ref)
			}
		}
		if len(kept) == 0 {
			return fmt.Errorf("cannot delete all %d pages", len(staged))
		}
		return rebuildPageTree(ctx, alloc, doc, allPages, kept)
	}

	// Collect kept page indices (0-based), preserving original order.
	kept := make([]int, 0, len(allPages)-len(deleteSet))
	for i := range allPages {
		if !deleteSet[i+1] {
			kept = append(kept, i)
		}
	}

	sort.Ints(kept)

	keptRefs := make([]ast.PdfIndirectRef, len(kept))
	for i, idx := range kept {
		keptRefs[i] = allPages[idx].Ref
	}
	return rebuildPageTree(ctx, alloc, doc, allPages, keptRefs)
}

// stagedPageKids returns the pages of the page tree an earlier deletion or
// insertion staged, in order, and false when none did.
//
// Both write the tree as a single /Pages node whose /Kids are the pages
// themselves, at the object number of the catalog's /Pages. Rebuilding it from
// the Reader's pages instead dropped what the earlier one had done: a second
// insertion lost the first, and an insertion after a deletion put the deleted
// page back. A staged node that is not flat, one a remediation rewrote without
// changing which pages the document holds, reports false, and so does one
// whose kids are not all indirect: the Reader's pages are then the document's.
//
// # Corpus evidence
//
//   - All corpora: of 100 files given two insertions on one Writer, all 100
//     lost the first, and of the 33 with two pages or more, all 33 lost an
//     insertion queued before a deletion and got back the page a deletion
//     queued before an insertion had removed.
func stagedPageKids(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) ([]ast.PdfIndirectRef, bool) {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return nil, false
	}
	catalogDict := ResolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	pagesRef, ok := catalogDict["Pages"].(ast.PdfIndirectRef)
	if !ok {
		return nil, false
	}
	node, ok := alloc.Pending()[pagesRef.ObjectNum].(ast.PdfDict)
	if !ok {
		return nil, false
	}
	kids, err := resolveArrayAllocFirst(ctx, alloc, doc, node["Kids"])
	if err != nil {
		return nil, false
	}
	refs := make([]ast.PdfIndirectRef, 0, len(kids))
	for _, kid := range kids {
		ref, isRef := kid.(ast.PdfIndirectRef)
		if !isRef {
			return nil, false
		}
		kidDict, err := resolveDictAllocFirst(ctx, alloc, doc, ref)
		if err != nil || kidDict == nil {
			return nil, false
		}
		// ISO 32000-1, 7.7.3.2 and 7.7.3.3: an intermediate node is /Type
		// /Pages and carries /Kids, a leaf is a page and does not.
		if _, hasKids := kidDict["Kids"]; hasKids {
			return nil, false
		}
		if typ, _ := kidDict["Type"].(ast.PdfName); typ == "Pages" {
			return nil, false
		}
		refs = append(refs, ref)
	}
	return refs, true
}

// rebuildPageTree replaces the document's page tree with a flat /Pages node
// listing kept, in order. A kept page the Reader does not hold is one an
// earlier insertion staged, already a leaf of that node.
//
// # Corpus evidence
//
//   - govdocs thread_100_100852.pdf: before destinations naming a dropped page
//     were pruned, deleting page 1 left the page behind with all 1191 bytes of
//     it.
func rebuildPageTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, allPages []*document.Page, kept []ast.PdfIndirectRef) error {
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

	kids := make(ast.PdfArray, len(kept))
	keptNums := make(map[int]bool, len(kept))
	for i, ref := range kept {
		kids[i] = ref
		keptNums[ref.ObjectNum] = true
	}

	// Replace the existing /Pages dict with a flat tree.
	alloc.AllocAt(pagesRef.ObjectNum, ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  kids,
		"Count": ast.PdfInt(len(kept)),
	})

	// A destination that still names a dropped page is both a broken GoTo and a
	// live reference to that page, so the writer carries the page and its
	// content stream into the output.
	removed := make(map[int]bool)
	readerPages := make(map[int]*document.Page, len(allPages))
	for _, page := range allPages {
		readerPages[page.Ref.ObjectNum] = page
		if !keptNums[page.Ref.ObjectNum] {
			removed[page.Ref.ObjectNum] = true
		}
	}

	// Update each kept page's /Parent to point to the pages node. A page an
	// earlier insertion staged already names it.
	for _, ref := range kept {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("rebuilding page tree canceled: %w", err)
		}
		page, ok := readerPages[ref.ObjectNum]
		if !ok {
			continue
		}
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
		destsTruncated := pruneCatalogDestinations(ctx, alloc, doc, catalogRef, catalogDict, removed)

		// A field keeps the widgets that present it on each page, and every
		// widget names its page in /P, so a widget left behind keeps the page
		// too. Delete reuses the source object numbers in place, which is
		// exactly what fieldKidOverrides is keyed by.
		dropped := droppedPageAnnots(ctx, alloc, doc, allPages, removed)
		fieldOverrides, fieldsTruncated := fieldKidOverrides(ctx, alloc, doc, catalogDict, dropped)
		for objNum, patched := range fieldOverrides {
			alloc.AllocAt(objNum, patched)
		}

		if err := pruneStructTree(ctx, alloc, doc, catalogDict, removed); err != nil {
			return fmt.Errorf("pruning the structure tree: %w", err)
		}
		outlineTruncated := pruneOutlineDestinations(ctx, alloc, doc, catalogDict, removed)
		pruneArticleBeads(ctx, alloc, doc, catalogDict, removed)
		webTruncated := pruneWebCaptureSets(ctx, alloc, doc, catalogDict, removed)

		// Each prune unhooks one kind of reference to the page. Any of them
		// stopping short leaves a reference the rewrite will follow, so the page
		// and its content would ship inside a document reporting one fewer page.
		// Refuse before anything is written rather than delete it halfway.
		if destsTruncated || fieldsTruncated || outlineTruncated || webTruncated {
			return fmt.Errorf("pruning references to the removed pages: %w", ErrPagePruneIncomplete)
		}
	}

	return nil
}
