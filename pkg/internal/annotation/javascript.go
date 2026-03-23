package annotation

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// ExtractDocumentScripts extracts document-level JavaScript scripts from the PDF Name tree.
func ExtractDocumentScripts(ctx context.Context, doc *document.Document) ([]string, error) {
	catalog, err := document.GetCatalog(ctx, doc)
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

func extractJSTree(ctx context.Context, doc *document.Document, nodeObj ast.PdfObject, scripts *[]string) error {
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
				if err := extractJSTree(ctx, doc, kid, scripts); err != nil {
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
										data, err := doc.DecodeStream(stream)
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
