package document

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// Catalog represents the root node of the PDF document.
type Catalog struct {
	Dict ast.PdfDict
}

// PageTree represents an intermediate node in the page tree.
type PageTree struct {
	Dict ast.PdfDict
}

// Page represents a single page in the PDF document.
type Page struct {
	Ref       ast.PdfIndirectRef
	Dict      ast.PdfDict
	Resources ast.PdfDict
	MediaBox  ast.PdfArray
	CropBox   ast.PdfArray      // Inherited; defaults to MediaBox per PDF spec
	BleedBox  ast.PdfArray      // Non-inherited; defaults to CropBox
	TrimBox   ast.PdfArray      // Non-inherited; defaults to CropBox
	ArtBox    ast.PdfArray      // Non-inherited; defaults to CropBox
	DPM       map[string]string // Document Part Metadata
}

// BuildPageMap constructs a reverse-lookup map from each page's indirect reference
// to its 1-based page number, enabling clean destination resolution without
// mutating any struct fields.
func BuildPageMap(pages []*Page) map[ast.PdfIndirectRef]int {
	m := make(map[ast.PdfIndirectRef]int, len(pages))
	for i, p := range pages {
		m[p.Ref] = i + 1
	}
	return m
}

// GetCatalog retrieves the Document Catalog from the trailer.
func GetCatalog(ctx context.Context, doc *Document) (*Catalog, error) {
	rootObj, ok := doc.Index.Trailer["Root"]
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}

	resolvedObj, err := ResolveObject(ctx, doc, rootObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve catalog: %v", err)
	}

	catalogDict, ok := resolvedObj.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("catalog is not a dictionary")
	}

	return &Catalog{Dict: catalogDict}, nil
}

// GetPages extracts all pages from the Catalog by traversing the page tree.
func GetPages(ctx context.Context, doc *Document, catalog *Catalog) ([]*Page, error) {
	pagesObj, ok := catalog.Dict["Pages"]
	if !ok {
		return nil, fmt.Errorf("catalog missing /Pages")
	}

	resolvedPages, err := ResolveObject(ctx, doc, pagesObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve pages root: %v", err)
	}

	pagesDict, ok := resolvedPages.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("pages root is not a dictionary")
	}

	var pages []*Page
	// The root Pages node itself usually doesn't have a useful page reference for a single page,
	// but we start the traversal here.
	err = traversePageTree(ctx, doc, pagesDict, ast.PdfIndirectRef{}, nil, nil, nil, &pages)
	if err != nil {
		return nil, err
	}

	doc.Logger.Debug("page tree traversed",
		slog.Int("pages", len(pages)),
	)

	// Associate Document Parts (PDF 2.0)
	dpartMap, _ := GetDPartMap(ctx, doc)
	for _, page := range pages {
		if dpm, ok := dpartMap[page.Ref.ObjectNum]; ok {
			page.DPM = dpm
		} else {
			// Fallback: check if the page dict has a direct /DPart reference
			if dpartObj, ok := page.Dict["DPart"]; ok {
				dpartRes, _ := ResolveObject(ctx, doc, dpartObj)
				if dpartDict, ok := dpartRes.(ast.PdfDict); ok {
					if dpmObj, ok := dpartDict["DPM"]; ok {
						dpmRes, _ := ResolveObject(ctx, doc, dpmObj)
						if dpmDict, ok := dpmRes.(ast.PdfDict); ok {
							dpm := make(map[string]string)
							for k, v := range dpmDict {
								vRes, _ := ResolveObject(ctx, doc, v)
								if str, ok := vRes.(ast.PdfString); ok {
									dpm[string(k)] = DecodePdfString(str)
								} else if name, ok := vRes.(ast.PdfName); ok {
									dpm[string(k)] = string(name)
								}
							}
							page.DPM = dpm
						}
					}
				}
			}
		}
	}

	return pages, nil
}

func traversePageTree(ctx context.Context, doc *Document, node ast.PdfDict, nodeRef ast.PdfIndirectRef, inheritedResources ast.PdfDict, inheritedMediaBox, inheritedCropBox ast.PdfArray, pages *[]*Page) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Inherit or override Resources
	currentResources := inheritedResources
	if resObj, ok := node["Resources"]; ok {
		resResolved, err := ResolveObject(ctx, doc, resObj)
		if err == nil {
			if resDict, ok := resResolved.(ast.PdfDict); ok {
				currentResources = resDict
			}
		}
	}

	// Inherit or override MediaBox
	currentMediaBox := inheritedMediaBox
	if mbObj, ok := node["MediaBox"]; ok {
		mbResolved, err := ResolveObject(ctx, doc, mbObj)
		if err == nil {
			if mbArr, ok := mbResolved.(ast.PdfArray); ok {
				currentMediaBox = mbArr
			}
		}
	}

	// Inherit or override CropBox (inheritable per PDF spec)
	currentCropBox := inheritedCropBox
	if cbObj, ok := node["CropBox"]; ok {
		cbResolved, err := ResolveObject(ctx, doc, cbObj)
		if err == nil {
			if cbArr, ok := cbResolved.(ast.PdfArray); ok {
				currentCropBox = cbArr
			}
		}
	}

	typeObj, ok := node["Type"]
	if !ok {
		return fmt.Errorf("node missing /Type")
	}

	typeResolved, err := ResolveObject(ctx, doc, typeObj)
	if err != nil {
		return fmt.Errorf("failed to resolve node type: %v", err)
	}

	typeStr, ok := typeResolved.(ast.PdfName)
	if !ok {
		return fmt.Errorf("node /Type is not a name")
	}

	switch typeStr {
	case "Pages":
		kidsObj, ok := node["Kids"]
		if !ok {
			return fmt.Errorf("pages node missing /Kids")
		}

		kidsResolved, err := ResolveObject(ctx, doc, kidsObj)
		if err != nil {
			return fmt.Errorf("failed to resolve /Kids array: %v", err)
		}

		kidsArr, ok := kidsResolved.(ast.PdfArray)
		if !ok {
			return fmt.Errorf("/Kids is not an array")
		}

		for _, kidObj := range kidsArr {
			var kidRef ast.PdfIndirectRef
			if r, ok := kidObj.(ast.PdfIndirectRef); ok {
				kidRef = r
			}
			kidResolved, err := ResolveObject(ctx, doc, kidObj)
			if err != nil {
				return fmt.Errorf("failed to resolve kid: %v", err)
			}

			kidDict, ok := kidResolved.(ast.PdfDict)
			if !ok {
				return fmt.Errorf("kid is not a dictionary")
			}

			err = traversePageTree(ctx, doc, kidDict, kidRef, currentResources, currentMediaBox, currentCropBox, pages)
			if err != nil {
				return err
			}
		}
	case "Page":
		// CropBox defaults to MediaBox per PDF spec §14.11.2
		effectiveCropBox := currentCropBox
		if effectiveCropBox == nil {
			effectiveCropBox = currentMediaBox
		}

		// BleedBox, TrimBox, ArtBox are non-inheritable; default to CropBox.
		bleedBox := resolveBoxArray(ctx, doc, node, "BleedBox", effectiveCropBox)
		trimBox := resolveBoxArray(ctx, doc, node, "TrimBox", effectiveCropBox)
		artBox := resolveBoxArray(ctx, doc, node, "ArtBox", effectiveCropBox)

		*pages = append(*pages, &Page{
			Ref:       nodeRef,
			Dict:      node,
			Resources: currentResources,
			MediaBox:  currentMediaBox,
			CropBox:   effectiveCropBox,
			BleedBox:  bleedBox,
			TrimBox:   trimBox,
			ArtBox:    artBox,
		})
	}

	return nil
}

// resolveBoxArray extracts a named rectangle array from a page dict,
// falling back to the provided default if the key is absent.
func resolveBoxArray(ctx context.Context, doc *Document, dict ast.PdfDict, key ast.PdfName, fallback ast.PdfArray) ast.PdfArray {
	obj, ok := dict[key]
	if !ok {
		return fallback
	}
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return fallback
	}
	arr, ok := resolved.(ast.PdfArray)
	if !ok || len(arr) < 4 {
		return fallback
	}
	return arr
}
