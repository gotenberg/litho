package mutations

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/crypto"
)

// MergeSource represents one input document for a merge operation.
type MergeSource struct {
	Doc   *document.Document
	Pages []*document.Page
}

// Merge combines multiple PDF documents into a single output written to dst.
// Pages are concatenated in order. bookmarkCfg controls how bookmarks are
// produced for the output.
//
// A non-nil encryptCfg encrypts the output. Passing nil writes it in the
// clear, which is what a merge of encrypted sources produced before the
// parameter existed. Encryption is a property of the output alone: each
// source is decrypted as it resolves, so sources under different passwords
// merge into one file under the single key derived here.
func Merge(ctx context.Context, sources []MergeSource, dst io.Writer, version string, xrefStream bool, compact CompactOptions, bookmarkCfg BookmarkConfig, encryptCfg *crypto.EncryptConfig) error {
	if len(sources) == 0 {
		return fmt.Errorf("no sources to merge")
	}

	alloc := writer.NewObjAllocator(0)
	var allPageRefs []ast.PdfObject
	var firstInfoRef ast.PdfIndirectRef
	var hasInfo bool
	// readVersion is the latest version a reader following the catalog entry
	// (document.initCatalogVersion) gives any source; see
	// [raiseMergedCatalogVersion].
	var readVersion string
	cumulativeOffsets := make([]int, len(sources))
	formSources := make([]acroFormSource, 0, len(sources))

	// The copier that carried each source's pages, reused for everything else
	// taken from that source. A copier memoises what it has already copied, so
	// reusing it is what keeps a catalog entry and a page pointing at the same
	// output object (see [mergeOptionalContent]).
	copiers := make([]*writer.Copier, len(sources))

	// The structure trees of the tagged sources, joined into one (see
	// [structOutput]), each source's, and the pages of the untagged ones.
	var tree *structOutput
	structSources := make([]*structSource, len(sources))
	var untagged []untaggedCopy
	langString, lang, declared := mergedLang(ctx, sources)

	for srcIdx, src := range sources {
		cumulativeOffsets[srcIdx] = len(allPageRefs)
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("merge canceled: %w", err)
		}

		// Track the highest version across sources, compared as numbers:
		// "1.10" is later than "1.5", which a string comparison reverses.
		version = writer.AtLeastVersion(version, sourceVersion(src.Doc))
		readVersion = writer.AtLeastVersion(readVersion, src.Doc.Version)

		copier := writer.NewCopier(src.Doc, alloc)
		// The output carries the structure of every tagged source, so a drawn
		// signature is tagged under the element that enclosed its widget.
		overrides, err := bakeSignedSignatures(ctx, src.Doc, src.Pages, pageCopyOverrides(src.Pages), bakeTagged)
		if err != nil {
			return fmt.Errorf("merging source %d: %w", srcIdx, err)
		}
		structs, overrides, err := newStructSource(ctx, src.Doc, overrides, src.Pages)
		if err != nil {
			return fmt.Errorf("merging source %d: %w", srcIdx, err)
		}
		if len(overrides) > 0 {
			copier.WithOverrides(overrides)
		}
		if structs != nil {
			if tree == nil {
				tree = newStructOutput(alloc, lang)
			}
			tree.expect(structs, copier)
			structSources[srcIdx] = structs
		}
		copiers[srcIdx] = copier
		srcCatalogRef, srcHasCatalog := CatalogRef(ctx, src.Doc)
		if srcCatalog, err := document.ResolveDict(ctx, src.Doc, srcCatalogRef); srcHasCatalog && err == nil && srcCatalog != nil {
			formSources = append(formSources, acroFormSource{Doc: src.Doc, Catalog: srcCatalog, Copier: copier})
		}

		srcPageRefs := make([]ast.PdfIndirectRef, 0, len(src.Pages))
		for pageNum, page := range src.Pages {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("merge canceled: %w", err)
			}

			newRef, err := copyPageForOutput(ctx, copier, alloc, src.Doc, page)
			if err != nil {
				return fmt.Errorf("copying page %d from source %d: %w", pageNum+1, srcIdx, err)
			}
			allPageRefs = append(allPageRefs, newRef)
			srcPageRefs = append(srcPageRefs, newRef)
		}
		if structs != nil {
			if err := tree.add(ctx, structs, srcPageRefs, -1); err != nil {
				return fmt.Errorf("carrying the structure tree of source %d: %w", srcIdx, err)
			}
		} else {
			untagged = append(untagged, untaggedCopy{copier: copier, pages: srcPageRefs})
		}

		// Copy /Info from the first source that has one.
		if !hasInfo {
			firstInfoRef, hasInfo = copyTrailerInfo(ctx, src.Doc, copier, alloc)
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
	// something fails here the merged document is still valid, only without
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
	if declared {
		catalogDict["Lang"] = langString
	}
	raiseMergedCatalogVersion(catalogDict, readVersion)
	mergeOptionalContent(ctx, sources, copiers, catalogDict)
	if labels, ok := mergePageLabels(ctx, alloc, sources, allPageRefs); ok {
		catalogDict["PageLabels"] = labels
	}

	// Re-attach the interactive form, naming every field that kept a widget
	// across all the sources.
	rebuildAcroForm(ctx, alloc, allPageRefs, formSources, catalogDict)
	if err := rebuildEmbeddedFiles(ctx, alloc, formSources, catalogDict); err != nil {
		return err
	}

	// After every copy from the sources, whose content may name structure keys
	// the tree has to carry or, from an untagged source, drop.
	var rootRef ast.PdfIndirectRef
	if tree != nil {
		for _, u := range untagged {
			tree.untag(u.copier, u.pages)
		}
		var err error
		rootRef, err = tree.finish(ctx)
		if err != nil {
			return fmt.Errorf("carrying the structure tree: %w", err)
		}
		catalogDict["StructTreeRoot"] = rootRef
	}

	// Handle bookmarks for the merged output, once its structure tree is
	// whole, so each item can open the structure element it names (see
	// [outlineTargets]).
	if bookmarkCfg.Handling != BookmarkNone {
		newBookmarks := resolveMergeBookmarks(ctx, sources, cumulativeOffsets, bookmarkCfg)
		if len(newBookmarks) > 0 {
			newPageRefs := make(map[int]ast.PdfIndirectRef, len(allPageRefs))
			for i, ref := range allPageRefs {
				if r, ok := ref.(ast.PdfIndirectRef); ok {
					newPageRefs[i+1] = r
				}
			}
			targets := outputOutlineTargets(ctx, alloc, rootRef, structSources)
			outlineRef := BuildOutline(ctx, alloc, newPageRefs, newBookmarks, targets)
			if outlineRef.ObjectNum > 0 {
				catalogDict["Outlines"] = outlineRef
			}
		}
	}

	catalogRef := alloc.Alloc(catalogDict)
	// The copied pages keep what their sources state, which can be short of
	// what ISO 32000-1, Table 30 requires of a page.
	if err := completeRewrittenPageTree(ctx, alloc, catalogRef); err != nil {
		return err
	}

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

	// Last, after every alloc.Alloc above: see [applyEncryption] on why an
	// object allocated after the sweep is written in the clear.
	if encryptCfg != nil {
		if err := applyEncryption(ctx, alloc, trailer, encryptCfg, version); err != nil {
			return fmt.Errorf("encrypting merge output: %w", err)
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

// mergedLang returns the /Lang string the output's catalog holds and the
// natural language it names, and reports false when no source's catalog holds
// one: the /Lang of the first source whose catalog names a language, else the
// empty one of the first source whose catalog says its language is unknown.
//
// The catalog's /Lang is the language of every text of the output that names
// none of its own (ISO 32000-1, 14.9.2.1): the outline titles, annotation
// contents and field descriptions ISO 14289-1, 7.2 asks a language of, and the
// content of an untagged source. Taking the first source's entry alone left a
// merge whose first source names none with no language, while later sources
// named theirs. The first source naming one keeps the entries and metadata a
// merge takes from its first source in their language whenever that source
// names one. A tagged source naming another language states it on its
// elements, and the content of one naming none inherits the catalog's (see
// [structOutput.add]). With no language named, the empty identifier still
// says the language is unknown (ISO 32000-1, 14.9.2), as its source did, where
// no entry says nothing.
//
// # Corpus evidence
//
//   - All corpora: merging each of 1124 tagged files with the next, 126 merges
//     pair a first source naming no language with a second naming one, and 36
//     outputs added one of ISO 14289-1, 7.2 tests 2, 24, 25 and 34 over both
//     their sources. None does now.
//   - verapdf PDF_A-1a__veraPDF_test_suite_6-8-4-t01-pass-c.pdf and -pass-d.pdf,
//     PDF_A-2a__veraPDF_test_suite_6-7-4-t01-pass-c.pdf and -pass-d.pdf: each
//     pair names no language, the second through the empty /Lang, and 2 of
//     those 36 merges kept adding test 34 until that entry was carried.
func mergedLang(ctx context.Context, sources []MergeSource) (ast.PdfString, string, bool) {
	var unknown ast.PdfString
	found := false
	for _, src := range sources {
		if ctx.Err() != nil {
			return "", "", false
		}
		langString, lang, held := documentLang(ctx, src.Doc)
		if lang != "" {
			return langString, lang, true
		}
		if held && !found {
			unknown, found = langString, true
		}
	}
	return unknown, "", found
}

// raiseMergedCatalogVersion raises the catalog /Version of a document that
// takes pages from other documents to floor, the latest version any of them
// reads as, and reports whether it changed the entry. It edits catalog in
// place.
//
// A merge carries the catalog entries of its first source and the pages of
// every source, and a page insertion keeps the target's catalog (see
// [InsertPages]). litho reads the entry in both directions, as pdf.js does
// (see document.initCatalogVersion), so a first source whose catalog said
// /1.4 made the merge with a PDF 2.0 source read as 1.4 under a %PDF-2.0
// header. ISO 32000-1, 7.7.2 (Table 28) lets the entry raise a document's
// version, never lower it, and pages taken from a later source need that
// version.
//
// Only an entry that is present and is a version number is raised. An absent
// one defers to the header, which already names the latest source, and one
// that is no version number is ignored by readers, so it misdescribes nothing.
// The entry never falls below the catalog's own reading, since floor includes
// it or [writer.AtLeastVersion] keeps it.
func raiseMergedCatalogVersion(catalog ast.PdfDict, floor string) bool {
	current, ok := catalog["Version"].(ast.PdfName)
	if !ok || !writer.IsVersion(string(current)) {
		return false
	}
	raised := ast.PdfName(writer.AtLeastVersion(string(current), floor))
	catalog["Version"] = raised
	return raised != current
}

// mergeOptionalContent rebuilds the output's /OCProperties so it describes the
// optional content of every source, not just the first.
//
// ISO 32000-1, 8.11.4.3 requires /OCProperties /OCGs to contain all the
// document's optional content groups, and 8.11.2.3 decides whether a marked
// content sequence is drawn by matching the group it names against the
// configuration. Carrying only the first source's entry left every later
// source's groups absent from /OCGs and from /OFF, so their switched-off layers
// were drawn.
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
//
// The configuration is named, as ISO 14289-1, 7.10 and ISO 19005-2 and -3, 6.9
// ask of every optional content configuration dictionary (and ISO 19005-4,
// 6.10): the /Name of the first source's default configuration that has one,
// and [mergedConfigName] otherwise. It is the only configuration the output
// writes, so no other one shares its name. /Name is a text string presented
// in a user interface (ISO 32000-1, 8.11.4.3, Table 101).
//
// # Corpus evidence
//
//   - pdfjs issue9462.pdf: merging it with itself showed the hidden layers of
//     every page after the first.
//   - All corpora: merging each of 1124 tagged files with the next, 43 outputs
//     added ISO 14289-1, 7.10 test 1 and ISO 19005-2, 6.9 test 1 over both
//     their sources, and none does now.
func mergeOptionalContent(ctx context.Context, sources []MergeSource, copiers []*writer.Copier, dst ast.PdfDict) {
	var ocgs, off ast.PdfArray
	var name ast.PdfString

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
		if name == "" {
			name = optionalContentConfigName(ctx, src.Doc, ocp["D"])
		}

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

	if name == "" {
		name = document.EncodePdfString(mergedConfigName)
	}
	config := ast.PdfDict{"Name": name, "Order": ocgs}
	if len(off) > 0 {
		config["OFF"] = off
	}
	dst["OCProperties"] = ast.PdfDict{
		"OCGs": ocgs,
		"D":    config,
	}
}

// mergedConfigName names the default optional content configuration litho
// writes for a merge none of whose sources names its own, and for the target
// of a page insertion that had none, whose source names none either.
const mergedConfigName = "Default"

// optionalContentConfigName returns the /Name of an optional content
// configuration dictionary, or "" when it names none or an empty one (ISO
// 32000-1, 8.11.4.3, Table 101).
func optionalContentConfigName(ctx context.Context, doc *document.Document, config ast.PdfObject) ast.PdfString {
	dict, err := document.ResolveDict(ctx, doc, config)
	if err != nil || dict == nil {
		return ""
	}
	name, err := document.ResolveString(ctx, doc, dict["Name"])
	if err != nil || strings.TrimSpace(document.DecodePdfString(name)) == "" {
		return ""
	}
	return name
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
