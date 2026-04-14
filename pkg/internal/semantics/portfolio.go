package semantics

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// IsPortfolio checks whether the document catalog contains a /Collection
// dictionary, which marks the PDF as a Portfolio (PDF 1.7+).
func IsPortfolio(ctx context.Context, doc *document.Document) (bool, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return false, err
	}
	_, ok := catalog.Dict["Collection"]
	return ok, nil
}

// ExtractPortfolioItems traverses the /Names -> /EmbeddedFiles name tree and
// returns each embedded file's name, MIME type, and decoded data. This allows
// callers to recursively open embedded PDFs via pdf.Open.
func ExtractPortfolioItems(ctx context.Context, doc *document.Document) ([]models.EmbeddedFile, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}

	namesObj, ok := catalog.Dict["Names"]
	if !ok {
		return nil, nil
	}
	namesDict, err := document.ResolveDict(ctx, doc, namesObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve /Names: %w", err)
	}
	if namesDict == nil {
		return nil, fmt.Errorf("/Names is not a dictionary")
	}

	efObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return nil, nil
	}

	var files []models.EmbeddedFile
	if err := collectEmbeddedFiles(ctx, doc, efObj, &files); err != nil {
		return nil, err
	}
	return files, nil
}

// collectEmbeddedFiles recursively walks an EmbeddedFiles name tree node,
// extracting each file specification into a models.EmbeddedFile.
func collectEmbeddedFiles(ctx context.Context, doc *document.Document, nodeObj ast.PdfObject, files *[]models.EmbeddedFile) error {
	nodeDict, err := document.ResolveDict(ctx, doc, nodeObj)
	if err != nil {
		return err
	}
	if nodeDict == nil {
		return nil
	}

	// Intermediate node: recurse into Kids.
	if kidsObj, ok := nodeDict["Kids"]; ok {
		kidsArr, err := document.ResolveArray(ctx, doc, kidsObj)
		if err != nil {
			return fmt.Errorf("failed to resolve /Kids: %w", err)
		}
		if kidsArr == nil {
			return nil
		}
		for _, kid := range kidsArr {
			if err := collectEmbeddedFiles(ctx, doc, kid, files); err != nil {
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
	namesArr, err := document.ResolveArray(ctx, doc, namesObj)
	if err != nil {
		return fmt.Errorf("failed to resolve /Names array: %w", err)
	}
	if namesArr == nil {
		return nil
	}

	for i := 1; i < len(namesArr); i += 2 {
		ef, err := extractFileSpec(ctx, doc, namesArr[i])
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
func extractFileSpec(ctx context.Context, doc *document.Document, obj ast.PdfObject) (*models.EmbeddedFile, error) {
	specDict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve file specification: %w", err)
	}
	if specDict == nil {
		return nil, nil
	}

	// Determine filename: prefer /UF (Unicode filename), fall back to /F.
	filename := "embedded"
	if ufObj, ok := specDict["UF"]; ok {
		ufStr, err := document.ResolveString(ctx, doc, ufObj)
		if err != nil {
			doc.Logger.Debug("resolving portfolio /UF filename", slog.Any("err", err))
		}
		if ufStr != "" {
			filename = document.DecodePdfString(ufStr)
		}
	} else if fObj, ok := specDict["F"]; ok {
		fStr, err := document.ResolveString(ctx, doc, fObj)
		if err != nil {
			doc.Logger.Debug("resolving portfolio /F filename", slog.Any("err", err))
		}
		if fStr != "" {
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
	efDict, err := document.ResolveDict(ctx, doc, efObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve /EF: %w", err)
	}
	if efDict == nil {
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

	stream, err := document.ResolveStream(ctx, doc, streamObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve embedded file stream: %w", err)
	}
	if stream.Dict == nil {
		return nil, nil
	}

	// Extract MIME type from stream's /Subtype (e.g., "application/pdf").
	mimeType := ""
	if stObj, ok := stream.Dict["Subtype"]; ok {
		stName, err := document.ResolveName(ctx, doc, stObj)
		if err != nil {
			doc.Logger.Debug("resolving portfolio stream subtype", slog.Any("err", err))
		}
		if stName != "" {
			// PDF stores MIME types with / replaced by # in name objects,
			// but many writers use the plain MIME string as the name value.
			mimeType = string(stName)
		}
	}

	return &models.EmbeddedFile{
		Name:     filename,
		MIMEType: mimeType,
		Open:     doc.OpenFunc(stream),
	}, nil
}
