package semantics

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// Bead represents a chunk of an article thread on a page.
type Bead struct {
	ThreadTitle string
	PageRef     ast.PdfIndirectRef
	Rect        [4]float64
}

// Thread represents an article thread with sequential reading order.
type Thread struct {
	Title string
	Beads []Bead
}

// getFloat is a helper to safely extract a float64 from a PdfObject.
func getFloat(obj ast.PdfObject) float64 {
	switch v := obj.(type) {
	case ast.PdfReal:
		return float64(v)
	case ast.PdfInt:
		return float64(v)
	}
	return 0
}

// GetThreads extracts article threads from the document.
func GetThreads(doc *document.Document) ([]Thread, error) {
	rootObj, ok := doc.Index.Trailer["Root"]
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}

	rootRes, err := doc.Resolve(rootObj.(ast.PdfIndirectRef))
	if err != nil {
		return nil, err
	}

	catalog, ok := rootRes.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("catalog is not a dictionary")
	}

	threadsObj, ok := catalog["Threads"]
	if !ok {
		return nil, nil // No threads
	}

	var threadsArr ast.PdfArray
	if ref, ok := threadsObj.(ast.PdfIndirectRef); ok {
		res, err := doc.Resolve(ref)
		if err != nil {
			return nil, err
		}
		threadsArr, ok = res.(ast.PdfArray)
		if !ok {
			return nil, fmt.Errorf("/Threads is not an array")
		}
	} else if arr, ok := threadsObj.(ast.PdfArray); ok {
		threadsArr = arr
	} else {
		return nil, fmt.Errorf("invalid /Threads type")
	}

	var result []Thread

	for _, tRefObj := range threadsArr {
		var tDict ast.PdfDict
		if ref, ok := tRefObj.(ast.PdfIndirectRef); ok {
			res, err := doc.Resolve(ref)
			if err != nil {
				continue
			}
			tDict, ok = res.(ast.PdfDict)
			if !ok {
				continue
			}
		} else {
			continue
		}

		var thread Thread

		// Info dictionary containing Title
		if infoObj, ok := tDict["I"]; ok {
			if ref, ok := infoObj.(ast.PdfIndirectRef); ok {
				infoRes, _ := doc.Resolve(ref)
				if infoDict, ok := infoRes.(ast.PdfDict); ok {
					if titleObj, ok := infoDict["Title"]; ok {
						if titleRef, ok := titleObj.(ast.PdfIndirectRef); ok {
							titleRes, _ := doc.Resolve(titleRef)
							if titleStr, ok := titleRes.(ast.PdfString); ok {
								thread.Title = decodePdfString(titleStr)
							}
						} else if titleStr, ok := titleObj.(ast.PdfString); ok {
							thread.Title = decodePdfString(titleStr)
						}
					}
				}
			}
		}

		// First bead
		if firstObj, ok := tDict["F"]; ok {
			var currentBeadRef ast.PdfIndirectRef
			if ref, ok := firstObj.(ast.PdfIndirectRef); ok {
				currentBeadRef = ref
			}

			// Follow the linked list
			visited := make(map[int]bool)
			for currentBeadRef.ObjectNum != 0 {
				if visited[currentBeadRef.ObjectNum] {
					break // Prevent infinite loops
				}
				visited[currentBeadRef.ObjectNum] = true

				res, err := doc.Resolve(currentBeadRef)
				if err != nil {
					break
				}

				beadDict, ok := res.(ast.PdfDict)
				if !ok {
					break
				}

				var bead Bead
				bead.ThreadTitle = thread.Title

				// Page Reference
				if pObj, ok := beadDict["P"]; ok {
					if pRef, ok := pObj.(ast.PdfIndirectRef); ok {
						bead.PageRef = pRef
					}
				}

				// Rectangle
				if rObj, ok := beadDict["R"]; ok {
					var rArr ast.PdfArray
					if ref, ok := rObj.(ast.PdfIndirectRef); ok {
						res, _ := doc.Resolve(ref)
						rArr, _ = res.(ast.PdfArray)
					} else if arr, ok := rObj.(ast.PdfArray); ok {
						rArr = arr
					}
					if len(rArr) == 4 {
						for i := range 4 {
							bead.Rect[i] = getFloat(rArr[i])
						}
					}
				}

				thread.Beads = append(thread.Beads, bead)

				// Next bead
				if nextObj, ok := beadDict["N"]; ok {
					if nRef, ok := nextObj.(ast.PdfIndirectRef); ok {
						currentBeadRef = nRef
					} else {
						break
					}
				} else {
					break
				}
			}
		}

		result = append(result, thread)
	}

	return result, nil
}
