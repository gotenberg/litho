package compliance

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// langTagAlphaNum and langTagAlphaOnly encode the two language-tag grammars the
// profiles require: RFC 3066 / BCP 47 (alphanumeric subtags) for PDF/A-2/3/4 and
// PDF/UA, and the stricter alpha-only subtags ISO 19005-1 §6.8.4 t1 demands for
// PDF/A-1.
//
//nolint:gochecknoglobals // compiled-once regex tables.
var (
	langTagAlphaNum  = regexp.MustCompile(`^[a-zA-Z]{1,8}(-[a-zA-Z0-9]{1,8})*$`)
	langTagAlphaOnly = regexp.MustCompile(`^[a-zA-Z]{1,8}(-[a-zA-Z]{1,8})*$`)
)

// spanLangSubjects pairs each /Span marked-content property whose text needs a
// determinable natural language with the test that reports it. Mirrors the three
// ISO 14289-1 §7.2 SEMarkedContent rules, which differ only in the property key.
//
//nolint:gochecknoglobals // static spec table (ISO 14289-1 §7.2 t30/t31/t32).
var spanLangSubjects = []struct {
	key  ast.PdfName
	rule Rule
}{
	{"ActualText", Rule_14289_1_7_2_t30},
	{"Alt", Rule_14289_1_7_2_t31},
	{"E", Rule_14289_1_7_2_t32},
}

// contentOpCancelCheckInterval is how many content-stream operators a scan
// processes between cancellation checks. Checking every operator would dominate
// the scan, and 200 operators is short enough to abandon a crafted stream
// promptly. Anti-DoS guard, not spec-mandated.
const contentOpCancelCheckInterval = 200

// checkMarkedContentLang validates the language tag of any /Lang set on a
// marked-content sequence (BDC/DP) in a page content stream. A /Lang on a
// marked-content property list is a language tag like any other, so it must
// match the RFC 3066 / BCP 47 grammar; an ill-formed value fails UA-1 §7.2 t29
// or UA-2 §8.4.4 t2. The dict-based catalog and structure-tree checks operate on
// object dictionaries and never reach content-operator language tags, so this
// content-stream pass is required to cover them. An empty tag is "not present"
// and passes [IsValidLangTag].
func checkMarkedContentLang(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !IsPDFUA(profile) {
		return nil
	}
	rule := langValidRule(profile)

	var results []Violation
	forEachMarkedContentLang(ctx, doc, catalog, func(pageIdx int, lang ast.PdfString) bool {
		// A /Lang present on a marked-content sequence must be a valid
		// Language-Tag; for PDF/UA an empty value is invalid (UA-1 §7.2 t29
		// / UA-2 §8.4.4 t2), unlike PDF/A where an empty text string is
		// permitted. This pass runs only under PDF/UA (guarded above).
		tag := strings.TrimSpace(document.DecodePdfString(lang))
		if tag == "" || !IsValidUALangTag(lang) {
			results = append(results, failf(rule,
				"page %d: marked-content /Lang %q is not a valid language tag", pageIdx+1, tag)...)
		}
		return false
	})
	return results
}

// forEachMarkedContentLang invokes fn for every /Lang entry on a BDC or DP
// marked-content operator across all page content streams. lang is the entry as
// written (empty when it does not resolve to a string). fn returns true to stop
// the scan early. The dict-based catalog and structure-tree checks never
// reach content-operator language tags, so this content-stream pass is the only
// place they are observed.
func forEachMarkedContentLang(ctx context.Context, doc *document.Document, catalog ast.PdfDict, fn func(pageIdx int, lang ast.PdfString) bool) {
	forEachPageContentOps(ctx, doc, catalog, func(pageIdx int, page ast.PdfDict, ops []content.Operation) bool {
		props := markedContentProps{doc: doc, page: page}
		for _, op := range ops {
			if op.Operator != "BDC" && op.Operator != "DP" {
				continue
			}
			pd := props.dict(ctx, op)
			if pd == nil {
				continue
			}
			langObj, ok := pd["Lang"]
			if !ok {
				continue
			}
			// Best-effort: a /Lang that resolves to a non-string is reported by
			// the caller as the empty tag it decodes to, which fails the
			// Language-Tag grammar just as the malformed value does.
			lang, _ := document.ResolveString(ctx, doc, langObj)
			if fn(pageIdx, lang) {
				return true
			}
		}
		return false
	})
}

// forEachPageContentOps parses every page content stream and invokes fn with the
// page index, the page dict and the parsed operations. A page whose content is
// absent or unparsable is skipped. fn returns true to stop the scan early.
func forEachPageContentOps(ctx context.Context, doc *document.Document, catalog ast.PdfDict, fn func(pageIdx int, page ast.PdfDict, ops []content.Operation) bool) {
	for pageIdx, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return
		}
		data, err := resolvePageContentBytes(ctx, doc, page)
		if err != nil || len(data) == 0 {
			continue
		}
		ops, err := content.ParseStreamData(ctx, data)
		if err != nil {
			continue
		}
		if fn(pageIdx, page, ops) {
			return
		}
	}
}

// markedContentProps resolves the property list of a BDC or DP operator on one
// page. A named property list is looked up in the page /Properties resource,
// resolved once and reused: a content stream can carry thousands of
// marked-content operators.
type markedContentProps struct {
	doc      *document.Document
	page     ast.PdfDict
	props    ast.PdfDict
	resolved bool
	// parentTree is the structure tree's ParentTree when the scan judges
	// MCID-owned text through the tree, nil otherwise; owners caches the entry
	// page's /StructParents selects in it.
	parentTree     ast.PdfDict
	owners         ast.PdfArray
	ownersResolved bool
}

// dict returns the property list op carries, or nil when it carries none or the
// list cannot be resolved. veraPDF leaves its properties dict null in the same
// cases (GFOpMarkedContent.initializePropertiesDict), so every property-derived
// test passes for such an operator.
func (m *markedContentProps) dict(ctx context.Context, op content.Operation) ast.PdfDict {
	if len(op.Operands) < 2 {
		return nil
	}
	switch v := op.Operands[1].(type) {
	case ast.PdfDict:
		return v
	case ast.PdfName:
		if !m.resolved {
			if res := resolveResources(ctx, m.doc, m.page); res != nil {
				// Best-effort: a missing/unresolvable /Properties dict leaves
				// props nil, handled by the guard below.
				m.props, _ = document.ResolveDict(ctx, m.doc, res["Properties"])
			}
			m.resolved = true
		}
		if m.props == nil {
			return nil
		}
		// Best-effort: an unresolvable property entry yields nil, which callers
		// treat as "no property list".
		pd, _ := document.ResolveDict(ctx, m.doc, m.props[v])
		return pd
	}
	return nil
}

// checkSpanMarkedContentLang reports ISO 14289-1 §7.2 t30, t31 and t32: text
// carried by the /ActualText, /Alt and /E entries of a /Span marked-content
// sequence must have a determinable natural language.
//
// The three rules share one shape, differing only in the property key:
//
//	tag != 'Span' || <key> == null || Lang != null || inheritedLang != null ||
//	gContainsCatalogLang == true
//
// A language is in scope when the catalog carries a /Lang
// (GFPDDocument.getcontainsLang), when the sequence's own property list carries
// one, or when an enclosing sequence or the structure element owning a
// sequence's marked-content identifier supplies one
// (GFOpMarkedContent.getInheritedLang, GFOp_BDC.getStructParentLang).
//
// Presence decides, never the value: an empty /Lang is a non-null COSString for
// veraPDF and determines the language, exactly as §7.2 t34 already treats it. A
// malformed tag is a present one here; its grammar is reported separately by
// [checkMarkedContentLang] as §7.2 t29.
//
// PDF/UA-2 is out of scope: PDFUA-2-ISO32005.xml declares no SEMarkedContent
// rule and no inheritedLang term, so ISO 32005 has no counterpart to implement.
func checkSpanMarkedContentLang(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	// gContainsCatalogLang: a catalog /Lang that resolves to a string, the empty
	// string included, determines the language for every marked-content sequence.
	if hasStringKeyPresent(ctx, doc, catalog, "Lang") {
		return nil
	}

	var parentTree ast.PdfDict
	if treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"]); err == nil && treeRoot != nil {
		// Best-effort: without a resolvable ParentTree no sequence reaches a
		// structure element, which is veraPDF's null struct-parent language.
		parentTree, _ = document.ResolveDict(ctx, doc, treeRoot["ParentTree"])
	}

	var results []Violation
	forEachPageContentOps(ctx, doc, catalog, func(pageIdx int, page ast.PdfDict, ops []content.Operation) bool {
		results = append(results, spanLangViolations(ctx, doc, parentTree, page, pageIdx, ops)...)
		return ctx.Err() != nil
	})
	return results
}

// spanLangViolations scans one page's operations for /Span marked-content
// sequences whose natural language cannot be determined, following the Form
// XObjects the page draws.
func spanLangViolations(ctx context.Context, doc *document.Document, parentTree, page ast.PdfDict, pageIdx int, ops []content.Operation) []Violation {
	scan := spanLangScan{doc: doc, parentTree: parentTree, pageIdx: pageIdx, seen: make(map[int]bool)}
	return scan.ops(ctx, ops, page, resolveResources(ctx, doc, page), false, 0)
}

// spanLangScan carries what one page's /Span scan shares across the Form
// XObjects it follows: the structure tree's ParentTree, the page number the
// findings name, and the XObjects already scanned, so a cyclic or repeated
// invocation chain is walked once.
type spanLangScan struct {
	doc        *document.Document
	parentTree ast.PdfDict
	pageIdx    int
	seen       map[int]bool
}

// ops scans one content stream. owner is the dictionary the stream belongs to (the
// page, or a Form XObject's stream dictionary), which names its property lists
// and its /StructParents; res resolves the XObjects it draws. covered reports
// whether the sequence enclosing the stream already supplies a language, and
// depth caps the invocation chain at [maxFormXObjectDepth].
func (s *spanLangScan) ops(ctx context.Context, ops []content.Operation, owner, res ast.PdfDict, covered bool, depth int) []Violation {
	if depth > maxFormXObjectDepth {
		return nil
	}
	props := markedContentProps{doc: s.doc, page: owner}
	owners := structElemLang{
		doc:     s.doc,
		entries: pageParentTreeEntries(ctx, s.doc, s.parentTree, owner),
		cache:   make(map[int]bool),
	}

	// open records, per open marked-content sequence, whether it supplies a
	// language; inScope counts the ones that do, so testing the whole enclosing
	// chain stays constant time. Nesting is bounded by len(ops), which the caller
	// already holds.
	var open []bool
	inScope := 0

	var results []Violation
	for i, op := range ops {
		if i%contentOpCancelCheckInterval == 0 && ctx.Err() != nil {
			return results
		}
		switch op.Operator {
		case "BMC":
			// BMC carries no property list, so it supplies neither /Lang nor an
			// /MCID: veraPDF never initializes a properties dict for it.
			open = append(open, false)
		case "BDC":
			pd := props.dict(ctx, op)
			supplies := pd != nil && (hasStringKeyPresent(ctx, s.doc, pd, "Lang") || owners.hasLang(ctx, pd))
			open = append(open, supplies)
			if supplies {
				inScope++
			} else if inScope == 0 && !covered {
				results = append(results, spanPropLangViolations(ctx, s.doc, op, pd, s.pageIdx)...)
			}
		case "EMC":
			if n := len(open); n > 0 {
				if open[n-1] {
					inScope--
				}
				open = open[:n-1]
			}
		case "Do":
			// veraPDF builds the marked content of a Form XObject a page draws
			// as it does the page's own, so pdfjs issue19971.pdf's form reports
			// each /Span with /ActualText. The form paints inside the sequence the
			// Do sits in (ISO 32000-1 §14.6), whose language covers it.
			results = append(results, s.form(ctx, op, res, covered || inScope > 0, depth)...)
		}
	}
	return results
}

// form scans the Form XObject a Do operator draws. A Do naming an absent or
// non-Form XObject, or one that cannot be read, shows no marked content (ISO
// 32000-1 §8.10.1).
func (s *spanLangScan) form(ctx context.Context, op content.Operation, res ast.PdfDict, covered bool, depth int) []Violation {
	if res == nil || len(op.Operands) < 1 {
		return nil
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return nil
	}
	xobjects, err := document.ResolveDict(ctx, s.doc, res["XObject"])
	if err != nil || xobjects == nil {
		return nil
	}
	if ref, isRef := xobjects[name].(ast.PdfIndirectRef); isRef {
		if s.seen[ref.ObjectNum] {
			return nil
		}
		s.seen[ref.ObjectNum] = true
	}
	stream, err := document.ResolveStream(ctx, s.doc, xobjects[name])
	if err != nil || stream.Dict == nil {
		return nil
	}
	if subtype, _ := document.ResolveName(ctx, s.doc, stream.Dict["Subtype"]); subtype != "Form" {
		return nil
	}
	data, err := s.doc.DecodeStream(ctx, stream)
	if err != nil {
		s.doc.Logger.Debug("compliance.accessibility: cannot decode Form XObject stream", "error", err)
		return nil
	}
	formOps, err := content.ParseStreamData(ctx, data)
	if err != nil {
		s.doc.Logger.Debug("compliance.accessibility: cannot parse Form XObject content stream", "error", err)
		return nil
	}
	// A Form XObject without its own /Resources inherits the invoking stream's
	// (ISO 32000-1 §8.10.2), so nested names still resolve.
	subRes, err := document.ResolveDict(ctx, s.doc, stream.Dict["Resources"])
	if err != nil || subRes == nil {
		subRes = res
	}
	return s.ops(ctx, formOps, stream.Dict, subRes, covered, depth+1)
}

// spanPropLangViolations reports §7.2 t30/t31/t32 for a marked-content sequence
// already known to have no language in scope. Only a /Span tag is a subject, and
// only a property that resolves to a string counts as present, matching
// veraPDF's typed getStringAttribute accessors.
func spanPropLangViolations(ctx context.Context, doc *document.Document, op content.Operation, pd ast.PdfDict, pageIdx int) []Violation {
	if pd == nil || len(op.Operands) < 2 {
		return nil
	}
	if tag, ok := op.Operands[0].(ast.PdfName); !ok || tag != "Span" {
		return nil
	}

	var results []Violation
	for _, subject := range spanLangSubjects {
		if hasStringKeyPresent(ctx, doc, pd, subject.key) {
			results = append(results, failf(subject.rule,
				"page %d: /Span marked content /%s", pageIdx+1, subject.key)...)
		}
	}
	return results
}

// structElemLang answers, for one page, whether the structure element owning a
// marked-content identifier reaches a /Lang. entries is that page's ParentTree
// array, indexed by marked-content identifier (ISO 32000-1 §14.7.4.4). cache
// keeps a page whose thousands of sequences share a few identifiers from walking
// the same /P chain over and over.
type structElemLang struct {
	doc     *document.Document
	entries ast.PdfArray
	cache   map[int]bool
}

// hasLang reports whether the structure element owning the /MCID in pd, or one
// of that element's /P ancestors, carries a /Lang. It mirrors
// GFOp_BDC.getStructParentLang: the identifier selects a ParentTree entry, and
// the walk stops at the first ancestor holding a /Lang key whatever its value.
func (s *structElemLang) hasLang(ctx context.Context, pd ast.PdfDict) bool {
	if len(s.entries) == 0 {
		return false
	}
	// An /MCID counts only as an integer, as GFOpMarkedContent.getMCID reads it
	// ([mcidValue]): a null one no longer borrows element 0's /Lang.
	mcid, ok := mcidValue(ctx, s.doc, pd["MCID"])
	if !ok || mcid < 0 || mcid >= len(s.entries) {
		return false
	}
	if found, ok := s.cache[mcid]; ok {
		return found
	}
	elem, err := document.ResolveDict(ctx, s.doc, s.entries[mcid])
	found := err == nil && elem != nil && structElemChainHasLang(ctx, s.doc, elem)
	s.cache[mcid] = found
	return found
}

// structElemChainHasLang reports whether elem or one of its /P ancestors carries
// a /Lang key. Presence decides, not the value, matching veraPDF's
// COSObject.getStringKey walk. seen breaks a crafted /P cycle and
// maxStructTreeWalkDepth bounds a long chain.
func structElemChainHasLang(ctx context.Context, doc *document.Document, elem ast.PdfDict) bool {
	seen := make(map[int]bool)
	for depth := 0; elem != nil && depth <= maxStructTreeWalkDepth; depth++ {
		if hasStringKeyPresent(ctx, doc, elem, "Lang") {
			return true
		}
		parentObj, ok := elem["P"]
		if !ok {
			return false
		}
		if ref, ok := parentObj.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				return false
			}
			seen[ref.ObjectNum] = true
		}
		parent, err := document.ResolveDict(ctx, doc, parentObj)
		if err != nil {
			return false
		}
		elem = parent
	}
	return false
}

// pageParentTreeEntries returns the ParentTree array a page's /StructParents key
// selects, or nil when the page declares none or the lookup fails. The array is
// indexed by marked-content identifier (ISO 32000-1 §14.7.4.4).
func pageParentTreeEntries(ctx context.Context, doc *document.Document, parentTree, page ast.PdfDict) ast.PdfArray {
	if parentTree == nil {
		return nil
	}
	spObj, ok := page["StructParents"]
	if !ok {
		return nil
	}
	key, err := document.ResolveInt(ctx, doc, spObj)
	if err != nil {
		return nil
	}
	return numberTreeLookup(ctx, doc, parentTree, int(key), 0, make(map[int]bool))
}

// checkLangRequired ensures the catalog has a /Lang key with a valid RFC 3066 tag.
// Rule mapping:
//   - Missing /Lang or empty /Lang in catalog -> langRequiredRule (t34 for PDF/UA-1, 8.4.4 t01 for PDF/UA-2)
//   - Present but invalid /Lang format        -> langValidRule (t29 for PDF/UA-1, 8.4.4 t02 for PDF/UA-2)
func checkLangRequired(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	langRule := langRequiredRule(profile)
	validRule := langValidRule(profile)

	catalogLang := resolveNonEmptyLang(ctx, doc, catalog)

	if catalogLang == "" {
		// A /Lang entry that is present but empty is an invalid Language-Tag,
		// not an absent one. The empty string fails the Language-Tag grammar, so
		// it is the lang-valid rule (UA-1 §7.2 t29 / UA-2 §8.4.4 t2) that fires,
		// not the undeterminable-language path. Only a genuinely absent /Lang
		// takes the determinability path below.
		if langPresentButEmpty(ctx, doc, catalog) {
			return failf(validRule, "catalog /Lang is empty")
		}

		// PDF/UA-2 §8.4.4 requires catalog /Lang explicitly (t1); the grammar of
		// any /Lang on a structure element is still validated (empty/malformed
		// fires §8.4.4 t2), since the catalog-present struct-walk below is
		// skipped on this early return.
		if profile == "PDF/UA-2" {
			return append(fail(langRule, "catalog"), validateStructureLangs(ctx, doc, catalog, validRule)...)
		}

		// PDF/UA-1 §7.2 t34: natural language must be determinable. If catalog
		// /Lang is absent, the language can still be determined through the
		// structure tree. The tree root or individual elements may carry /Lang.
		results := checkStructureTreeLangCoverage(ctx, doc, catalog, langRule, validRule)
		results = append(results, checkAnnotFormLang(ctx, doc, catalog)...)

		// §7.2 t33: the document metadata's natural language must be
		// determinable. With no catalog /Lang, each language alternative in the
		// metadata must name its language: one whose single item is qualified
		// only "x-default" leaves it undetermined.
		//
		// The rule's subject is a language alternative (veraPDF's XMPLangAlt,
		// test xDefault == false || gContainsCatalogLang == true), so a packet
		// carrying no alternative at all presents nothing to qualify and passes.
		// Reporting on a bare packet flagged every metadata stream without a
		// dc:title or dc:description.
		//
		// A packet that is not well-formed XML presents no alternative either:
		// XML 1.0 §2.1 gives it no infoset, so veraPDF builds no XMPLangAlt
		// from it. Reading one out of a malformed packet condemned a document
		// on a qualifier only litho's lenient scan could see.
		if documentMetadataLangUndetermined(ctx, doc, catalog) {
			results = append(results, fail(Rule_14289_1_7_2_t33, "document metadata")...)
		}
		return results
	}

	if !langEntryValid(ctx, doc, catalog) {
		return failf(validRule, "lang=%s", catalogLang)
	}

	// Catalog provides global /Lang. Validate /Lang format in structure elements.
	return validateStructureLangs(ctx, doc, catalog, validRule)
}

// documentMetadataLangUndetermined reports whether any metadata stream the
// document carries leaves its natural language undetermined for ISO 14289-1,
// 7.2 t33. The caller has established that the catalog carries no /Lang.
//
// ISO 32000-1, 14.3.2 lets any component carry a /Metadata stream, and veraPDF
// judges each one, not the catalog's alone: it models the /Metadata of every
// dictionary in the cross-reference table whose /Type is not /Catalog
// (GFCosDict.getMetadata over GFCosDocument's indirectObjects), which
// [xmpCollectMetadataStreams] and [xmpPacketBytes] already mirror. pdfjs
// bug867484.pdf gives a structure element a packet whose dc:title and
// dc:description carry only x-default; veraPDF reports it, and litho, which
// read the catalog's packet only, did not.
func documentMetadataLangUndetermined(ctx context.Context, doc *document.Document, catalog ast.PdfDict) bool {
	if data, ok := readCatalogXMP(ctx, doc, catalog); ok && xmpLangUndetermined(data) {
		return true
	}
	for _, objNum := range xmpCollectMetadataStreams(ctx, doc, catalog) {
		if data := xmpPacketBytes(ctx, doc, objNum); data != nil && xmpLangUndetermined(data) {
			return true
		}
	}
	return false
}

// xmpLangUndetermined reports whether a metadata packet holds a language
// alternative qualified only by x-default ([xmpLangAltOnlyXDefault]). A packet
// that is not well-formed XML presents no alternative at all (XML 1.0, 2.1), so
// it leaves no language undetermined.
func xmpLangUndetermined(data []byte) bool {
	return xmpWellFormed(data) && xmpLangAltOnlyXDefault(data)
}

// resolveNonEmptyLang resolves a /Lang entry from a dict and returns the trimmed
// string, or "" if absent, unresolvable, or empty/whitespace.
func resolveNonEmptyLang(ctx context.Context, doc *document.Document, dict ast.PdfDict) string {
	langObj, ok := dict["Lang"]
	if !ok {
		return ""
	}
	langStr, err := document.ResolveString(ctx, doc, langObj)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(document.DecodePdfString(langStr))
}

// langEntryValid reports whether dict's /Lang passes the PDF/UA Language-Tag
// test as written ([IsValidUALangTag]). An absent or unreadable entry passes:
// the callers judge presence and emptiness themselves.
func langEntryValid(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	obj, ok := dict["Lang"]
	if !ok {
		return true
	}
	s, err := document.ResolveString(ctx, doc, obj)
	if err != nil {
		return true
	}
	return IsValidUALangTag(s)
}

// langPresentButEmpty reports whether dict carries a /Lang key that resolves to
// an empty (or whitespace-only) string. Such a value is an invalid Language-Tag,
// not an absent one: the key exists but its value fails the tag grammar.
func langPresentButEmpty(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	obj, ok := dict["Lang"]
	if !ok {
		return false
	}
	langStr, err := document.ResolveString(ctx, doc, obj)
	if err != nil {
		return false
	}
	return strings.TrimSpace(document.DecodePdfString(langStr)) == ""
}

// checkStructureTreeLangCoverage verifies that all structure elements can
// determine their natural language through /Lang inheritance in the structure
// tree, even when catalog /Lang is absent. An element inherits /Lang from its
// nearest ancestor that carries a non-empty /Lang value.
// checkAnnotFormLang reports ISO 14289-1 §7.2 t24/t25: the natural language for
// text in an annotation's /Contents entry (t24) and in a form field's /TU key
// (t25) must be determinable. It is called only when the catalog carries no
// /Lang, so an object supplies its own determinable language only through its
// own /Lang entry (verapdf-corpus 7.2-t24/t25-pass-a rely on catalog /Lang; the
// fail files have neither catalog nor object /Lang, and their enclosing
// structure element carries no /Lang either).
func checkAnnotFormLang(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []Violation {
	var results []Violation
	treeRoot, _ := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])

	for _, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return results
		}
		annots, err := document.ResolveArray(ctx, doc, page["Annots"])
		if err != nil {
			continue
		}
		for _, annotObj := range annots {
			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}
			// veraPDF's test is Contents == null || ..., and its getStringKey
			// returns null only for an absent or non-string entry, so an empty
			// /Contents still needs a determinable language.
			contentsObj, ok := annot["Contents"]
			if !ok {
				continue
			}
			if _, err := document.ResolveString(ctx, doc, contentsObj); err != nil {
				continue
			}
			if !objLangDeterminable(ctx, doc, treeRoot, annot) {
				results = append(results, fail(Rule_14289_1_7_2_t24, "annotation /Contents")...)
			}
		}
	}

	acroForm, err := document.ResolveDict(ctx, doc, catalog["AcroForm"])
	if err == nil && acroForm != nil {
		if fields, err := document.ResolveArray(ctx, doc, acroForm["Fields"]); err == nil {
			seen := make(map[int]bool)
			checkFieldTULang(ctx, doc, treeRoot, fields, seen, 0, &results)
		}
	}

	return results
}

// objLangDeterminable reports whether an annotation or form field supplies a
// determinable natural language: its own /Lang, or the /Lang of the structure
// element it is nested in (via /StructParent) or one of that element's
// ancestors. The catalog-/Lang case is handled by the caller (this runs only
// when the catalog carries no /Lang).
func objLangDeterminable(ctx context.Context, doc *document.Document, treeRoot, obj ast.PdfDict) bool {
	if resolveNonEmptyLang(ctx, doc, obj) != "" {
		return true
	}
	return structElementLang(ctx, doc, treeRoot, obj) != ""
}

// structElementLang returns the /Lang of the structure element that obj's
// /StructParent references directly, or "" if absent. veraPDF determines an
// annotation's or form field's language from that element's own /Lang only, not
// from an inherited ancestor value: verapdf-corpus 7.2-t25-fail-a has a /Form
// element with no /Lang whose parent carries /Lang, and it still fails, while
// 7.2-t24/t25-pass-b carry /Lang directly on the element and pass.
func structElementLang(ctx context.Context, doc *document.Document, treeRoot, obj ast.PdfDict) string {
	if treeRoot == nil {
		return ""
	}
	spObj, ok := obj["StructParent"]
	if !ok {
		return ""
	}
	key, err := document.ResolveInt(ctx, doc, spObj)
	if err != nil {
		return ""
	}
	ptDict, err := document.ResolveDict(ctx, doc, treeRoot["ParentTree"])
	if err != nil || ptDict == nil {
		return ""
	}
	elem, err := document.ResolveDict(ctx, doc, numberTreeLookupObj(ctx, doc, ptDict, int(key), 0, make(map[int]bool)))
	if err != nil || elem == nil {
		return ""
	}
	return resolveNonEmptyLang(ctx, doc, elem)
}

// checkFieldTULang walks the form-field tree, reporting §7.2 t25 for a field
// whose /TU entry is present but whose language is undeterminable. seen and
// depth bound a shared or cyclic /Kids tree.
func checkFieldTULang(ctx context.Context, doc *document.Document, treeRoot ast.PdfDict, fields ast.PdfArray, seen map[int]bool, depth int, results *[]Violation) {
	if ctx.Err() != nil || depth > maxFieldTreeDepth {
		return
	}
	for _, fieldObj := range fields {
		if ref, ok := fieldObj.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
		}
		field, err := document.ResolveDict(ctx, doc, fieldObj)
		if err != nil || field == nil {
			continue
		}
		// veraPDF's test is TU == null || ..., and its getStringKey returns null
		// only for an absent or non-string entry, so an empty /TU still needs a
		// determinable language.
		if tuObj, ok := field["TU"]; ok {
			if _, err := document.ResolveString(ctx, doc, tuObj); err == nil && !objLangDeterminable(ctx, doc, treeRoot, field) {
				*results = append(*results, fail(Rule_14289_1_7_2_t25, "form field /TU")...)
			}
		}
		if kids, err := document.ResolveArray(ctx, doc, field["Kids"]); err == nil && len(kids) > 0 {
			checkFieldTULang(ctx, doc, treeRoot, kids, seen, depth+1, results)
		}
	}
}

func checkStructureTreeLangCoverage(ctx context.Context, doc *document.Document, catalog ast.PdfDict, langRule, validRule Rule) []Violation {
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		// No structure tree, so no element can carry a /Lang. ISO 14289-1 §7.2
		// t34 still needs a subject: its object is SETextItem, which exists only
		// where a content stream shows text. A document that paints no text
		// presents nothing whose language could be undetermined, and veraPDF
		// reports nothing on it. The absent tree is itself a violation, but of
		// §7.1 t11, reported by the structure-tree check.
		//
		// With no tree to walk, the marked content around each text-showing
		// operator is the only source of a language, so the scan reads it per
		// item rather than taking the document-wide answer the tree walk below
		// has to settle for.
		if documentShowsUndeterminedText(ctx, doc, catalog, false) {
			return fail(langRule, "catalog")
		}
		return nil
	}

	// Structure tree root /Lang provides fallback for all elements.
	rootLang := resolveNonEmptyLang(ctx, doc, treeRoot)
	if rootLang != "" && !langEntryValid(ctx, doc, treeRoot) {
		return failf(validRule, "lang=%s, StructTreeRoot", rootLang)
	}

	// A /Lang set inline via a BDC marked-content operator also makes the
	// language determinable (veraPDF reads these tags for §7.2 t34), even though
	// the structure-tree walk below cannot see them. The answer is per element,
	// resolved through the marked-content sequence that owns each element's
	// text: a tag on one sequence leaves the text of every other sequence
	// undetermined.
	textElems, inlineLangElems := structElemsShowingText(ctx, doc, catalog, treeRoot)

	var results []Violation
	walkStructureElementsWithLang(ctx, doc, treeRoot, hasStringKeyPresent(ctx, doc, treeRoot, "Lang"),
		func(elem ast.PdfDict, path string, objNum int, ancestorLang bool) {
			// A present-but-empty /Lang on a structure element is an invalid
			// Language-Tag for PDF/UA (UA-1 §7.2 t29 / UA-2 §8.4.4 t2), not an
			// absent one. Report it and stop, so the element is not also counted
			// toward the undeterminable-language path below.
			if langPresentButEmpty(ctx, doc, elem) {
				results = append(results, Violation{Rule: validRule, Context: fmt.Sprintf("empty /Lang, %s", path)})
				return
			}
			elemLang := resolveNonEmptyLang(ctx, doc, elem)
			if elemLang != "" && !langEntryValid(ctx, doc, elem) {
				// Validate format of this element's own /Lang. A malformed tag is
				// still a present one, so it determines the language for veraPDF
				// (Lang != null) and only the format rule fires.
				results = append(results, Violation{Rule: validRule, Context: fmt.Sprintf("lang=%s, %s", elemLang, path)})
				return
			}
			if ancestorLang || elemLang != "" {
				return
			}

			// Text carried by the /ActualText, /Alt and /E attributes draws its
			// natural language only from the structure tree (this element's /Lang or
			// an ancestor's), never from a content-stream marked-content tag, so its
			// determinability is decided here without parsing content. When no such
			// /Lang covers the element (catalog /Lang is already known absent on this
			// path), veraPDF reports the source-specific test (ISO 14289-1 §7.2
			// t21/t22/t23) regardless of whether a sibling carries /Lang.
			if _, ok := elem["ActualText"]; ok {
				results = append(results, Violation{Rule: Rule_14289_1_7_2_t21, Context: path})
			}
			if _, ok := elem["Alt"]; ok {
				results = append(results, Violation{Rule: Rule_14289_1_7_2_t22, Context: path})
			}
			if _, ok := elem["E"]; ok {
				results = append(results, Violation{Rule: Rule_14289_1_7_2_t23, Context: path})
			}

			// Determinability is per element, never per document: veraPDF resolves
			// an SETextItem's language from the /Lang of the structure element that
			// owns its marked content or of one of that element's /P ancestors, so a
			// /Lang on an unrelated sibling leaves this element undetermined.
			if textElems[objNum] && !inlineLangElems[objNum] {
				results = append(results, Violation{Rule: langRule, Context: path})
			}
		})

	// Text that no structure element owns has no element /Lang to inherit, so
	// with no catalog /Lang only a marked-content /Lang around it determines its
	// language, and the walk above never sees it. veraPDF resolves every
	// SETextItem that way, artifact text included: pdfjs issue16176.pdf shows a
	// Tj outside every marked-content sequence and bug1755507.pdf three inside a
	// /Part with no /MCID, and veraPDF reports both under §7.2 t34. Text in an
	// MCID-bearing sequence belongs to its owning element, which the walk above
	// already judges, so the scan treats that sequence as covered.
	if documentShowsUndeterminedText(ctx, doc, catalog, true) {
		results = append(results, fail(langRule, "text outside the structure tree")...)
	}

	return results
}

// structElemsShowingText returns the object numbers of the structure elements
// that own text shown by a page content stream.
//
// ISO 14289-1 §7.2 t34 has SETextItem as its object, so only an element that
// owns a text-showing operator can carry one. Ownership follows the forward link
// veraPDF uses: the page /StructParents key selects a ParentTree entry whose
// array is indexed by marked-content identifier (ISO 32000-1 §14.7.4.4,
// GFOp_BDC.getStructElem). An element that owns only images, line art or
// nothing at all is not a subject: in verapdf-corpus 7.2-t22-fail-a the /Figure
// owning MCID 6 carries no /Lang, yet veraPDF reports no t34 because that
// sequence draws an image XObject rather than text.
//
// An element reached only through a direct (non-indirect) /K entry has no object
// number and cannot be named here, so it is never a subject. Under-reporting is
// the safe direction: veraPDF's own link runs through the ParentTree, which can
// only name indirect elements.
func structElemsShowingText(ctx context.Context, doc *document.Document, catalog, treeRoot ast.PdfDict) (elems, inlineLang map[int]bool) {
	elems, inlineLang = make(map[int]bool), make(map[int]bool)
	ptDict, err := document.ResolveDict(ctx, doc, treeRoot["ParentTree"])
	if err != nil || ptDict == nil {
		return elems, inlineLang
	}

	forEachPageContentOps(ctx, doc, catalog, func(_ int, page ast.PdfDict, ops []content.Operation) bool {
		arr := pageParentTreeEntries(ctx, doc, ptDict, page)
		for mcid, covered := range mcidsShowingText(ctx, ops) {
			if mcid >= len(arr) {
				continue
			}
			if ref, ok := arr[mcid].(ast.PdfIndirectRef); ok {
				elems[ref.ObjectNum] = true
				if covered {
					inlineLang[ref.ObjectNum] = true
				}
			}
		}
		return ctx.Err() != nil
	})
	return elems, inlineLang
}

// mcidsShowingText returns the marked-content identifiers whose sequence
// contains a text-showing operator. A text-showing operator belongs to the
// innermost enclosing sequence that carries an /MCID, matching how veraPDF
// resolves a text item's owner through the enclosing marked content
// (GFOpMarkedContent.getInheritedLang walks the same chain outwards).
//
// Only an inline property-list dict is read, never a named entry in the page
// /Properties: an unresolved name leaves the element out of the subject set,
// which under-reports rather than over-reports.
func mcidsShowingText(ctx context.Context, ops []content.Operation) map[int]bool {
	found := make(map[int]bool)
	// One entry per open marked-content sequence: its /MCID (or -1) and whether
	// it carries a /Lang. Bounded by len(ops), which the caller already holds.
	type openSeq struct {
		mcid    int
		hasLang bool
	}
	var open []openSeq
	for i, op := range ops {
		if i%contentOpCancelCheckInterval == 0 && ctx.Err() != nil {
			return found
		}
		switch op.Operator {
		case "BMC", "BDC":
			open = append(open, openSeq{mcid: markedContentID(op), hasLang: markedContentHasLang(op)})
		case "EMC":
			if len(open) > 0 {
				open = open[:len(open)-1]
			}
		case "Tj", "TJ", "'", "\"":
			// veraPDF resolves a text item's language by walking the enclosing
			// sequences outwards (GFOp_BDC.getInheritedLang), so a /Lang on any
			// of them covers this text, not only one on the sequence that owns
			// the /MCID.
			covered := false
			for _, o := range slices.Backward(open) {
				if o.hasLang {
					covered = true
					break
				}
			}
			for _, o := range slices.Backward(open) {
				if o.mcid >= 0 {
					found[o.mcid] = found[o.mcid] || covered
					break
				}
			}
		}
	}
	return found
}

// documentShowsUndeterminedText reports whether any page paints text that no
// enclosing marked-content /Lang covers, following a Do into the Form XObjects
// the page invokes.
//
// ISO 14289-1 §7.2 t34 takes SETextItem as its object and passes it on
// Lang != null, so the rule has a subject only where text is shown and nothing
// around it names a language. veraPDF resolves that language by walking outwards
// from the text-showing operator through the marked content enclosing it
// (GFOpMarkedContent.getInheritedLang), which is the walk this scan mirrors.
//
// A page whose content litho cannot decode or parse counts as painting such
// text. litho cannot prove that page shows none, and exempting it would drop a
// violation veraPDF reports on the very files whose streams are damaged.
//
// mcidCovers treats a marked-content sequence carrying an /MCID as covered, for
// callers that judge MCID-owned text through the structure tree instead.
func documentShowsUndeterminedText(ctx context.Context, doc *document.Document, catalog ast.PdfDict, mcidCovers bool) bool {
	var parentTree ast.PdfDict
	if mcidCovers {
		// Best-effort: a missing or unresolvable tree or ParentTree leaves it nil,
		// and then no MCID owns its text ([markedContentHasMCID]).
		treeRoot, _ := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
		if treeRoot != nil {
			parentTree, _ = document.ResolveDict(ctx, doc, treeRoot["ParentTree"])
		}
	}
	for _, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return false
		}
		data, err := resolvePageContentBytes(ctx, doc, page)
		if err != nil {
			doc.Logger.Debug("compliance.accessibility: cannot read page content", "error", err)
			return true
		}
		if len(data) == 0 {
			continue
		}
		ops, err := content.ParseStreamData(ctx, data)
		if err != nil {
			doc.Logger.Debug("compliance.accessibility: cannot parse page content", "error", err)
			return true
		}
		props := &markedContentProps{doc: doc, page: page, parentTree: parentTree}
		// The visited set is per page: a Form XObject with no /Resources of its
		// own resolves its nested names against the invoking page's, so the same
		// XObject can reach different content from two pages.
		res := resolveInheritedResources(ctx, doc, page)
		if opsShowUndeterminedText(ctx, doc, ops, props, res, false, mcidCovers, make(map[int]bool), 0) {
			return true
		}
	}
	return false
}

// opsShowUndeterminedText reports whether ops paint text no /Lang covers,
// directly or through a Form XObject they invoke. covered carries the enclosing
// scope's answer into a nested stream, seen skips an XObject already scanned,
// and depth caps the invocation chain at [maxFormXObjectDepth], so a cyclic or
// deeply nested chain cannot loop.
func opsShowUndeterminedText(ctx context.Context, doc *document.Document, ops []content.Operation, props *markedContentProps, res ast.PdfDict, covered, mcidCovers bool, seen map[int]bool, depth int) bool {
	if depth > maxFormXObjectDepth {
		return false
	}
	// One entry per open marked-content sequence, true once the sequence or one
	// enclosing it carries a /Lang. The bottom entry is the caller's scope.
	stack := []bool{covered}
	for i, op := range ops {
		if i%contentOpCancelCheckInterval == 0 && ctx.Err() != nil {
			return false
		}
		cur := stack[len(stack)-1]
		switch op.Operator {
		case "BMC", "BDC":
			stack = append(stack, cur || markedContentLangPresent(ctx, props, op) ||
				(mcidCovers && markedContentHasMCID(ctx, props, op)))
		case "EMC":
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case "Tj", "TJ", "'", "\"":
			if !cur {
				return true
			}
		case "Do":
			// The XObject paints inside the sequence the Do sits in (ISO 32000-1
			// §14.6), so that sequence's /Lang covers its text too.
			if formShowsUndeterminedText(ctx, doc, op, res, cur, mcidCovers, props.parentTree, seen, depth) {
				return true
			}
		}
	}
	return false
}

// markedContentLangPresent reports whether the property list of a marked-content
// operator carries a /Lang key. Presence is what counts, not the value: veraPDF
// takes the language as determined from the first enclosing sequence holding the
// key even when the tag is empty or malformed, and judges that value under UA-1
// §7.2 t29 instead (see [checkMarkedContentLang]).
// markedContentHasMCID reports whether a BDC operator's property list carries an
// integer /MCID ([mcidValue]) that ties its content to a structure element: the
// ParentTree entry the page's (or Form XObject's) /StructParents selects maps
// the identifier to an element (ISO 32000-1, 14.7.4.4). veraPDF finds a text
// item's owner that way alone, so an MCID the ParentTree does not map, or any
// MCID of a tree with no ParentTree, owns nothing, and its text draws no /Lang
// from the element that lists it in /K. Measured against veraPDF.
func markedContentHasMCID(ctx context.Context, props *markedContentProps, op content.Operation) bool {
	pd := props.dict(ctx, op)
	if pd == nil {
		return false
	}
	mcid, ok := mcidValue(ctx, props.doc, pd["MCID"])
	if !ok || mcid < 0 {
		return false
	}
	if !props.ownersResolved {
		props.owners = pageParentTreeEntries(ctx, props.doc, props.parentTree, props.page)
		props.ownersResolved = true
	}
	if mcid >= len(props.owners) {
		return false
	}
	_, isRef := props.owners[mcid].(ast.PdfIndirectRef)
	return isRef
}

func markedContentLangPresent(ctx context.Context, props *markedContentProps, op content.Operation) bool {
	pd := props.dict(ctx, op)
	if pd == nil {
		return false
	}
	_, ok := pd["Lang"]
	return ok
}

// formShowsUndeterminedText reports whether the Form XObject a Do operator
// invokes paints text no /Lang covers. A Do naming an absent or non-Form XObject
// paints no text: per ISO 32000-1 §8.10.1 an unresolvable name makes the
// operator a no-op.
func formShowsUndeterminedText(ctx context.Context, doc *document.Document, op content.Operation, res ast.PdfDict, covered, mcidCovers bool, parentTree ast.PdfDict, seen map[int]bool, depth int) bool {
	if res == nil || len(op.Operands) < 1 {
		return false
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return false
	}
	xobjects, err := document.ResolveDict(ctx, doc, res["XObject"])
	if err != nil || xobjects == nil {
		return false
	}
	if ref, isRef := xobjects[name].(ast.PdfIndirectRef); isRef {
		if seen[ref.ObjectNum] {
			return false
		}
		seen[ref.ObjectNum] = true
	}
	stream, err := document.ResolveStream(ctx, doc, xobjects[name])
	if err != nil {
		doc.Logger.Debug("compliance.accessibility: cannot resolve XObject stream", "error", err)
		return false
	}
	if stream.Dict == nil {
		return false
	}
	// An image XObject paints no text, and an unreadable /Subtype names no
	// content this scan can follow, so both leave the operator out of the answer.
	if subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"]); subtype != "Form" {
		return false
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance.accessibility: cannot decode Form XObject stream", "error", err)
		return true
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		doc.Logger.Debug("compliance.accessibility: cannot parse Form XObject content stream", "error", err)
		return true
	}
	// A Form XObject without its own /Resources inherits the invoking stream's
	// (ISO 32000-1 §8.10.2), so nested names still resolve.
	subRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
	if err != nil || subRes == nil {
		subRes = res
	}
	return opsShowUndeterminedText(ctx, doc, ops, &markedContentProps{doc: doc, page: stream.Dict, parentTree: parentTree}, subRes, covered, mcidCovers, seen, depth+1)
}

// markedContentID returns the /MCID an inline BDC property list carries, or -1
// when the operator has no inline property list or no /MCID in it.
func markedContentID(op content.Operation) int {
	if op.Operator != "BDC" || len(op.Operands) < 2 {
		return -1
	}
	props, ok := op.Operands[1].(ast.PdfDict)
	if !ok {
		return -1
	}
	mcid, ok := props["MCID"].(ast.PdfInt)
	if !ok || mcid < 0 {
		return -1
	}
	return int(mcid)
}

// markedContentHasLang reports whether a BDC operator's inline property list
// carries a /Lang entry. Presence is what counts, as for a structure element:
// veraPDF treats an empty or malformed tag as determining the language, and
// reports its format separately. Only an inline dict is read, never a named
// entry in the page /Properties, matching [markedContentID].
func markedContentHasLang(op content.Operation) bool {
	if op.Operator != "BDC" || len(op.Operands) < 2 {
		return false
	}
	props, ok := op.Operands[1].(ast.PdfDict)
	if !ok {
		return false
	}
	_, has := props["Lang"]
	return has
}

// walkStructureElementsWithLang is like walkStructureElements but tracks /Lang
// inheritance through the tree. The callback receives whether an ancestor
// carries a /Lang entry, the element's own entry excluded. Presence is what
// counts, not the value: veraPDF walks the /P chain to the first element with a
// /Lang key and treats the language as determined from there, even when the tag
// is empty or malformed (GFPDStructElem.getparentLang, GFOp_BDC
// .getStructParentLang).
func walkStructureElementsWithLang(ctx context.Context, doc *document.Document, root ast.PdfDict, rootLang bool, fn structLangVisitor) {
	kidsObj, ok := root["K"]
	if !ok {
		return
	}
	walkStructKidsWithLang(ctx, doc, kidsObj, "root", make(map[int]bool), 0, rootLang, fn)
}

// structLangVisitor receives each structure element of a /Lang-tracking walk.
// objNum is the element's object number, or 0 when a direct /K entry embeds it
// inline and no ParentTree entry can name it.
type structLangVisitor func(elem ast.PdfDict, path string, objNum int, ancestorLang bool)

// walkStructKidsWithLang is the /Lang-tracking twin of [walkStructKids]; seen
// gives it the same one-visit-per-node guard against a fan-out denial of service.
func walkStructKidsWithLang(ctx context.Context, doc *document.Document, obj ast.PdfObject, path string, seen map[int]bool, depth int, ancestorLang bool, fn structLangVisitor) {
	if ctx.Err() != nil || depth > maxStructTreeWalkDepth {
		return
	}

	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		objNum := 0
		if ref, ok := obj.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				return
			}
			seen[ref.ObjectNum] = true
			objNum = ref.ObjectNum
		}
		dict, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || dict == nil {
			return
		}
		visitStructElemWithLang(ctx, doc, dict, path, seen, depth, objNum, ancestorLang, fn)
		return
	}

	for i, kid := range arr {
		if ctx.Err() != nil {
			return
		}
		objNum := 0
		if ref, ok := kid.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
			objNum = ref.ObjectNum
		}
		kidDict, err := document.ResolveDict(ctx, doc, kid)
		if err != nil || kidDict == nil {
			continue
		}
		childPath := fmt.Sprintf("%s[%d]", path, i)
		visitStructElemWithLang(ctx, doc, kidDict, childPath, seen, depth, objNum, ancestorLang, fn)
	}
}

func visitStructElemWithLang(ctx context.Context, doc *document.Document, elem ast.PdfDict, path string, seen map[int]bool, depth, objNum int, ancestorLang bool, fn structLangVisitor) {
	// A structure element with no /S still carries its /Lang to its kids, and
	// its kids still show text veraPDF judges ([isStructElemDict]).
	if !isStructElemDict(ctx, doc, elem) {
		return
	}
	typeName, _ := document.ResolveName(ctx, doc, elem["S"])
	elemPath := fmt.Sprintf("%s/%s", path, string(typeName))

	fn(elem, elemPath, objNum, ancestorLang)

	childLang := ancestorLang || hasStringKeyPresent(ctx, doc, elem, "Lang")
	if kidsObj, ok := elem["K"]; ok {
		walkStructKidsWithLang(ctx, doc, kidsObj, elemPath, seen, depth+1, childLang, fn)
	}
}

// checkOutlineLang verifies that outline entries (bookmarks) have a determinable
// natural language. Per ISO 14289-1:2014 §7.2 t2, an outline item's /Title text
// needs a determinable language, and the document catalog /Lang is its only
// source: ISO 32000-1:2008 Table 153 (entries in an outline item dictionary)
// defines no /Lang entry, and ISO 32000-1:2008 §14.9.2 attaches language to the
// catalog, to structure elements, and to marked content, none of which covers an
// outline title. So the catalog /Lang decides the whole outline tree.
func checkOutlineLang(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	outlinesObj, ok := catalog["Outlines"]
	if !ok {
		return nil
	}
	outlines, err := document.ResolveDict(ctx, doc, outlinesObj)
	if err != nil || outlines == nil {
		return nil
	}

	// If the catalog declares /Lang at all, outline entries inherit it and
	// their language is determinable. A present-but-empty /Lang is an invalid
	// Language-Tag (reported once as §7.2 t29 in [checkLangRequired]), not an
	// undeterminable-language defect, so it must not also fire t2 per outline.
	if _, hasLang := catalog["Lang"]; hasLang {
		return nil
	}

	// The catalog declares no /Lang, so no outline item's language is
	// determinable: report one violation per item, as veraPDF does per PDOutline.
	var results []Violation
	walkOutlineItemsForLang(ctx, doc, outlines["First"], &results, 0, make(map[int]bool))
	return results
}

// walkOutlineItemsForLang reports every outline item as undeterminable-language,
// walking siblings iteratively via /Next and recursing into children via /First.
// The caller established that the catalog declares no /Lang.
// visited tracks resolved item object numbers across the whole walk: a crafted
// /Next or /First cycle would otherwise recurse forever (a 700-byte file
// exhausts the goroutine stack, which is not recoverable in Go), and a long flat
// /Next chain must not recurse one frame per sibling.
func walkOutlineItemsForLang(ctx context.Context, doc *document.Document, itemObj ast.PdfObject, results *[]Violation, depth int, visited map[int]bool) {
	if depth > maxStructTreeWalkDepth {
		return
	}
	for itemObj != nil {
		if ctx.Err() != nil {
			return
		}
		if ref, ok := itemObj.(ast.PdfIndirectRef); ok {
			if visited[ref.ObjectNum] {
				return
			}
			visited[ref.ObjectNum] = true
		}

		// Every dictionary-based object reached through /First and /Next is an
		// item, whatever keys it carries. ISO 32000-1, Table 153 requires /Title,
		// but veraPDF models an item without one (OutlinesHelper walks
		// PDOutlineDictionary.getOutlineItem, which asks only isDictionaryBased),
		// and a non-dictionary value ends the chain. pdfjs issue10880.pdf points
		// /Outlines /First at a page object: veraPDF reports it and litho, which
		// stopped at the missing /Title, did not. Measured against veraPDF on an
		// untitled item, on a page in that place, and on an untitled item followed
		// by a titled one.
		resolved, err := document.ResolveObject(ctx, doc, itemObj)
		if err != nil {
			return
		}
		var item ast.PdfDict
		switch v := resolved.(type) {
		case ast.PdfDict:
			item = v
		case ast.PdfStream:
			item = v.Dict
		}
		if item == nil {
			return
		}

		// A /Lang on the item itself grants no exemption: ISO 32000-1:2008
		// Table 153 defines no /Lang entry on an outline item dictionary, so a
		// consumer has no defined way to read one. ISO 14289-1:2014 §7.2 t2 is
		// therefore unsatisfiable here once the catalog omits /Lang.
		*results = append(*results, fail(Rule_14289_1_7_2_t02, "outline item")...)

		// Recurse into children; siblings continue the loop.
		walkOutlineItemsForLang(ctx, doc, item["First"], results, depth+1, visited)
		itemObj = item["Next"]
	}
}

// validateStructureLangs walks the structure tree and validates any /Lang entries.
func validateStructureLangs(ctx context.Context, doc *document.Document, catalog ast.PdfDict, rule Rule) []Violation {
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}

	var results []Violation
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		langObj, ok := elem["Lang"]
		if !ok {
			return
		}
		langStr, err := document.ResolveString(ctx, doc, langObj)
		if err != nil {
			doc.Logger.Debug("compliance.accessibility: cannot resolve /Lang in structure element",
				"path", path, "error", err)
			return
		}
		trimmed := strings.TrimSpace(document.DecodePdfString(langStr))
		if trimmed == "" {
			// A present-but-empty /Lang is an invalid Language-Tag for PDF/UA
			// (this walk runs only under PDF/UA via checkAccessibilityGroup).
			// UA-1 §7.2 t29 / UA-2 §8.4.4 t2.
			results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("empty /Lang, %s", path)})
			return
		}
		if !IsValidUALangTag(langStr) {
			results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("lang=%s, %s", trimmed, path)})
		}
	})

	return results
}

// IsValidLangTag reports whether tag is a well-formed language identifier for
// PDF/A-2/3/4 and PDF/UA (ISO 19005-2/3 §6.7.4 t1, ISO 14289-1 §7.2 t29, ISO
// 14289-2 §8.4.4 t2): a 1-8 letter primary subtag followed by any number of 1-8
// alphanumeric subtags. An empty or whitespace-only tag is "not present" and
// passes. PDF/A-1's stricter §6.8.4 t1 grammar (no digits in subtags) is
// [isValidLangTagPDFA1].
//
// The grammar is RFC 3066 as referenced by ISO 32000-1:2008, 14.9.2, NOT the
// tighter RFC 5646: subtags may be 1-8 alphanumerics (e.g. "en-12" is valid),
// and structural well-formedness is checked, not the IANA subtag registry.
func IsValidLangTag(tag string) bool {
	trimmed := strings.TrimSpace(tag)
	return trimmed == "" || langTagAlphaNum.MatchString(trimmed)
}

// IsValidUALangTag reports whether a /Lang text string passes the PDF/UA
// Language-Tag test (ISO 14289-1, 7.2 t29; ISO 14289-2, 8.4.4 t2) as veraPDF
// runs it: the grammar of [IsValidLangTag] against the value as written, with
// nothing trimmed and no byte dropped. "EN-US\x00" (pdfjs issue12706.pdf) and
// "EN-US " fail it and veraPDF reports both, while [IsValidLangTag], which
// trims and decodes for display, lets them through. An empty value fails too,
// so a caller that treats it apart checks it first.
func IsValidUALangTag(s ast.PdfString) bool {
	return !document.ContainsUndefinedPDFDocBytes(s) && langTagAlphaNum.MatchString(document.DecodePdfString(s))
}

// isValidLangTagPDFA1 applies the PDF/A-1 grammar of ISO 19005-1 §6.8.4 t1,
// whose subtags are letters only (no digits), unlike the alphanumeric subtags
// every later profile allows.
func isValidLangTagPDFA1(tag string) bool {
	trimmed := strings.TrimSpace(tag)
	return trimmed == "" || langTagAlphaOnly.MatchString(trimmed)
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
