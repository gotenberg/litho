package interactive

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractAnnotations extracts annotations from a specific page.
func ExtractAnnotations(ctx context.Context, doc *document.Document, page *document.Page, pageMap map[ast.PdfIndirectRef]int) ([]models.Annotation, error) {
	annotsObj, ok := page.Dict["Annots"]
	if !ok {
		return nil, nil // No annotations on this page
	}

	annotsResolved, err := document.ResolveObject(ctx, doc, annotsObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve /Annots: %v", err)
	}

	annotsArr, ok := annotsResolved.(ast.PdfArray)
	if !ok {
		return nil, fmt.Errorf("/Annots is not an array")
	}

	var annotations []models.Annotation

	for _, annotRef := range annotsArr {
		annotResolved, err := document.ResolveObject(ctx, doc, annotRef)
		if err != nil {
			continue
		}

		annotDict, ok := annotResolved.(ast.PdfDict)
		if !ok {
			continue
		}

		var annot models.Annotation

		// Extract Subtype
		if subtypeObj, ok := annotDict["Subtype"]; ok {
			subtypeRes, _ := document.ResolveObject(ctx, doc, subtypeObj)
			if subtypeName, ok := subtypeRes.(ast.PdfName); ok {
				annot.Subtype = string(subtypeName)
			}
		}

		if annot.Subtype == "" {
			continue
		}

		// Extract Rect
		if rectObj, ok := annotDict["Rect"]; ok {
			rectRes, _ := document.ResolveObject(ctx, doc, rectObj)
			if rectArr, ok := rectRes.(ast.PdfArray); ok && len(rectArr) == 4 {
				for i := range 4 {
					annot.Rect[i] = document.GetFloat(rectArr[i])
				}
			}
		}

		// Extract Contents (Text, Highlight, etc.)
		if contentsObj, ok := annotDict["Contents"]; ok {
			contentsRes, _ := document.ResolveObject(ctx, doc, contentsObj)
			if contentsStr, ok := contentsRes.(ast.PdfString); ok {
				annot.Contents = document.DecodePdfString(contentsStr)
			}
		}

		// Extract QuadPoints (used by Redact, Highlight, Underline, etc.)
		if qpObj, ok := annotDict["QuadPoints"]; ok {
			qpRes, _ := document.ResolveObject(ctx, doc, qpObj)
			if qpArr, ok := qpRes.(ast.PdfArray); ok && len(qpArr) >= 8 {
				for i := 0; i+7 < len(qpArr); i += 8 {
					var quad [8]float64
					for j := range 8 {
						quad[j] = document.GetFloat(qpArr[i+j])
					}
					annot.QuadPoints = append(annot.QuadPoints, quad)
				}
			}
		}

		// Handle specific subtypes
		if annot.Subtype == "Link" {
			// Extract Action (/A)
			if aObj, ok := annotDict["A"]; ok {
				aRes, _ := document.ResolveObject(ctx, doc, aObj)
				if aDict, ok := aRes.(ast.PdfDict); ok {
					if sObj, ok := aDict["S"]; ok {
						sRes, _ := document.ResolveObject(ctx, doc, sObj)
						if sName, ok := sRes.(ast.PdfName); ok {
							switch sName {
							case "URI":
								if uriObj, ok := aDict["URI"]; ok {
									uriRes, _ := document.ResolveObject(ctx, doc, uriObj)
									if uriStr, ok := uriRes.(ast.PdfString); ok {
										annot.URI = document.DecodePdfString(uriStr)
									}
								}
							case "GoTo":
								if dObj, ok := aDict["D"]; ok {
									dRes, _ := document.ResolveObject(ctx, doc, dObj)
									if dArr, ok := dRes.(ast.PdfArray); ok && len(dArr) > 0 {
										if destRef, ok := dArr[0].(ast.PdfIndirectRef); ok {
											if pageNum, exists := pageMap[destRef]; exists {
												annot.DestPage = pageNum
											}
										}
									}
								}
							}
						}
					}
				}
			} else if destObj, ok := annotDict["Dest"]; ok {
				// Sometimes Dest is directly on the Link annotation instead of inside an Action
				destRes, _ := document.ResolveObject(ctx, doc, destObj)
				if destArr, ok := destRes.(ast.PdfArray); ok && len(destArr) > 0 {
					if destRef, ok := destArr[0].(ast.PdfIndirectRef); ok {
						if pageNum, exists := pageMap[destRef]; exists {
							annot.DestPage = pageNum
						}
					}
				}
			}
		}

		annotations = append(annotations, annot)
	}

	return annotations, nil
}
