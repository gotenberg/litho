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

// maxDocumentScriptsSize bounds the total script text collected from the name
// tree. Anti-DoS guard, not spec-mandated.
//
// maxJSNameTreeDepth and the visited set bound how many NODES are walked.
// Neither says anything about the bytes each node contributes: every /JS entry
// may be a stream decoding under its own decompression cap, so without this the
// total grows with the number of entries a file names rather than with anything
// a document could legitimately script. Measured on a leaf of 20 compressed
// streams, 328 KB of input collected 160 MB.
//
// 4 MB is chosen against real documents rather than by round number, and
// leaves better than 60x headroom over the heaviest one.
//
// # Corpus evidence
//
//   - All corpora: of 59 files naming /JavaScript, 30 carry document-level
//     scripts.
//   - pdfjs issue20914.pdf: carries the largest total, 60,881 bytes across 65
//     scripts.
const maxDocumentScriptsSize = 4 << 20

func extractJSTree(ctx context.Context, doc *document.Document, nodeObj ast.PdfObject, scripts *[]string) error {
	total := 0
	return extractJSTreeAt(ctx, doc, nodeObj, scripts, make(map[int]bool), 0, &total)
}

// addScript records one script and enforces maxDocumentScriptsSize across the
// whole walk. total is threaded through the recursion rather than recomputed
// per node so the bound is on the sum, which is the quantity a crafted file
// controls.
func addScript(scripts *[]string, total *int, s string) error {
	*scripts = append(*scripts, s)
	*total += len(s)
	if *total > maxDocumentScriptsSize {
		return fmt.Errorf("document JavaScript exceeds %d MB limit", maxDocumentScriptsSize>>20)
	}
	return nil
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
func extractJSTreeAt(ctx context.Context, doc *document.Document, nodeObj ast.PdfObject, scripts *[]string, visited map[int]bool, depth int, total *int) error {
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
		if document.VisitedBefore(kidsObj, visited) {
			return nil
		}
		kidsArr, err := document.ResolveArray(ctx, doc, kidsObj)
		if err == nil && kidsArr != nil {
			for _, kid := range kidsArr {
				if err := extractJSTreeAt(ctx, doc, kid, scripts, visited, depth+1, total); err != nil {
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
										if err := addScript(scripts, total, document.DecodePdfString(str)); err != nil {
											return err
										}
									} else if stream, ok := jsRes.(ast.PdfStream); ok {
										data, err := doc.DecodeStream(ctx, stream)
										if err == nil {
											if err := addScript(scripts, total, string(data)); err != nil {
												return err
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
	}

	return nil
}
