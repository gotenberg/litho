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
	for pageIdx, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return results
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
			tag := strings.TrimSpace(document.DecodePdfString(lang))
			// A /Lang present on a marked-content sequence must be a valid
			// Language-Tag; for PDF/UA an empty value is invalid (UA-1 §7.2 t29
			// / UA-2 §8.4.4 t2), unlike PDF/A where an empty text string is
			// permitted. This pass runs only under PDF/UA (guarded above).
			if tag == "" || !IsValidLangTag(tag) {
				results = append(results, failf(rule,
					"page %d: marked-content /Lang %q is not a valid language tag", pageIdx+1, tag)...)
			}
		}
	}
	return results
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
		return checkStructureTreeLangCoverage(ctx, doc, catalog, langRule, validRule)
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
			if resolveNonEmptyLang(ctx, doc, elem) != "" {
				anyLangFound = true
			}
		})
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
	walkStructKidsWithLang(ctx, doc, kidsObj, "root", 0, rootLang, fn)
}

func walkStructKidsWithLang(ctx context.Context, doc *document.Document, obj ast.PdfObject, path string, depth int, inheritedLang string, fn func(elem ast.PdfDict, path string, inheritedLang string)) {
	if ctx.Err() != nil || depth > maxStructTreeWalkDepth {
		return
	}

	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		dict, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || dict == nil {
			return
		}
		visitStructElemWithLang(ctx, doc, dict, path, depth, inheritedLang, fn)
		return
	}

	for i, kid := range arr {
		if ctx.Err() != nil {
			return
		}
		kidDict, err := document.ResolveDict(ctx, doc, kid)
		if err != nil || kidDict == nil {
			continue
		}
		childPath := fmt.Sprintf("%s[%d]", path, i)
		visitStructElemWithLang(ctx, doc, kidDict, childPath, depth, inheritedLang, fn)
	}
}

func visitStructElemWithLang(ctx context.Context, doc *document.Document, elem ast.PdfDict, path string, depth int, inheritedLang string, fn func(elem ast.PdfDict, path string, inheritedLang string)) {
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
		walkStructKidsWithLang(ctx, doc, kidsObj, elemPath, depth+1, childLang, fn)
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
	walkOutlineItemsForLang(ctx, doc, outlines["First"], &results, 0)
	return results
}

func walkOutlineItemsForLang(ctx context.Context, doc *document.Document, itemObj ast.PdfObject, results *[]Violation, depth int) {
	if ctx.Err() != nil || depth > maxStructTreeWalkDepth || itemObj == nil {
		return
	}

	item, err := document.ResolveDict(ctx, doc, itemObj)
	if err != nil || item == nil {
		return
	}

	_, hasTitle := item["Title"]
	if !hasTitle {
		return
	}

	// Outline items need /Lang when catalog /Lang is absent.
	lang := resolveNonEmptyLang(ctx, doc, item)
	if lang == "" {
		*results = append(*results, fail(Rule_14289_1_7_2_t02, "outline item")...)
	}

	// Recurse into children and siblings.
	walkOutlineItemsForLang(ctx, doc, item["First"], results, depth+1)
	walkOutlineItemsForLang(ctx, doc, item["Next"], results, depth)
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
