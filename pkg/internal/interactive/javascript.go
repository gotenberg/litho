package interactive

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// ExtractDocumentScripts extracts document-level JavaScript scripts from the PDF Name tree.
func ExtractDocumentScripts(doc *document.Document) ([]string, error) {
	catalog, err := document.GetCatalog(doc)
	if err != nil {
		return nil, err
	}

	namesObj, ok := catalog.Dict["Names"]
	if !ok {
		return nil, nil // No names dictionary
	}

	namesRes, err := document.ResolveObject(doc, namesObj)
	if err != nil {
		return nil, err
	}
	namesDict, ok := namesRes.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("/Names is not a dictionary")
	}

	jsObj, ok := namesDict["JavaScript"]
	if !ok {
		return nil, nil // No document-level JS
	}

	var scripts []string
	err = extractJSTree(doc, jsObj, &scripts)
	if err != nil {
		return nil, err
	}

	return scripts, nil
}

func extractJSTree(doc *document.Document, nodeObj ast.PdfObject, scripts *[]string) error {
	nodeRes, err := document.ResolveObject(doc, nodeObj)
	if err != nil {
		return err
	}

	nodeDict, ok := nodeRes.(ast.PdfDict)
	if !ok {
		return nil
	}

	// Traverse Kids
	if kidsObj, ok := nodeDict["Kids"]; ok {
		kidsRes, _ := document.ResolveObject(doc, kidsObj)
		if kidsArr, ok := kidsRes.(ast.PdfArray); ok {
			for _, kid := range kidsArr {
				if err := extractJSTree(doc, kid, scripts); err != nil {
					return err
				}
			}
		}
		return nil
	}

	// Process Names array (leaf node)
	if namesObj, ok := nodeDict["Names"]; ok {
		namesRes, _ := document.ResolveObject(doc, namesObj)
		if namesArr, ok := namesRes.(ast.PdfArray); ok {
			// Names array alternates between name string and value object (action dict)
			for i := 1; i < len(namesArr); i += 2 {
				valObj := namesArr[i]
				valRes, _ := document.ResolveObject(doc, valObj)

				if actionDict, ok := valRes.(ast.PdfDict); ok {
					if sObj, ok := actionDict["S"]; ok {
						sRes, _ := document.ResolveObject(doc, sObj)
						if sName, ok := sRes.(ast.PdfName); ok && sName == "JavaScript" {
							if jsObj, ok := actionDict["JS"]; ok {
								jsRes, _ := document.ResolveObject(doc, jsObj)
								if str, ok := jsRes.(ast.PdfString); ok {
									*scripts = append(*scripts, document.DecodePdfString(str))
								} else if stream, ok := jsRes.(ast.PdfStream); ok {
									data, err := filter.DecodeStream(stream)
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

	return nil
}
