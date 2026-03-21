package document

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// GetDPartMap traverses the /DPartRoot tree and returns a mapping from
// Page Object Number to the inherited Document Part Metadata (DPM).
func GetDPartMap(ctx context.Context, doc *Document) (map[int]map[string]string, error) {
	catalog, err := GetCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}

	dprObj, ok := catalog.Dict["DPartRoot"]
	if !ok {
		return nil, nil // No DPartRoot
	}

	dprDict, err := ResolveDict(ctx, doc, dprObj)
	if err != nil {
		return nil, err
	}
	if dprDict == nil {
		return nil, fmt.Errorf("DPartRoot is not a dictionary")
	}

	pageDPMMap := make(map[int]map[string]string)
	traverseDPart(ctx, doc, dprDict, make(map[string]string), pageDPMMap)

	return pageDPMMap, nil
}

func traverseDPart(ctx context.Context, doc *Document, node ast.PdfDict, inheritedDPM map[string]string, pageDPMMap map[int]map[string]string) {
	currentDPM := make(map[string]string)
	maps.Copy(currentDPM, inheritedDPM)

	if dpmObj, ok := node["DPM"]; ok {
		dpmDict, _ := ResolveDict(ctx, doc, dpmObj)
		for k, v := range dpmDict {
			vRes, _ := ResolveObject(ctx, doc, v)
			if vStr, ok := vRes.(ast.PdfString); ok {
				currentDPM[string(k)] = DecodePdfString(vStr)
			} else if vName, ok := vRes.(ast.PdfName); ok {
				currentDPM[string(k)] = string(vName)
			}
		}
	}

	if kObj, ok := node["K"]; ok {
		kRes, _ := ResolveObject(ctx, doc, kObj)
		if kArr, ok := kRes.(ast.PdfArray); ok {
			for _, kid := range kArr {
				var kidRef ast.PdfIndirectRef
				if ref, ok := kid.(ast.PdfIndirectRef); ok {
					kidRef = ref
				}

				kidDict, _ := ResolveDict(ctx, doc, kid)
				if kidDict != nil {
					// Check if it's a Page dictionary
					isPage := false
					if tObj, ok := kidDict["Type"]; ok {
						tName, _ := ResolveName(ctx, doc, tObj)
						if tName == "Page" {
							isPage = true
						}
					}

					if isPage {
						if kidRef.ObjectNum != 0 {
							pageDPMMap[kidRef.ObjectNum] = currentDPM
						}
					} else {
						// It's another DPart node
						traverseDPart(ctx, doc, kidDict, currentDPM, pageDPMMap)
					}
				}
			}
		} else if kDict, ok := kRes.(ast.PdfDict); ok {
			traverseDPart(ctx, doc, kDict, currentDPM, pageDPMMap)
		}
	}
}
