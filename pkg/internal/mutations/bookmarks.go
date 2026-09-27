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
//
// A page an earlier deletion removed from the staged page tree gets no
// destination, as [pruneOutlineDestinations] leaves an item naming a deleted
// page: a destination naming it is enough to write the page into the output.
func SetBookmarks(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page, bookmarks []models.Bookmark) error {
	if len(bookmarks) == 0 {
		return nil
	}

	var held map[int]bool
	if staged, ok := stagedPageKids(ctx, alloc, doc); ok {
		held = make(map[int]bool, len(staged))
		for _, ref := range staged {
			held[ref.ObjectNum] = true
		}
	}

	// Build page number to indirect ref mapping.
	pageRefs := make(map[int]ast.PdfIndirectRef, len(pages))
	for i, p := range pages {
		if held != nil && !held[p.Ref.ObjectNum] {
			continue
		}
		pageRefs[i+1] = p.Ref // 1-based
	}

	return WriteOutline(ctx, alloc, doc, pageRefs, bookmarks)
}

// WriteOutline writes a bookmark tree to the allocator and patches the
// catalog's /Outlines entry. pageRefs maps 1-based page numbers in the
// output to their indirect references in the allocator. This is the
// low-level entry point behind [SetBookmarks]. [Split] and [Merge] build
// fresh catalogs and call [BuildOutline] instead.
//
// In a tagged document each item opens the first structure element whose
// content begins on its page beside the page (see [outlineTargets]).
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

	// Update the catalog to reference the new /Outlines.
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return fmt.Errorf("trailer missing /Root indirect reference")
	}

	catalogDict, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return err
	}

	var targets *outlineTargets
	if root, err := resolveDictAllocFirst(ctx, alloc, doc, catalogDict["StructTreeRoot"]); err == nil && root != nil {
		targets = &outlineTargets{byPage: StructPageTargets(ctx, AllocResolver(ctx, alloc, doc), root["K"])}
	}

	outlineRef := BuildOutline(ctx, alloc, pageRefs, outlineItems(bookmarks), targets)
	if outlineRef.ObjectNum == 0 {
		return nil
	}

	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Outlines"] = outlineRef

	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return nil
}

// BuildOutline allocates outline objects for the given item tree and
// returns the ref to the root /Outlines dict (or a zero ref if nothing was
// written). The caller is responsible for inserting the returned ref into
// the catalog. Used by [Split] and [Merge], which build fresh catalogs, and
// by [WriteOutline]. targets decides the structure destination each item
// opens, and a nil one opens none.
func BuildOutline(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	pageRefs map[int]ast.PdfIndirectRef,
	items []OutlineItem,
	targets *outlineTargets,
) ast.PdfIndirectRef {
	if len(items) == 0 || ctx.Err() != nil {
		return ast.PdfIndirectRef{}
	}

	outlineRef := alloc.Alloc(ast.PdfNull{}) // placeholder
	refs := buildOutlineItems(alloc, items, outlineRef, pageRefs, targets)
	if len(refs) == 0 {
		return ast.PdfIndirectRef{}
	}

	outlineDict := ast.PdfDict{
		"Type":  ast.PdfName("Outlines"),
		"First": refs[0],
		"Last":  refs[len(refs)-1],
		"Count": ast.PdfInt(visibleOutlineCount(items)),
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
func visibleOutlineCount(bookmarks []OutlineItem) int {
	total := 0
	for i := range bookmarks {
		total += 1 + visibleOutlineCount(bookmarks[i].Children)
	}
	return total
}

// buildOutlineItems creates outline item objects and returns their refs.
//
// An item opening a page opens it through an explicit destination (ISO
// 32000-1, 12.3.2.2). When targets names a structure destination for it, the
// item opens both through a GoTo action instead: /D the page and /SD the
// structure element (ISO 32000-2, 12.6.4.2, Table 202), whose view parameters
// the page destination takes too. A reader that knows no structure
// destination follows /D, and ISO 32000-1, Table 153 forbids /Dest beside /A.
func buildOutlineItems(alloc *writer.ObjAllocator, items []OutlineItem, parent ast.PdfIndirectRef, pageRefs map[int]ast.PdfIndirectRef, targets *outlineTargets) []ast.PdfIndirectRef {
	refs := make([]ast.PdfIndirectRef, len(items))

	// Every ref exists before any dict is built, so each item can name its
	// /Prev and /Next.
	for i := range items {
		refs[i] = alloc.Alloc(ast.PdfNull{}) // placeholder
	}

	for i, item := range items {
		d := ast.PdfDict{
			// An outline item's /Title is a text string (ISO 32000-1, 12.3.3).
			"Title":  document.EncodePdfString(item.Title),
			"Parent": parent,
		}

		if pageRef, ok := pageRefs[item.PageNumber]; ok {
			if sd := targets.structDest(item, pageRef); sd != nil {
				dest := make(ast.PdfArray, 0, len(sd))
				dest = append(dest, pageRef)
				d["A"] = ast.PdfDict{
					"S":  ast.PdfName("GoTo"),
					"D":  append(dest, sd[1:]...),
					"SD": sd,
				}
			} else {
				d["Dest"] = ast.PdfArray{pageRef, ast.PdfName("Fit")}
			}
		}

		if i > 0 {
			d["Prev"] = refs[i-1]
		}
		if i < len(items)-1 {
			d["Next"] = refs[i+1]
		}

		// Recurse for children.
		if len(item.Children) > 0 {
			childRefs := buildOutlineItems(alloc, item.Children, refs[i], pageRefs, targets)
			if len(childRefs) > 0 {
				d["First"] = childRefs[0]
				d["Last"] = childRefs[len(childRefs)-1]
				d["Count"] = ast.PdfInt(visibleOutlineCount(item.Children))
			}
		}

		alloc.AllocAt(refs[i].ObjectNum, d)
	}

	return refs
}

// StagedBookmarks returns the outline an earlier stage queued as bookmarks,
// numbering pages as pages does, and reports false when no stage queued one:
// the caller then reads the document's own outline.
//
// A stage that sets the outline ([SetBookmarks], a generated outline) queues a
// new tree at new object numbers, and one that drops it queues a catalog
// without /Outlines. Reading the document's outline instead, to add generated
// bookmarks to it, wrote over the staged one. An item whose destination names
// no page of pages keeps its title and children and gets page 0.
//
// # Corpus evidence
//
//   - All corpora: of 100 files given an outline and then bookmarks generated
//     in the append mode on one Writer, 30 lost the outline.
func StagedBookmarks(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page) ([]models.Bookmark, bool) {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return nil, false
	}
	catalog, ok := alloc.Pending()[catalogRef.ObjectNum].(ast.PdfDict)
	if !ok {
		return nil, false
	}
	outlinesObj, ok := catalog["Outlines"]
	if !ok {
		return nil, true
	}
	outlinesRef, ok := outlinesObj.(ast.PdfIndirectRef)
	if !ok {
		return nil, false
	}
	root, ok := alloc.Pending()[outlinesRef.ObjectNum].(ast.PdfDict)
	if !ok {
		return nil, false
	}
	pageNums := make(map[int]int, len(pages))
	for i, page := range pages {
		pageNums[page.Ref.ObjectNum] = i + 1
	}
	return stagedOutlineItems(ctx, alloc, doc, root["First"], pageNums, make(map[int]bool), 0), true
}

// stagedOutlineItems reads one sibling chain of outline items, and their
// children, from alloc first. It walks as [scrubOutlineItems] does: an item
// reached twice ends its chain, and the descent stops at [maxOutlineDepth].
func stagedOutlineItems(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, first ast.PdfObject, pageNums map[int]int, visited map[int]bool, depth int) []models.Bookmark {
	if depth > maxOutlineDepth {
		return nil
	}
	var out []models.Bookmark
	for current := first; current != nil; {
		if ctx.Err() != nil {
			return out
		}
		ref, isRef := current.(ast.PdfIndirectRef)
		if !isRef || visited[ref.ObjectNum] {
			return out
		}
		visited[ref.ObjectNum] = true
		item, err := resolveDictAllocFirst(ctx, alloc, doc, ref)
		if err != nil || item == nil {
			return out
		}
		var title string
		if resolved, err := resolveAllocFirst(ctx, alloc, doc, item["Title"]); err == nil {
			if s, isStr := resolved.(ast.PdfString); isStr {
				title = document.DecodePdfString(s)
			}
		}
		out = append(out, models.Bookmark{
			Title:      title,
			PageNumber: stagedOutlinePage(ctx, alloc, doc, item, pageNums),
			Children:   stagedOutlineItems(ctx, alloc, doc, item["First"], pageNums, visited, depth+1),
		})
		current = item["Next"]
	}
	return out
}

// stagedOutlinePage returns the page number an outline item's explicit
// destination names, through /Dest or a GoTo action's /D (ISO 32000-1, 12.3.3
// and 12.6.4.2), or 0 when it names none of pageNums.
func stagedOutlinePage(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, item ast.PdfDict, pageNums map[int]int) int {
	dest, ok := item["Dest"]
	if !ok {
		actionObj, hasAction := item["A"]
		if !hasAction {
			return 0
		}
		action, err := resolveDictAllocFirst(ctx, alloc, doc, actionObj)
		if err != nil || action == nil {
			return 0
		}
		if s, _ := action["S"].(ast.PdfName); s != "GoTo" {
			return 0
		}
		dest = action["D"]
	}
	arr, err := resolveArrayAllocFirst(ctx, alloc, doc, dest)
	if err != nil || len(arr) == 0 {
		return 0
	}
	if ref, isRef := arr[0].(ast.PdfIndirectRef); isRef {
		return pageNums[ref.ObjectNum]
	}
	return 0
}
