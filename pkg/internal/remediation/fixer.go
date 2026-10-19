package remediation

import (
	"context"
	"iter"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// safety tags a fixer with the worst-case impact of its edits. The writer
// uses the tag to decide whether a fixer's output can be rolled back when a
// post-remediation integrity probe detects content loss.
type safety int

const (
	// safetyStructural fixers edit catalog, trailer, and metadata objects.
	// They cannot change what renders on any page.
	safetyStructural safety = iota
	// safetyResource fixers touch resource dictionaries (fonts, colorspaces,
	// images) in ways that preserve visible rendering. Patches here add
	// missing metadata or correct flags; they do not rewrite glyphs or
	// content streams.
	safetyResource
	// safetyContent fixers may alter rendered output. They rewrite content
	// streams, remap glyphs, drop operators, or replace image data. These
	// are the fixers the writer rolls back when a regression is detected.
	safetyContent
)

// String returns a short label for log output.
func (s safety) String() string {
	switch s {
	case safetyStructural:
		return "structural"
	case safetyResource:
		return "resource"
	case safetyContent:
		return "content"
	default:
		return "unknown"
	}
}

// fixer is a targeted repair that handles one or more related violations.
type fixer struct {
	name    string
	safety  safety
	matches func(v compliance.Violation) bool
	apply   func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, violations []compliance.Violation, profile string) ([]string, error)
}

// registry lists all fixers in priority order. Structural fixers run first,
// then resource fixers, then content-modifying fixers last so rollback can
// discard only the content tier when the output regresses.
//
// [remediate] runs every fixer outside the content tier before any in it,
// wherever the registry places them, and takes the rollback snapshot between
// the two. Keep the content tier last here all the same, so the list reads in
// the order it runs.
var registry = []fixer{
	// Structural: catalog, trailer, metadata.
	{name: "trailer-id", safety: safetyStructural, matches: matchTrailerID, apply: fixTrailerID},
	{name: "info-date", safety: safetyStructural, matches: matchInfoDate, apply: fixInfoDate},
	{name: "xmp-metadata", safety: safetyStructural, matches: matchXMP, apply: fixXMP},
	{name: "xmp-schema", safety: safetyStructural, matches: matchXMPSchema, apply: fixXMPSchema},
	// Runs after the XMP fixers so it splices into the packet they queued
	// rather than the stale source one.
	{name: "wtpdf-declaration", safety: safetyStructural, matches: matchWTPDFDeclaration, apply: fixWTPDFDeclaration},
	{name: "output-intent", safety: safetyStructural, matches: matchOutputIntent, apply: fixOutputIntent},
	{name: "corrupt-icc", safety: safetyStructural, matches: matchCorruptICC, apply: fixCorruptICC},
	{name: "output-intent-cleanup", safety: safetyStructural, matches: matchOutputIntentCleanup, apply: fixOutputIntentCleanup},
	// auto-tag synthesizes a real structure tree first; the flat mark-info,
	// struct-tree, rolemap, and content-tag fixers below then see a tagged
	// document and no-op (struct-tree augments /Namespaces for PDF/UA-2),
	// remaining the fallback for a document auto-tagging cannot process.
	{name: "auto-tag", safety: safetyStructural, matches: matchAutoTag, apply: fixAutoTag},
	{name: "mark-info", safety: safetyStructural, matches: matchMarkInfo, apply: fixMarkInfo},
	{name: "suspects", safety: safetyStructural, matches: matchSuspects, apply: fixSuspects},
	{name: "struct-tree", safety: safetyStructural, matches: matchStructTree, apply: fixStructTree},
	{name: "rolemap", safety: safetyStructural, matches: matchRoleMap, apply: fixRoleMap},
	{name: "content-tag", safety: safetyStructural, matches: matchContentTag, apply: fixContentTag},
	// marked-content-close repairs the pages content-tag leaves because they
	// already hold marked content: it runs after auto-tag and content-tag so
	// it closes what their rewrites leave open, not the source's content.
	{name: "marked-content-close", safety: safetyStructural, matches: matchContentTag, apply: fixMarkedContentClose},
	// parent-tree runs after every structure-producing fixer: marked content
	// links to a structure element only through the page's /StructParents key
	// indexing into the structure tree's /ParentTree, so structure they create
	// stays unassociated until that mapping is registered here.
	{name: "parent-tree", safety: safetyStructural, matches: matchParentTree, apply: fixParentTree},
	{name: "single-doc-root", safety: safetyStructural, matches: matchSingleDocRoot, apply: fixSingleDocRoot},
	// empty-figure runs before struct-dest, so no structure destination it
	// writes names a Figure that goes, and before figure-alt, which gives the
	// Figures that stay their alternate text.
	{name: "empty-figure", safety: safetyStructural, matches: matchEmptyFigure, apply: fixEmptyFigure},
	{name: "struct-dest", safety: safetyStructural, matches: matchStructDest, apply: fixStructDest},
	{name: "struct-parent", safety: safetyStructural, matches: matchStructParent, apply: fixStructParent},
	{name: "annot-nesting", safety: safetyStructural, matches: matchAnnotNesting, apply: fixAnnotNesting},
	{name: "note-id", safety: safetyStructural, matches: matchNoteID, apply: fixNoteID},
	{name: "note-namespace", safety: safetyStructural, matches: matchNoteNamespace, apply: fixNoteNamespace},
	{name: "heading-level", safety: safetyStructural, matches: matchHeadingLevel, apply: fixHeadingLevel},
	{name: "table-sections", safety: safetyStructural, matches: matchTableSections, apply: fixTableSections},
	{name: "table-cells", safety: safetyStructural, matches: matchTableCells, apply: fixTableCells},
	{name: "list-body", safety: safetyStructural, matches: matchListBody, apply: fixListBody},
	// list-content moves marked content, not structure elements, so it
	// repoints the /ParentTree slots itself rather than relying on the
	// parent-tree fixer above: that one only registers pages holding no entry
	// yet and skips every page already keyed, which is all of them here.
	{name: "list-content", safety: safetyStructural, matches: matchListContent, apply: fixListContent},
	{name: "figure-alt", safety: safetyStructural, matches: matchFigureAlt, apply: fixFigureAlt},
	{name: "list-numbering", safety: safetyStructural, matches: matchListNumbering, apply: fixListNumbering},
	{name: "viewer-prefs", safety: safetyStructural, matches: matchViewerPrefs, apply: fixViewerPrefs},
	{name: "catalog-lang", safety: safetyStructural, matches: matchCatalogLang, apply: fixCatalogLang},
	{name: "need-appearances", safety: safetyStructural, matches: matchNeedAppearances, apply: fixNeedAppearances},
	// Resource-level: non-content-altering edits to resource dictionaries.
	{name: "graphics", safety: safetyResource, matches: matchGraphics, apply: fixGraphics},
	{name: "transparency", safety: safetyResource, matches: matchTransparency, apply: fixTransparency},
	{name: "group-cs", safety: safetyResource, matches: matchGroupCS, apply: fixGroupCS},
	{name: "name-utf8", safety: safetyResource, matches: matchNameUTF8, apply: fixNameUTF8},
	{name: "cid-gidmap", safety: safetyResource, matches: matchCIDToGIDMap, apply: fixCIDToGIDMap},
	{name: "font-keys", safety: safetyResource, matches: matchFontKeys, apply: fixFontKeys},
	// A /ToUnicode rewrite changes what text a glyph carries, never what
	// renders, and maps every code exactly as before.
	{name: "tounicode-ranges", safety: safetyResource, matches: matchToUnicodeRanges, apply: fixToUnicodeRangeIssues},
	{name: "devicen", safety: safetyResource, matches: matchDeviceNColorants, apply: fixDeviceNColorants},
	{name: "sep-canon", safety: safetyResource, matches: matchSepCanon, apply: fixSepCanon},
	// Runs after corrupt-icc, which rewrites a destination profile an
	// ICCBased space may share, so a profile is re-encoded once.
	{name: "icc-space-v2", safety: safetyResource, matches: matchICCSpaceV2, apply: fixICCSpaceV2},
	{name: "icc-dedup", safety: safetyResource, matches: matchICCDedup, apply: fixICCDedup},
	{name: "file-structure", safety: safetyStructural, matches: matchFileStructure, apply: fixFileStructure},
	{name: "catalog-cleanup", safety: safetyStructural, matches: matchCatalogCleanup, apply: fixCatalogCleanup},
	{name: "annotations", safety: safetyResource, matches: matchAnnotations, apply: fixAnnotations},
	{name: "forbidden-annots", safety: safetyResource, matches: matchForbiddenAnnots, apply: fixForbiddenAnnots},
	{name: "annot-artifact", safety: safetyResource, matches: matchAnnotArtifact, apply: fixAnnotArtifact},
	{name: "annot-flags", safety: safetyResource, matches: matchAnnotFlags, apply: fixAnnotFlags},
	{name: "annot-opacity", safety: safetyResource, matches: matchAnnotOpacity, apply: fixAnnotOpacity},
	// Runs after output-intent and corrupt-icc: its decision reads the
	// destination profile they may install or replace.
	{name: "annot-color", safety: safetyResource, matches: matchAnnotColor, apply: fixAnnotColor},
	{name: "annot-appearance", safety: safetyResource, matches: matchAnnotAppearance, apply: fixAnnotAppearance},
	{name: "real-limits", safety: safetyResource, matches: matchRealLimits, apply: fixRealLimits},
	{name: "string-limit", safety: safetyResource, matches: matchStringLimit, apply: fixStringLimit},
	{name: "page-boxes", safety: safetyResource, matches: matchPageBoxes, apply: fixPageBoxes},
	{name: "page-resources", safety: safetyResource, matches: matchPageResources, apply: fixPageResources},
	{name: "nested-resources", safety: safetyResource, matches: matchNestedResources, apply: fixNestedResources},
	{name: "embedded-subtype", safety: safetyResource, matches: matchEmbeddedSubtype, apply: fixEmbeddedSubtype},
	{name: "filespec-fuf", safety: safetyResource, matches: matchFileSpecFUF, apply: fixFileSpecFUF},
	{name: "page-tabs", safety: safetyResource, matches: matchPageTabs, apply: fixPageTabs},
	{name: "form-tooltip", safety: safetyResource, matches: matchFormTooltip, apply: fixFormTooltip},
	{name: "javascript", safety: safetyResource, matches: matchJavaScript, apply: fixJavaScript},
	{name: "xfa", safety: safetyResource, matches: matchXFA, apply: fixXFA},
	{name: "pressteps", safety: safetyResource, matches: matchPresSteps, apply: fixPresSteps},
	{name: "requirements", safety: safetyResource, matches: matchRequirements, apply: fixRequirements},
	{name: "af-relationship", safety: safetyResource, matches: matchAFRelationship, apply: fixAFRelationship},
	// Content-modifying: glyph, content-stream, or image edits.
	//
	// marked-content-lang claims the /Lang violations catalog-lang does, and
	// hex-string the malformed hex strings file-structure also claims: both
	// rewrite content streams, so they run here with the rest of the tier,
	// where a rollback discards their repairs and no other.
	{name: "marked-content-lang", safety: safetyContent, matches: matchMarkedContentLang, apply: fixMarkedContentLang},
	{name: "hex-string", safety: safetyContent, matches: matchHexString, apply: fixHexString},
	{name: "unknown-ops", safety: safetyContent, matches: matchUnknownOps, apply: fixUnknownOps},
	{name: "content-limits", safety: safetyContent, matches: matchContentLimits, apply: fixContentLimits},
	{name: "text-control", safety: safetyContent, matches: matchTextControl, apply: fixTextControl},
	{name: "fonts", safety: safetyContent, matches: matchFontFixes, apply: fixFontIssues},
	{name: "font-widths", safety: safetyContent, matches: matchFontWidth, apply: fixFontWidth},
	{name: "tt-encoding", safety: safetyContent, matches: matchTrueTypeEncoding, apply: fixTrueTypeEncoding},
	{name: "cid-sysinfo", safety: safetyContent, matches: matchCIDSystemInfo, apply: fixCIDSystemInfo},
	// Inlining a usecmap chain maps every code exactly as the chain did, so
	// the glyph a content byte selects does not change.
	{name: "cmap-flatten", safety: safetyContent, matches: matchCMapFlatten, apply: fixCMapFlatten},
	// font-embed runs after every other font fixer, and reads the fonts
	// through the allocator, so the font it embeds a program in carries
	// their repairs. Embedding changes the glyphs that render, so it sits in
	// the content tier a rollback discards.
	{name: "font-embed", safety: safetyContent, matches: matchFontEmbed, apply: fixFontEmbed},
	{name: "images", safety: safetyContent, matches: matchImageIssues, apply: fixImageIssues},
	{name: "lzw", safety: safetyContent, matches: matchLZW, apply: fixLZW},
	{name: "signature-strip", safety: safetyContent, matches: matchSignatureStrip, apply: fixSignatureStrip},
	{name: "embedded-compliance", safety: safetyContent, matches: matchEmbeddedCompliance, apply: fixEmbeddedCompliance},
	{name: "stream-file-keys", safety: safetyContent, matches: matchStreamFileKeys, apply: fixStreamFileKeys},
	{name: "aggressive", safety: safetyContent, matches: matchAggressive, apply: fixAggressive},
	{name: "aggressive-limits", safety: safetyContent, matches: matchAggressiveLimits, apply: fixAggressiveLimits},
}

// sweptObjects returns the cross-reference entries a repair that sweeps every
// object visits, in ascending object-number order: the ones the document was
// opened with ([document.Document.OpenedXRefEntries]), which are the ones
// validation judges.
//
// An object the file's table leaves out is not one of them, even once the
// raw-byte augmentation has added it to the index. ISO 32000-1, 7.3.10 reads a
// reference to an undefined object as the null object, veraPDF reads the
// file's own table and judges no such object, and validation sweeps the
// objects the table lists for that reason, so a sweep over the index as it
// stands repaired objects for findings no validation reported. The
// augmentation runs at the first lookup that misses the index, for whichever
// caller makes it, so what such a sweep reached also depended on what ran
// before it (CONTRIBUTING "Deterministic output"), and an incremental update
// that rewrote one of those objects listed it in its own cross-reference
// section, putting an object no reader following the file's table could reach
// under the judgement of the next validation. A repair that follows a
// reference to such an object still reaches it, as validation does.
//
// # Corpus evidence
//
//   - All corpora: 464 files define objects their table leaves out, and in
//     458 of them those objects are cross-reference streams alone.
//   - All corpora: over the PDF/A-1b and PDF/A-2b remediations, default and
//     aggressive, of 249 files, among them the 6 whose table leaves out other
//     objects, no output changes.
func sweptObjects(doc *document.Document) iter.Seq2[int, xref.XRefEntry] {
	return doc.OpenedXRefEntries()
}

// walkIndirectObjects iterates every in-use indirect object and offers
// its top-level container (dict, array, or stream dict) to the supplied
// transformers. When a transformer reports a change the result is
// allocated back to the same object number. Streams keep their raw
// body. Returns the number of objects that were rewritten.
func walkIndirectObjects(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	dictFn func(ast.PdfDict) (ast.PdfDict, bool),
	arrayFn func(ast.PdfArray) (ast.PdfArray, bool),
) (int, error) {
	return walkNumberedObjects(ctx, alloc, doc,
		func(_ int, d ast.PdfDict) (ast.PdfDict, bool) { return dictFn(d) },
		func(_ int, a ast.PdfArray) (ast.PdfArray, bool) { return arrayFn(a) })
}

// walkNumberedObjects is [walkIndirectObjects] with the number of the object
// passed to the transformers, for a repair that reads what the parse of that
// object recorded, such as [document.Document.IntegerOverflows].
//
// The walk offers every object the file defines in use, the ones stored in an
// object stream (ISO 32000-1, 7.5.7) included. Their cross-reference entry is
// type 2 rather than a byte offset (Table 18), and the walk took type 1 entries
// alone, so the limit, name and string repairs never reached them while
// validation judges them where they are stored. Writing one back is sound: the
// repair defines the object again under the same number, which an incremental
// update lists in a later cross-reference section that supersedes the copy in
// the object stream (7.5.8), and which a full rewrite copies in place of the
// source object, as it does any other object the fixers replace. Objects in an
// object stream have generation 0 (7.5.7), which is what their entry reports.
//
// # Corpus evidence
//
//   - All corpora: over the PDF/A-1b and PDF/A-2b remediations, default and
//     aggressive, of 200 of the 662 files that store objects in object
//     streams, 8 outputs change and no residual violation moves.
//   - verapdf PDF_UA-1__7-18.3-t01-pass-a.pdf: its PDF/A-1b remediation
//     clamps four out-of-range numbers held in an object stream.
func walkNumberedObjects(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	dictFn func(int, ast.PdfDict) (ast.PdfDict, bool),
	arrayFn func(int, ast.PdfArray) (ast.PdfArray, bool),
) (int, error) {
	count := 0
	for objNum, entry := range sweptObjects(doc) {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		if entry.Free || objNum == 0 {
			continue
		}
		// Read alloc-first: an earlier fixer may have queued a replacement
		// at this object number, and transforming the stale original would
		// clobber that fix on AllocAt.
		obj, pending := alloc.Pending()[objNum]
		if !pending {
			resolved, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation})
			if err != nil || resolved == nil {
				continue
			}
			obj = resolved
		}
		switch v := obj.(type) {
		case ast.PdfDict:
			if patched, changed := dictFn(objNum, v); changed {
				alloc.AllocAt(objNum, patched)
				count++
			}
		case ast.PdfArray:
			if patched, changed := arrayFn(objNum, v); changed {
				alloc.AllocAt(objNum, patched)
				count++
			}
		case ast.PdfStream:
			if patched, changed := dictFn(objNum, v.Dict); changed {
				raw, err := v.Bytes(ctx)
				if err != nil {
					// Reading the raw stream body failed (cancelled
					// context, decryption error, I/O fault). Leaving
					// the original object in place is safer than
					// allocating a new stream with an empty body and
					// silently dropping the payload.
					doc.Logger.Debug("remediation: skipping stream rewrite, body read failed",
						slog.Int("obj", objNum), slog.Any("err", err))
					continue
				}
				alloc.AllocAt(objNum, ast.NewStream(patched, raw))
				count++
			}
		}
	}
	return count, nil
}

// livePageDict returns the page dictionary as the next fixer should see it:
// the replacement queued in alloc when an earlier fixer rewrote the page,
// otherwise the parsed document's dict. Fixers that copy and re-allocate a
// page dict must start from this view, or they resurrect state an earlier
// fixer removed (e.g. forbidden annotations) by writing a stale copy over
// the queued one.
func livePageDict(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) ast.PdfDict {
	if d := resolveFromAllocOrDoc(ctx, alloc, doc, page.Ref); d != nil {
		return d
	}
	return page.Dict
}

// liveArray resolves an array value alloc-first so a replacement queued by an
// earlier fixer (e.g. a filtered /Annots) is seen instead of the stale
// original.
func liveArray(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) ast.PdfArray {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if arr, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfArray); ok {
			return arr
		}
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		return nil
	}
	return arr
}

// liveAnnotation returns the annotation an entry of a page's /Annots names, as the
// next fixer should see it: the replacement an earlier fixer queued at its
// object number, or else the document's copy, and nil when it is no readable
// dictionary.
//
// An annotation the annotations or annot-flags fixer removed is queued as the
// null object. [resolveFromAllocOrDoc] reads past a queued value that is no
// dictionary to the document's copy, so a fixer that wrote its repair of that
// copy back brought the annotation back as an orphan object, and the rules that
// sweep every object judged it again. Every fixer that writes an annotation back
// reads it here, from the /Annots it reads through [liveArray].
//
// # Corpus evidence
//
//   - litho laterreads_2u.pdf: the PDF/A-1b to 3b outputs held a removed
//     Screen annotation again, which annot-opacity or annot-appearance wrote
//     back from the array as the file wrote it.
//   - govdocs thread_400_400575.pdf, pdfcpu
//     pkg_testdata__text_annotations.pdf, pdfjs 160F-2019.pdf,
//     annotation_hidden_noview.pdf and annotation_hidden_print.pdf: their
//     PDF/A outputs change bytes alone, and no rule moves on litho or
//     veraPDF.
func liveAnnotation(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, entry ast.PdfObject) ast.PdfDict {
	if ref, ok := entry.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		if obj, pending := alloc.Pending()[ref.ObjectNum]; pending {
			annot, _ := obj.(ast.PdfDict)
			return annot
		}
	}
	annot, err := document.ResolveDict(ctx, doc, entry)
	if err != nil {
		return nil
	}
	return annot
}

// liveDictValue is the dictionary counterpart of [liveArray]: it resolves a
// value that is either an inline dictionary or a reference to one, reading the
// replacement an earlier fixer queued at that object number before the
// document's copy. A fixer that copies a /Resources or /ColorSpace dictionary
// and writes the copy back must start from this view, or its write discards the
// earlier fixer's edit. Returns nil when the value is not a readable dictionary.
func liveDictValue(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) ast.PdfDict {
	if ref, ok := obj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		return resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	d, err := document.ResolveDict(ctx, doc, obj)
	if err != nil {
		return nil
	}
	return d
}

// resolveStreamFromAllocOrDoc returns the patched stream queued in alloc
// when one exists at ref.ObjectNum, otherwise falls back to the source
// document's resolver. Fixers that chain patches to the /Metadata stream
// use this so later passes see earlier edits.
func resolveStreamFromAllocOrDoc(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef, obj ast.PdfObject) (ast.PdfStream, bool) {
	if ref.ObjectNum > 0 {
		if pending, ok := alloc.Pending()[ref.ObjectNum]; ok {
			if s, ok := pending.(ast.PdfStream); ok {
				return s, true
			}
		}
	}
	s, err := document.ResolveStream(ctx, doc, obj)
	if err != nil {
		return ast.PdfStream{}, false
	}
	return s, true
}

// liveStream returns the stream at ref as the next fixer should see it: the
// replacement an earlier fixer queued at that object number, or the document's
// copy when nothing is queued there. It reports false when the object is not a
// stream, including when the queued replacement is something else, so a caller
// never writes the document's stream back over it. Unlike
// [resolveStreamFromAllocOrDoc], it never falls back to the document past a
// pending write.
func liveStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef) (ast.PdfStream, bool) {
	obj, pending := alloc.Pending()[ref.ObjectNum]
	if !pending {
		resolved, err := doc.Resolve(ctx, ref)
		if err != nil {
			return ast.PdfStream{}, false
		}
		obj = resolved
	}
	s, ok := obj.(ast.PdfStream)
	return s, ok && s.Dict != nil
}

// clausePrefix checks if a violation's rule clause starts with any of the given prefixes.
func clausePrefix(v compliance.Violation, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(v.Rule.Clause, p) {
			return true
		}
	}
	return false
}

// descriptionContains checks if a violation's description contains a substring (case-insensitive).
func descriptionContains(v compliance.Violation, sub string) bool {
	return strings.Contains(strings.ToLower(v.Rule.Description), strings.ToLower(sub))
}

// contextContains checks if a violation's context (the bracketed detail
// emitted by the validator) contains a substring (case-insensitive).
func contextContains(v compliance.Violation, sub string) bool {
	return strings.Contains(strings.ToLower(v.Context), strings.ToLower(sub))
}
