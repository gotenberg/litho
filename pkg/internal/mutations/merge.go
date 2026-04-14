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

		for pageNum, page := range src.Pages {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("merge canceled: %w", err)
			}

			newRef, err := copier.CopyRef(ctx, page.Ref)
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
	if len(sources) > 0 {
		firstCopier := writer.NewCopier(sources[0].Doc, alloc)
		copyCatalogEntries(ctx, sources[0].Doc, firstCopier, catalogDict)
	}

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
