package compliance

import (
	"context"
	"fmt"
	"regexp"
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
	forEachMarkedContentLang(ctx, doc, catalog, func(pageIdx int, tag string) bool {
		// A /Lang present on a marked-content sequence must be a valid
		// Language-Tag; for PDF/UA an empty value is invalid (UA-1 §7.2 t29
		// / UA-2 §8.4.4 t2), unlike PDF/A where an empty text string is
		// permitted. This pass runs only under PDF/UA (guarded above).
		if tag == "" || !IsValidLangTag(tag) {
			results = append(results, failf(rule,
				"page %d: marked-content /Lang %q is not a valid language tag", pageIdx+1, tag)...)
		}
		return false
	})
	return results
}

// forEachMarkedContentLang invokes fn for every /Lang entry on a BDC or DP
// marked-content operator across all page content streams. tag is the resolved,
// trimmed Language-Tag ("" when the entry is present but empty). fn returns true
// to stop the scan early. The dict-based catalog and structure-tree checks never
// reach content-operator language tags, so this content-stream pass is the only
// place they are observed.
func forEachMarkedContentLang(ctx context.Context, doc *document.Document, catalog ast.PdfDict, fn func(pageIdx int, tag string) bool) {
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
		var props ast.PdfDict // page /Properties, resolved lazily for named property lists
		propsResolved := false
		for _, op := range ops {
			if op.Operator != "BDC" && op.Operator != "DP" || len(op.Operands) < 2 {
				continue
			}
			var pd ast.PdfDict
			switch v := op.Operands[1].(type) {
			case ast.PdfDict:
				pd = v
			case ast.PdfName:
				if !propsResolved {
					if res := resolveResources(ctx, doc, page); res != nil {
						// Best-effort: a missing/unresolvable /Properties dict
						// leaves props nil, handled by the guard below.
						props, _ = document.ResolveDict(ctx, doc, res["Properties"])
					}
					propsResolved = true
				}
				if props != nil {
					// Best-effort: an unresolvable property entry leaves pd
					// nil, handled by the pd == nil check below.
					pd, _ = document.ResolveDict(ctx, doc, props[v])
				}
			}
			if pd == nil {
				continue
			}
			langObj, ok := pd["Lang"]
			if !ok {
				continue
			}
			lang, _ := document.ResolveString(ctx, doc, langObj)
			if fn(pageIdx, strings.TrimSpace(document.DecodePdfString(lang))) {
				return
			}
		}
	}
}

// documentHasMarkedContentLang reports whether any BDC or DP marked-content
// operator carries a /Lang entry. veraPDF derives an SETextItem's language from
// such inline tags, so their presence, even when the tag is empty or malformed
// (which §7.2 t29 reports separately), makes the language determinable for the
// §7.2 t34 coverage rule. The coverage check must honour them or it falsely
// fires t34 on files whose only /Lang is inline.
func documentHasMarkedContentLang(ctx context.Context, doc *document.Document, catalog ast.PdfDict) bool {
	found := false
	forEachMarkedContentLang(ctx, doc, catalog, func(_ int, _ string) bool {
		found = true
		return true
	})
	return found
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
		// determinable. With no catalog /Lang, it is determined by a specific
		// xml:lang in the XMP packet; a packet present but qualified only with
		// "x-default" leaves it undetermined.
		if xmpData, ok := readCatalogXMP(ctx, doc, catalog); ok && !xmpHasSpecificLang(xmpData) {
			results = append(results, fail(Rule_14289_1_7_2_t33, "document metadata")...)
		}
		return results
	}

	if !IsValidLangTag(catalogLang) {
		return failf(validRule, "lang=%s", catalogLang)
	}

	// Catalog provides global /Lang. Validate /Lang format in structure elements.
	return validateStructureLangs(ctx, doc, catalog, validRule)
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
			contents, _ := document.ResolveString(ctx, doc, annot["Contents"])
			if len(contents) == 0 {
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
// whose /TU entry carries text but whose language is undeterminable. seen and
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
		if tu, _ := document.ResolveString(ctx, doc, field["TU"]); len(tu) > 0 {
			if !objLangDeterminable(ctx, doc, treeRoot, field) {
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
		// No structure tree at all. Cannot determine language for anything.
		return fail(langRule, "catalog")
	}

	// Structure tree root /Lang provides fallback for all elements.
	rootLang := resolveNonEmptyLang(ctx, doc, treeRoot)
	if rootLang != "" && !IsValidLangTag(rootLang) {
		return failf(validRule, "lang=%s, StructTreeRoot", rootLang)
	}

	// First pass: check if ANY element in the tree has /Lang. If so, language
	// is at least partially determinable. Elements without structure-level /Lang
	// may still have language set via BDC marked content operators in the content
	// stream, which litho does not currently parse. To avoid false positives on
	// files that use inline BDC /Lang, coverage violations only fire when the
	// entire tree is devoid of /Lang.
	anyLangFound := rootLang != ""
	if !anyLangFound {
		walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, _ string) {
			if anyLangFound {
				return
			}
			// A present /Lang makes the language determinable (veraPDF: Lang !=
			// null), even when empty. An empty value is still reported as invalid
			// by t29, but it must not also count as undeterminable for t34.
			if resolveNonEmptyLang(ctx, doc, elem) != "" || langPresentButEmpty(ctx, doc, elem) {
				anyLangFound = true
			}
		})
	}
	// A /Lang set inline via a BDC/DP marked-content operator also makes the
	// language determinable (veraPDF reads these tags for §7.2 t34), even though
	// the structure-tree walk above cannot see them. Without this, t34 falsely
	// fires on files whose only /Lang is inline.
	if !anyLangFound {
		anyLangFound = documentHasMarkedContentLang(ctx, doc, catalog)
	}

	var results []Violation
	walkStructureElementsWithLang(ctx, doc, treeRoot, rootLang, func(elem ast.PdfDict, path string, inheritedLang string) {
		// A present-but-empty /Lang on a structure element is an invalid
		// Language-Tag for PDF/UA (UA-1 §7.2 t29 / UA-2 §8.4.4 t2), not an
		// absent one. Report it and stop, so the element is not also counted
		// toward the undeterminable-language path below.
		if langPresentButEmpty(ctx, doc, elem) {
			results = append(results, Violation{Rule: validRule, Context: fmt.Sprintf("empty /Lang, %s", path)})
			return
		}
		elemLang := resolveNonEmptyLang(ctx, doc, elem)
		effectiveLang := inheritedLang
		if elemLang != "" {
			// Validate format of this element's own /Lang.
			if !IsValidLangTag(elemLang) {
				results = append(results, Violation{Rule: validRule, Context: fmt.Sprintf("lang=%s, %s", elemLang, path)})
				return
			}
			effectiveLang = elemLang
		}

		// Text carried by the /ActualText, /Alt and /E attributes draws its
		// natural language only from the structure tree (this element's /Lang or
		// an ancestor's), never from a content-stream marked-content tag, so its
		// determinability is decided here without parsing content. When no such
		// /Lang covers the element (catalog /Lang is already known absent on this
		// path), veraPDF reports the source-specific test (ISO 14289-1 §7.2
		// t21/t22/t23) regardless of whether a sibling carries /Lang.
		if effectiveLang == "" {
			if _, ok := elem["ActualText"]; ok {
				results = append(results, Violation{Rule: Rule_14289_1_7_2_t21, Context: path})
			}
			if _, ok := elem["Alt"]; ok {
				results = append(results, Violation{Rule: Rule_14289_1_7_2_t22, Context: path})
			}
			if _, ok := elem["E"]; ok {
				results = append(results, Violation{Rule: Rule_14289_1_7_2_t23, Context: path})
			}
		}

		if effectiveLang == "" && !anyLangFound {
			// No /Lang from self or any ancestor, and no /Lang anywhere in
			// the tree. Language is truly not determinable.
			results = append(results, Violation{Rule: langRule, Context: path})
		}
	})

	return results
}

// walkStructureElementsWithLang is like walkStructureElements but tracks the
// inherited /Lang value through the tree. The callback receives the effective
// inherited language (from ancestors, not including the element's own /Lang).
func walkStructureElementsWithLang(ctx context.Context, doc *document.Document, root ast.PdfDict, rootLang string, fn func(elem ast.PdfDict, path string, inheritedLang string)) {
	kidsObj, ok := root["K"]
	if !ok {
		return
	}
	walkStructKidsWithLang(ctx, doc, kidsObj, "root", make(map[int]bool), 0, rootLang, fn)
}

// walkStructKidsWithLang is the /Lang-tracking twin of [walkStructKids]; seen
// gives it the same one-visit-per-node guard against a fan-out denial of service.
func walkStructKidsWithLang(ctx context.Context, doc *document.Document, obj ast.PdfObject, path string, seen map[int]bool, depth int, inheritedLang string, fn func(elem ast.PdfDict, path string, inheritedLang string)) {
	if ctx.Err() != nil || depth > maxStructTreeWalkDepth {
		return
	}

	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		if ref, ok := obj.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				return
			}
			seen[ref.ObjectNum] = true
		}
		dict, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || dict == nil {
			return
		}
		visitStructElemWithLang(ctx, doc, dict, path, seen, depth, inheritedLang, fn)
		return
	}

	for i, kid := range arr {
		if ctx.Err() != nil {
			return
		}
		if ref, ok := kid.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
		}
		kidDict, err := document.ResolveDict(ctx, doc, kid)
		if err != nil || kidDict == nil {
			continue
		}
		childPath := fmt.Sprintf("%s[%d]", path, i)
		visitStructElemWithLang(ctx, doc, kidDict, childPath, seen, depth, inheritedLang, fn)
	}
}

func visitStructElemWithLang(ctx context.Context, doc *document.Document, elem ast.PdfDict, path string, seen map[int]bool, depth int, inheritedLang string, fn func(elem ast.PdfDict, path string, inheritedLang string)) {
	if _, hasS := elem["S"]; !hasS {
		return
	}
	typeName, _ := document.ResolveName(ctx, doc, elem["S"])
	elemPath := fmt.Sprintf("%s/%s", path, string(typeName))

	fn(elem, elemPath, inheritedLang)

	// Compute the effective /Lang for children: own /Lang overrides inherited.
	childLang := inheritedLang
	if ownLang := resolveNonEmptyLang(ctx, doc, elem); ownLang != "" {
		childLang = ownLang
	}

	if kidsObj, ok := elem["K"]; ok {
		walkStructKidsWithLang(ctx, doc, kidsObj, elemPath, seen, depth+1, childLang, fn)
	}
}

// checkOutlineLang verifies that outline entries (bookmarks) have a determinable
// natural language. Per PDF/UA-1 §7.2 t2, each outline item's /Title text
// must have a determinable language: either the item itself has /Lang, or
// the catalog has /Lang.
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

	// Walk outline items and check for /Lang.
	var results []Violation
	walkOutlineItemsForLang(ctx, doc, outlines["First"], &results, 0, make(map[int]bool))
	return results
}

// walkOutlineItemsForLang checks each outline item for a determinable language,
// walking siblings iteratively via /Next and recursing into children via /First.
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

		item, err := document.ResolveDict(ctx, doc, itemObj)
		if err != nil || item == nil {
			return
		}

		if _, hasTitle := item["Title"]; !hasTitle {
			return
		}

		// Outline items need /Lang when catalog /Lang is absent.
		if resolveNonEmptyLang(ctx, doc, item) == "" {
			*results = append(*results, fail(Rule_14289_1_7_2_t02, "outline item")...)
		}

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
		if !IsValidLangTag(trimmed) {
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
