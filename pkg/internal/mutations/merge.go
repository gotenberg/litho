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

// MergeSource represents one input document for a merge operation.
type MergeSource struct {
	Doc   *document.Document
	Pages []*document.Page
}

// Merge combines multiple PDF documents into a single output written to dst.
// Pages are concatenated in order. bookmarkCfg controls how bookmarks are
// produced for the output.
func Merge(ctx context.Context, sources []MergeSource, dst io.Writer, version string, xrefStream bool, bookmarkCfg BookmarkConfig) error {
	if len(sources) == 0 {
		return fmt.Errorf("no sources to merge")
	}

	alloc := writer.NewObjAllocator(0)
	var allPageRefs []ast.PdfObject
	var firstInfoRef ast.PdfIndirectRef
	var hasInfo bool
	cumulativeOffsets := make([]int, len(sources))
	formSources := make([]acroFormSource, 0, len(sources))

	// The copier that carried the first source's pages, reused for everything
	// else taken from that source. A copier memoises what it has already
	// copied, so reusing it is what keeps a catalog entry and a page pointing
	// at the same output object (see the /OCProperties note below).
	var firstCopier *writer.Copier

	for srcIdx, src := range sources {
		cumulativeOffsets[srcIdx] = len(allPageRefs)
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("merge canceled: %w", err)
		}

		// Track the highest version across sources.
		if version == "" || src.Doc.Version > version {
			version = src.Doc.Version
		}

		copier := writer.NewCopier(src.Doc, alloc)
		if srcIdx == 0 {
			firstCopier = copier
		}
		srcCatalogRef, srcHasCatalog := CatalogRef(ctx, src.Doc)
		if srcCatalog, err := document.ResolveDict(ctx, src.Doc, srcCatalogRef); srcHasCatalog && err == nil && srcCatalog != nil {
			formSources = append(formSources, acroFormSource{Doc: src.Doc, Catalog: srcCatalog, Copier: copier})
		}

		for pageNum, page := range src.Pages {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("merge canceled: %w", err)
			}

			newRef, err := copyPageForOutput(ctx, copier, alloc, page)
			if err != nil {
				return fmt.Errorf("copying page %d from source %d: %w", pageNum+1, srcIdx, err)
			}
			allPageRefs = append(allPageRefs, newRef)
		}

		// Copy /Info from the first source that has one.
		if !hasInfo {
			if infoObj, ok := src.Doc.Index.Trailer["Info"]; ok {
				if infoRef, ok := infoObj.(ast.PdfIndirectRef); ok {
					ref, err := copier.CopyRef(ctx, infoRef)
					if err == nil {
						firstInfoRef = ref
						hasInfo = true
					} else {
						src.Doc.Logger.Debug("copying /Info from source failed",
							slog.Int("source", srcIdx),
							slog.Any("err", err),
						)
					}
				}
			}
		}
	}

	// Build new Pages dict.
	pagesDict := ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray(allPageRefs),
		"Count": ast.PdfInt(len(allPageRefs)),
	}
	pagesRef := alloc.Alloc(pagesDict)

	// Update each page's /Parent.
	for _, ref := range allPageRefs {
		pageRef, ok := ref.(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		if obj, ok := alloc.Pending()[pageRef.ObjectNum]; ok {
			if pageDict, ok := obj.(ast.PdfDict); ok {
				pageDict["Parent"] = pagesRef
			}
		}
	}

	// Build catalog.
	catalogDict := ast.PdfDict{
		"Type":  ast.PdfName("Catalog"),
		"Pages": pagesRef,
	}

	// Copy catalog-level entries from the first source. Best effort: if
	// something fails here we still produce a valid merged document without
	// the viewer preferences or language tags from the first source.
	//
	// Through the copier that already carried that source's pages. A fresh one
	// starts with an empty memo table, so it copied /OCProperties a second
	// time and the output held two disjoint sets of optional content groups:
	// the ones the pages reference through their /Properties, and the ones the
	// configuration lists in /OCGs and /OFF. ISO 32000-1, 8.11.2.3 decides
	// visibility by matching the group a marked-content sequence names against
	// that configuration, so every hidden layer became visible, and 8.11.4.3
	// requires /OCGs to contain all the document's groups, which it no longer
	// did. Merging pdfjs issue9462.pdf revealed two switched-off layers, and
	// litho's own reader cannot see the difference because it extracts hidden
	// optional content deliberately.
	if firstCopier != nil {
		copyCatalogEntries(ctx, sources[0].Doc, firstCopier, catalogDict)
	}

	// Re-attach the interactive form, naming every field that kept a widget
	// across all the sources.
	rebuildAcroForm(ctx, alloc, allPageRefs, formSources, catalogDict)
	rebuildEmbeddedFiles(ctx, alloc, formSources, catalogDict)

	// Handle bookmarks for the merged output.
	if bookmarkCfg.Handling != BookmarkNone {
		newBookmarks := resolveMergeBookmarks(ctx, sources, cumulativeOffsets, bookmarkCfg)
		if len(newBookmarks) > 0 {
			newPageRefs := make(map[int]ast.PdfIndirectRef, len(allPageRefs))
			for i, ref := range allPageRefs {
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
	if hasInfo {
		trailer["Info"] = firstInfoRef
	}

	// Carry the first source's trailer /ID, which ISO 19005-1, 6.1.3 requires
	// and a merge of conformant files used to drop.
	if firstCopier != nil {
		if idArr, ok := sources[0].Doc.Index.Trailer["ID"].(ast.PdfArray); ok {
			if copiedID, err := firstCopier.CopyObject(ctx, idArr); err == nil {
				trailer["ID"] = copiedID
			} else {
				sources[0].Doc.Logger.Debug("copying trailer /ID for merge failed", slog.Any("err", err))
			}
		}
	}

	if version == "" {
		version = "1.4"
	}

	return writer.WriteRewrite(ctx, dst, writer.RewriteConfig{
		Version:       version,
		Allocator:     alloc,
		Trailer:       trailer,
		UseXRefStream: xrefStream,
	})
}
