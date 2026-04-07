package pdf

import (
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// Merge combines multiple PDF documents into a single output written to dst.
// Pages are concatenated in order: all pages from sources[0], then all from
// sources[1], etc. Each source Reader must remain valid until Merge returns.
func Merge(ctx context.Context, sources []*Reader, dst io.Writer, opts ...WriteOption) error {
	if len(sources) == 0 {
		return fmt.Errorf("no sources to merge")
	}

	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}

	alloc := writer.NewObjAllocator(0)
	var allPageRefs []ast.PdfObject
	var firstInfoRef ast.PdfIndirectRef
	var hasInfo bool
	version := cfg.version

	for srcIdx, src := range sources {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("merge canceled: %w", err)
		}

		// Track the highest version across sources.
		if version == "" || src.doc.Version > version {
			version = src.doc.Version
		}

		copier := writer.NewCopier(src.doc, alloc)

		// Copy all pages from this source.
		pages, err := src.resolvePages(ctx)
		if err != nil {
			return fmt.Errorf("resolving pages from source %d: %w", srcIdx, err)
		}

		for pageNum, page := range pages {
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
			if infoObj, ok := src.doc.Index.Trailer["Info"]; ok {
				if infoRef, ok := infoObj.(ast.PdfIndirectRef); ok {
					ref, err := copier.CopyRef(ctx, infoRef)
					if err == nil {
						firstInfoRef = ref
						hasInfo = true
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

	// Copy catalog-level entries from the first source.
	if len(sources) > 0 {
		firstCopier := writer.NewCopier(sources[0].doc, alloc)
		if err := copyCatalogEntries(ctx, sources[0].doc, firstCopier, catalogDict); err != nil {
			// best effort, don't fail the merge
			_ = err
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
		UseXRefStream: cfg.xrefStream,
	})
}

// copyCatalogEntries is defined in split.go and shared by both Split and Merge.
