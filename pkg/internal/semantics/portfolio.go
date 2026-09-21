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
	if err := collectEmbeddedFiles(ctx, doc, efObj, &files, nil); err != nil {
		return nil, err
	}
	return files, nil
}

// collectEmbeddedFiles recursively walks an EmbeddedFiles name tree node,
// extracting each file specification into a models.EmbeddedFile.
//
// When seen is non-nil, the object number of every embedded file stream reached
// is recorded in it. Entries are never suppressed: a portfolio may list one
// stream under several names, and each is its own item.
//
// seen is NOT a cycle guard, despite its name and type: it records file STREAM
// object numbers for the caller, is keyed on a different population than the
// nodes this walk descends, and one caller passes nil. The visited set below is
// what ends a cycle.
func collectEmbeddedFiles(ctx context.Context, doc *document.Document, nodeObj ast.PdfObject, files *[]models.EmbeddedFile, seen map[int]bool) error {
	return collectEmbeddedFilesAt(ctx, doc, nodeObj, files, seen, make(map[int]bool))
}

// collectEmbeddedFilesAt walks one name-tree node. visited marks each node by
// object number: a name tree is a strict tree, so a /Kids entry reached twice is
// malformed, and without the set a node naming itself recurses until the
// goroutine stack is exhausted. A stack overflow is a fatal runtime error, not a
// panic, so the recover() at every pkg/pdf entry point cannot convert it.
func collectEmbeddedFilesAt(ctx context.Context, doc *document.Document, nodeObj ast.PdfObject, files *[]models.EmbeddedFile, seen, visited map[int]bool) error {
	// A direct dictionary carries no object number and cannot name itself, so
	// it closes no cycle and needs no mark.
	if ref, ok := nodeObj.(ast.PdfIndirectRef); ok {
		if visited[ref.ObjectNum] {
			return nil
		}
		visited[ref.ObjectNum] = true
	}

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
			if err := collectEmbeddedFilesAt(ctx, doc, kid, files, seen, visited); err != nil {
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
		ef, streamNum, err := extractFileSpec(ctx, doc, namesArr[i])
		if err != nil {
			return err
		}
		if ef != nil {
			*files = append(*files, *ef)
			if seen != nil && streamNum > 0 {
				seen[streamNum] = true
			}
		}
	}
	return nil
}

// extractFileSpec extracts a single EmbeddedFile from a File Specification
// dictionary. It reads /F or /UF for the filename, /EF -> /F for the stream
// data, and the stream's /Subtype for the MIME type.
//
// The second result is the object number of the embedded file stream, or 0 when
// the stream is a direct object. Callers listing files from more than one place
// in the document use it to report a stream reached twice only once.
func extractFileSpec(ctx context.Context, doc *document.Document, obj ast.PdfObject) (*models.EmbeddedFile, int, error) {
	specDict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to resolve file specification: %w", err)
	}
	if specDict == nil {
		return nil, 0, nil
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
		return nil, 0, nil
	}
	efDict, err := document.ResolveDict(ctx, doc, efObj)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to resolve /EF: %w", err)
	}
	if efDict == nil {
		return nil, 0, nil
	}

	// The stream is stored under /F (or /UF) in the EF dictionary.
	var streamObj ast.PdfObject
	if fObj, ok := efDict["F"]; ok {
		streamObj = fObj
	} else if ufObj, ok := efDict["UF"]; ok {
		streamObj = ufObj
	} else {
		return nil, 0, nil
	}

	streamNum := 0
	if ref, ok := streamObj.(ast.PdfIndirectRef); ok {
		streamNum = ref.ObjectNum
	}

	stream, err := document.ResolveStream(ctx, doc, streamObj)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to resolve embedded file stream: %w", err)
	}
	if stream.Dict == nil {
		return nil, 0, nil
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
	}, streamNum, nil
}
