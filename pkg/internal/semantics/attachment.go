package semantics

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// ExtractAttachments extracts embedded files from the PDF and saves them to disk.
func ExtractAttachments(doc *document.Document, outputDir string) ([]string, error) {
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

	embeddedFilesObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return nil, nil // No embedded files
	}

	var savedFiles []string

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %v", err)
	}

	err = extractNamesTree(doc, embeddedFilesObj, outputDir, &savedFiles)
	if err != nil {
		return nil, err
	}

	return savedFiles, nil
}

func extractNamesTree(doc *document.Document, nodeObj ast.PdfObject, outputDir string, savedFiles *[]string) error {
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
				if err := extractNamesTree(doc, kid, outputDir, savedFiles); err != nil {
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
			// Names array alternates between name string and value object
			for i := 1; i < len(namesArr); i += 2 {
				valObj := namesArr[i]
				valRes, _ := document.ResolveObject(doc, valObj)

				if fileSpecDict, ok := valRes.(ast.PdfDict); ok {
					// We have a File Specification dictionary
					if efObj, ok := fileSpecDict["EF"]; ok {
						efRes, _ := document.ResolveObject(doc, efObj)
						if efDict, ok := efRes.(ast.PdfDict); ok {
							if fObj, ok := efDict["F"]; ok {
								fRes, _ := document.ResolveObject(doc, fObj)
								if stream, ok := fRes.(ast.PdfStream); ok {
									// Extract filename from File Specification (/F or /UF)
									filename := "attachment"
									if nameObj, ok := fileSpecDict["F"]; ok {
										nameRes, _ := document.ResolveObject(doc, nameObj)
										if nameStr, ok := nameRes.(ast.PdfString); ok {
											filename = document.DecodePdfString(nameStr)
										}
									}

									// Sanitize filename to prevent directory traversal
									filename = filepath.Base(filename)

									// Decode and save stream
									data, err := filter.DecodeStream(stream)
									if err == nil {
										outPath := filepath.Join(outputDir, filename)
										err = os.WriteFile(outPath, data, 0o600)
										if err == nil {
											*savedFiles = append(*savedFiles, outPath)
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
