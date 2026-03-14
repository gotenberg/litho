package semantics

import (
	"fmt"
	"path/filepath"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// IsPortfolio checks whether the document catalog contains a /Collection
// dictionary, which marks the PDF as a Portfolio (PDF 1.7+).
func IsPortfolio(doc *document.Document) (bool, error) {
	catalog, err := document.GetCatalog(doc)
	if err != nil {
		return false, err
	}
	_, ok := catalog.Dict["Collection"]
	return ok, nil
}

// ExtractPortfolioItems traverses the /Names -> /EmbeddedFiles name tree and
// returns each embedded file's name, MIME type, and decoded data. This allows
// callers to recursively open embedded PDFs via pdf.Open.
func ExtractPortfolioItems(doc *document.Document) ([]models.EmbeddedFile, error) {
	catalog, err := document.GetCatalog(doc)
	if err != nil {
		return nil, err
	}

	namesObj, ok := catalog.Dict["Names"]
	if !ok {
		return nil, nil
	}
	namesRes, err := document.ResolveObject(doc, namesObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve /Names: %w", err)
	}
	namesDict, ok := namesRes.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("/Names is not a dictionary")
	}

	efObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return nil, nil
	}

	var files []models.EmbeddedFile
	if err := collectEmbeddedFiles(doc, efObj, &files); err != nil {
		return nil, err
	}
	return files, nil
}

// collectEmbeddedFiles recursively walks an EmbeddedFiles name tree node,
// extracting each file specification into a models.EmbeddedFile.
func collectEmbeddedFiles(doc *document.Document, nodeObj ast.PdfObject, files *[]models.EmbeddedFile) error {
	nodeRes, err := document.ResolveObject(doc, nodeObj)
	if err != nil {
		return err
	}
	nodeDict, ok := nodeRes.(ast.PdfDict)
	if !ok {
		return nil
	}

	// Intermediate node: recurse into Kids.
	if kidsObj, ok := nodeDict["Kids"]; ok {
		kidsRes, err := document.ResolveObject(doc, kidsObj)
		if err != nil {
			return fmt.Errorf("failed to resolve /Kids: %w", err)
		}
		kidsArr, ok := kidsRes.(ast.PdfArray)
		if !ok {
			return nil
		}
		for _, kid := range kidsArr {
			if err := collectEmbeddedFiles(doc, kid, files); err != nil {
				return err
			}
		}
		return nil
	}

	// Leaf node: process Names array (alternating key/value pairs).
	namesObj, ok := nodeDict["Names"]
	if !ok {
		return nil
	}
	namesRes, err := document.ResolveObject(doc, namesObj)
	if err != nil {
		return fmt.Errorf("failed to resolve /Names array: %w", err)
	}
	namesArr, ok := namesRes.(ast.PdfArray)
	if !ok {
		return nil
	}

	for i := 1; i < len(namesArr); i += 2 {
		ef, err := extractFileSpec(doc, namesArr[i])
		if err != nil {
			return err
		}
		if ef != nil {
			*files = append(*files, *ef)
		}
	}
	return nil
}

// extractFileSpec extracts a single EmbeddedFile from a File Specification
// dictionary. It reads /F or /UF for the filename, /EF -> /F for the stream
// data, and the stream's /Subtype for the MIME type.
func extractFileSpec(doc *document.Document, obj ast.PdfObject) (*models.EmbeddedFile, error) {
	specRes, err := document.ResolveObject(doc, obj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve file specification: %w", err)
	}
	specDict, ok := specRes.(ast.PdfDict)
	if !ok {
		return nil, nil
	}

	// Determine filename: prefer /UF (Unicode filename), fall back to /F.
	filename := "embedded"
	if ufObj, ok := specDict["UF"]; ok {
		ufRes, _ := document.ResolveObject(doc, ufObj)
		if ufStr, ok := ufRes.(ast.PdfString); ok {
			filename = document.DecodePdfString(ufStr)
		}
	} else if fObj, ok := specDict["F"]; ok {
		fRes, _ := document.ResolveObject(doc, fObj)
		if fStr, ok := fRes.(ast.PdfString); ok {
			filename = document.DecodePdfString(fStr)
		}
	}
	// Sanitize to prevent directory traversal.
	filename = filepath.Base(filename)

	// Get the /EF dictionary containing the actual file stream.
	efObj, ok := specDict["EF"]
	if !ok {
		return nil, nil
	}
	efRes, err := document.ResolveObject(doc, efObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve /EF: %w", err)
	}
	efDict, ok := efRes.(ast.PdfDict)
	if !ok {
		return nil, nil
	}

	// The stream is stored under /F (or /UF) in the EF dictionary.
	var streamObj ast.PdfObject
	if fObj, ok := efDict["F"]; ok {
		streamObj = fObj
	} else if ufObj, ok := efDict["UF"]; ok {
		streamObj = ufObj
	} else {
		return nil, nil
	}

	streamRes, err := document.ResolveObject(doc, streamObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve embedded file stream: %w", err)
	}
	stream, ok := streamRes.(ast.PdfStream)
	if !ok {
		return nil, nil
	}

	// Extract MIME type from stream's /Subtype (e.g., "application/pdf").
	mimeType := ""
	if stObj, ok := stream.Dict["Subtype"]; ok {
		stRes, _ := document.ResolveObject(doc, stObj)
		if stName, ok := stRes.(ast.PdfName); ok {
			// PDF stores MIME types with / replaced by # in name objects,
			// but many writers use the plain MIME string as the name value.
			mimeType = string(stName)
		}
	}

	return &models.EmbeddedFile{
		Name:     filename,
		MIMEType: mimeType,
		Open:     openStreamFunc(stream),
	}, nil
}
