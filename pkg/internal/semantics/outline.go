package semantics

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// GetOutline extracts the hierarchical outline (bookmarks) from the document.
func GetOutline(doc *document.Document) ([]models.Bookmark, error) {
	catalog, err := document.GetCatalog(doc)
	if err != nil {
		return nil, err
	}

	outlinesObj, ok := catalog.Dict["Outlines"]
	if !ok {
		return nil, nil // Document has no outline
	}

	outlinesResolved, err := resolveObject(doc, outlinesObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve outlines: %v", err)
	}

	outlinesDict, ok := outlinesResolved.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("outlines is not a dictionary")
	}

	firstObj, ok := outlinesDict["First"]
	if !ok {
		return nil, nil // Empty outline
	}

	// Fetch pages and build a clean ref → 1-based page number map.
	pages, err := document.GetPages(doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pages for outline mapping: %v", err)
	}

	return parseOutlineItems(doc, firstObj, document.BuildPageMap(pages))
}

func parseOutlineItems(doc *document.Document, itemObj ast.PdfObject, pageMap map[ast.PdfIndirectRef]int) ([]models.Bookmark, error) {
	var bookmarks []models.Bookmark
	currentObj := itemObj

	for currentObj != nil {
		resolved, err := resolveObject(doc, currentObj)
		if err != nil {
			break
		}

		dict, ok := resolved.(ast.PdfDict)
		if !ok {
			break
		}

		var b models.Bookmark

		if titleObj, ok := dict["Title"]; ok {
			titleRes, _ := resolveObject(doc, titleObj)
			if titleStr, ok := titleRes.(ast.PdfString); ok {
				b.Title = decodePdfString(titleStr)
			}
		}

		// Resolve Dest array
		if destObj, ok := dict["Dest"]; ok {
			destRes, _ := resolveObject(doc, destObj)
			if destArr, ok := destRes.(ast.PdfArray); ok && len(destArr) > 0 {
				if pageRef, ok := destArr[0].(ast.PdfIndirectRef); ok {
					if pageNum, exists := pageMap[pageRef]; exists {
						b.PageNumber = pageNum
					}
				}
			}
		} else if aObj, ok := dict["A"]; ok {
			// Some bookmarks use an Action (/A) instead of an explicit /Dest
			aRes, _ := resolveObject(doc, aObj)
			if aDict, ok := aRes.(ast.PdfDict); ok {
				if sObj, ok := aDict["S"]; ok {
					sRes, _ := resolveObject(doc, sObj)
					if sName, ok := sRes.(ast.PdfName); ok && sName == "GoTo" {
						if dObj, ok := aDict["D"]; ok {
							dRes, _ := resolveObject(doc, dObj)
							if dArr, ok := dRes.(ast.PdfArray); ok && len(dArr) > 0 {
								if pageRef, ok := dArr[0].(ast.PdfIndirectRef); ok {
									if pageNum, exists := pageMap[pageRef]; exists {
										b.PageNumber = pageNum
									}
								}
							}
						}
					}
				}
			}
		}

		// Recursively parse children
		if firstChildObj, ok := dict["First"]; ok {
			children, err := parseOutlineItems(doc, firstChildObj, pageMap)
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
