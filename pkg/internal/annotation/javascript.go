package annotation

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// ExtractDocumentScripts extracts document-level JavaScript scripts from the PDF Name tree.
func ExtractDocumentScripts(ctx context.Context, doc *document.Document) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("getting catalog for document scripts: %w", err)
	}

	namesObj, ok := catalog.Dict["Names"]
	if !ok {
		return nil, nil // No names dictionary
	}

	namesDict, err := document.ResolveDict(ctx, doc, namesObj)
	if err != nil {
		return nil, fmt.Errorf("resolving Names dict: %w", err)
	}
	if namesDict == nil {
		return nil, fmt.Errorf("/Names is not a dictionary")
	}

	jsObj, ok := namesDict["JavaScript"]
	if !ok {
		return nil, nil // No document-level JS
	}

	var scripts []string
	err = extractJSTree(ctx, doc, jsObj, &scripts)
	if err != nil {
		return nil, fmt.Errorf("extracting document scripts: %w", err)
	}

	return scripts, nil
}

// maxJSNameTreeDepth bounds the walk over the document-level JavaScript name
// tree. Anti-DoS guard, not spec-mandated: ISO 32000-1, 7.9.6 describes a
// balanced name tree and no real document nests one deeply. The value matches
// maxNameTreeDepth in core/document, which bounds this same structure for key
// lookup, and maxNameTreeWalkDepth in remediation/fixer_embedded.go.
const maxJSNameTreeDepth = 64

func extractJSTree(ctx context.Context, doc *document.Document, nodeObj ast.PdfObject, scripts *[]string) error {
	return extractJSTreeAt(ctx, doc, nodeObj, scripts, make(map[int]bool), 0)
}

// extractJSTreeAt collects the scripts at and below one name-tree node.
//
// visited marks each node by object number. A name tree is a strict tree, so a
// /Kids entry reached twice is malformed: without the set, a node naming itself
// recurses until the goroutine stack is exhausted, and a stack overflow is a
// fatal runtime error that no recover() can convert into an error. The set also
// ends the 2^depth fan-out a node whose /Kids lists the same child twice would
// otherwise cause, which is the hazard searchNameTreeAt records for the lookup
// walk over this same structure.
func extractJSTreeAt(ctx context.Context, doc *document.Document, nodeObj ast.PdfObject, scripts *[]string, visited map[int]bool, depth int) error {
	if depth > maxJSNameTreeDepth {
		return nil
	}
	if ref, ok := nodeObj.(ast.PdfIndirectRef); ok {
		if visited[ref.ObjectNum] {
			return nil
		}
		visited[ref.ObjectNum] = true
	}

	nodeDict, err := document.ResolveDict(ctx, doc, nodeObj)
	if err != nil {
		return fmt.Errorf("resolving JS name tree node: %w", err)
	}
	if nodeDict == nil {
		return nil
	}

	// Traverse Kids
	if kidsObj, ok := nodeDict["Kids"]; ok {
		kidsArr, err := document.ResolveArray(ctx, doc, kidsObj)
		if err == nil && kidsArr != nil {
			for _, kid := range kidsArr {
				if err := extractJSTreeAt(ctx, doc, kid, scripts, visited, depth+1); err != nil {
					return fmt.Errorf("traversing JS name tree kids: %w", err)
				}
			}
		}
		return nil
	}

	// Process Names array (leaf node)
	if namesObj, ok := nodeDict["Names"]; ok {
		namesArr, err := document.ResolveArray(ctx, doc, namesObj)
		if err == nil && namesArr != nil {
			// Names array alternates between name string and value object (action dict)
			for i := 1; i < len(namesArr); i += 2 {
				valObj := namesArr[i]
				actionDict, err := document.ResolveDict(ctx, doc, valObj)

				if err == nil && actionDict != nil {
					if sObj, ok := actionDict["S"]; ok {
						sName, err := document.ResolveName(ctx, doc, sObj)
						if err == nil && sName == "JavaScript" {
							if jsObj, ok := actionDict["JS"]; ok {
								jsRes, err := document.ResolveObject(ctx, doc, jsObj)
								if err == nil {
									if str, ok := jsRes.(ast.PdfString); ok {
										*scripts = append(*scripts, document.DecodePdfString(str))
									} else if stream, ok := jsRes.(ast.PdfStream); ok {
										data, err := doc.DecodeStream(ctx, stream)
										if err == nil {
											*scripts = append(*scripts, string(data))
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return nil
}
