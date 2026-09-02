package semantics

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractEmbeddedFiles returns every file embedded in the document.
//
// Three places carry them. The /Names -> /EmbeddedFiles name tree holds
// document-level attachments (ISO 32000-1, 7.11.4). A FileAttachment annotation
// carries its own file specification in /FS (ISO 32000-1, 12.5.6.15). An /AF
// array associates one with the document or a page (ISO 32000-2, 14.13). A file
// reached by any of them never appears in the name tree unless the producer put
// it there too, so listing only the tree reports "no attachments" for a
// document that plainly has one.
//
// A stream reached more than once is reported once. Name tree entries come
// first, in tree order, then annotation entries in page order, then associated
// files.
func ExtractEmbeddedFiles(ctx context.Context, doc *document.Document) ([]models.EmbeddedFile, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}

	seen := make(map[int]bool)

	treeFiles, err := nameTreeAttachments(ctx, doc, catalog, seen)
	if err != nil {
		return nil, err
	}
	annotFiles, err := collectAnnotationAttachments(ctx, doc, catalog, seen)
	if err != nil {
		return nil, err
	}
	afFiles := collectAssociatedFiles(ctx, doc, catalog, seen)

	files := make([]models.EmbeddedFile, 0, len(treeFiles)+len(annotFiles)+len(afFiles))
	files = append(files, treeFiles...)
	files = append(files, annotFiles...)
	files = append(files, afFiles...)
	return files, nil
}

// collectAssociatedFiles reports the files attached through /AF that neither
// the name tree nor an annotation already supplied.
//
// ISO 32000-2, 14.13 associates a file with the document or one of its parts
// through an /AF array of file specifications. A producer may use it as the
// only route to an embedded file: pdfjs bug1937438_mml_from_latex.pdf attaches
// two stylesheets that way with no name tree at all, and a Factur-X invoice in
// the corpus reaches its factur-x.xml the same way.
//
// The catalog and the pages are walked, which is where the corpus puts them.
func collectAssociatedFiles(
	ctx context.Context,
	doc *document.Document,
	catalog *document.Catalog,
	seen map[int]bool,
) []models.EmbeddedFile {
	var files []models.EmbeddedFile
	collect := func(dict ast.PdfDict) {
		afObj, ok := dict["AF"]
		if !ok {
			return
		}
		specs, err := document.ResolveArray(ctx, doc, afObj)
		if err != nil {
			// A single file specification is allowed where an array is
			// expected, so fall back to reading /AF as one entry.
			specs = ast.PdfArray{afObj}
		}
		for _, specObj := range specs {
			ef, streamNum, err := extractFileSpec(ctx, doc, specObj)
			if err != nil {
				doc.Logger.Debug("extracting associated file specification", "err", err)
				continue
			}
			if ef == nil {
				continue
			}
			if streamNum > 0 {
				if seen[streamNum] {
					continue
				}
				seen[streamNum] = true
			}
			files = append(files, *ef)
		}
	}

	collect(catalog.Dict)

	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		doc.Logger.Debug("resolving pages for associated files", "err", err)
		return files
	}
	for _, page := range pages {
		if ctx.Err() != nil {
			return files
		}
		collect(page.Dict)
	}
	return files
}

// nameTreeAttachments collects the document-level attachments from the
// /Names -> /EmbeddedFiles tree, recording each stream's object number in seen.
func nameTreeAttachments(
	ctx context.Context,
	doc *document.Document,
	catalog *document.Catalog,
	seen map[int]bool,
) ([]models.EmbeddedFile, error) {
	namesObj, ok := catalog.Dict["Names"]
	if !ok {
		return nil, nil
	}
	namesDict, err := document.ResolveDict(ctx, doc, namesObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve /Names: %w", err)
	}
	if namesDict == nil {
		return nil, nil
	}
	efObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return nil, nil
	}

	var files []models.EmbeddedFile
	if err := collectEmbeddedFiles(ctx, doc, efObj, &files, seen); err != nil {
		return nil, err
	}
	return files, nil
}

// collectAnnotationAttachments walks every page's annotations and extracts the
// embedded file carried by each FileAttachment annotation whose stream is not
// already in seen.
func collectAnnotationAttachments(
	ctx context.Context,
	doc *document.Document,
	catalog *document.Catalog,
	seen map[int]bool,
) ([]models.EmbeddedFile, error) {
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages for annotation attachments: %w", err)
	}

	var files []models.EmbeddedFile
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("collecting annotation attachments canceled: %w", err)
		}

		annotsObj, ok := page.Dict["Annots"]
		if !ok {
			continue
		}
		annotsArr, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			doc.Logger.Debug("resolving /Annots for attachments", "err", err)
			continue
		}

		for _, annotRef := range annotsArr {
			annotDict, err := document.ResolveDict(ctx, doc, annotRef)
			if err != nil || annotDict == nil {
				continue
			}

			subtype, err := document.ResolveName(ctx, doc, annotDict["Subtype"])
			if err != nil || subtype != "FileAttachment" {
				continue
			}

			fsObj, ok := annotDict["FS"]
			if !ok {
				continue
			}

			ef, streamNum, err := extractFileSpec(ctx, doc, fsObj)
			if err != nil {
				doc.Logger.Debug("extracting FileAttachment file specification", "err", err)
				continue
			}
			if ef == nil {
				continue
			}
			// A direct stream (object number 0) cannot be reached twice, so it
			// is always new.
			if streamNum > 0 {
				if seen[streamNum] {
					continue
				}
				seen[streamNum] = true
			}
			files = append(files, *ef)
		}
	}
	return files, nil
}
