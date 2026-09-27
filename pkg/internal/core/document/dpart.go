package document

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// ResolveDPartMap traverses the /DPartRoot tree and returns a mapping from
// [Page] object number to the inherited Document Part Metadata (DPM).
func ResolveDPartMap(ctx context.Context, doc *Document) (map[int]map[string]string, error) {
	catalog, err := ResolveCatalog(ctx, doc)
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

	// A DPart node may name itself or an ancestor as its kid. The cycle guard
	// inside ResolveObject is per-resolution, so re-resolving that reference
	// from a fresh frame succeeds every time and nothing below stops the walk:
	// a 425-byte file whose only DPart node is its own kid recursed until the
	// goroutine stack was exhausted, which is a fatal runtime error that the
	// recover() at every pkg/pdf entry point cannot convert into an error.
	//
	// The visited set bounds the walk to the file's distinct DPart objects, a
	// natural bound rather than an arbitrary depth cap, and it detects the
	// cycle instead of merely outlasting it. It holds the /K arrays written as
	// objects of their own too: direct DPart nodes that each name their own
	// array again close a cycle through no node's object number.
	visited := make(map[int]bool)
	if ref, ok := dprObj.(ast.PdfIndirectRef); ok {
		visited[ref.ObjectNum] = true
	}
	traverseDPart(ctx, doc, dprDict, make(map[string]string), pageDPMMap, visited)

	return pageDPMMap, nil
}

func traverseDPart(ctx context.Context, doc *Document, node ast.PdfDict, inheritedDPM map[string]string, pageDPMMap map[int]map[string]string, visited map[int]bool) {
	if ctx.Err() != nil {
		return
	}
	currentDPM := make(map[string]string)
	maps.Copy(currentDPM, inheritedDPM)

	if dpmObj, ok := node["DPM"]; ok {
		dpmDict, err := ResolveDict(ctx, doc, dpmObj)
		if err != nil {
			doc.Logger.Debug("resolving DPart /DPM dict", slog.Any("err", err))
		}
		for k, v := range dpmDict {
			vRes, err := ResolveObject(ctx, doc, v)
			if err != nil {
				doc.Logger.Debug("resolving DPM value", slog.String("key", string(k)), slog.Any("err", err))
			}
			if vStr, ok := vRes.(ast.PdfString); ok {
				currentDPM[string(k)] = DecodePdfString(vStr)
			} else if vName, ok := vRes.(ast.PdfName); ok {
				currentDPM[string(k)] = string(vName)
			}
		}
	}

	if kObj, ok := node["K"]; ok {
		kRes, err := ResolveObject(ctx, doc, kObj)
		if err != nil {
			doc.Logger.Debug("resolving DPart /K object", slog.Any("err", err))
		}
		if kArr, ok := kRes.(ast.PdfArray); ok {
			// A /K array written as an object of its own is walked once, as
			// a node is: the direct nodes it holds can each name it again as
			// their /K, and nothing else would stop that recursion.
			if VisitedBefore(kObj, visited) {
				kArr = nil
			}
			for _, kid := range kArr {
				var kidRef ast.PdfIndirectRef
				if ref, ok := kid.(ast.PdfIndirectRef); ok {
					kidRef = ref
				}

				kidDict, err := ResolveDict(ctx, doc, kid)
				if err != nil {
					doc.Logger.Debug("resolving DPart kid dict", slog.Any("err", err))
				}
				if kidDict != nil {
					// Check if it's a Page dictionary
					isPage := false
					if tObj, ok := kidDict["Type"]; ok {
						tName, err := ResolveName(ctx, doc, tObj)
						if err != nil {
							doc.Logger.Debug("resolving DPart kid type", slog.Any("err", err))
						}
						if tName == "Page" {
							isPage = true
						}
					}

					if isPage {
						if kidRef.ObjectNum != 0 {
							pageDPMMap[kidRef.ObjectNum] = currentDPM
						}
					} else if enterDPart(visited, kidRef.ObjectNum) {
						// It's another DPart node
						traverseDPart(ctx, doc, kidDict, currentDPM, pageDPMMap, visited)
					}
				}
			}
		} else if kDict, ok := kRes.(ast.PdfDict); ok {
			var kObjNum int
			if ref, ok := kObj.(ast.PdfIndirectRef); ok {
				kObjNum = ref.ObjectNum
			}
			if enterDPart(visited, kObjNum) {
				traverseDPart(ctx, doc, kDict, currentDPM, pageDPMMap, visited)
			}
		}
	}
}

// enterDPart reports whether the walk may descend into a node, marking it
// visited when it may. A node with no object number is a direct dictionary:
// direct values cannot name themselves, so they close no cycle and need no mark.
func enterDPart(visited map[int]bool, objNum int) bool {
	if objNum == 0 {
		return true
	}
	if visited[objNum] {
		return false
	}
	visited[objNum] = true
	return true
}
