package document

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

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
	Rotate    int               // Inherited; clockwise degrees (0, 90, 180, 270)
	DPM       map[string]string // Document Part Metadata
}

// BuildPageMap constructs a reverse-lookup map from each page's [ast.PdfIndirectRef]
// to its 1-based page number, enabling clean destination resolution without
// mutating any struct fields.
func BuildPageMap(pages []*Page) map[ast.PdfIndirectRef]int {
	m := make(map[ast.PdfIndirectRef]int, len(pages))
	for i, p := range pages {
		m[p.Ref] = i + 1
	}
	return m
}

// GetCatalog retrieves the [Catalog] from the document trailer.
func GetCatalog(ctx context.Context, doc *Document) (*Catalog, error) {
	rootObj, ok := doc.Index.Trailer["Root"]
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}

	catalogDict, err := ResolveDict(ctx, doc, rootObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve catalog: %w", err)
	}
	if catalogDict == nil {
		return nil, fmt.Errorf("catalog is not a dictionary")
	}

	return &Catalog{Dict: catalogDict}, nil
}

// GetPages extracts all [Page] entries from the [Catalog] by traversing the page tree.
func GetPages(ctx context.Context, doc *Document, catalog *Catalog) ([]*Page, error) {
	pagesObj, ok := catalog.Dict["Pages"]
	if !ok {
		return nil, fmt.Errorf("catalog missing /Pages")
	}

	pagesDict, err := ResolveDict(ctx, doc, pagesObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve pages root: %w", err)
	}
	if pagesDict == nil {
		return nil, fmt.Errorf("pages root is not a dictionary")
	}

	var pages []*Page
	// The root Pages node itself usually doesn't have a useful page reference for a single page,
	// but we start the traversal here.
	err = traversePageTree(ctx, doc, pagesDict, ast.PdfIndirectRef{}, nil, nil, nil, 0, &pages)
	if err != nil {
		return nil, fmt.Errorf("traversing page tree: %w", err)
	}

	doc.Logger.Debug("page tree traversed",
		slog.Int("pages", len(pages)),
	)

	// Associate Document Parts (PDF 2.0)
	dpartMap, err := GetDPartMap(ctx, doc)
	if err != nil {
		doc.Logger.Debug("resolving DPart map", slog.Any("err", err))
	}
	for _, page := range pages {
		if dpm, ok := dpartMap[page.Ref.ObjectNum]; ok {
			page.DPM = dpm
		} else {
			// Fallback: check if the page dict has a direct /DPart reference
			if dpartObj, ok := page.Dict["DPart"]; ok {
				dpartDict, err := ResolveDict(ctx, doc, dpartObj)
				if err != nil {
					doc.Logger.Debug("resolving page /DPart dict", slog.Any("err", err))
				}
				if dpartDict != nil {
					if dpmObj, ok := dpartDict["DPM"]; ok {
						dpmDict, err := ResolveDict(ctx, doc, dpmObj)
						if err != nil {
							doc.Logger.Debug("resolving page /DPM dict", slog.Any("err", err))
						}
						if dpmDict != nil {
							dpm := make(map[string]string)
							for k, v := range dpmDict {
								vRes, err := ResolveObject(ctx, doc, v)
								if err != nil {
									doc.Logger.Debug("resolving DPM value", slog.String("key", string(k)), slog.Any("err", err))
								}
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

func traversePageTree(ctx context.Context, doc *Document, node ast.PdfDict, nodeRef ast.PdfIndirectRef, inheritedResources ast.PdfDict, inheritedMediaBox, inheritedCropBox ast.PdfArray, inheritedRotate int, pages *[]*Page) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("page tree traversal canceled: %w", err)
	}

	// Merge Resources: the node's resources are merged with inherited ones.
	// Sub-dicts (/Font, /XObject, /ExtGState, etc.) are merged so that
	// entries from the current node take precedence, but missing entries
	// fall back to the parent. This handles PDFs where a page defines
	// and handles PDFs where a page defines partial resources.
	currentResources := mergeResourceDicts(ctx, doc, inheritedResources, node)

	// Inherit or override MediaBox
	currentMediaBox := inheritedMediaBox
	if mbObj, ok := node["MediaBox"]; ok {
		mbArr, err := ResolveArray(ctx, doc, mbObj)
		if err == nil {
			if mbArr != nil {
				currentMediaBox = mbArr
			}
		}
	}

	// Inherit or override CropBox (inheritable per PDF spec)
	currentCropBox := inheritedCropBox
	if cbObj, ok := node["CropBox"]; ok {
		cbArr, err := ResolveArray(ctx, doc, cbObj)
		if err == nil {
			if cbArr != nil {
				currentCropBox = cbArr
			}
		}
	}

	// Inherit or override Rotate (inheritable per PDF spec §7.7.3.3)
	currentRotate := inheritedRotate
	if rotObj, ok := node["Rotate"]; ok {
		if rotInt, ok := rotObj.(ast.PdfInt); ok {
			currentRotate = int(rotInt)
		}
	}

	typeObj, ok := node["Type"]
	if !ok {
		return fmt.Errorf("node missing /Type")
	}

	typeStr, err := ResolveName(ctx, doc, typeObj)
	if err != nil {
		return fmt.Errorf("failed to resolve node type: %w", err)
	}
	if typeStr == "" {
		return fmt.Errorf("node /Type is not a name")
	}

	switch typeStr {
	case "Pages":
		kidsObj, ok := node["Kids"]
		if !ok {
			return fmt.Errorf("pages node missing /Kids")
		}

		kidsArr, err := ResolveArray(ctx, doc, kidsObj)
		if err != nil {
			return fmt.Errorf("failed to resolve /Kids array: %w", err)
		}
		if kidsArr == nil {
			return fmt.Errorf("/Kids is not an array")
		}

		for _, kidObj := range kidsArr {
			var kidRef ast.PdfIndirectRef
			if r, ok := kidObj.(ast.PdfIndirectRef); ok {
				kidRef = r
			}
			kidDict, err := ResolveDict(ctx, doc, kidObj)
			if err != nil {
				return fmt.Errorf("failed to resolve kid: %w", err)
			}
			if kidDict == nil {
				return fmt.Errorf("kid is not a dictionary")
			}

			err = traversePageTree(ctx, doc, kidDict, kidRef, currentResources, currentMediaBox, currentCropBox, currentRotate, pages)
			if err != nil {
				return fmt.Errorf("traversing page tree kid: %w", err)
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
			Rotate:    currentRotate,
		})
	}

	return nil
}

// mergeResourceDicts merges a node's /Resources with inherited resources.
// Top-level keys (Font, XObject, etc.) from the node take precedence.
// Within each sub-dict, entries are merged with node entries winning.
// If the node has no /Resources, the inherited dict is returned as-is.
func mergeResourceDicts(ctx context.Context, doc *Document, inherited ast.PdfDict, node ast.PdfDict) ast.PdfDict {
	resObj, ok := node["Resources"]
	if !ok {
		return inherited
	}
	nodeRes, err := ResolveDict(ctx, doc, resObj)
	if err != nil {
		return inherited
	}
	if nodeRes == nil {
		return inherited
	}
	if inherited == nil {
		return nodeRes
	}

	// Merge: start with a copy of inherited, overlay node entries.
	merged := make(ast.PdfDict, len(inherited)+len(nodeRes))
	maps.Copy(merged, inherited)
	for k, v := range nodeRes {
		// For sub-dicts (Font, XObject, ExtGState, etc.), merge entries.
		inheritedSub, inheritedIsDict := inherited[k]
		if !inheritedIsDict {
			merged[k] = v
			continue
		}
		nodeSub, err := ResolveDict(ctx, doc, v)
		if err != nil {
			doc.Logger.Debug("resolving resource sub-dict for merge", slog.String("key", string(k)), slog.Any("err", err))
		}
		if nodeSub == nil {
			merged[k] = v
			continue
		}
		inheritedSubDict, err := ResolveDict(ctx, doc, inheritedSub)
		if err != nil {
			doc.Logger.Debug("resolving inherited resource sub-dict for merge", slog.String("key", string(k)), slog.Any("err", err))
		}
		if inheritedSubDict == nil {
			merged[k] = v
			continue
		}
		// Merge sub-dict: inherited entries as base, node entries override.
		sub := make(ast.PdfDict, len(inheritedSubDict)+len(nodeSub))
		maps.Copy(sub, inheritedSubDict)
		maps.Copy(sub, nodeSub)
		merged[k] = sub
	}
	return merged
}

// resolveBoxArray extracts a named rectangle array from a page dict,
// falling back to the provided default if the key is absent.
func resolveBoxArray(ctx context.Context, doc *Document, dict ast.PdfDict, key ast.PdfName, fallback ast.PdfArray) ast.PdfArray {
	obj, ok := dict[key]
	if !ok {
		return fallback
	}
	arr, err := ResolveArray(ctx, doc, obj)
	if err != nil {
		return fallback
	}
	if len(arr) < 4 {
		return fallback
	}
	return arr
}
