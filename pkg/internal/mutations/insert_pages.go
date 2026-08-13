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

	// Copy source page objects into the target allocator. copyPageForOutput
	// carries the Resources, MediaBox, and Rotate the source pages inherited
	// from their own tree, since the flat target tree gives them nothing to
	// inherit.
	copier := writer.NewCopier(srcDoc, alloc)
	copiedRefs := make([]ast.PdfIndirectRef, len(pagesToInsert))
	for i, page := range pagesToInsert {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("inserting pages canceled: %w", err)
		}
		ref, err := copyPageForOutput(ctx, copier, alloc, page)
		if err != nil {
			return fmt.Errorf("copying source page %d: %w", i+1, err)
		}
		copiedRefs[i] = ref
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

	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return fmt.Errorf("catalog missing /Pages")
	}
	pagesRef, ok := pagesObj.(ast.PdfIndirectRef)
	if !ok {
		return fmt.Errorf("/Pages is not an indirect reference")
	}

	// Build new /Kids: target pages [0..pos) + inserted + target pages [pos..).
	totalCount := len(targetPages) + len(copiedRefs)
	kids := make(ast.PdfArray, 0, totalCount)
	for i := range pos {
		kids = append(kids, targetPages[i].Ref)
	}
	for _, ref := range copiedRefs {
		kids = append(kids, ref)
	}
	for i := pos; i < len(targetPages); i++ {
		kids = append(kids, targetPages[i].Ref)
	}

	// Replace the /Pages dict with a flat tree.
	alloc.AllocAt(pagesRef.ObjectNum, ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  kids,
		"Count": ast.PdfInt(totalCount),
	})

	// Update /Parent on all pages (existing and inserted) to point to the
	// pages node.
	for i := range pos {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("inserting pages canceled: %w", err)
		}
		page := targetPages[i]
		newDict := make(ast.PdfDict, len(page.Dict)+4)
		maps.Copy(newDict, page.Dict)
		newDict["Parent"] = pagesRef
		materializeInheritedAttrs(newDict, page)
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

	for i := pos; i < len(targetPages); i++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("inserting pages canceled: %w", err)
		}
		page := targetPages[i]
		newDict := make(ast.PdfDict, len(page.Dict)+4)
		maps.Copy(newDict, page.Dict)
		newDict["Parent"] = pagesRef
		materializeInheritedAttrs(newDict, page)
		alloc.AllocAt(page.Ref.ObjectNum, newDict)
	}

	return nil
}
