package document

import (
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// GetDPartMap traverses the /DPartRoot tree and returns a mapping from
// Page Object Number to the inherited Document Part Metadata (DPM).
func GetDPartMap(doc *Document) (map[int]map[string]string, error) {
	catalog, err := GetCatalog(doc)
	if err != nil {
		return nil, err
	}

	dprObj, ok := catalog.Dict["DPartRoot"]
	if !ok {
		return nil, nil // No DPartRoot
	}

	dprRes, err := ResolveObject(doc, dprObj)
	if err != nil {
		return nil, err
	}

	dprDict, ok := dprRes.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("DPartRoot is not a dictionary")
	}

	pageDPMMap := make(map[int]map[string]string)
	traverseDPart(doc, dprDict, make(map[string]string), pageDPMMap)

	return pageDPMMap, nil
}

func traverseDPart(doc *Document, node ast.PdfDict, inheritedDPM map[string]string, pageDPMMap map[int]map[string]string) {
	currentDPM := make(map[string]string)
	maps.Copy(currentDPM, inheritedDPM)

	if dpmObj, ok := node["DPM"]; ok {
		dpmRes, _ := ResolveObject(doc, dpmObj)
		if dpmDict, ok := dpmRes.(ast.PdfDict); ok {
			for k, v := range dpmDict {
				vRes, _ := ResolveObject(doc, v)
				if vStr, ok := vRes.(ast.PdfString); ok {
					currentDPM[string(k)] = DecodePdfString(vStr)
				} else if vName, ok := vRes.(ast.PdfName); ok {
					currentDPM[string(k)] = string(vName)
				}
			}
		}
	}

	if kObj, ok := node["K"]; ok {
		kRes, _ := ResolveObject(doc, kObj)
		if kArr, ok := kRes.(ast.PdfArray); ok {
			for _, kid := range kArr {
				var kidRef ast.PdfIndirectRef
				if ref, ok := kid.(ast.PdfIndirectRef); ok {
					kidRef = ref
				}

				kidRes, _ := ResolveObject(doc, kid)
				if kidDict, ok := kidRes.(ast.PdfDict); ok {
					// Check if it's a Page dictionary
					isPage := false
					if tObj, ok := kidDict["Type"]; ok {
						tRes, _ := ResolveObject(doc, tObj)
						if tName, ok := tRes.(ast.PdfName); ok && tName == "Page" {
							isPage = true
						}
					}

					if isPage {
						if kidRef.ObjectNum != 0 {
							pageDPMMap[kidRef.ObjectNum] = currentDPM
						}
					} else {
						// It's another DPart node
						traverseDPart(doc, kidDict, currentDPM, pageDPMMap)
					}
				}
			}
		} else if kDict, ok := kRes.(ast.PdfDict); ok {
			traverseDPart(doc, kDict, currentDPM, pageDPMMap)
		}
	}
}
