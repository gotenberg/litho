package mutations

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// Split extracts the specified 1-based pages from a document into a new PDF
// written to dst. Pages are included in the order given; duplicates are allowed.
// bookmarkCfg controls how bookmarks are produced for the output.
func Split(ctx context.Context, doc *document.Document, allPages []*document.Page, pages []int, dst io.Writer, version string, xrefStream bool, bookmarkCfg BookmarkConfig) error {
	if len(pages) == 0 {
		return fmt.Errorf("no pages specified")
	}

	// Validate page numbers.
	for _, p := range pages {
		if p < 1 || p > len(allPages) {
			return fmt.Errorf("page %d is out of bounds (document has %d pages)", p, len(allPages))
		}
	}

	alloc := writer.NewObjAllocator(0)
	copier := writer.NewCopier(doc, alloc)

	// Copy each selected page, building new page refs.
	pageRefs := make([]ast.PdfObject, 0, len(pages))
	for _, pageNum := range pages {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("split canceled: %w", err)
		}

		srcPage := allPages[pageNum-1]
		newRef, err := copier.CopyRef(ctx, srcPage.Ref)
		if err != nil {
			return fmt.Errorf("copying page %d: %w", pageNum, err)
		}
		pageRefs = append(pageRefs, newRef)
	}

	// Build new Pages dict.
	pagesDict := ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray(pageRefs),
		"Count": ast.PdfInt(len(pageRefs)),
	}
	pagesRef := alloc.Alloc(pagesDict)

	// Update each page's /Parent to point to the new Pages dict.
	for _, ref := range pageRefs {
		pageRef, ok := ref.(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		obj, ok := alloc.Pending()[pageRef.ObjectNum]
		if !ok {
			continue
		}
		pageDict, ok := obj.(ast.PdfDict)
		if !ok {
			continue
		}
		pageDict["Parent"] = pagesRef
	}

	// Build new catalog.
	catalogDict := ast.PdfDict{
		"Type":  ast.PdfName("Catalog"),
		"Pages": pagesRef,
	}

	// Copy catalog-level entries that should survive split.
	copyCatalogEntries(ctx, doc, copier, catalogDict)

	// Handle bookmarks for the output.
	if bookmarkCfg.Handling != BookmarkNone {
		newBookmarks, err := resolveSplitBookmarks(ctx, doc, allPages, pages, bookmarkCfg)
		if err != nil {
			doc.Logger.Debug("resolving split bookmarks failed",
				slog.Any("err", err),
			)
		}
		if err == nil && len(newBookmarks) > 0 {
			// Build the new-page-number → new-page-ref map from the
			// allocator's pageRefs slice.
			newPageRefs := make(map[int]ast.PdfIndirectRef, len(pageRefs))
			for i, ref := range pageRefs {
				if r, ok := ref.(ast.PdfIndirectRef); ok {
					newPageRefs[i+1] = r
				}
			}
			outlineRef := BuildOutline(ctx, alloc, newPageRefs, newBookmarks)
			if outlineRef.ObjectNum > 0 {
				catalogDict["Outlines"] = outlineRef
			}
		}
	}

	catalogRef := alloc.Alloc(catalogDict)

	// Build trailer.
	trailer := ast.PdfDict{
		"Root": catalogRef,
	}

	// Copy /Info if present.
	if infoObj, ok := doc.Index.Trailer["Info"]; ok {
		if infoRef, ok := infoObj.(ast.PdfIndirectRef); ok {
			newInfoRef, err := copier.CopyRef(ctx, infoRef)
			if err == nil {
				trailer["Info"] = newInfoRef
			} else {
				doc.Logger.Debug("copying /Info for split failed",
					slog.Any("err", err),
				)
			}
		}
	}

	if version == "" {
		version = doc.Version
	}

	return writer.WriteRewrite(ctx, dst, writer.RewriteConfig{
		Version:       version,
		Allocator:     alloc,
		Trailer:       trailer,
		UseXRefStream: xrefStream,
	})
}

// copyCatalogEntries copies select catalog-level entries that should survive
// a split operation (ViewerPreferences, PageLayout, PageMode, Lang, MarkInfo).
func copyCatalogEntries(ctx context.Context, doc *document.Document, copier *writer.Copier, dst ast.PdfDict) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return
	}
	catalogDict, err := document.ResolveDict(ctx, doc, catalogRef)
	if err != nil {
		doc.Logger.Debug("resolving catalog for entry copy failed",
			slog.Any("err", err),
		)
		return
	}

	keepKeys := []ast.PdfName{
		"ViewerPreferences", "PageLayout", "PageMode", "Lang", "MarkInfo",
	}
	for _, key := range keepKeys {
		if val, ok := catalogDict[key]; ok {
			copied, err := copier.CopyObject(ctx, val)
			if err != nil {
				doc.Logger.Debug("copying catalog entry failed",
					slog.String("key", string(key)),
					slog.Any("err", err),
				)
				continue
			}
			dst[key] = copied
		}
	}
}
