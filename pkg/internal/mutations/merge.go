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

	// The copier that carried each source's pages, reused for everything else
	// taken from that source. A copier memoises what it has already copied, so
	// reusing it is what keeps a catalog entry and a page pointing at the same
	// output object (see [mergeOptionalContent]).
	copiers := make([]*writer.Copier, len(sources))

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
		if overrides := pageCopyOverrides(src.Pages); len(overrides) > 0 {
			copier.WithOverrides(overrides)
		}
		copiers[srcIdx] = copier
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
	// time and the output held two disjoint sets of optional content groups
	// (see [mergeOptionalContent], which then replaces the entry with one
	// covering every source).
	if copiers[0] != nil {
		copyCatalogEntries(ctx, sources[0].Doc, copiers[0], catalogDict)
	}
	mergeOptionalContent(ctx, sources, copiers, catalogDict)

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
	if copiers[0] != nil {
		if idArr, ok := sources[0].Doc.Index.Trailer["ID"].(ast.PdfArray); ok {
			if copiedID, err := copiers[0].CopyObject(ctx, idArr); err == nil {
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

// mergeOptionalContent rebuilds the output's /OCProperties so it describes the
// optional content of every source, not just the first.
//
// ISO 32000-1, 8.11.4.3 requires /OCProperties /OCGs to contain all the
// document's optional content groups, and 8.11.2.3 decides whether a marked
// content sequence is drawn by matching the group it names against the
// configuration. Carrying only the first source's entry left every later
// source's groups absent from /OCGs and from /OFF, so their switched-off layers
// were drawn: merging pdfjs issue9462.pdf with itself showed the hidden layers
// of every page after the first.
//
// Each source's groups are copied through the copier that already carried that
// source's pages, so the configuration and the pages name the same objects.
//
// Visibility is normalised rather than copied. A source using /BaseState /OFF
// hides everything it does not list in /ON (Table 101), which cannot be
// expressed once several sources with different base states share one
// configuration, so each source's hidden groups are resolved to an explicit
// /OFF list under the default /ON base state. /Order is rebuilt as a flat list:
// it drives presentation only, and a nested order naming groups from one source
// says nothing about the others. Usage application dictionaries (/AS) are
// dropped rather than carried, since a stale one would name groups by reference.
func mergeOptionalContent(ctx context.Context, sources []MergeSource, copiers []*writer.Copier, dst ast.PdfDict) {
	var ocgs, off ast.PdfArray

	for i, src := range sources {
		if err := ctx.Err(); err != nil {
			return
		}
		copier := copiers[i]
		if copier == nil {
			continue
		}
		catalogRef, ok := CatalogRef(ctx, src.Doc)
		if !ok {
			continue
		}
		catalog, err := CatalogDict(ctx, src.Doc, catalogRef)
		if err != nil {
			continue
		}
		ocp, err := document.ResolveDict(ctx, src.Doc, catalog["OCProperties"])
		if err != nil || ocp == nil {
			continue
		}
		groups, err := document.ResolveArray(ctx, src.Doc, ocp["OCGs"])
		if err != nil {
			continue
		}
		hidden := hiddenOptionalContent(ctx, src.Doc, ocp, groups)

		for _, group := range groups {
			copied, err := copier.CopyObject(ctx, group)
			if err != nil {
				src.Doc.Logger.Debug("copying an optional content group for merge failed",
					slog.Any("err", err),
				)
				continue
			}
			ocgs = append(ocgs, copied)
			if ref, ok := group.(ast.PdfIndirectRef); ok && hidden[ref.ObjectNum] {
				off = append(off, copied)
			}
		}
	}

	if len(ocgs) == 0 {
		// No source declared optional content, so whatever copyCatalogEntries
		// carried (nothing, or an entry with no groups) stands.
		return
	}

	config := ast.PdfDict{"Order": ocgs}
	if len(off) > 0 {
		config["OFF"] = off
	}
	dst["OCProperties"] = ast.PdfDict{
		"OCGs": ocgs,
		"D":    config,
	}
}

// hiddenOptionalContent returns the object numbers of the groups a source's
// default configuration does not draw.
//
// Per ISO 32000-1, Table 101 the base state applies to every group not named in
// /ON or /OFF: the default /ON means only /OFF is hidden, while /BaseState /OFF
// inverts that, hiding everything absent from /ON.
func hiddenOptionalContent(ctx context.Context, doc *document.Document, ocp ast.PdfDict, groups ast.PdfArray) map[int]bool {
	hidden := make(map[int]bool)
	config, err := document.ResolveDict(ctx, doc, ocp["D"])
	if err != nil || config == nil {
		return hidden
	}

	if base, ok := config["BaseState"].(ast.PdfName); ok && base == "OFF" {
		on := optionalContentRefs(ctx, doc, config["ON"])
		for _, group := range groups {
			if ref, ok := group.(ast.PdfIndirectRef); ok && !on[ref.ObjectNum] {
				hidden[ref.ObjectNum] = true
			}
		}
		return hidden
	}

	return optionalContentRefs(ctx, doc, config["OFF"])
}

// optionalContentRefs collects the object numbers an /ON or /OFF array names.
func optionalContentRefs(ctx context.Context, doc *document.Document, obj ast.PdfObject) map[int]bool {
	out := make(map[int]bool)
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		return out
	}
	for _, entry := range arr {
		if ref, ok := entry.(ast.PdfIndirectRef); ok {
			out[ref.ObjectNum] = true
		}
	}
	return out
}
