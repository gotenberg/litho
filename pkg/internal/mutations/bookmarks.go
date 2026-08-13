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
// low-level entry point used by Split and Merge, which already have the
// new page refs from their copier.
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
		"Count": ast.PdfInt(len(bookmarks)),
	}
	alloc.AllocAt(outlineRef.ObjectNum, outlineDict)

	// Update the catalog to reference the new /Outlines.
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return fmt.Errorf("trailer missing /Root indirect reference")
	}

	// Read the current catalog from the allocator if already modified,
	// otherwise from the document.
	var catalogDict ast.PdfDict
	if pending, ok := alloc.Pending()[catalogRef.ObjectNum]; ok {
		if d, ok := pending.(ast.PdfDict); ok {
			catalogDict = d
		}
	}
	if catalogDict == nil {
		d, err := CatalogDict(ctx, doc, catalogRef)
		if err != nil {
			return err
		}
		catalogDict = d
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
// the catalog. Used by Split and Merge which build fresh catalogs.
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
		"Count": ast.PdfInt(len(bookmarks)),
	}
	alloc.AllocAt(outlineRef.ObjectNum, outlineDict)
	return outlineRef
}

// buildOutlineItems creates outline item objects and returns their refs.
func buildOutlineItems(alloc *writer.ObjAllocator, bookmarks []models.Bookmark, parent ast.PdfIndirectRef, pageRefs map[int]ast.PdfIndirectRef) []ast.PdfIndirectRef {
	refs := make([]ast.PdfIndirectRef, len(bookmarks))

	// First pass: allocate refs.
	for i := range bookmarks {
		refs[i] = alloc.Alloc(ast.PdfNull{}) // placeholder
	}

	// Second pass: build dicts with links.
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
				d["Count"] = ast.PdfInt(len(bm.Children))
			}
		}

		alloc.AllocAt(refs[i].ObjectNum, d)
	}

	return refs
}
