package compliance

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkAccessibilityGroup validates PDF/UA accessibility requirements.
// For non-PDF/UA profiles, it returns nil immediately.
func checkAccessibilityGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if !isPDFUA(profile) {
		slog.Debug("compliance_2/accessibility: skipping for non-PDF/UA profile", "profile", profile)
		return nil
	}

	slog.Debug("compliance_2/accessibility: starting checks", "profile", profile)

	results := make([]Result, 0, 16) //nolint:mnd // estimated capacity for accessibility checks.

	// 1. Language (§7.2/§8.2) — /Lang key required in catalog.
	results = append(results, checkLangRequired(ctx, doc, catalog, profile)...)

	// 1b. Outline language (§7.2 t2, PDF/UA-1) — outline entries must have determinable language.
	results = append(results, checkOutlineLang(ctx, doc, catalog, profile)...)

	// 2. DisplayDocTitle (§7.1) — /ViewerPreferences must have /DisplayDocTitle=true.
	results = append(results, checkDisplayDocTitle(ctx, doc, catalog, profile)...)

	// 3. Suspects (§7.1) — /MarkInfo /Suspects must be false or absent.
	results = append(results, checkSuspects(ctx, doc, catalog, profile)...)

	// 4. Tab order (§7.18.1, PDF/UA-1 only) — Page /Tabs must be /S.
	results = append(results, checkTabOrder(ctx, doc, catalog, profile)...)

	// 5. XMP title (PDF/UA) — XMP must contain dc:title.
	results = append(results, checkXMPTitle(ctx, doc, catalog, profile)...)

	// 6. Images alt text (PDF/UA) — Figure structure elements must have /Alt.
	results = append(results, checkFigureAltText(ctx, doc, catalog, profile)...)

	// 7. Artifact tagging (PDF/UA) — Headers/footers/page numbers should be tagged as Artifact.
	results = append(results, checkArtifactTagging(ctx, doc, catalog, profile)...)

	// 8. Annotation alt text (PDF/UA) — Non-Link/Widget annotations must have /Contents or structure element /Alt.
	results = append(results, checkAnnotationAltText(ctx, doc, catalog, profile)...)

	// 8b. Widget /AA check (PDF/UA) — Widget annotations must not have /AA key (§7.18.5 / §8.10.2.3).
	results = append(results, checkWidgetAA(ctx, doc, catalog, profile)...)

	// 9. OC config names (PDF/UA) — Optional content configuration dicts must have /Name key.
	results = append(results, checkOCConfigNames(ctx, doc, catalog, profile)...)

	// 10. PDF/UA identification (PDF/UA-2) — XMP must contain pdfuaid:part=2.
	results = append(results, checkPDFUAIdentification(ctx, doc, catalog, profile)...)

	// 11. Namespaces (PDF/UA-2) — /StructTreeRoot must have /Namespaces array.
	results = append(results, checkNamespaces(ctx, doc, catalog, profile)...)

	// 12. Link nesting (PDF/UA-2) — Link annotations must be nested in Link structure elements.
	results = append(results, checkLinkNesting(ctx, doc, catalog, profile)...)

	// 13. Formula MathML (PDF/UA-2) — Formula structure elements must have /AF with MathML.
	results = append(results, checkFormulaMathML(ctx, doc, catalog, profile)...)

	// 14. Invisible annotations in structure (PDF/UA-2) — annotations with Invisible flag must be artifacts.
	results = append(results, checkInvisibleAnnotArtifact(ctx, doc, catalog, profile)...)

	// 15. File specification /F and /UF (PDF/UA-1) — §7.11 t01.
	results = append(results, checkPDFUAFileSpecFUF(ctx, doc, catalog, profile)...)

	slog.Debug("compliance_2/accessibility: checks complete", "violations", len(results))
	return results
}

// --- 1. Language ---

// checkLangRequired ensures the catalog has a /Lang key with a valid RFC 3066 tag.
// Rule mapping:
//   - Missing /Lang or empty /Lang in catalog -> langRequiredRule (t34 for PDF/UA-1, 8.4.4 t01 for PDF/UA-2)
//   - Present but invalid /Lang format        -> langValidRule (t29 for PDF/UA-1, 8.4.4 t02 for PDF/UA-2)
func checkLangRequired(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	slog.Debug("compliance_2/accessibility: checking /Lang requirement")

	langRule := langRequiredRule(profile)
	validRule := langValidRule(profile)

	catalogLang := resolveNonEmptyLang(ctx, doc, catalog)

	if catalogLang == "" {
		// PDF/UA-2 §8.4.4 requires catalog /Lang explicitly.
		if profile == "PDF/UA-2" {
			slog.Debug("compliance_2/accessibility: catalog missing /Lang key (required for PDF/UA-2)")
			return fail(langRule, "catalog")
		}

		// PDF/UA-1 §7.2 t34: natural language must be determinable. If catalog
		// /Lang is absent, the language can still be determined through the
		// structure tree — the tree root or individual elements may carry /Lang.
		slog.Debug("compliance_2/accessibility: catalog missing /Lang, checking structure tree inheritance")
		return checkStructureTreeLangCoverage(ctx, doc, catalog, langRule, validRule)
	}

	slog.Debug("compliance_2/accessibility: catalog /Lang value", "lang", catalogLang)

	if !isValidLangTag(catalogLang) {
		slog.Debug("compliance_2/accessibility: catalog /Lang is invalid BCP 47", "lang", catalogLang)
		return failf(validRule, "lang=%s", catalogLang)
	}

	slog.Debug("compliance_2/accessibility: catalog /Lang is valid, checking structure elements")

	// Catalog provides global /Lang — validate /Lang format in structure elements.
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
func checkStructureTreeLangCoverage(ctx context.Context, doc *document.Document, catalog ast.PdfDict, langRule, validRule Rule) []Result {
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		// No structure tree at all — can't determine language for anything.
		slog.Debug("compliance_2/accessibility: no StructTreeRoot, language cannot be determined")
		return fail(langRule, "catalog")
	}

	// Check if the structure tree root itself has /Lang.
	rootLang := resolveNonEmptyLang(ctx, doc, treeRoot)
	if rootLang != "" && !isValidLangTag(rootLang) {
		return failf(validRule, "lang=%s, StructTreeRoot", rootLang)
	}

	// First pass: check if ANY element in the tree has /Lang. If so, language
	// is at least partially determinable. Elements without structure-level /Lang
	// may still have language set via BDC marked content operators in the content
	// stream, which litho does not currently parse. To avoid false positives on
	// files that use inline BDC /Lang, we only flag coverage violations when the
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

	var results []Result
	walkStructureElementsWithLang(ctx, doc, treeRoot, rootLang, func(elem ast.PdfDict, path string, inheritedLang string) {
		elemLang := resolveNonEmptyLang(ctx, doc, elem)
		effectiveLang := inheritedLang
		if elemLang != "" {
			// Validate format of this element's own /Lang.
			if !isValidLangTag(elemLang) {
				results = append(results, Result{Rule: validRule, Context: fmt.Sprintf("lang=%s, %s", elemLang, path)})
				return
			}
			effectiveLang = elemLang
		}

		if effectiveLang == "" && !anyLangFound {
			// No /Lang from self or any ancestor, and no /Lang anywhere in
			// the tree — language is truly not determinable.
			slog.Debug("compliance_2/accessibility: structure element has no determinable language",
				"path", path)
			results = append(results, Result{Rule: langRule, Context: path})
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
func checkOutlineLang(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
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
	var results []Result
	walkOutlineItemsForLang(ctx, doc, outlines["First"], &results, 0)
	return results
}

func walkOutlineItemsForLang(ctx context.Context, doc *document.Document, itemObj ast.PdfObject, results *[]Result, depth int) {
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

	// Check if this outline item has /Lang.
	lang := resolveNonEmptyLang(ctx, doc, item)
	if lang == "" {
		*results = append(*results, fail(Rule_14289_1_7_2_t02, "outline item")...)
	}

	// Recurse into children and siblings.
	walkOutlineItemsForLang(ctx, doc, item["First"], results, depth+1)
	walkOutlineItemsForLang(ctx, doc, item["Next"], results, depth)
}

// validateStructureLangs walks the structure tree and validates any /Lang entries.
func validateStructureLangs(ctx context.Context, doc *document.Document, catalog ast.PdfDict, rule Rule) []Result {
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		slog.Debug("compliance_2/accessibility: no StructTreeRoot for lang validation")
		return nil
	}

	var results []Result
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		langObj, ok := elem["Lang"]
		if !ok {
			return
		}
		langStr, err := document.ResolveString(ctx, doc, langObj)
		if err != nil {
			slog.Debug("compliance_2/accessibility: cannot resolve /Lang in structure element",
				"path", path, "error", err)
			return
		}
		trimmed := strings.TrimSpace(document.DecodePdfString(langStr))
		if trimmed == "" {
			// Empty /Lang in structure element — not an invalid-format error.
			// The "language not determined" checks (t21-t24, t30-t34) handle this case separately.
			slog.Debug("compliance_2/accessibility: empty /Lang in structure element, skipping format check",
				"path", path)
			return
		}
		if !isValidLangTag(trimmed) {
			slog.Debug("compliance_2/accessibility: invalid /Lang in structure element",
				"lang", trimmed, "path", path)
			results = append(results, Result{Rule: rule, Context: fmt.Sprintf("lang=%s, %s", trimmed, path)})
		}
	})

	return results
}

// isValidLangTag validates a language tag against RFC 3066 / RFC 5646 (BCP 47)
// well-formedness. The PDF spec (ISO 32000-1:2008, 14.9.2) references RFC 3066
// which allows 1-8 ALPHA for the primary language subtag (RFC 5646 tightened
// this to 2-8). We accept 1-8 to match VeraPDF's behavior and the spec reference.
//
// It does NOT check against the IANA subtag registry — only structural validity.
// An empty or whitespace-only tag is treated as "not present" rather than invalid —
// callers should skip validation instead of reporting an invalid-tag violation.
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
		if n < 2 { //nolint:mnd // grandfathered tags require at least one subtag after "i"
			return false
		}
		for _, sub := range parts[1:] {
			if !isAlphaNumSubtag(sub, 1, 8) { //nolint:mnd // BCP 47 subtag length bounds
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
	if pLen < 1 || pLen > 8 { //nolint:mnd // RFC 3066: primary language is 1-8 alpha
		return false
	}

	i := 1 // current index into parts

	// Optional extlang: up to 3 sequences of exactly 3 ALPHA, only after 2-3 char primary.
	if pLen <= 3 { //nolint:mnd // extlang only valid after 2-3 char primary
		for extCount := 0; extCount < 3 && i < n; extCount++ { //nolint:mnd // up to 3 extlang subtags
			if len(parts[i]) == 3 && isAlphaStr(parts[i]) { //nolint:mnd // extlang is exactly 3 ALPHA
				i++
			} else {
				break
			}
		}
	}

	// Optional script: exactly 4 ALPHA.
	if i < n && len(parts[i]) == 4 && isAlphaStr(parts[i]) { //nolint:mnd // script is exactly 4 ALPHA
		i++
	}

	// Optional region: 2 ALPHA or 3 DIGIT.
	if i < n {
		sub := parts[i]
		if (len(sub) == 2 && isAlphaStr(sub)) || (len(sub) == 3 && isDigitStr(sub)) { //nolint:mnd // region: 2 ALPHA or 3 DIGIT
			i++
		}
	}

	// Variants: 5-8 alphanum, or DIGIT followed by 3 alphanum.
	for i < n {
		sub := parts[i]
		sLen := len(sub)
		if sLen >= 5 && sLen <= 8 && isAlphaNumStr(sub) { //nolint:mnd // variant: 5-8 alphanum
			i++
		} else if sLen == 4 && isDigit(sub[0]) && isAlphaNumStr(sub[1:]) { //nolint:mnd // variant: DIGIT 3alphanum
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
		for i < n && isAlphaNumSubtag(parts[i], 2, 8) { //nolint:mnd // extension subtag: 2-8 alphanum
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
	if len(parts) < 2 { //nolint:mnd // "x" must be followed by at least one subtag
		return false
	}
	for _, sub := range parts[1:] {
		if !isAlphaNumSubtag(sub, 1, 8) { //nolint:mnd // private-use subtag: 1-8 alphanum
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

// --- 2. DisplayDocTitle ---

func checkDisplayDocTitle(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	slog.Debug("compliance_2/accessibility: checking /DisplayDocTitle")

	rule := displayDocTitleRule(profile)

	vpObj, ok := catalog["ViewerPreferences"]
	if !ok {
		slog.Debug("compliance_2/accessibility: catalog missing /ViewerPreferences")
		return fail(rule, "ViewerPreferences missing")
	}

	vp, err := document.ResolveDict(ctx, doc, vpObj)
	if err != nil || vp == nil {
		slog.Debug("compliance_2/accessibility: cannot resolve /ViewerPreferences", "error", err)
		return fail(rule, "ViewerPreferences unresolvable")
	}

	ddtObj, ok := vp["DisplayDocTitle"]
	if !ok {
		slog.Debug("compliance_2/accessibility: /DisplayDocTitle not present")
		return fail(rule, "DisplayDocTitle missing")
	}

	ddt, err := document.ResolveBool(ctx, doc, ddtObj)
	if err != nil || !bool(ddt) {
		slog.Debug("compliance_2/accessibility: /DisplayDocTitle is not true", "value", ddt)
		return fail(rule, "DisplayDocTitle=false")
	}

	slog.Debug("compliance_2/accessibility: /DisplayDocTitle is true")
	return nil
}

// --- 3. Suspects ---

func checkSuspects(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	slog.Debug("compliance_2/accessibility: checking /Suspects")

	rule := suspectsRule(profile)

	miObj, ok := catalog["MarkInfo"]
	if !ok {
		slog.Debug("compliance_2/accessibility: catalog missing /MarkInfo (Suspects check passes)")
		return nil
	}

	mi, err := document.ResolveDict(ctx, doc, miObj)
	if err != nil || mi == nil {
		slog.Debug("compliance_2/accessibility: cannot resolve /MarkInfo", "error", err)
		return nil
	}

	susObj, ok := mi["Suspects"]
	if !ok {
		slog.Debug("compliance_2/accessibility: /Suspects absent (OK)")
		return nil
	}

	sus, err := document.ResolveBool(ctx, doc, susObj)
	if err != nil {
		slog.Debug("compliance_2/accessibility: cannot resolve /Suspects", "error", err)
		return nil
	}

	if bool(sus) {
		slog.Debug("compliance_2/accessibility: /Suspects is true")
		return fail(rule, "Suspects=true")
	}

	slog.Debug("compliance_2/accessibility: /Suspects is false (OK)")
	return nil
}

// --- 4. Tab order ---

func checkTabOrder(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	slog.Debug("compliance_2/accessibility: checking tab order")

	isPDFUA1 := profile == string("PDF/UA-1")
	isPDFUA2 := profile == string("PDF/UA-2")

	if !isPDFUA1 && !isPDFUA2 {
		return nil
	}

	rule := tabOrderRule(profile)

	pages := collectPages(ctx, doc, catalog)
	var results []Result

	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		// Check if page has annotations.
		annotsObj, hasAnnots := page["Annots"]
		if hasAnnots {
			annots, err := document.ResolveArray(ctx, doc, annotsObj)
			if err != nil || len(annots) == 0 {
				hasAnnots = false
			}
		}

		if !hasAnnots && isPDFUA2 {
			// PDF/UA-2 only requires /Tabs on pages with annotations.
			continue
		}

		tabsObj, ok := page["Tabs"]
		if !ok {
			if isPDFUA1 {
				// PDF/UA-1 §7.18.7: every page must have /Tabs = /S when
				// the document has a structure tree (which is always the
				// case for PDF/UA-1). Flag all pages missing /Tabs.
				slog.Debug("compliance_2/accessibility: page missing /Tabs", "page", pageIdx)
				results = append(results, Result{Rule: rule, Context: fmt.Sprintf("page %d, Tabs missing", pageIdx)})
			} else if isPDFUA2 && hasAnnots {
				slog.Debug("compliance_2/accessibility: page missing /Tabs", "page", pageIdx)
				results = append(results, Result{Rule: rule, Context: fmt.Sprintf("page %d, Tabs missing", pageIdx)})
			}
			continue
		}

		tabsName, err := document.ResolveName(ctx, doc, tabsObj)
		if err != nil {
			slog.Debug("compliance_2/accessibility: cannot resolve /Tabs", "page", pageIdx, "error", err)
			continue
		}

		if isPDFUA1 && tabsName != "S" {
			slog.Debug("compliance_2/accessibility: /Tabs is not /S", "page", pageIdx, "tabs", tabsName)
			results = append(results, Result{Rule: rule, Context: fmt.Sprintf("page %d, Tabs=%s", pageIdx, tabsName)})
		} else if isPDFUA2 && hasAnnots && tabsName != "S" && tabsName != "A" && tabsName != "W" {
			slog.Debug("compliance_2/accessibility: /Tabs is not /S, /A or /W", "page", pageIdx, "tabs", tabsName)
			results = append(results, Result{Rule: rule, Context: fmt.Sprintf("page %d, Tabs=%s", pageIdx, tabsName)})
		}
	}

	return results
}

// --- 5. XMP title ---

func checkXMPTitle(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	slog.Debug("compliance_2/accessibility: checking XMP dc:title")

	rule := xmpTitleRule(profile)

	xmpData, ok := readCatalogXMP(ctx, doc, catalog)
	if !ok {
		slog.Debug("compliance_2/accessibility: no XMP metadata available")
		return fail(rule, "no XMP metadata")
	}

	if !containsDCTitle(xmpData) {
		slog.Debug("compliance_2/accessibility: XMP does not contain dc:title")
		return fail(rule, "")
	}

	slog.Debug("compliance_2/accessibility: XMP contains dc:title")
	return nil
}

// --- 6. Figure alt text ---

func checkFigureAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	slog.Debug("compliance_2/accessibility: checking Figure alt text")

	rule := figureAltRule(profile)

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		slog.Debug("compliance_2/accessibility: no StructTreeRoot for Figure alt check")
		return nil
	}

	var results []Result
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		if typeName != "Figure" {
			return
		}

		// PDF/UA-1 §7.3 (Rule_14289_1_7_3_t01): Figure elements must have /Alt.
		// PDF/UA-2 §8.2.5.28.2: Figure elements must have /Alt or /ActualText.
		hasAlt := hasNonEmptyStringKey(ctx, doc, elem, "Alt")
		hasActualText := hasNonEmptyStringKey(ctx, doc, elem, "ActualText")

		if profile == "PDF/UA-1" {
			// PDF/UA-1 strictly requires /Alt on Figure elements.
			if !hasAlt {
				slog.Debug("compliance_2/accessibility: Figure missing /Alt", "path", path)
				results = append(results, Result{Rule: rule, Context: path})
			}
		} else {
			// PDF/UA-2 accepts either /Alt or /ActualText.
			if !hasAlt && !hasActualText {
				slog.Debug("compliance_2/accessibility: Figure missing /Alt or /ActualText", "path", path)
				results = append(results, Result{Rule: rule, Context: path})
			}
		}
	})

	return results
}

// --- 7. Artifact tagging ---

func checkArtifactTagging(ctx context.Context, _ *document.Document, _ ast.PdfDict, _ string) []Result {
	// Artifact tagging checks require deep content stream analysis to determine
	// if headers/footers/page numbers are properly marked as Artifact. This is
	// a heuristic check that cannot be fully validated without rendering. We log
	// the check but return no violations — a future enhancement may add
	// heuristic detection of repeated header/footer content.
	slog.Debug("compliance_2/accessibility: artifact tagging check (heuristic, not enforced)")
	return nil
}

// --- 8. Annotation alt text ---

func checkAnnotationAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	slog.Debug("compliance_2/accessibility: checking annotation alt text")

	rule := annotAltRule(profile)

	// Build a map from annotation object number to whether the enclosing
	// structure element has a non-empty /Alt entry.
	structAltMap := buildAnnotStructAltMap(ctx, doc, catalog)

	pages := collectPages(ctx, doc, catalog)
	var results []Result

	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}

		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			continue
		}

		for annotIdx, annotObj := range annots {
			if ctx.Err() != nil {
				return results
			}

			// Grab the object number before resolving, for structure tree lookup.
			annotObjNum := 0
			if ref, ok := annotObj.(ast.PdfIndirectRef); ok {
				annotObjNum = ref.ObjectNum
			}

			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}

			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])

			// Link, Widget, and Popup annotations are excluded from this check.
			// Popup annotations are explicitly exempt — they inherit content
			// from their parent annotation and do not need /Contents or /Alt.
			if subtype == "Link" || subtype == "Widget" || subtype == "Popup" {
				slog.Debug("compliance_2/accessibility: skipping exempt annotation subtype",
					"page", pageIdx, "annot", annotIdx, "subtype", string(subtype))
				continue
			}

			// Check if annotation is hidden or has Rect outside the crop-box.
			if isAnnotHidden(ctx, doc, annot) || isAnnotOutsideCropBox(ctx, doc, annot, page) {
				continue
			}

			// Check /Contents key — must be present and non-empty.
			if contentsObj, hasContents := annot["Contents"]; hasContents {
				contentsStr, err := document.ResolveString(ctx, doc, contentsObj)
				if err == nil && strings.TrimSpace(string(contentsStr)) != "" {
					continue
				}
				slog.Debug("compliance_2/accessibility: annotation has empty /Contents",
					"page", pageIdx, "annot", annotIdx, "subtype", string(subtype))
			}

			// Annotation lacks /Contents — check the enclosing structure element for /Alt.
			if annotObjNum > 0 && structAltMap[annotObjNum] {
				slog.Debug("compliance_2/accessibility: annotation has /Alt via structure element",
					"page", pageIdx, "annot", annotIdx, "objNum", annotObjNum)
				continue
			}

			loc := fmt.Sprintf("page %d, annot %d (/%s)", pageIdx, annotIdx, subtype)
			slog.Debug("compliance_2/accessibility: annotation missing /Contents and struct /Alt", "loc", loc)
			results = append(results, Result{Rule: rule, Context: loc})
		}
	}

	return results
}

// buildAnnotStructAltMap walks the structure tree and returns a set of annotation
// object numbers whose enclosing structure element has a non-empty /Alt entry.
// PDF structure elements reference annotations via OBJR (object reference) dicts
// in their /K children: { /Type /OBJR /Obj <indirect ref to annotation> }.
func buildAnnotStructAltMap(ctx context.Context, doc *document.Document, catalog ast.PdfDict) map[int]bool {
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}

	result := make(map[int]bool)
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, _ string) {
		// Check if this structure element has a non-empty /Alt.
		if !hasNonEmptyStringKey(ctx, doc, elem, "Alt") {
			return
		}

		// Scan /K children for OBJR dicts pointing to annotations.
		kObj, ok := elem["K"]
		if !ok {
			return
		}
		collectOBJRAnnotObjNums(ctx, doc, kObj, result)
	})

	return result
}

// collectOBJRAnnotObjNums finds OBJR dicts in the /K children of a structure element
// and records the object numbers of their /Obj references.
func collectOBJRAnnotObjNums(ctx context.Context, doc *document.Document, kObj ast.PdfObject, dest map[int]bool) {
	// /K can be a single item or an array.
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		// Try as a single dict.
		checkOBJR(ctx, doc, kObj, dest)
		return
	}
	for _, item := range arr {
		checkOBJR(ctx, doc, item, dest)
	}
}

// checkOBJR checks if obj is an OBJR dict and records the /Obj object number.
func checkOBJR(ctx context.Context, doc *document.Document, obj ast.PdfObject, dest map[int]bool) {
	dict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || dict == nil {
		return
	}
	typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
	if typeName != "OBJR" {
		return
	}
	// /Obj is an indirect reference to the annotation.
	if ref, ok := dict["Obj"].(ast.PdfIndirectRef); ok {
		dest[ref.ObjectNum] = true
	}
}

// isAnnotHidden checks if an annotation has the Hidden flag set.
func isAnnotHidden(ctx context.Context, doc *document.Document, annot ast.PdfDict) bool {
	fObj, ok := annot["F"]
	if !ok {
		return false
	}
	flags, err := document.ResolveInt(ctx, doc, fObj)
	if err != nil {
		return false
	}
	return int(flags)&annotFlagHidden != 0
}

// isAnnotOutsideCropBox checks if the annotation's /Rect is entirely outside
// the page's effective crop box (CropBox if present, otherwise MediaBox).
func isAnnotOutsideCropBox(ctx context.Context, doc *document.Document, annot, page ast.PdfDict) bool {
	annotRect := resolveRect(ctx, doc, annot["Rect"])
	if annotRect == nil {
		return false
	}

	// CropBox defaults to MediaBox per PDF spec.
	cropBox := resolveRect(ctx, doc, page["CropBox"])
	if cropBox == nil {
		cropBox = resolveRect(ctx, doc, page["MediaBox"])
	}
	if cropBox == nil {
		return false
	}

	// Two rectangles are non-overlapping if one is entirely to the left, right,
	// above, or below the other.
	return annotRect[2] <= cropBox[0] || // annot right ≤ crop left
		annotRect[0] >= cropBox[2] || // annot left ≥ crop right
		annotRect[3] <= cropBox[1] || // annot top ≤ crop bottom
		annotRect[1] >= cropBox[3] // annot bottom ≥ crop top
}

// resolveRect resolves a PDF rectangle (4-element number array) and normalizes it
// so that [0],[1] is the lower-left and [2],[3] is the upper-right.
func resolveRect(ctx context.Context, doc *document.Document, obj ast.PdfObject) []float64 {
	if obj == nil {
		return nil
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) < 4 { //nolint:mnd // rectangle has 4 elements
		return nil
	}
	r := make([]float64, 4) //nolint:mnd // rectangle has 4 elements
	for i := range 4 {      //nolint:mnd // rectangle has 4 elements
		r[i] = document.GetFloat(arr[i])
	}
	// Normalize: ensure [0]<[2] and [1]<[3].
	if r[0] > r[2] {
		r[0], r[2] = r[2], r[0]
	}
	if r[1] > r[3] {
		r[1], r[3] = r[3], r[1]
	}
	return r
}

// --- 8b. Widget /AA ---

// checkWidgetAA validates that Widget annotations do not have /AA (additional actions)
// entries. For PDF/UA-1, the presence of /AA on a Widget is forbidden per §7.18.
// For PDF/UA-2, if /AA is present, the Widget must have a /Contents entry (§8.10.2.3 t02).
func checkWidgetAA(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	slog.Debug("compliance_2/accessibility: checking Widget /AA")

	pages := collectPages(ctx, doc, catalog)
	var results []Result

	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}

		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			continue
		}

		for annotIdx, annotObj := range annots {
			if ctx.Err() != nil {
				return results
			}

			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}

			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if subtype != "Widget" {
				continue
			}

			_, hasAA := annot["AA"]
			if !hasAA {
				continue
			}

			loc := fmt.Sprintf("page %d, widget annot %d", pageIdx+1, annotIdx)

			if profile == "PDF/UA-2" {
				// PDF/UA-2 §8.10.2.3 t02: if Widget has /AA, /Contents must be present.
				if _, hasContents := annot["Contents"]; !hasContents {
					slog.Debug("compliance_2/accessibility: Widget has /AA without /Contents",
						"loc", loc)
					results = append(results, Result{
						Rule:    Rule_14289_2_8_10_2_3_t02,
						Context: loc,
					})
				}
			} else {
				// PDF/UA-1: Widget annotations should not have /AA trigger actions.
				slog.Debug("compliance_2/accessibility: Widget has forbidden /AA", "loc", loc)
				results = append(results, Result{
					Rule:    Rule_14289_1_7_18_1_t03,
					Context: fmt.Sprintf("widget annotation has /AA at %s", loc),
				})
			}
		}
	}

	return results
}

// --- 9. OC config names ---

func checkOCConfigNames(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	slog.Debug("compliance_2/accessibility: checking OC config names")

	rule := ocConfigNameRule(profile)

	ocProps, err := document.ResolveDict(ctx, doc, catalog["OCProperties"])
	if err != nil || ocProps == nil {
		slog.Debug("compliance_2/accessibility: no /OCProperties, skipping OC check")
		return nil
	}

	var results []Result

	// Check default config (/D).
	dObj, ok := ocProps["D"]
	if ok {
		dConfig, err := document.ResolveDict(ctx, doc, dObj)
		if err == nil && dConfig != nil {
			if !hasNonEmptyName(ctx, doc, dConfig) {
				slog.Debug("compliance_2/accessibility: default OC config missing /Name")
				results = append(results, Result{Rule: rule, Context: "default config (D)"})
			}
		}
	}

	// Check /Configs array.
	configsObj, ok := ocProps["Configs"]
	if !ok {
		return results
	}

	configs, err := document.ResolveArray(ctx, doc, configsObj)
	if err != nil {
		return results
	}

	for i, cfgObj := range configs {
		if ctx.Err() != nil {
			return results
		}

		cfg, err := document.ResolveDict(ctx, doc, cfgObj)
		if err != nil || cfg == nil {
			continue
		}

		if !hasNonEmptyName(ctx, doc, cfg) {
			slog.Debug("compliance_2/accessibility: OC config missing /Name", "index", i)
			results = append(results, Result{Rule: rule, Context: fmt.Sprintf("config %d", i)})
		}
	}

	return results
}

// hasNonEmptyName checks if a dict has a non-empty /Name string entry.
func hasNonEmptyName(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	nameObj, ok := dict["Name"]
	if !ok {
		return false
	}
	nameStr, err := document.ResolveString(ctx, doc, nameObj)
	if err != nil {
		return false
	}
	return string(nameStr) != ""
}

// --- 10. PDF/UA identification (PDF/UA-2) ---

func checkPDFUAIdentification(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if profile != "PDF/UA-2" {
		return nil
	}

	slog.Debug("compliance_2/accessibility: checking PDF/UA-2 identification in XMP")

	xmpData, ok := readCatalogXMP(ctx, doc, catalog)
	if !ok {
		slog.Debug("compliance_2/accessibility: no XMP metadata for PDF/UA-2 identification")
		return fail(Rule_14289_2_5_t01, "no XMP metadata")
	}

	if !containsPDFUAIDPart(xmpData, "2") {
		slog.Debug("compliance_2/accessibility: XMP does not contain pdfuaid:part=2")
		return fail(Rule_14289_2_5_t02, "pdfuaid:part is not 2")
	}

	slog.Debug("compliance_2/accessibility: PDF/UA-2 identification found")
	return nil
}

// --- 11. Namespaces (PDF/UA-2) ---

func checkNamespaces(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if profile != "PDF/UA-2" {
		return nil
	}

	slog.Debug("compliance_2/accessibility: checking /Namespaces in StructTreeRoot")

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		slog.Debug("compliance_2/accessibility: no StructTreeRoot for namespace check")
		return nil
	}

	nsObj, ok := treeRoot["Namespaces"]
	if !ok {
		slog.Debug("compliance_2/accessibility: StructTreeRoot missing /Namespaces")
		return fail(Rule_14289_2_8_2_4_t01, "Namespaces missing from StructTreeRoot")
	}

	nsArray, err := document.ResolveArray(ctx, doc, nsObj)
	if err != nil || len(nsArray) == 0 {
		slog.Debug("compliance_2/accessibility: /Namespaces is empty or unresolvable", "error", err)
		return fail(Rule_14289_2_8_2_4_t01, "Namespaces empty")
	}

	var results []Result
	for i, nsEntryObj := range nsArray {
		if ctx.Err() != nil {
			return results
		}

		nsDict, err := document.ResolveDict(ctx, doc, nsEntryObj)
		if err != nil || nsDict == nil {
			slog.Debug("compliance_2/accessibility: cannot resolve namespace entry", "index", i, "error", err)
			continue
		}

		// Each namespace dict must have /NS (URI).
		nsURIObj, ok := nsDict["NS"]
		if !ok {
			slog.Debug("compliance_2/accessibility: namespace entry missing /NS", "index", i)
			results = append(results, Result{
				Rule:    Rule_14289_2_8_2_4_t01,
				Context: fmt.Sprintf("namespace %d missing /NS", i),
			})
			continue
		}

		nsURI, err := document.ResolveString(ctx, doc, nsURIObj)
		if err != nil || string(nsURI) == "" {
			slog.Debug("compliance_2/accessibility: namespace /NS is empty", "index", i)
			results = append(results, Result{
				Rule:    Rule_14289_2_8_2_4_t01,
				Context: fmt.Sprintf("namespace %d has empty /NS", i),
			})
		} else {
			slog.Debug("compliance_2/accessibility: namespace entry valid", "index", i, "ns", string(nsURI))
		}
	}

	return results
}

// --- 12. Link nesting (PDF/UA-2) ---

func checkLinkNesting(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if profile != "PDF/UA-2" {
		return nil
	}

	slog.Debug("compliance_2/accessibility: checking link nesting in structure tree")

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		slog.Debug("compliance_2/accessibility: no StructTreeRoot for link nesting check")
		return nil
	}

	// Collect all Link annotations from pages.
	pages := collectPages(ctx, doc, catalog)
	linkAnnotCount := 0
	for _, page := range pages {
		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}
		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			continue
		}
		for _, annotObj := range annots {
			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if subtype == "Link" {
				linkAnnotCount++
			}
		}
	}

	if linkAnnotCount == 0 {
		slog.Debug("compliance_2/accessibility: no link annotations found")
		return nil
	}

	// Check that Link structure elements exist in the tree.
	var linkStructCount int
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		if typeName == "Link" || typeName == "Reference" {
			linkStructCount++
		}
	})

	slog.Debug("compliance_2/accessibility: link annotation/structure counts",
		"annotations", linkAnnotCount, "structElements", linkStructCount)

	if linkStructCount == 0 && linkAnnotCount > 0 {
		return fail(Rule_14289_2_8_2_5_20_t01, "link annotations exist but no Link/Reference structure elements found")
	}

	return nil
}

// --- 13. Formula MathML (PDF/UA-2) ---

func checkFormulaMathML(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if profile != "PDF/UA-2" {
		return nil
	}

	slog.Debug("compliance_2/accessibility: checking Formula structure elements for /AF (MathML)")

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		slog.Debug("compliance_2/accessibility: no StructTreeRoot for Formula check")
		return nil
	}

	var results []Result
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		if typeName != "Formula" {
			return
		}

		// Formula elements should have one of:
		// (a) /AF (associated files) with MathML content
		// (b) Non-empty /Alt text
		// (c) Non-empty /ActualText
		// (d) Child structure elements providing the formula content
		if altObj, hasAlt := elem["Alt"]; hasAlt {
			altStr, err := document.ResolveString(ctx, doc, altObj)
			if err == nil && strings.TrimSpace(string(altStr)) != "" {
				slog.Debug("compliance_2/accessibility: Formula has /Alt text", "path", path)
				return
			}
		}
		if atObj, hasAT := elem["ActualText"]; hasAT {
			atStr, err := document.ResolveString(ctx, doc, atObj)
			if err == nil && strings.TrimSpace(string(atStr)) != "" {
				slog.Debug("compliance_2/accessibility: Formula has /ActualText", "path", path)
				return
			}
		}

		// Check for /AF.
		if afObj, hasAF := elem["AF"]; hasAF {
			afArr, err := document.ResolveArray(ctx, doc, afObj)
			if err != nil {
				// Might be a single dict, which is acceptable.
				afDict, err := document.ResolveDict(ctx, doc, afObj)
				if err == nil && afDict != nil {
					return
				}
			} else if len(afArr) > 0 {
				return
			}
		}

		// Check for child structure elements — Formula elements that contain
		// structural children (e.g., math content elements) provide the
		// formula content directly and do not require /AF or /Alt.
		if kObj, hasK := elem["K"]; hasK {
			if hasStructuralChildren(ctx, doc, kObj) {
				slog.Debug("compliance_2/accessibility: Formula has structural children", "path", path)
				return
			}
		}

		slog.Debug("compliance_2/accessibility: Formula missing /AF, /Alt, /ActualText, and structural children", "path", path)
		results = append(results, Result{Rule: Rule_14289_2_8_2_5_29_t01, Context: path})
	})

	return results
}

// --- 14. Invisible annotations in structure (PDF/UA-2) ---

// checkInvisibleAnnotArtifact validates that annotations with the Invisible flag
// set are not included in the logical structure tree (they must be artifacts).
// Rule: ISO 14289-2, 8.9.2.2, test 1.
// hasStructuralChildren checks if /K contains at least one child dict with an /S key
// (a structure element), as opposed to only MCR/OBJR/integer references.
func hasStructuralChildren(ctx context.Context, doc *document.Document, kObj ast.PdfObject) bool {
	// Try as array first.
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err == nil {
		for _, item := range arr {
			d, err := document.ResolveDict(ctx, doc, item)
			if err == nil && d != nil {
				if _, hasS := d["S"]; hasS {
					return true
				}
			}
		}
		return false
	}
	// Try as single dict.
	d, err := document.ResolveDict(ctx, doc, kObj)
	if err == nil && d != nil {
		if _, hasS := d["S"]; hasS {
			return true
		}
	}
	return false
}

func checkInvisibleAnnotArtifact(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if profile != "PDF/UA-2" {
		return nil
	}

	slog.Debug("compliance_2/accessibility: checking invisible annotation artifact requirement")

	pages := collectPages(ctx, doc, catalog)
	var results []Result

	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}

		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			continue
		}

		for annotIdx, annotObj := range annots {
			if ctx.Err() != nil {
				return results
			}

			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}

			// Check if annotation has the Invisible flag set.
			fObj, ok := annot["F"]
			if !ok {
				continue
			}
			flags, err := document.ResolveInt(ctx, doc, fObj)
			if err != nil {
				continue
			}

			invisible := int(flags)&annotFlagInvisible != 0
			if !invisible {
				continue
			}

			// Invisible annotation — it must NOT be in the logical structure
			// (i.e., it must not have /StructParent).
			if _, hasStructParent := annot["StructParent"]; hasStructParent {
				subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
				loc := fmt.Sprintf("page %d, annot %d (%s)", pageIdx+1, annotIdx, subtype)
				slog.Debug("compliance_2/accessibility: invisible annotation in structure tree", "loc", loc)
				results = append(results, Result{Rule: Rule_14289_2_8_9_2_2_t01, Context: loc})
			}
		}
	}

	return results
}

// --- 15. File specification /F and /UF (PDF/UA-1 §7.11) ---

// checkPDFUAFileSpecFUF validates that file specification dictionaries for embedded
// files contain both /F and /UF keys (ISO 14289-1, §7.11 t01).
func checkPDFUAFileSpecFUF(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if profile != "PDF/UA-1" {
		return nil
	}

	slog.Debug("compliance_2/accessibility: checking file specification /F and /UF")

	rule := Rule_14289_1_7_11_t01
	var results []Result

	// Walk embedded files from catalog /Names/EmbeddedFiles name tree.
	namesObj, ok := catalog["Names"]
	if !ok {
		slog.Debug("compliance_2/accessibility: no /Names in catalog, skipping file spec check")
		return nil
	}

	namesDict, err := document.ResolveDict(ctx, doc, namesObj)
	if err != nil || namesDict == nil {
		return nil
	}

	efObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		slog.Debug("compliance_2/accessibility: no /EmbeddedFiles in Names dict")
		return nil
	}

	// Walk the name tree to find file spec dicts.
	fileSpecs := collectNameTreeValues(ctx, doc, efObj)
	for i, fsObj := range fileSpecs {
		if ctx.Err() != nil {
			return results
		}

		fsDict, err := document.ResolveDict(ctx, doc, fsObj)
		if err != nil || fsDict == nil {
			continue
		}

		hasF := false
		hasUF := false

		if fObj, ok := fsDict["F"]; ok {
			fStr, err := document.ResolveString(ctx, doc, fObj)
			if err == nil && len(fStr) > 0 {
				hasF = true
			}
		}
		if ufObj, ok := fsDict["UF"]; ok {
			ufStr, err := document.ResolveString(ctx, doc, ufObj)
			if err == nil && len(ufStr) > 0 {
				hasUF = true
			}
		}

		if !hasF || !hasUF {
			slog.Debug("compliance_2/accessibility: file spec missing /F or /UF",
				"index", i, "hasF", hasF, "hasUF", hasUF)
			results = append(results, Result{
				Rule:    rule,
				Context: fmt.Sprintf("file spec %d: F=%v, UF=%v", i, hasF, hasUF),
			})
		}
	}

	if len(results) == 0 {
		slog.Debug("compliance_2/accessibility: all file specs have /F and /UF")
	}
	return results
}

// collectNameTreeValues walks a name tree node and returns all leaf values.
func collectNameTreeValues(ctx context.Context, doc *document.Document, obj ast.PdfObject) []ast.PdfObject {
	return collectNameTreeValuesRecursive(ctx, doc, obj, 0)
}

const maxNameTreeDepth = 50

func collectNameTreeValuesRecursive(ctx context.Context, doc *document.Document, obj ast.PdfObject, depth int) []ast.PdfObject {
	if ctx.Err() != nil || depth > maxNameTreeDepth {
		return nil
	}

	dict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || dict == nil {
		return nil
	}

	var values []ast.PdfObject

	// Check /Names array (leaf node): alternating [name, value, name, value, ...].
	if namesArr, err := document.ResolveArray(ctx, doc, dict["Names"]); err == nil && len(namesArr) > 0 {
		for i := 1; i < len(namesArr); i += 2 {
			values = append(values, namesArr[i])
		}
	}

	// Check /Kids array (intermediate node).
	if kidsArr, err := document.ResolveArray(ctx, doc, dict["Kids"]); err == nil {
		for _, kid := range kidsArr {
			if ctx.Err() != nil {
				return values
			}
			values = append(values, collectNameTreeValuesRecursive(ctx, doc, kid, depth+1)...)
		}
	}

	return values
}

// --- Structure tree walker ---

// maxStructTreeWalkDepth limits recursion in structure tree traversal.
const maxStructTreeWalkDepth = 200

// walkStructureElements recursively walks the structure tree, calling fn for
// each structure element dict encountered.
func walkStructureElements(ctx context.Context, doc *document.Document, root ast.PdfDict, fn func(elem ast.PdfDict, path string)) {
	kidsObj, ok := root["K"]
	if !ok {
		return
	}
	walkStructKids(ctx, doc, kidsObj, "root", 0, fn)
}

func walkStructKids(ctx context.Context, doc *document.Document, obj ast.PdfObject, path string, depth int, fn func(elem ast.PdfDict, path string)) {
	if ctx.Err() != nil || depth > maxStructTreeWalkDepth {
		return
	}

	// /K can be a single element or an array.
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		// Try as a single dict (structure element).
		dict, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || dict == nil {
			return
		}
		visitStructElem(ctx, doc, dict, path, depth, fn)
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
		visitStructElem(ctx, doc, kidDict, childPath, depth, fn)
	}
}

func visitStructElem(ctx context.Context, doc *document.Document, elem ast.PdfDict, path string, depth int, fn func(elem ast.PdfDict, path string)) {
	// Only visit dicts that look like structure elements (have /S key).
	if _, hasS := elem["S"]; hasS {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		elemPath := fmt.Sprintf("%s/%s", path, string(typeName))
		fn(elem, elemPath)

		// Recurse into children.
		if kidsObj, ok := elem["K"]; ok {
			walkStructKids(ctx, doc, kidsObj, elemPath, depth+1, fn)
		}
	}
}

// hasNonEmptyStringKey checks if a dict has a non-empty string value for the given key.
func hasNonEmptyStringKey(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) bool {
	obj, ok := dict[key]
	if !ok {
		return false
	}
	str, err := document.ResolveString(ctx, doc, obj)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(str)) != ""
}

// --- XMP helpers ---

// readCatalogXMP reads and decodes the /Metadata stream from the catalog.
func readCatalogXMP(ctx context.Context, doc *document.Document, catalog ast.PdfDict) ([]byte, bool) {
	metaObj, ok := catalog["Metadata"]
	if !ok {
		return nil, false
	}

	stream, err := document.ResolveStream(ctx, doc, metaObj)
	if err != nil {
		slog.Debug("compliance_2/accessibility: failed to resolve /Metadata stream", "error", err)
		return nil, false
	}

	data, err := doc.DecodeStream(stream)
	if err != nil {
		// Fallback to raw bytes if decode fails (metadata streams are often uncompressed).
		slog.Debug("compliance_2/accessibility: doc.DecodeStream failed, trying raw", "error", err)
		data, err = stream.Bytes()
		if err != nil {
			slog.Debug("compliance_2/accessibility: failed to read raw metadata stream", "error", err)
			return nil, false
		}
	}

	if len(data) == 0 {
		return nil, false
	}

	return data, true
}

// containsDCTitle checks if XMP data contains a dc:title element.
func containsDCTitle(xmpData []byte) bool {
	// Look for dc:title in XMP (simple substring search).
	return strings.Contains(string(xmpData), "dc:title") ||
		strings.Contains(string(xmpData), "<dc:title>") ||
		strings.Contains(string(xmpData), "dc:title>")
}

// containsPDFUAIDPart checks if XMP data contains pdfuaid:part with the given value.
func containsPDFUAIDPart(xmpData []byte, part string) bool {
	xmp := string(xmpData)

	// Look for <pdfuaid:part>N</pdfuaid:part> patterns.
	needle := fmt.Sprintf("pdfuaid:part>%s<", part)
	if strings.Contains(xmp, needle) {
		return true
	}

	// Also check attribute form: pdfuaid:part="N".
	needle = fmt.Sprintf("pdfuaid:part=\"%s\"", part)
	return strings.Contains(xmp, needle)
}

// --- Rule selectors ---

func langRequiredRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_4_4_t01
	}
	// PDF/UA-1 §7.2: natural language for text in page content shall be determined.
	return Rule_14289_1_7_2_t34
}

func langValidRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_4_4_t02
	}
	// PDF/UA-1 §7.2 t29: /Lang values shall be valid RFC 3066 tags.
	return Rule_14289_1_7_2_t29
}

func displayDocTitleRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_11_2_t01
	}
	return Rule_14289_1_7_1_t10
}

func suspectsRule(profile string) Rule {
	// Both PDF/UA-1 and PDF/UA-2 share the same Suspects requirement.
	return Rule_14289_1_7_1_t04
}

func tabOrderRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_9_3_3_t01
	}
	return Rule_14289_1_7_18_3_t01
}

func xmpTitleRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_11_1_t01
	}
	return Rule_14289_1_7_1_t09
}

func figureAltRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_2_5_28_2_t01
	}
	return Rule_14289_1_7_3_t01
}

func annotAltRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_9_2_3_t01
	}
	return Rule_14289_1_7_18_1_t02
}

func ocConfigNameRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_7_t01
	}
	return Rule_14289_1_7_10_t01
}
