package semantics

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// ExtractAttachments extracts embedded files from the PDF and saves them to disk.
func ExtractAttachments(ctx context.Context, doc *document.Document, outputDir string) ([]string, error) {
	catalog, err := document.GetCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}

	namesObj, ok := catalog.Dict["Names"]
	if !ok {
		return nil, nil // No names dictionary
	}

	namesDict, err := document.ResolveDict(ctx, doc, namesObj)
	if err != nil {
		return nil, err
	}
	if namesDict == nil {
		return nil, fmt.Errorf("/Names is not a dictionary")
	}

	embeddedFilesObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return nil, nil // No embedded files
	}

	var savedFiles []string

	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %v", err)
	}

	err = extractNamesTree(ctx, doc, embeddedFilesObj, outputDir, &savedFiles)
	if err != nil {
		return nil, err
	}

	return savedFiles, nil
}

func extractNamesTree(ctx context.Context, doc *document.Document, nodeObj ast.PdfObject, outputDir string, savedFiles *[]string) error {
	nodeDict, err := document.ResolveDict(ctx, doc, nodeObj)
	if err != nil {
		return err
	}

	if nodeDict == nil {
		return nil
	}

	// Traverse Kids
	if kidsObj, ok := nodeDict["Kids"]; ok {
		kidsArr, _ := document.ResolveArray(ctx, doc, kidsObj)
		for _, kid := range kidsArr {
			if err := extractNamesTree(ctx, doc, kid, outputDir, savedFiles); err != nil {
				return err
			}
		}
		return nil
	}

	// Process Names array (leaf node)
	if namesObj, ok := nodeDict["Names"]; ok {
		namesArr, _ := document.ResolveArray(ctx, doc, namesObj)
		if namesArr != nil {
			// Names array alternates between name string and value object
			for i := 1; i < len(namesArr); i += 2 {
				valObj := namesArr[i]
				fileSpecDict, _ := document.ResolveDict(ctx, doc, valObj)

				if fileSpecDict != nil {
					// We have a File Specification dictionary
					if efObj, ok := fileSpecDict["EF"]; ok {
						efDict, _ := document.ResolveDict(ctx, doc, efObj)
						if efDict != nil {
							if fObj, ok := efDict["F"]; ok {
								stream, _ := document.ResolveStream(ctx, doc, fObj)
								if stream.Dict != nil {
									// Extract filename from File Specification (/F or /UF)
									filename := "attachment"
									if nameObj, ok := fileSpecDict["F"]; ok {
										nameStr, _ := document.ResolveString(ctx, doc, nameObj)
										if nameStr != "" {
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
