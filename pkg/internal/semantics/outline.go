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
func ResolveOutline(ctx context.Context, doc *document.Document) ([]models.Bookmark, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}

	outlinesObj, ok := catalog.Dict["Outlines"]
	if !ok {
		return nil, nil // Document has no outline
	}

	outlinesDict, err := document.ResolveDictRequired(ctx, doc, outlinesObj, "outlines")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve outlines: %v", err)
	}

	firstObj, ok := outlinesDict["First"]
	if !ok {
		return nil, nil // Empty outline
	}

	// Fetch pages and build a clean ref → 1-based page number map.
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pages for outline mapping: %v", err)
	}

	return parseOutlineItems(ctx, doc, firstObj, document.BuildPageMap(pages))
}

func parseOutlineItems(ctx context.Context, doc *document.Document, itemObj ast.PdfObject, pageMap map[ast.PdfIndirectRef]int) ([]models.Bookmark, error) {
	var bookmarks []models.Bookmark
	currentObj := itemObj

	for currentObj != nil {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("parsing outline items: %w", err)
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

		// Resolve Dest — may be a direct array or a named destination string/name.
		if destObj, ok := dict["Dest"]; ok {
			b.PageNumber = resolveDestination(ctx, doc, destObj, pageMap)
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
							b.PageNumber = resolveDestination(ctx, doc, dObj, pageMap)
						}
					}
				}
			}
		}

		// Recursively parse children
		if firstChildObj, ok := dict["First"]; ok {
			children, err := parseOutlineItems(ctx, doc, firstChildObj, pageMap)
			if err != nil {
				return nil, fmt.Errorf("failed to parse outline children: %w", err)
			}
			b.Children = children
		}

		bookmarks = append(bookmarks, b)

		// Move to the next sibling in the linked list
		if nextObj, ok := dict["Next"]; ok {
			currentObj = nextObj
		} else {
			break
		}
	}

	return bookmarks, nil
}

// resolveDestination resolves a destination object to a 1-based page number.
// The destination may be a direct PdfArray, a PdfString (named destination),
// or a PdfName (named destination). Named destinations are looked up via the
// catalog's /Names -> /Dests tree or the legacy /Dests dictionary.
func resolveDestination(ctx context.Context, doc *document.Document, destObj ast.PdfObject, pageMap map[ast.PdfIndirectRef]int) int {
	destRes, err := document.ResolveObject(ctx, doc, destObj)
	if err != nil {
		doc.Logger.Debug("resolving destination object", slog.Any("err", err))
	}

	// Try as a direct destination array first.
	if destArr, ok := destRes.(ast.PdfArray); ok {
		return pageFromDestArray(destArr, pageMap)
	}

	// Named destination: look up in the name tree.
	var name string
	switch v := destRes.(type) {
	case ast.PdfString:
		name = document.DecodePdfString(v)
	case ast.PdfName:
		name = string(v)
	default:
		return 0
	}

	destArr, err := document.ResolveNamedDest(ctx, doc, name)
	if err != nil || destArr == nil {
		return 0
	}
	return pageFromDestArray(destArr, pageMap)
}

// pageFromDestArray extracts the 1-based page number from a destination array.
func pageFromDestArray(arr ast.PdfArray, pageMap map[ast.PdfIndirectRef]int) int {
	if len(arr) == 0 {
		return 0
	}
	if pageRef, ok := arr[0].(ast.PdfIndirectRef); ok {
		if pageNum, exists := pageMap[pageRef]; exists {
			return pageNum
		}
	}
	return 0
}
