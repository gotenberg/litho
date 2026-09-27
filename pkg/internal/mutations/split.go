package mutations

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/crypto"
)

// Split extracts the specified 1-based pages from a document into a new PDF
// written to dst. Pages are included in the order given; duplicates are allowed.
// bookmarkCfg controls how bookmarks are produced for the output.
//
// A non-nil encryptCfg encrypts the output. Passing nil writes it in the
// clear, which is what a split of an encrypted source produced before the
// parameter existed: the reader decrypts every object as it resolves it, so
// the copied graph is plaintext whatever the source was.
func Split(ctx context.Context, doc *document.Document, allPages []*document.Page, pages []int, dst io.Writer, version string, xrefStream bool, compact CompactOptions, bookmarkCfg BookmarkConfig, encryptCfg *crypto.EncryptConfig) error {
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

	keptIndices := make([]int, len(pages))
	keptPages := make([]*document.Page, len(pages))
	for i, pageNum := range pages {
		keptIndices[i] = pageNum - 1
		keptPages[i] = allPages[pageNum-1]
	}

	overrides := pageCopyOverrides(keptPages)
	if removed := removedPageSet(allPages, keptIndices); len(removed) > 0 {
		splitDestOverrides(ctx, doc, keptPages, removed, overrides)
		if srcCatalog, err := document.ResolveCatalog(ctx, doc); err == nil && srcCatalog != nil {
			dropped := droppedPageAnnots(ctx, nil, doc, allPages, removed)
			// The truncation flag is dropped here on purpose. Split copies
			// outward from the pages it keeps, so an unextracted page is not
			// reachable from the copied root however deep the field tree runs;
			// measured across chain depths well past the bound, split never
			// carried the excluded page. Only delete, which rewrites whatever
			// the catalog reaches, has to refuse.
			fieldOverrides, _ := fieldKidOverrides(ctx, nil, doc, srcCatalog.Dict, dropped)
			maps.Copy(overrides, fieldOverrides)
		}
	}
	// The output carries the structure of the pages it keeps, so a drawn
	// signature is tagged under the element that enclosed its widget.
	overrides, err := bakeSignedSignatures(ctx, doc, keptPages, overrides, bakeTagged)
	if err != nil {
		return err
	}
	structs, overrides, err := newStructSource(ctx, doc, overrides, keptPages)
	if err != nil {
		return err
	}
	if len(overrides) > 0 {
		copier.WithOverrides(overrides)
	}
	var tree *structOutput
	if structs != nil {
		tree = newStructOutput(alloc, structs.lang)
		tree.expect(structs, copier)
	}

	// Copy each selected page, building new page refs.
	pageRefs := make([]ast.PdfObject, 0, len(pages))
	copiedPages := make([]ast.PdfIndirectRef, 0, len(pages))
	for _, pageNum := range pages {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("split canceled: %w", err)
		}

		srcPage := allPages[pageNum-1]
		newRef, err := copyPageForOutput(ctx, copier, alloc, doc, srcPage)
		if err != nil {
			return fmt.Errorf("copying page %d: %w", pageNum, err)
		}
		pageRefs = append(pageRefs, newRef)
		copiedPages = append(copiedPages, newRef)
	}
	if tree != nil {
		if err := tree.add(ctx, structs, copiedPages, -1); err != nil {
			return fmt.Errorf("carrying the structure tree: %w", err)
		}
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
	if labels, ok := splitPageLabels(ctx, alloc, doc, len(allPages), pageRefs, keptIndices); ok {
		catalogDict["PageLabels"] = labels
	}

	// Re-attach the interactive form to whichever fields kept a widget.
	srcCatalogRef, hasSrcCatalog := CatalogRef(ctx, doc)
	if catalogDict2, err := document.ResolveDict(ctx, doc, srcCatalogRef); hasSrcCatalog && err == nil && catalogDict2 != nil {
		srcs := []acroFormSource{{Doc: doc, Catalog: catalogDict2, Copier: copier}}
		rebuildAcroForm(ctx, alloc, pageRefs, srcs, catalogDict)
		if err := rebuildEmbeddedFiles(ctx, alloc, srcs, catalogDict); err != nil {
			return err
		}
	}

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

	// After every copy from the source, whose content may name structure keys
	// the tree has to carry.
	if tree != nil {
		rootRef, err := tree.finish(ctx)
		if err != nil {
			return fmt.Errorf("carrying the structure tree: %w", err)
		}
		catalogDict["StructTreeRoot"] = rootRef
	}

	catalogRef := alloc.Alloc(catalogDict)
	// The copied pages keep what their source states, which can be short of
	// what ISO 32000-1, Table 30 requires of a page.
	if err := completeRewrittenPageTree(ctx, alloc, catalogRef); err != nil {
		return err
	}

	// Build trailer.
	trailer := ast.PdfDict{
		"Root": catalogRef,
	}

	// Carry the trailer /ID. ISO 19005-1, 6.1.3 requires it outright, so a
	// split of a conformant file that dropped it stopped being conformant;
	// copying rather than generating also keeps the output deterministic
	// (ISO 32000-1, 14.4 treats the first element as the document's identity).
	if idArr, ok := doc.Index.Trailer["ID"].(ast.PdfArray); ok {
		if copiedID, err := copier.CopyObject(ctx, idArr); err == nil {
			trailer["ID"] = copiedID
		} else {
			doc.Logger.Debug("copying trailer /ID for split failed", slog.Any("err", err))
		}
	}

	if infoRef, ok := copyTrailerInfo(ctx, doc, copier, alloc); ok {
		trailer["Info"] = infoRef
	}

	// copyCatalogEntries carried the source's catalog /Version, so the header
	// follows the rule a full rewrite does.
	if version == "" {
		version = sourceVersion(doc)
	}

	// Last, after every alloc.Alloc above: see applyEncryption on why an
	// object allocated after the sweep is written in the clear.
	if encryptCfg != nil {
		if err := applyEncryption(ctx, alloc, trailer, encryptCfg, version); err != nil {
			return fmt.Errorf("encrypting split output: %w", err)
		}
	}

	return writer.WriteRewrite(ctx, dst, writer.RewriteConfig{
		Version:           version,
		Allocator:         alloc,
		Trailer:           trailer,
		UseXRefStream:     xrefStream,
		PackObjectStreams: compact.PackObjectStreams,
		DedupStreams:      compact.DedupStreams,
	})
}

// copyCatalogEntries copies select catalog-level entries that should survive
// a split operation (ViewerPreferences, PageLayout, PageMode, Lang, MarkInfo).
//
// # Corpus evidence
//
//   - All corpora: split lost /SpiderInfo on 4 of 4 files carrying it, and
//     merge lost it too, while rotate keeps it because rotate edits in place.
func copyCatalogEntries(ctx context.Context, doc *document.Document, copier *writer.Copier, dst ast.PdfDict) {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return
	}
	catalogDict, err := CatalogDict(ctx, doc, catalogRef)
	if err != nil {
		doc.Logger.Debug("resolving catalog for entry copy failed",
			slog.Any("err", err),
		)
		return
	}

	// Page-INDEPENDENT catalog entries only. Each of these describes the
	// document rather than a particular page, so carrying it into a subset or a
	// concatenation cannot leave it pointing at a page that is no longer there.
	//
	// Dropping them broke conformance outright: a PDF/A-1a file that veraPDF
	// passed with zero failures came out of a split failing 6.7.2 (the document
	// XMP that PDF/A requires) and 6.1.3, because /Metadata and /OutputIntents
	// went with it.
	//
	// /StructTreeRoot and /PageLabels are deliberately NOT here. Both are keyed
	// to pages: a structure tree names the pages its elements live on and a page
	// label tree is indexed by page number, so copying either wholesale would
	// describe pages the output does not have. The caller rebuilds both for the
	// pages it copies instead: the elements of those pages (see newStructSource
	// and structOutput) and their labels (see [splitPageLabels] and
	// [mergePageLabels]).
	//
	// /Collection marks a PDF Portfolio (ISO 32000-1, 12.3.5), whose payload is
	// the embedded file set rebuildEmbeddedFiles already carries. Measured: a
	// split of a portfolio kept its attachment and lost the entry, so the output
	// held the contents of a container while denying it was one, and
	// pdf.Reader.IsPortfolio answered false over them.
	//
	// /PronunciationLexicon is an array of PLS documents describing the whole
	// document's pronunciation (ISO 32000-2, 14.9.6), named by no page.
	// Measured: a one-lexicon document came out of a split and a merge with
	// none.
	//
	// /SpiderInfo records a web capture (ISO 32000-1, 14.10.2), describing how
	// the document was captured rather than any page in it. Split and merge
	// dropped it, while rotate keeps it because rotate edits in place.
	//
	// All three are page-independent by the rule above, and none needs
	// remapping.
	//
	// /DPartRoot, /Requirements and a catalog /AA are deliberately NOT here,
	// for two different reasons. /DPartRoot is page-KEYED like the pair above:
	// document.ResolveDPartMap returns a map from PAGE object number to that
	// page's inherited metadata, so carrying the tree whole would describe
	// pages the output does not have. /Requirements and /AA are page-
	// independent but forbidden by the profiles litho targets, and litho's own
	// remediation deletes both (ISO 19005-2/3, 6.11; part 4, 6.12), so
	// carrying them would re-emit a key this codebase removes elsewhere.
	//
	// /Perms and /DSS are deliberately NOT here either. Each describes the
	// source's signatures: /Perms names the certification and usage rights
	// signatures (ISO 32000-1, 12.8.4, Table 258) and /DSS holds their
	// validation data (ISO 32000-2, 12.8.4.3). A split or a merge keeps none
	// of those signatures (see [bakeSignedSignatures]), as a full rewrite
	// keeps none (see [dropSecurityStore]).
	keepKeys := []ast.PdfName{
		"ViewerPreferences", "PageLayout", "PageMode", "Lang", "MarkInfo",
		"Metadata", "OutputIntents", "OCProperties", "Extensions", "Version",
		"Collection", "PronunciationLexicon", "SpiderInfo",
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
