package annotation

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractAnnotations extracts annotations from a specific page.
//
// # Corpus evidence
//
//   - pdfjs issue11287.pdf: writes /Annots null on one page, and calling that
//     an error made litho the only reader that could not list annotations for
//     the document.
func ExtractAnnotations(ctx context.Context, doc *document.Document, page *document.Page, pageMap map[ast.PdfIndirectRef]int) ([]models.Annotation, error) {
	annotsObj, ok := page.Dict["Annots"]
	if !ok {
		return nil, nil // No annotations on this page
	}

	annotsArr, err := document.ResolveArray(ctx, doc, annotsObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve /Annots: %w", err)
	}
	if annotsArr == nil {
		// document.ResolveArray reports a nil array only for null, and per
		// ISO 32000-1, 7.3.7 a dictionary entry whose value is null is
		// equivalent to the entry being absent, so a null /Annots is no
		// annotations rather than an error.
		return nil, nil
	}

	var annotations []models.Annotation

	for _, annotRef := range annotsArr {
		annotDict, err := document.ResolveDict(ctx, doc, annotRef)
		if err != nil || annotDict == nil {
			continue
		}

		var annot models.Annotation

		// Extract Subtype
		if subtypeObj, ok := annotDict["Subtype"]; ok {
			subtypeName, err := document.ResolveName(ctx, doc, subtypeObj)
			if err == nil && subtypeName != "" {
				annot.Subtype = string(subtypeName)
			}
		}

		if annot.Subtype == "" {
			continue
		}

		// Extract Rect
		if rectObj, ok := annotDict["Rect"]; ok {
			rectArr, err := document.ResolveArray(ctx, doc, rectObj)
			if err == nil && rectArr != nil && len(rectArr) == 4 {
				for i := range 4 {
					annot.Rect[i] = document.Float(rectArr[i])
				}
			}
		}

		// Extract Contents (Text, Highlight, etc.)
		if contentsObj, ok := annotDict["Contents"]; ok {
			contentsStr, err := document.ResolveString(ctx, doc, contentsObj)
			if err == nil && contentsStr != "" {
				annot.Contents = document.DecodePdfString(contentsStr)
			}
		}

		// Extract QuadPoints (used by Redact, Highlight, Underline, etc.)
		if qpObj, ok := annotDict["QuadPoints"]; ok {
			qpArr, err := document.ResolveArray(ctx, doc, qpObj)
			if err == nil && qpArr != nil && len(qpArr) >= 8 {
				for i := 0; i+7 < len(qpArr); i += 8 {
					var quad [8]float64
					for j := range 8 {
						quad[j] = document.Float(qpArr[i+j])
					}
					annot.QuadPoints = append(annot.QuadPoints, quad)
				}
			}
		}

		// Handle specific subtypes
		if annot.Subtype == "Link" {
			// Extract Action (/A)
			if aObj, ok := annotDict["A"]; ok {
				aDict, err := document.ResolveDict(ctx, doc, aObj)
				if err == nil && aDict != nil {
					if sObj, ok := aDict["S"]; ok {
						sName, err := document.ResolveName(ctx, doc, sObj)
						if err == nil && sName != "" {
							switch sName {
							case "URI":
								if uriObj, ok := aDict["URI"]; ok {
									uriStr, err := document.ResolveString(ctx, doc, uriObj)
									if err == nil && uriStr != "" {
										annot.URI = document.DecodePdfString(uriStr)
									}
								}
							case "GoTo":
								if dObj, ok := aDict["D"]; ok {
									annot.DestPage = document.ResolveDestinationPage(ctx, doc, dObj, pageMap)
								}
							}
						}
					}
				}
			} else if destObj, ok := annotDict["Dest"]; ok {
				// Sometimes Dest is directly on the Link annotation instead of inside an Action
				annot.DestPage = document.ResolveDestinationPage(ctx, doc, destObj, pageMap)
			}
		}

		annotations = append(annotations, annot)
	}

	return annotations, nil
}
