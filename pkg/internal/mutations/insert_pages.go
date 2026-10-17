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
// 1-based, and one named more than once is inserted each time, a page of its
// own (see [pageRanks] and [shareRepeatedPages]).
//
// It treats the copied pages as [Merge] treats a later source's: their fields
// join the form under their own names, their signed signature fields are drawn
// into the pages and stay out of the form (see [bakeSignedSignatures] and
// [mergeInsertedFields]), and the target's catalog /Version is raised to the
// version the source reads as (see [raiseMergedCatalogVersion]). The rewrite
// raises the header (see [FullRewriteConfig.PageSources]). Every page keeps
// the label it displayed (see [insertPageLabels]), and the inserted optional
// content stays drawn or hidden as its source configured it (see
// [insertOptionalContent]).
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
		return fmt.Errorf("inserting at position %d: %w (positions run 0 to %d)", pos, ErrInvalidPageNumber, len(targetPages))
	}
	// A copied page with no media box a viewer uses is sized from the pages of
	// both documents, as the source reads it. The caller's default page size
	// is the write's, which runs later: a page no size of the two documents
	// holds is left to it (see [document.MediaBoxDefault.DerivedOnly]).
	media := &document.MediaBoxDefault{Docs: []*document.Document{doc, srcDoc}, DerivedOnly: true}

	// Determine which source pages to insert, and where each stands in the
	// source.
	pagesToInsert := srcPages
	srcIndices := make([]int, len(srcPages))
	for i := range srcIndices {
		srcIndices[i] = i
	}
	if len(sourcePageNums) > 0 {
		pagesToInsert = make([]*document.Page, len(sourcePageNums))
		srcIndices = make([]int, len(sourcePageNums))
		for i, num := range sourcePageNums {
			if num < 1 || num > len(srcPages) {
				return fmt.Errorf("inserting source page %d: %w (source has %d pages)", num, ErrInvalidPageNumber, len(srcPages))
			}
			pagesToInsert[i] = srcPages[num-1]
			srcIndices[i] = num - 1
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

	// The inserted pages carry their structure into the target's tree, or into
	// a tree of their own when the target has none (see [structOutput]). A
	// target whose tree cannot be extended whole keeps it as it is, and the
	// pages join it untagged.
	target, targetUsable, err := newTargetStructOutput(ctx, alloc, doc, catalogRef)
	if err != nil {
		return fmt.Errorf("reading the target's structure tree: %w", err)
	}
	targetTagged := target != nil || !targetUsable
	marking := bakeUnmarked
	switch {
	case targetUsable && hasStructTree(ctx, srcDoc):
		marking = bakeTagged
	case targetTagged:
		// No structure element of the target encloses a copied widget.
		marking = bakeArtifacts
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

	// Copy source page objects into the target allocator. copyPageForOutput
	// carries the Resources, MediaBox, CropBox and Rotate the source pages
	// inherited from their own tree, since the flat target tree gives them
	// nothing to inherit. A source page named more than once is copied once
	// for each naming, each later round by a copier of its own (see
	// [pageRanks] and [shareRepeatedPages]), with elements of its own in the
	// structure tree.
	removed := unselectedPages(srcPages, pagesToInsert)
	copiedRefs := make([]ast.PdfIndirectRef, len(pagesToInsert))
	firstCopies := make(map[int]ast.PdfIndirectRef)
	var copier *writer.Copier
	var untagged []untaggedCopy
	newTree, carried := false, false
	for rank, indices := range pageRanks(pagesToInsert) {
		rankPages := pagesAt(pagesToInsert, indices)
		overrides, structs, err := pageCopyPlan(ctx, srcDoc, srcPages, rankPages, removed, marking)
		if err != nil {
			return err
		}
		c := writer.NewCopier(srcDoc, alloc)
		if len(overrides) > 0 {
			c.WithOverrides(overrides)
		}
		if rank == 0 {
			copier = c
		} else if err := shareRepeatedPages(ctx, srcDoc, copier, c, rankPages, firstCopies); err != nil {
			return err
		}
		if structs != nil {
			if target == nil {
				_, lang := catalogLang(ctx, alloc, doc, catalogDict)
				target, newTree = newStructOutput(alloc, lang), true
			}
			target.expect(structs, c)
		}

		copied := make([]ast.PdfIndirectRef, 0, len(indices))
		for _, i := range indices {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("inserting pages canceled: %w", err)
			}
			ref, err := copyPageForOutput(ctx, c, alloc, srcDoc, pagesToInsert[i], media)
			if err != nil {
				return fmt.Errorf("copying source page %d: %w", i+1, err)
			}
			copiedRefs[i] = ref
			copied = append(copied, ref)
			if rank == 0 && pagesToInsert[i].Ref.ObjectNum > 0 {
				firstCopies[pagesToInsert[i].Ref.ObjectNum] = ref
			}
		}
		if structs == nil {
			untagged = append(untagged, untaggedCopy{copier: c, pages: copied})
			continue
		}
		// In a tree of their own the pages' elements are the whole tree. In
		// the target's they join where their pages did, a later round's after
		// the elements an earlier one put there.
		place := -1
		if !newTree {
			place = target.insertionIndex(ctx, structs, current, at)
		}
		if err := target.add(ctx, structs, copied, place); err != nil {
			return fmt.Errorf("carrying the inserted pages' structure: %w", err)
		}
		carried = true
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
	if err := insertPageLabels(ctx, alloc, doc, catalogRef, current, at, copiedRefs, srcDoc, len(srcPages), srcIndices); err != nil {
		return err
	}
	if err := insertOptionalContent(ctx, alloc, doc, catalogRef, srcDoc, copier); err != nil {
		return fmt.Errorf("adding the inserted pages' optional content: %w", err)
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

	switch {
	case carried:
		// Last, after every copy from the source, whose content may name
		// structure keys the tree has to carry or, from a round copied
		// without its structure, drop.
		for _, u := range untagged {
			target.untag(u.copier, u.pages)
		}
		rootRef, err := target.finish(ctx)
		if err != nil {
			return fmt.Errorf("carrying the inserted pages' structure: %w", err)
		}
		catalog, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
		if err != nil {
			return fmt.Errorf("naming the structure tree in the catalog: %w", err)
		}
		if current, _ := catalog["StructTreeRoot"].(ast.PdfIndirectRef); current != rootRef {
			updated := maps.Clone(catalog)
			updated["StructTreeRoot"] = rootRef
			alloc.AllocAt(catalogRef.ObjectNum, updated)
		}
	case targetTagged:
		// The inserted content joins a tagged document without its structure,
		// so its keys index none of the target's /ParentTree entries.
		for _, u := range untagged {
			stripStructKeys(alloc, u.copier, u.pages)
		}
	}
	return nil
}

// hasStructTree reports whether doc's catalog names a structure tree root
// (ISO 32000-1, 14.7.2).
func hasStructTree(ctx context.Context, doc *document.Document) bool {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil || catalog == nil {
		return false
	}
	root, err := document.ResolveDict(ctx, doc, catalog.Dict["StructTreeRoot"])
	return err == nil && root != nil
}
