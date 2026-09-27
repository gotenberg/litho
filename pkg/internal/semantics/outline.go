package semantics

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ResolveOutline extracts the hierarchical outline (bookmarks) from the document.
//
// # Corpus evidence
//
//   - pdfjs issue2618: declares object 19 in its xref table at an offset that
//     lands on the `xref` keyword, and no `19 0 obj` exists anywhere in the
//     file; erroring made `litho bookmarks` the only command that could not
//     read a document poppler and pikepdf both report as having no outline.
//   - pdfjs colors.pdf: points /Outlines at a free object; treating that as a
//     broken outline made `litho bookmarks` fail on a file poppler and pikepdf
//     both read as having no outline.
func ResolveOutline(ctx context.Context, doc *document.Document) ([]models.Bookmark, error) {
	return ResolveOutlineItems(ctx, doc, func(b models.Bookmark, _ ast.PdfDict, children []models.Bookmark) models.Bookmark {
		b.Children = children
		return b
	})
}

// ResolveOutlineItems is [ResolveOutline] for a tree of items of any type. It
// walks the outline as ResolveOutline does and makes each item with build,
// from the item's bookmark, whose Children it leaves empty, the outline item
// dictionary the document holds (ISO 32000-1, 12.3.3, Table 153), and the
// item's children, made first.
func ResolveOutlineItems[T any](ctx context.Context, doc *document.Document, build func(b models.Bookmark, item ast.PdfDict, children []T) T) ([]T, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}

	outlinesObj, ok := catalog.Dict["Outlines"]
	if !ok {
		return nil, nil // Document has no outline
	}

	// Resolve the reference before type-checking it. An object litho cannot read
	// is indistinguishable from one the file never defined, and per ISO 32000-1,
	// 7.3.9 a reference to an undefined object is a reference to null. Erroring
	// on one makes `litho bookmarks` the only command that cannot read a
	// document poppler and pikepdf both report as simply having no outline.
	outlinesRes, err := document.ResolveObject(ctx, doc, outlinesObj)
	if err != nil {
		doc.Logger.Debug("resolving outlines", slog.Any("err", err))
		return nil, nil
	}

	outlinesDict, err := document.ResolveDict(ctx, doc, outlinesRes)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve outlines: %w", err)
	}
	if outlinesDict == nil {
		// Per ISO 32000-1, 7.3.9, a reference to an undefined object is a
		// reference to null, and per 7.3.7 an entry whose value is null is
		// equivalent to the entry being absent, so /Outlines pointing at a
		// free object is no outline rather than a broken one. A
		// non-dictionary /Outlines still errors above, since
		// document.ResolveDict reports the type mismatch.
		return nil, nil
	}

	firstObj, ok := outlinesDict["First"]
	if !ok {
		return nil, nil // Empty outline
	}

	// Fetch pages and build a clean ref → 1-based page number map.
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pages for outline mapping: %w", err)
	}

	return parseOutlineItems(ctx, doc, firstObj, document.BuildPageMap(pages), build, make(map[int]bool), 0)
}

// maxOutlineDepth caps outline /First nesting. Anti-DoS guard, not spec-mandated:
// real outlines nest a handful of levels. Paired with visited (which breaks
// /Next and /First cycles) it bounds a crafted outline tree.
const maxOutlineDepth = 100

// parseOutlineItems walks one outline level, iterating siblings via /Next and
// recursing into children via /First. visited records resolved item object
// numbers across the whole walk: a crafted /Next or /First cycle would otherwise
// loop forever (a 700-byte file hangs until the caller's context deadline, and a
// /First cycle exhausts the goroutine stack, which is not recoverable in Go).
func parseOutlineItems[T any](ctx context.Context, doc *document.Document, itemObj ast.PdfObject, pageMap map[ast.PdfIndirectRef]int, build func(models.Bookmark, ast.PdfDict, []T) T, visited map[int]bool, depth int) ([]T, error) {
	if depth > maxOutlineDepth {
		return nil, nil
	}
	var items []T
	currentObj := itemObj

	for currentObj != nil {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("parsing outline items: %w", err)
		}
		if ref, ok := currentObj.(ast.PdfIndirectRef); ok {
			if visited[ref.ObjectNum] {
				break
			}
			visited[ref.ObjectNum] = true
		}
		dict, err := document.ResolveDict(ctx, doc, currentObj)
		if err != nil {
			break
		}

		if dict == nil {
			break
		}

		var b models.Bookmark

		if titleObj, ok := dict["Title"]; ok {
			titleStr, err := document.ResolveString(ctx, doc, titleObj)
			if err != nil {
				doc.Logger.Debug("resolving outline title", slog.Any("err", err))
			}
			if titleStr != "" {
				b.Title = document.DecodePdfString(titleStr)
			}
		}

		// Resolve Dest. May be a direct array or a named destination string/name.
		if destObj, ok := dict["Dest"]; ok {
			b.PageNumber = document.ResolveDestinationPage(ctx, doc, destObj, pageMap)
		} else if aObj, ok := dict["A"]; ok {
			// Some bookmarks use an Action (/A) instead of an explicit /Dest.
			aDict, err := document.ResolveDict(ctx, doc, aObj)
			if err != nil {
				doc.Logger.Debug("resolving outline action dict", slog.Any("err", err))
			}
			if aDict != nil {
				if sObj, ok := aDict["S"]; ok {
					sName, err := document.ResolveName(ctx, doc, sObj)
					if err != nil {
						doc.Logger.Debug("resolving outline action subtype", slog.Any("err", err))
					}
					if sName == "GoTo" {
						if dObj, ok := aDict["D"]; ok {
							b.PageNumber = document.ResolveDestinationPage(ctx, doc, dObj, pageMap)
						}
					}
				}
			}
		}

		// Recursively parse children
		var children []T
		if firstChildObj, ok := dict["First"]; ok {
			children, err = parseOutlineItems(ctx, doc, firstChildObj, pageMap, build, visited, depth+1)
			if err != nil {
				return nil, fmt.Errorf("failed to parse outline children: %w", err)
			}
		}

		items = append(items, build(b, dict, children))

		// Move to the next sibling in the linked list
		if nextObj, ok := dict["Next"]; ok {
			currentObj = nextObj
		} else {
			break
		}
	}

	return items, nil
}
