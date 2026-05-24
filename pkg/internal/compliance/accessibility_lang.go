package compliance

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkLangRequired ensures the catalog has a /Lang key with a valid RFC 3066 tag.
// Rule mapping:
//   - Missing /Lang or empty /Lang in catalog -> langRequiredRule (t34 for PDF/UA-1, 8.4.4 t01 for PDF/UA-2)
//   - Present but invalid /Lang format        -> langValidRule (t29 for PDF/UA-1, 8.4.4 t02 for PDF/UA-2)
func checkLangRequired(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	langRule := langRequiredRule(profile)
	validRule := langValidRule(profile)

	catalogLang := resolveNonEmptyLang(ctx, doc, catalog)

	if catalogLang == "" {
		// PDF/UA-2 §8.4.4 requires catalog /Lang explicitly.
		if profile == "PDF/UA-2" {
			return fail(langRule, "catalog")
		}

		// PDF/UA-1 §7.2 t34: natural language must be determinable. If catalog
		// /Lang is absent, the language can still be determined through the
		// structure tree. The tree root or individual elements may carry /Lang.
		return checkStructureTreeLangCoverage(ctx, doc, catalog, langRule, validRule)
	}

	if !isValidLangTag(catalogLang) {
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
	if rootLang != "" && !isValidLangTag(rootLang) {
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
		elemLang := resolveNonEmptyLang(ctx, doc, elem)
		effectiveLang := inheritedLang
		if elemLang != "" {
			// Validate format of this element's own /Lang.
			if !isValidLangTag(elemLang) {
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

	// If catalog has /Lang, all outline entries inherit it.
	catalogLang := resolveNonEmptyLang(ctx, doc, catalog)
	if catalogLang != "" {
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
			// Empty /Lang in structure element. Not an invalid-format error.
			// The "language not determined" checks (t21-t24, t30-t34) handle this case separately.
			return
		}
		if !isValidLangTag(trimmed) {
			results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("lang=%s, %s", trimmed, path)})
		}
	})

	return results
}

// isValidLangTag validates a language tag against RFC 3066 / RFC 5646 (BCP 47)
// well-formedness. The PDF spec (ISO 32000-1:2008, 14.9.2) references RFC 3066
// which allows 1-8 ALPHA for the primary language subtag (RFC 5646 tightened
// this to 2-8). The validator accepts 1-8 to match VeraPDF's behavior and the spec reference.
//
// It does NOT check against the IANA subtag registry, only structural validity.
// An empty or whitespace-only tag is treated as "not present" rather than
// invalid. Callers should skip validation instead of reporting an
// invalid-tag violation.
//
// Grammar (simplified from RFC 5646 §2.1, with RFC 3066 primary language):
//
//	Language-Tag = langtag / privateuse / grandfathered
//	langtag      = language ["-" script] ["-" region] *("-" variant) *("-" extension) ["-" privateuse]
//	language     = 1*8ALPHA ["-" extlang]   (RFC 3066 allows 1-8 ALPHA)
//	extlang      = 3ALPHA *2("-" 3ALPHA)
//	script       = 4ALPHA
//	region       = 2ALPHA / 3DIGIT
//	variant      = 5*8alphanum / (DIGIT 3alphanum)
//	extension    = singleton 1*("-" 2*8alphanum)
//	privateuse   = "x" 1*("-" 1*8alphanum)
//	singleton    = DIGIT / %x41-57 / %x59-5A / %x61-77 / %x79-7A  (any alphanum except 'x'/'X')
func isValidLangTag(tag string) bool {
	trimmed := strings.TrimSpace(tag)
	if trimmed == "" {
		return true
	}

	parts := strings.Split(trimmed, "-")
	n := len(parts)

	// Private-use tag: x-...
	if len(parts[0]) == 1 && (parts[0][0] == 'x' || parts[0][0] == 'X') {
		return validPrivateUse(parts)
	}

	// Grandfathered/IANA: i-...
	if len(parts[0]) == 1 && (parts[0][0] == 'i' || parts[0][0] == 'I') {
		// "i" followed by at least one subtag of 1-8 alphanum.
		if n < 2 {
			return false
		}
		for _, sub := range parts[1:] {
			if !isAlphaNumSubtag(sub, 1, 8) {
				return false
			}
		}
		return true
	}

	// Primary language subtag: 1-8 ALPHA per RFC 3066 (referenced by PDF spec ISO 32000-1:2008, 14.9.2).
	// RFC 5646 restricts to 2-8 ALPHA, but PDF conforming readers must accept RFC 3066.
	primary := parts[0]
	if !isAlphaStr(primary) {
		return false
	}
	pLen := len(primary)
	if pLen < 1 || pLen > 8 {
		return false
	}

	i := 1 // current index into parts

	// Optional extlang: up to 3 sequences of exactly 3 ALPHA, only after 2-3 char primary.
	if pLen <= 3 {
		for extCount := 0; extCount < 3 && i < n; extCount++ {
			if len(parts[i]) == 3 && isAlphaStr(parts[i]) {
				i++
			} else {
				break
			}
		}
	}

	// Optional script: exactly 4 ALPHA.
	if i < n && len(parts[i]) == 4 && isAlphaStr(parts[i]) {
		i++
	}

	// Optional region: 2 ALPHA or 3 DIGIT.
	if i < n {
		sub := parts[i]
		if (len(sub) == 2 && isAlphaStr(sub)) || (len(sub) == 3 && isDigitStr(sub)) {
			i++
		}
	}

	// Variants: 5-8 alphanum, or DIGIT followed by 3 alphanum.
	for i < n {
		sub := parts[i]
		sLen := len(sub)
		//nolint:gocritic // variant subtag parsing requires compound conditions incompatible with switch.
		if sLen >= 5 && sLen <= 8 && isAlphaNumStr(sub) {
			i++
		} else if sLen == 4 && isDigit(sub[0]) && isAlphaNumStr(sub[1:]) {
			i++
		} else {
			break
		}
	}

	// Extensions: singleton 1*("-" 2*8alphanum), where singleton != 'x'/'X'.
	for i < n {
		sub := parts[i]
		if len(sub) != 1 || !isAlphaNumByte(sub[0]) {
			break
		}
		if sub[0] == 'x' || sub[0] == 'X' {
			break // private-use handled below
		}
		i++ // consume singleton
		if i >= n {
			return false // singleton must have at least one subtag
		}
		consumed := false
		for i < n && isAlphaNumSubtag(parts[i], 2, 8) {
			i++
			consumed = true
		}
		if !consumed {
			return false
		}
	}

	// Optional trailing private-use: x-...
	if i < n {
		if len(parts[i]) == 1 && (parts[i][0] == 'x' || parts[i][0] == 'X') {
			return validPrivateUse(parts[i:])
		}
		return false // unexpected trailing subtag
	}

	return true
}

// validPrivateUse checks that parts is ["x", sub1, sub2, ...] with each sub being 1-8 alphanum.
func validPrivateUse(parts []string) bool {
	if len(parts) < 2 {
		return false
	}
	for _, sub := range parts[1:] {
		if !isAlphaNumSubtag(sub, 1, 8) {
			return false
		}
	}
	return true
}

func isAlphaStr(s string) bool {
	for i := range len(s) {
		b := s[i]
		if (b < 'a' || b > 'z') && (b < 'A' || b > 'Z') {
			return false
		}
	}
	return len(s) > 0
}

func isDigitStr(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

func isAlphaNumStr(s string) bool {
	for i := range len(s) {
		b := s[i]
		if (b < 'a' || b > 'z') && (b < 'A' || b > 'Z') && (b < '0' || b > '9') {
			return false
		}
	}
	return len(s) > 0
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isAlphaNumByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// isAlphaNumSubtag checks that s is min..max alphanum characters.
func isAlphaNumSubtag(s string, min, max int) bool {
	l := len(s)
	if l < min || l > max {
		return false
	}
	return isAlphaNumStr(s)
}
