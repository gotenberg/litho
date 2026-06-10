package remediation

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
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
var registry = []fixer{
	// Structural: catalog, trailer, metadata.
	{name: "trailer-id", safety: safetyStructural, matches: matchTrailerID, apply: fixTrailerID},
	{name: "info-date", safety: safetyStructural, matches: matchInfoDate, apply: fixInfoDate},
	{name: "xmp-metadata", safety: safetyStructural, matches: matchXMP, apply: fixXMP},
	{name: "xmp-schema", safety: safetyStructural, matches: matchXMPSchema, apply: fixXMPSchema},
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
	{name: "table-sections", safety: safetyStructural, matches: matchTableSections, apply: fixTableSections},
	{name: "table-cells", safety: safetyStructural, matches: matchTableCells, apply: fixTableCells},
	{name: "list-body", safety: safetyStructural, matches: matchListBody, apply: fixListBody},
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
	{name: "devicen", safety: safetyResource, matches: matchDeviceNColorants, apply: fixDeviceNColorants},
	{name: "sep-canon", safety: safetyResource, matches: matchSepCanon, apply: fixSepCanon},
	{name: "icc-dedup", safety: safetyResource, matches: matchICCDedup, apply: fixICCDedup},
	{name: "file-structure", safety: safetyStructural, matches: matchFileStructure, apply: fixFileStructure},
	{name: "catalog-cleanup", safety: safetyStructural, matches: matchCatalogCleanup, apply: fixCatalogCleanup},
	{name: "annotations", safety: safetyResource, matches: matchAnnotations, apply: fixAnnotations},
	{name: "annot-flags", safety: safetyResource, matches: matchAnnotFlags, apply: fixAnnotFlags},
	{name: "annot-opacity", safety: safetyResource, matches: matchAnnotOpacity, apply: fixAnnotOpacity},
	{name: "annot-appearance", safety: safetyResource, matches: matchAnnotAppearance, apply: fixAnnotAppearance},
	{name: "real-limits", safety: safetyResource, matches: matchRealLimits, apply: fixRealLimits},
	{name: "string-limit", safety: safetyResource, matches: matchStringLimit, apply: fixStringLimit},
	{name: "page-boxes", safety: safetyResource, matches: matchPageBoxes, apply: fixPageBoxes},
	{name: "page-resources", safety: safetyResource, matches: matchPageResources, apply: fixPageResources},
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
	{name: "unknown-ops", safety: safetyContent, matches: matchUnknownOps, apply: fixUnknownOps},
	{name: "text-control", safety: safetyContent, matches: matchTextControl, apply: fixTextControl},
	{name: "fonts", safety: safetyContent, matches: matchFontFixes, apply: fixFontIssues},
	{name: "font-widths", safety: safetyContent, matches: matchFontWidth, apply: fixFontWidth},
	{name: "tt-encoding", safety: safetyContent, matches: matchTrueTypeEncoding, apply: fixTrueTypeEncoding},
	{name: "cid-sysinfo", safety: safetyContent, matches: matchCIDSystemInfo, apply: fixCIDSystemInfo},
	{name: "images", safety: safetyContent, matches: matchImageIssues, apply: fixImageIssues},
	{name: "lzw", safety: safetyContent, matches: matchLZW, apply: fixLZW},
	{name: "aggressive", safety: safetyContent, matches: matchAggressive, apply: fixAggressive},
	{name: "aggressive-limits", safety: safetyContent, matches: matchAggressiveLimits, apply: fixAggressiveLimits},
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
	count := 0
	for objNum, entry := range doc.Index.Entries {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		if entry.Type != 1 || objNum == 0 {
			continue
		}
		obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation})
		if err != nil || obj == nil {
			continue
		}
		switch v := obj.(type) {
		case ast.PdfDict:
			if patched, changed := dictFn(v); changed {
				alloc.AllocAt(objNum, patched)
				count++
			}
		case ast.PdfArray:
			if patched, changed := arrayFn(v); changed {
				alloc.AllocAt(objNum, patched)
				count++
			}
		case ast.PdfStream:
			if patched, changed := dictFn(v.Dict); changed {
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
