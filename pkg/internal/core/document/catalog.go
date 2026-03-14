package document

import (
	"fmt"
	"unicode/utf16"

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

// decodePdfString converts a PDF string to a Go string, handling UTF-16BE if present.
func decodePdfString(s ast.PdfString) string {
	b := []byte(s)
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		u16s := make([]uint16, (len(b)-2)/2)
		for i := range u16s {
			u16s[i] = uint16(b[2+i*2])<<8 | uint16(b[2+i*2+1])
		}
		return string(utf16.Decode(u16s))
	}
	return string(b)
}

// resolveObject is a helper to resolve indirect references if needed.
func resolveObject(doc *Document, obj ast.PdfObject) (ast.PdfObject, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return doc.Resolve(ref)
	}
	return obj, nil
}

// GetCatalog retrieves the Document Catalog from the trailer.
func GetCatalog(doc *Document) (*Catalog, error) {
	rootObj, ok := doc.Index.Trailer["Root"]
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}

	resolvedObj, err := resolveObject(doc, rootObj)
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
func GetPages(doc *Document, catalog *Catalog) ([]*Page, error) {
	pagesObj, ok := catalog.Dict["Pages"]
	if !ok {
		return nil, fmt.Errorf("catalog missing /Pages")
	}

	resolvedPages, err := resolveObject(doc, pagesObj)
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
	err = traversePageTree(doc, pagesDict, ast.PdfIndirectRef{}, nil, nil, &pages)
	if err != nil {
		return nil, err
	}

	// Associate Document Parts (PDF 2.0)
	dpartMap, _ := GetDPartMap(doc)
	for _, page := range pages {
		if dpm, ok := dpartMap[page.Ref.ObjectNum]; ok {
			page.DPM = dpm
		} else {
			// Fallback: check if the page dict has a direct /DPart reference
			if dpartObj, ok := page.Dict["DPart"]; ok {
				dpartRes, _ := resolveObject(doc, dpartObj)
				if dpartDict, ok := dpartRes.(ast.PdfDict); ok {
					if dpmObj, ok := dpartDict["DPM"]; ok {
						dpmRes, _ := resolveObject(doc, dpmObj)
						if dpmDict, ok := dpmRes.(ast.PdfDict); ok {
							dpm := make(map[string]string)
							for k, v := range dpmDict {
								vRes, _ := resolveObject(doc, v)
								if str, ok := vRes.(ast.PdfString); ok {
									dpm[string(k)] = decodePdfString(str)
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

func traversePageTree(doc *Document, node ast.PdfDict, nodeRef ast.PdfIndirectRef, inheritedResources ast.PdfDict, inheritedMediaBox ast.PdfArray, pages *[]*Page) error {
	// Inherit or override Resources
	currentResources := inheritedResources
	if resObj, ok := node["Resources"]; ok {
		resResolved, err := resolveObject(doc, resObj)
		if err == nil {
			if resDict, ok := resResolved.(ast.PdfDict); ok {
				currentResources = resDict
			}
		}
	}

	// Inherit or override MediaBox
	currentMediaBox := inheritedMediaBox
	if mbObj, ok := node["MediaBox"]; ok {
		mbResolved, err := resolveObject(doc, mbObj)
		if err == nil {
			if mbArr, ok := mbResolved.(ast.PdfArray); ok {
				currentMediaBox = mbArr
			}
		}
	}

	typeObj, ok := node["Type"]
	if !ok {
		return fmt.Errorf("node missing /Type")
	}

	typeResolved, err := resolveObject(doc, typeObj)
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

		kidsResolved, err := resolveObject(doc, kidsObj)
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
			kidResolved, err := resolveObject(doc, kidObj)
			if err != nil {
				return fmt.Errorf("failed to resolve kid: %v", err)
			}

			kidDict, ok := kidResolved.(ast.PdfDict)
			if !ok {
				return fmt.Errorf("kid is not a dictionary")
			}

			err = traversePageTree(doc, kidDict, kidRef, currentResources, currentMediaBox, pages)
			if err != nil {
				return err
			}
		}
	case "Page":
		*pages = append(*pages, &Page{
			Ref:       nodeRef,
			Dict:      node,
			Resources: currentResources,
			MediaBox:  currentMediaBox,
		})
	}

	return nil
}
