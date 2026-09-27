package mutations

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// InsertPages copies pages from a source document into the target document at
// the given 0-based position. Position 0 inserts before the first page;
// position equal to the current page count appends. If sourcePageNums is
// empty, all source pages are inserted. Page numbers in sourcePageNums are
// 1-based.
//
// It treats the copied pages as [Merge] treats a later source's: their fields
// join the form under their own names, their signed signature fields are drawn
// into the pages and stay out of the form (see [bakeSignedSignatures] and
// [mergeInsertedFields]), and the target's catalog /Version is raised to the
// version the source reads as (see [raiseMergedCatalogVersion]). The rewrite
// raises the header (see [FullRewriteConfig.PageSources]).
func InsertPages(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	targetPages []*document.Page,
	pos int,
	srcDoc *document.Document,
	srcPages []*document.Page,
	sourcePageNums []int,
) error {
	if pos < 0 || pos > len(targetPages) {
		return fmt.Errorf("insert position %d out of range [0, %d]", pos, len(targetPages))
	}

	// Determine which source pages to insert.
	pagesToInsert := srcPages
	if len(sourcePageNums) > 0 {
		pagesToInsert = make([]*document.Page, len(sourcePageNums))
		for i, num := range sourcePageNums {
			if num < 1 || num > len(srcPages) {
				return fmt.Errorf("source page %d out of range [1, %d]", num, len(srcPages))
			}
			pagesToInsert[i] = srcPages[num-1]
		}
	}

	if len(pagesToInsert) == 0 {
		return nil
	}

	// Resolve the catalog and pages tree.
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return fmt.Errorf("trailer missing /Root indirect reference")
	}

	catalogDict := ResolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return fmt.Errorf("resolving catalog dict")
	}

	// Copy source page objects into the target allocator. copyPageForOutput
	// carries the Resources, MediaBox, CropBox and Rotate the source pages
	// inherited from their own tree, since the flat target tree gives them
	// nothing to inherit.
	copier := writer.NewCopier(srcDoc, alloc)
	overrides := pageCopyOverrides(pagesToInsert)
	if removed := unselectedPages(srcPages, pagesToInsert); len(removed) > 0 {
		splitDestOverrides(ctx, srcDoc, pagesToInsert, removed, overrides)
		if srcCatalog, err := document.ResolveCatalog(ctx, srcDoc); err == nil && srcCatalog != nil {
			dropped := droppedPageAnnots(ctx, nil, srcDoc, srcPages, removed)
			// Dropped on purpose, for the same reason as split: insert copies
			// only the selected source pages into the target, so an unselected
			// source page is unreachable from what it copies. Measured past the
			// bound, insert never carried one.
			fieldOverrides, _ := fieldKidOverrides(ctx, nil, srcDoc, srcCatalog.Dict, dropped)
			maps.Copy(overrides, fieldOverrides)
		}
	}
	// A tagged target is tagged by the test [FlattenForm] applies, and none of
	// its structure elements encloses a copied widget: the insertion carries
	// the source's pages, not its structure.
	tagged := newFlattenTags(ctx, alloc, doc, catalogRef, catalogDict) != nil
	overrides, err := bakeSignedSignatures(ctx, srcDoc, pagesToInsert, overrides, tagged)
	if err != nil {
		return err
	}
	if len(overrides) > 0 {
		copier.WithOverrides(overrides)
	}
	copiedRefs := make([]ast.PdfIndirectRef, len(pagesToInsert))
	for i, page := range pagesToInsert {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("inserting pages canceled: %w", err)
		}
		ref, err := copyPageForOutput(ctx, copier, alloc, srcDoc, page)
		if err != nil {
			return fmt.Errorf("copying source page %d: %w", i+1, err)
		}
		copiedRefs[i] = ref
	}

	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return fmt.Errorf("catalog missing /Pages")
	}
	pagesRef, ok := pagesObj.(ast.PdfIndirectRef)
	if !ok {
		return fmt.Errorf("/Pages is not an indirect reference")
	}

	// The document's pages are the ones an earlier deletion or insertion
	// staged, when one did (see [stagedPageKids]); pos still names a place
	// among the Reader's pages. The inserted pages go before the first of the
	// Reader's pages from pos on that the document still holds, and after its
	// last page when there is none.
	current, at := make([]ast.PdfIndirectRef, len(targetPages)), pos
	for i, page := range targetPages {
		current[i] = page.Ref
	}
	if staged, ok := stagedPageKids(ctx, alloc, doc); ok {
		current, at = staged, len(staged)
		stagedAt := make(map[int]int, len(staged))
		for i, ref := range staged {
			stagedAt[ref.ObjectNum] = i
		}
		for _, page := range targetPages[pos:] {
			if i, held := stagedAt[page.Ref.ObjectNum]; held {
				at = i
				break
			}
		}
	}

	// Build new /Kids: pages [0..at) + inserted + pages [at..).
	kids := make(ast.PdfArray, 0, len(current)+len(copiedRefs))
	for _, ref := range current[:at] {
		kids = append(kids, ref)
	}
	for _, ref := range copiedRefs {
		kids = append(kids, ref)
	}
	for _, ref := range current[at:] {
		kids = append(kids, ref)
	}

	// Replace the /Pages dict with a flat tree.
	alloc.AllocAt(pagesRef.ObjectNum, ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  kids,
		"Count": ast.PdfInt(len(kids)),
	})

	// Update /Parent on all pages (existing and inserted) to point to the
	// pages node. A page an earlier insertion staged already names it.
	readerPages := make(map[int]*document.Page, len(targetPages))
	for _, page := range targetPages {
		readerPages[page.Ref.ObjectNum] = page
	}
	for _, ref := range current {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("inserting pages canceled: %w", err)
		}
		page, ok := readerPages[ref.ObjectNum]
		if !ok {
			continue
		}
		base := latestPageDict(alloc, page)
		newDict := make(ast.PdfDict, len(base)+4)
		maps.Copy(newDict, base)
		newDict["Parent"] = pagesRef
		materializeInheritedAttrs(ctx, alloc, doc, newDict, page)
		alloc.AllocAt(page.Ref.ObjectNum, newDict)
	}

	for _, ref := range copiedRefs {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("inserting pages canceled: %w", err)
		}
		if obj, ok := alloc.Pending()[ref.ObjectNum]; ok {
			if pageDict, ok := obj.(ast.PdfDict); ok {
				pageDict["Parent"] = pagesRef
			}
		}
	}

	if err := mergeInsertedFields(ctx, alloc, doc, catalogRef, current, srcDoc, copier, copiedRefs); err != nil {
		return fmt.Errorf("adding the inserted pages' form fields: %w", err)
	}

	// The inserted pages need the version their source reads as, as a merge's
	// pages do. The header is the rewrite's to raise (see
	// [FullRewriteConfig.PageSources]).
	if catalog := ResolveFromAllocOrDoc(ctx, alloc, doc, catalogRef); catalog != nil {
		raised := maps.Clone(catalog)
		if raiseMergedCatalogVersion(raised, srcDoc.Version) {
			alloc.AllocAt(catalogRef.ObjectNum, raised)
		}
	}
	return nil
}
