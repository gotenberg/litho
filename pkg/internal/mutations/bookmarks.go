package mutations

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// SetBookmarks replaces the document outline tree. Pages is the full set of
// resolved pages used to map bookmark page numbers to indirect references.
func SetBookmarks(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page, bookmarks []models.Bookmark) error {
	if len(bookmarks) == 0 {
		return nil
	}

	// Build page number to indirect ref mapping.
	pageRefs := make(map[int]ast.PdfIndirectRef, len(pages))
	for i, p := range pages {
		pageRefs[i+1] = p.Ref // 1-based
	}

	return WriteOutline(ctx, alloc, doc, pageRefs, bookmarks)
}

// WriteOutline writes a bookmark tree to the allocator and patches the
// catalog's /Outlines entry. pageRefs maps 1-based page numbers in the
// output to their indirect references in the allocator. This is the
// low-level entry point behind [SetBookmarks]. [Split] and [Merge] build
// fresh catalogs and call [BuildOutline] instead.
func WriteOutline(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pageRefs map[int]ast.PdfIndirectRef,
	bookmarks []models.Bookmark,
) error {
	if len(bookmarks) == 0 {
		return nil
	}

	// Build outline items as a linked list (each item has /Next, /Prev, /Parent).
	outlineRef := alloc.Alloc(ast.PdfNull{}) // placeholder, will be replaced
	items := buildOutlineItems(alloc, bookmarks, outlineRef, pageRefs)

	if len(items) == 0 {
		return nil
	}

	// Build the root /Outlines dict.
	outlineDict := ast.PdfDict{
		"Type":  ast.PdfName("Outlines"),
		"First": items[0],
		"Last":  items[len(items)-1],
		"Count": ast.PdfInt(visibleOutlineCount(bookmarks)),
	}
	alloc.AllocAt(outlineRef.ObjectNum, outlineDict)

	// Update the catalog to reference the new /Outlines.
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return fmt.Errorf("trailer missing /Root indirect reference")
	}

	catalogDict, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return err
	}

	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Outlines"] = outlineRef

	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return nil
}

// BuildOutline allocates outline objects for the given bookmark tree and
// returns the ref to the root /Outlines dict (or a zero ref if nothing was
// written). The caller is responsible for inserting the returned ref into
// the catalog. Used by [Split] and [Merge], which build fresh catalogs.
func BuildOutline(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	pageRefs map[int]ast.PdfIndirectRef,
	bookmarks []models.Bookmark,
) ast.PdfIndirectRef {
	if len(bookmarks) == 0 {
		return ast.PdfIndirectRef{}
	}

	outlineRef := alloc.Alloc(ast.PdfNull{}) // placeholder
	items := buildOutlineItems(alloc, bookmarks, outlineRef, pageRefs)
	if len(items) == 0 {
		return ast.PdfIndirectRef{}
	}

	outlineDict := ast.PdfDict{
		"Type":  ast.PdfName("Outlines"),
		"First": items[0],
		"Last":  items[len(items)-1],
		"Count": ast.PdfInt(visibleOutlineCount(bookmarks)),
	}
	alloc.AllocAt(outlineRef.ObjectNum, outlineDict)
	return outlineRef
}

// visibleOutlineCount returns how many outline items a viewer shows for
// bookmarks, counting descendants at every level rather than the immediate
// slice.
//
// Per ISO 32000-1, 12.3.3 an outline dictionary's /Count is the total number of
// visible items at all levels, and an outline item's /Count is the number of
// its visible descendants. len(bookmarks) is only correct for a flat list: a
// nested tree reported its top level, so a two-level outline written by
// [SetBookmarks] claimed fewer items than it contains.
//
// litho writes every item open, with /First and /Last and no closed marker, so
// every descendant is visible and the value is the whole subtree.
//
// The walk carries no depth bound of its own. It traverses the same tree
// [buildOutlineItems] already recurses over, and a tree read back from a
// document arrives capped at maxOutlineDepth (100) by the outline parser, so
// this adds no reach the writer did not already have.
func visibleOutlineCount(bookmarks []models.Bookmark) int {
	total := 0
	for i := range bookmarks {
		total += 1 + visibleOutlineCount(bookmarks[i].Children)
	}
	return total
}

// buildOutlineItems creates outline item objects and returns their refs.
func buildOutlineItems(alloc *writer.ObjAllocator, bookmarks []models.Bookmark, parent ast.PdfIndirectRef, pageRefs map[int]ast.PdfIndirectRef) []ast.PdfIndirectRef {
	refs := make([]ast.PdfIndirectRef, len(bookmarks))

	// Every ref exists before any dict is built, so each item can name its
	// /Prev and /Next.
	for i := range bookmarks {
		refs[i] = alloc.Alloc(ast.PdfNull{}) // placeholder
	}

	for i, bm := range bookmarks {
		d := ast.PdfDict{
			// An outline item's /Title is a text string (ISO 32000-1, 12.3.3).
			"Title":  document.EncodePdfString(bm.Title),
			"Parent": parent,
		}

		// Destination: [pageRef /Fit]
		if pageRef, ok := pageRefs[bm.PageNumber]; ok {
			d["Dest"] = ast.PdfArray{pageRef, ast.PdfName("Fit")}
		}

		if i > 0 {
			d["Prev"] = refs[i-1]
		}
		if i < len(bookmarks)-1 {
			d["Next"] = refs[i+1]
		}

		// Recurse for children.
		if len(bm.Children) > 0 {
			childRefs := buildOutlineItems(alloc, bm.Children, refs[i], pageRefs)
			if len(childRefs) > 0 {
				d["First"] = childRefs[0]
				d["Last"] = childRefs[len(childRefs)-1]
				d["Count"] = ast.PdfInt(visibleOutlineCount(bm.Children))
			}
		}

		alloc.AllocAt(refs[i].ObjectNum, d)
	}

	return refs
}
