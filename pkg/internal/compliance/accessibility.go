package compliance

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// checkAccessibilityGroup validates PDF/UA accessibility requirements.
// For non-PDF/UA profiles, it returns nil immediately.
func checkAccessibilityGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !isPDFUA(profile) {
		slog.Debug("compliance.accessibility: skipping for non-PDF/UA profile", "profile", profile)
		return nil
	}

	slog.Debug("compliance.accessibility: starting checks", "profile", profile)

	results := make([]Violation, 0, 16) //nolint:mnd // estimated capacity for accessibility checks.

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

	// 10. PDF/UA identification — XMP pdfuaid namespace validation.
	results = append(results, checkPDFUAIdentification(ctx, doc, catalog, profile)...)

	// 10b. PDF/UA-id property prefix validation (§5 t4/t5).
	results = append(results, checkPDFUAIDPrefixes(ctx, doc, catalog, profile)...)

	// 11. Namespaces (PDF/UA-2) — /StructTreeRoot must have /Namespaces array.
	results = append(results, checkNamespaces(ctx, doc, catalog, profile)...)

	// 11b. Namespace role mapping validation (PDF/UA-2 §8.2.4).
	results = append(results, checkNamespaceRoleMapping(ctx, doc, catalog, profile)...)

	// 12. Link nesting (PDF/UA-2) — Link annotations must be nested in Link structure elements.
	results = append(results, checkLinkNesting(ctx, doc, catalog, profile)...)

	// 13. Formula MathML (PDF/UA-2) — Formula structure elements must have /AF with MathML.
	results = append(results, checkFormulaMathML(ctx, doc, catalog, profile)...)

	// 14. Invisible annotations in structure (PDF/UA-2) — annotations with Invisible flag must be artifacts.
	results = append(results, checkInvisibleAnnotArtifact(ctx, doc, catalog, profile)...)

	// 15. File specification /F and /UF (PDF/UA-1) — §7.11 t01.
	results = append(results, checkPDFUAFileSpecFUF(ctx, doc, catalog, profile)...)

	// 16. Form field TU key (PDF/UA-1 §7.18.1 t03).
	results = append(results, checkFormFieldTU(ctx, doc, catalog, profile)...)

	// 17. Link annotation Contents (PDF/UA-1 §7.18.5 t02).
	results = append(results, checkLinkContents(ctx, doc, catalog, profile)...)

	// 18. Media clip data CT and Alt (PDF/UA-1 §7.18.6.2 t01/t02).
	results = append(results, checkMediaClipData(ctx, doc, catalog, profile)...)

	// 19. Content marking (PDF/UA-1 §7.1 t3).
	results = append(results, checkContentMarking(ctx, doc, catalog, profile)...)

	// 20. Encryption permissions (PDF/UA-1 §7.16 t01).
	results = append(results, checkEncryptionPermissions(doc, profile)...)

	// 21. Annotation structure nesting (PDF/UA-1 §7.18.x).
	results = append(results, checkAnnotationNesting(ctx, doc, catalog, profile)...)

	// 22. Form XObject restrictions (PDF/UA-1 §7.20).
	results = append(results, checkFormXObjectRestrictions(ctx, doc, catalog, profile)...)

	// 23. PDF/UA-2 form structure checks (§8.10.1, §8.10.2.3).
	results = append(results, checkPDFUA2FormStructure(ctx, doc, catalog, profile)...)

	// 24. Structure destinations (PDF/UA-2 §8.8).
	results = append(results, checkStructureDestinations(ctx, doc, catalog, profile)...)

	slog.Debug("compliance.accessibility: checks complete", "violations", len(results))
	return results
}

// --- 1. Language ---

// checkLangRequired ensures the catalog has a /Lang key with a valid RFC 3066 tag.
// Rule mapping:
//   - Missing /Lang or empty /Lang in catalog -> langRequiredRule (t34 for PDF/UA-1, 8.4.4 t01 for PDF/UA-2)
//   - Present but invalid /Lang format        -> langValidRule (t29 for PDF/UA-1, 8.4.4 t02 for PDF/UA-2)
func checkLangRequired(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	slog.Debug("compliance.accessibility: checking /Lang requirement")

	langRule := langRequiredRule(profile)
	validRule := langValidRule(profile)

	catalogLang := resolveNonEmptyLang(ctx, doc, catalog)

	if catalogLang == "" {
		// PDF/UA-2 §8.4.4 requires catalog /Lang explicitly.
		if profile == "PDF/UA-2" {
			slog.Debug("compliance.accessibility: catalog missing /Lang key (required for PDF/UA-2)")
			return fail(langRule, "catalog")
		}

		// PDF/UA-1 §7.2 t34: natural language must be determinable. If catalog
		// /Lang is absent, the language can still be determined through the
		// structure tree — the tree root or individual elements may carry /Lang.
		slog.Debug("compliance.accessibility: catalog missing /Lang, checking structure tree inheritance")
		return checkStructureTreeLangCoverage(ctx, doc, catalog, langRule, validRule)
	}

	slog.Debug("compliance.accessibility: catalog /Lang value", "lang", catalogLang)

	if !isValidLangTag(catalogLang) {
		slog.Debug("compliance.accessibility: catalog /Lang is invalid BCP 47", "lang", catalogLang)
		return failf(validRule, "lang=%s", catalogLang)
	}

	slog.Debug("compliance.accessibility: catalog /Lang is valid, checking structure elements")

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
func checkStructureTreeLangCoverage(ctx context.Context, doc *document.Document, catalog ast.PdfDict, langRule, validRule Rule) []Violation {
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		// No structure tree at all — can't determine language for anything.
		slog.Debug("compliance.accessibility: no StructTreeRoot, language cannot be determined")
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
			// the tree — language is truly not determinable.
			slog.Debug("compliance.accessibility: structure element has no determinable language",
				"path", path)
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
func validateStructureLangs(ctx context.Context, doc *document.Document, catalog ast.PdfDict, rule Rule) []Violation {
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		slog.Debug("compliance.accessibility: no StructTreeRoot for lang validation")
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
			slog.Debug("compliance.accessibility: cannot resolve /Lang in structure element",
				"path", path, "error", err)
			return
		}
		trimmed := strings.TrimSpace(document.DecodePdfString(langStr))
		if trimmed == "" {
			// Empty /Lang in structure element — not an invalid-format error.
			// The "language not determined" checks (t21-t24, t30-t34) handle this case separately.
			slog.Debug("compliance.accessibility: empty /Lang in structure element, skipping format check",
				"path", path)
			return
		}
		if !isValidLangTag(trimmed) {
			slog.Debug("compliance.accessibility: invalid /Lang in structure element",
				"lang", trimmed, "path", path)
			results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("lang=%s, %s", trimmed, path)})
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

func checkDisplayDocTitle(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	slog.Debug("compliance.accessibility: checking /DisplayDocTitle")

	rule := displayDocTitleRule(profile)

	vpObj, ok := catalog["ViewerPreferences"]
	if !ok {
		slog.Debug("compliance.accessibility: catalog missing /ViewerPreferences")
		return fail(rule, "ViewerPreferences missing")
	}

	vp, err := document.ResolveDict(ctx, doc, vpObj)
	if err != nil || vp == nil {
		slog.Debug("compliance.accessibility: cannot resolve /ViewerPreferences", "error", err)
		return fail(rule, "ViewerPreferences unresolvable")
	}

	ddtObj, ok := vp["DisplayDocTitle"]
	if !ok {
		slog.Debug("compliance.accessibility: /DisplayDocTitle not present")
		return fail(rule, "DisplayDocTitle missing")
	}

	ddt, err := document.ResolveBool(ctx, doc, ddtObj)
	if err != nil || !bool(ddt) {
		slog.Debug("compliance.accessibility: /DisplayDocTitle is not true", "value", ddt)
		return fail(rule, "DisplayDocTitle=false")
	}

	slog.Debug("compliance.accessibility: /DisplayDocTitle is true")
	return nil
}

// --- 3. Suspects ---

func checkSuspects(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	slog.Debug("compliance.accessibility: checking /Suspects")

	rule := suspectsRule(profile)

	miObj, ok := catalog["MarkInfo"]
	if !ok {
		slog.Debug("compliance.accessibility: catalog missing /MarkInfo (Suspects check passes)")
		return nil
	}

	mi, err := document.ResolveDict(ctx, doc, miObj)
	if err != nil || mi == nil {
		slog.Debug("compliance.accessibility: cannot resolve /MarkInfo", "error", err)
		return nil
	}

	susObj, ok := mi["Suspects"]
	if !ok {
		slog.Debug("compliance.accessibility: /Suspects absent (OK)")
		return nil
	}

	sus, err := document.ResolveBool(ctx, doc, susObj)
	if err != nil {
		slog.Debug("compliance.accessibility: cannot resolve /Suspects", "error", err)
		return nil
	}

	if bool(sus) {
		slog.Debug("compliance.accessibility: /Suspects is true")
		return fail(rule, "Suspects=true")
	}

	slog.Debug("compliance.accessibility: /Suspects is false (OK)")
	return nil
}

// --- 4. Tab order ---

func checkTabOrder(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	slog.Debug("compliance.accessibility: checking tab order")

	isPDFUA1 := profile == string("PDF/UA-1")
	isPDFUA2 := profile == string("PDF/UA-2")

	if !isPDFUA1 && !isPDFUA2 {
		return nil
	}

	rule := tabOrderRule(profile)

	pages := collectPages(ctx, doc, catalog)
	var results []Violation

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

		if !hasAnnots {
			// Both PDF/UA-1 and PDF/UA-2 only require /Tabs on pages with annotations.
			continue
		}

		tabsObj, ok := page["Tabs"]
		if !ok {
			if isPDFUA1 {
				// PDF/UA-1 §7.18.3: every page with annotations must have /Tabs = /S.
				slog.Debug("compliance.accessibility: page missing /Tabs", "page", pageIdx)
				results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("page %d, Tabs missing", pageIdx)})
			} else if isPDFUA2 {
				slog.Debug("compliance.accessibility: page missing /Tabs", "page", pageIdx)
				results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("page %d, Tabs missing", pageIdx)})
			}
			continue
		}

		tabsName, err := document.ResolveName(ctx, doc, tabsObj)
		if err != nil {
			slog.Debug("compliance.accessibility: cannot resolve /Tabs", "page", pageIdx, "error", err)
			continue
		}

		if isPDFUA1 && tabsName != "S" {
			slog.Debug("compliance.accessibility: /Tabs is not /S", "page", pageIdx, "tabs", tabsName)
			results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("page %d, Tabs=%s", pageIdx, tabsName)})
		} else if isPDFUA2 && hasAnnots && tabsName != "S" && tabsName != "A" && tabsName != "W" {
			slog.Debug("compliance.accessibility: /Tabs is not /S, /A or /W", "page", pageIdx, "tabs", tabsName)
			results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("page %d, Tabs=%s", pageIdx, tabsName)})
		}
	}

	return results
}

// --- 5. XMP title ---

func checkXMPTitle(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	slog.Debug("compliance.accessibility: checking XMP dc:title")

	rule := xmpTitleRule(profile)

	xmpData, ok := readCatalogXMP(ctx, doc, catalog)
	if !ok {
		slog.Debug("compliance.accessibility: no XMP metadata available")
		return fail(rule, "no XMP metadata")
	}

	if !containsDCTitle(xmpData) {
		slog.Debug("compliance.accessibility: XMP does not contain dc:title")
		return fail(rule, "")
	}

	slog.Debug("compliance.accessibility: XMP contains dc:title")
	return nil
}

// --- 6. Figure alt text ---

func checkFigureAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	slog.Debug("compliance.accessibility: checking Figure alt text")

	rule := figureAltRule(profile)

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		slog.Debug("compliance.accessibility: no StructTreeRoot for Figure alt check")
		return nil
	}

	roleMap := loadRoleMap(ctx, doc, treeRoot)

	var results []Violation
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		// Resolve through RoleMap to catch non-standard types mapped to Figure.
		stdType := resolveStandardType(typeName, roleMap, profile)
		if stdType != "Figure" {
			return
		}

		if profile == "PDF/UA-1" {
			// PDF/UA-1 §7.3: Figure must have non-empty /Alt, or /ActualText (presence suffices).
			hasAlt := hasNonEmptyStringKey(ctx, doc, elem, "Alt")
			hasActualText := hasStringKeyPresent(ctx, doc, elem, "ActualText")
			if !hasAlt && !hasActualText {
				slog.Debug("compliance.accessibility: Figure missing /Alt and /ActualText", "path", path)
				results = append(results, Violation{Rule: rule, Context: path})
			}
		} else {
			// PDF/UA-2 §8.2.5.28.2: Figure must have /Alt, /ActualText, or a /Caption child.
			// Presence (even empty) satisfies the requirement for PDF/UA-2.
			hasAlt := hasStringKeyPresent(ctx, doc, elem, "Alt")
			hasActualText := hasStringKeyPresent(ctx, doc, elem, "ActualText")
			hasCaptionChild := figureHasCaptionChild(ctx, doc, elem)
			if !hasAlt && !hasActualText && !hasCaptionChild {
				slog.Debug("compliance.accessibility: Figure missing /Alt, /ActualText, or Caption", "path", path)
				results = append(results, Violation{Rule: rule, Context: path})
			}
		}
	})

	return results
}

// --- 7. Artifact tagging ---

func checkArtifactTagging(ctx context.Context, _ *document.Document, _ ast.PdfDict, _ string) []Violation {
	// Artifact tagging checks require deep content stream analysis to determine
	// if headers/footers/page numbers are properly marked as Artifact. This is
	// a heuristic check that cannot be fully validated without rendering. We log
	// the check but return no violations — a future enhancement may add
	// heuristic detection of repeated header/footer content.
	slog.Debug("compliance.accessibility: artifact tagging check (heuristic, not enforced)")
	return nil
}

// --- 8. Annotation alt text ---

func checkAnnotationAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	slog.Debug("compliance.accessibility: checking annotation alt text")

	rule := annotAltRule(profile)

	// Build a map from annotation object number to whether the enclosing
	// structure element has a non-empty /Alt entry.
	structAltMap := buildAnnotStructAltMap(ctx, doc, catalog)

	pages := collectPages(ctx, doc, catalog)
	var results []Violation

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
			slog.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
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
				slog.Debug("compliance.accessibility: skipping exempt annotation subtype",
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
				slog.Debug("compliance.accessibility: annotation has empty /Contents",
					"page", pageIdx, "annot", annotIdx, "subtype", string(subtype))
			}

			// Annotation lacks /Contents — check the enclosing structure element for /Alt.
			if annotObjNum > 0 && structAltMap[annotObjNum] {
				slog.Debug("compliance.accessibility: annotation has /Alt via structure element",
					"page", pageIdx, "annot", annotIdx, "objNum", annotObjNum)
				continue
			}

			loc := fmt.Sprintf("page %d, annot %d (/%s)", pageIdx, annotIdx, subtype)
			slog.Debug("compliance.accessibility: annotation missing /Contents and struct /Alt", "loc", loc)
			results = append(results, Violation{Rule: rule, Context: loc})
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
func checkWidgetAA(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	slog.Debug("compliance.accessibility: checking Widget /AA")

	pages := collectPages(ctx, doc, catalog)
	var results []Violation

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
			slog.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
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
					slog.Debug("compliance.accessibility: Widget has /AA without /Contents",
						"loc", loc)
					results = append(results, Violation{
						Rule:    Rule_14289_2_8_10_2_3_t02,
						Context: loc,
					})
				}
			} else {
				// PDF/UA-1: Widget annotations should not have /AA trigger actions.
				slog.Debug("compliance.accessibility: Widget has forbidden /AA", "loc", loc)
				results = append(results, Violation{
					Rule:    Rule_14289_1_7_18_1_t03,
					Context: fmt.Sprintf("widget annotation has /AA at %s", loc),
				})
			}
		}
	}

	return results
}

// --- 9. OC config names ---

func checkOCConfigNames(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	slog.Debug("compliance.accessibility: checking OC config names")

	rule := ocConfigNameRule(profile)

	ocProps, err := document.ResolveDict(ctx, doc, catalog["OCProperties"])
	if err != nil || ocProps == nil {
		slog.Debug("compliance.accessibility: no /OCProperties, skipping OC check")
		return nil
	}

	var results []Violation

	// Check default config (/D).
	dObj, ok := ocProps["D"]
	if ok {
		dConfig, err := document.ResolveDict(ctx, doc, dObj)
		if err == nil && dConfig != nil {
			if !hasNonEmptyName(ctx, doc, dConfig) {
				slog.Debug("compliance.accessibility: default OC config missing /Name")
				results = append(results, Violation{Rule: rule, Context: "default config (D)"})
			}
			// §7.10 t02 (PDF/UA-1): /AS key forbidden in any OC config dict.
			if profile == "PDF/UA-1" {
				if _, hasAS := dConfig["AS"]; hasAS {
					results = append(results, Violation{
						Rule: Rule_14289_1_7_10_t02, Context: "default config (D)",
					})
				}
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
			slog.Debug("compliance.accessibility: OC config missing /Name", "index", i)
			results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("config %d", i)})
		}
		// §7.10 t02 (PDF/UA-1): /AS key forbidden.
		if profile == "PDF/UA-1" {
			if _, hasAS := cfg["AS"]; hasAS {
				results = append(results, Violation{
					Rule: Rule_14289_1_7_10_t02, Context: fmt.Sprintf("config %d", i),
				})
			}
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

func checkPDFUAIdentification(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !isPDFUA(profile) {
		return nil
	}

	xmpData, ok := readCatalogXMP(ctx, doc, catalog)
	if !ok {
		if profile == "PDF/UA-1" {
			return fail(Rule_14289_1_5_t01, "no XMP metadata")
		}
		return fail(Rule_14289_2_5_t01, "no XMP metadata")
	}

	switch profile {
	case "PDF/UA-1":
		if !containsPDFUAIDPart(xmpData, "1") {
			return fail(Rule_14289_1_5_t02, "pdfuaid:part is not 1")
		}
	case "PDF/UA-2":
		if !containsPDFUAIDPart(xmpData, "2") {
			return fail(Rule_14289_2_5_t02, "pdfuaid:part is not 2")
		}
	}

	return nil
}

// checkPDFUAIDPrefixes validates that pdfuaid properties use the correct
// "pdfuaid" namespace prefix and that pdfuaid:rev is a 4-digit year.
func checkPDFUAIDPrefixes(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !isPDFUA(profile) {
		return nil
	}

	xmpData, ok := readCatalogXMP(ctx, doc, catalog)
	if !ok {
		return nil
	}

	// Extract the prefix used for the pdfuaid namespace URI.
	// PDF/UA-2 may use "http://www.aiim.org/pdfua/ns/id/" instead of the
	// PDF/A ID namespace.
	const pdfuaIDNamespace = "http://www.aiim.org/pdfua/ns/id/"
	prefixMap := xmpExtractPrefixMap(xmpData)
	pdfuaidPrefix := prefixMap[xmpPdfaIDNamespace]
	pdfuaPrefix := prefixMap[pdfuaIDNamespace]

	var results []Violation
	raw := string(xmpData)

	if profile == "PDF/UA-1" {
		// t3: "part" property must use "pdfuaid" prefix.
		if !strings.Contains(raw, "pdfuaid:part") {
			// Check if "part" exists under a different prefix.
			if pdfuaidPrefix != "" && pdfuaidPrefix != "pdfuaid" && strings.Contains(raw, pdfuaidPrefix+":part") {
				results = append(results, failf(Rule_14289_1_5_t03,
					"property 'part' uses prefix %q instead of 'pdfuaid'", pdfuaidPrefix)...)
			}
		}
		// t4: "amd" property must use "pdfuaid" prefix.
		if strings.Contains(raw, ":amd") && pdfuaidPrefix != "pdfuaid" {
			// Only flag if "amd" appears with a non-pdfuaid prefix.
			if pdfuaidPrefix != "" && strings.Contains(raw, pdfuaidPrefix+":amd") {
				// The prefix is wrong but the property exists.
			} else if !strings.Contains(raw, "pdfuaid:amd") && strings.Contains(raw, ":amd") {
				results = append(results, failf(Rule_14289_1_5_t04,
					"property 'amd' does not use 'pdfuaid' prefix")...)
			}
		}
		// t5: "corr" property must use "pdfuaid" prefix.
		if strings.Contains(raw, ":corr") && pdfuaidPrefix != "pdfuaid" {
			if !strings.Contains(raw, "pdfuaid:corr") && strings.Contains(raw, ":corr") {
				results = append(results, failf(Rule_14289_1_5_t05,
					"property 'corr' does not use 'pdfuaid' prefix")...)
			}
		}
	}

	if profile == "PDF/UA-2" {
		// t4: "rev" property must use "pdfuaid" prefix.
		// Check both pdfa and pdfua namespace URIs.
		effectivePrefix := pdfuaidPrefix
		if effectivePrefix == "" {
			effectivePrefix = pdfuaPrefix
		}
		if effectivePrefix != "" && effectivePrefix != "pdfuaid" {
			if strings.Contains(raw, effectivePrefix+":rev") {
				results = append(results, failf(Rule_14289_2_5_t04,
					"property 'rev' uses prefix %q instead of 'pdfuaid'", effectivePrefix)...)
			}
		}
		// t5: pdfuaid:rev must be a 4-digit year.
		revValue := extractXMPPropertyValue(raw, "pdfuaid", "rev")
		if revValue == "" && effectivePrefix != "" {
			revValue = extractXMPPropertyValue(raw, effectivePrefix, "rev")
		}
		if revValue != "" && !isFourDigitYear(revValue) {
			results = append(results, failf(Rule_14289_2_5_t05,
				"pdfuaid:rev value %q is not a 4-digit year", revValue)...)
		}
	}

	return results
}

// extractXMPPropertyValue extracts the value of prefix:localName from raw XMP.
// Handles both attribute form (prefix:name="value") and element form (<prefix:name>value</prefix:name>).
func extractXMPPropertyValue(raw, prefix, localName string) string {
	key := prefix + ":" + localName
	// Attribute form: key="value"
	if idx := strings.Index(raw, key+"=\""); idx >= 0 {
		start := idx + len(key) + 2
		end := strings.Index(raw[start:], "\"")
		if end >= 0 {
			return raw[start : start+end]
		}
	}
	// Element form: <key>value</key>
	openTag := "<" + key + ">"
	closeTag := "</" + key + ">"
	if idx := strings.Index(raw, openTag); idx >= 0 {
		start := idx + len(openTag)
		end := strings.Index(raw[start:], closeTag)
		if end >= 0 {
			return strings.TrimSpace(raw[start : start+end])
		}
	}
	return ""
}

func isFourDigitYear(s string) bool {
	if len(s) != 4 { //nolint:mnd
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// --- 11. Namespaces (PDF/UA-2) ---

func checkNamespaces(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	slog.Debug("compliance.accessibility: checking /Namespaces in StructTreeRoot")

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		slog.Debug("compliance.accessibility: no StructTreeRoot for namespace check")
		return nil
	}

	nsObj, ok := treeRoot["Namespaces"]
	if !ok {
		slog.Debug("compliance.accessibility: StructTreeRoot missing /Namespaces")
		return fail(Rule_14289_2_8_2_4_t01, "Namespaces missing from StructTreeRoot")
	}

	nsArray, err := document.ResolveArray(ctx, doc, nsObj)
	if err != nil || len(nsArray) == 0 {
		slog.Debug("compliance.accessibility: /Namespaces is empty or unresolvable", "error", err)
		return fail(Rule_14289_2_8_2_4_t01, "Namespaces empty")
	}

	var results []Violation
	for i, nsEntryObj := range nsArray {
		if ctx.Err() != nil {
			return results
		}

		nsDict, err := document.ResolveDict(ctx, doc, nsEntryObj)
		if err != nil || nsDict == nil {
			slog.Debug("compliance.accessibility: cannot resolve namespace entry", "index", i, "error", err)
			continue
		}

		// Each namespace dict must have /NS (URI).
		nsURIObj, ok := nsDict["NS"]
		if !ok {
			slog.Debug("compliance.accessibility: namespace entry missing /NS", "index", i)
			results = append(results, Violation{
				Rule:    Rule_14289_2_8_2_4_t01,
				Context: fmt.Sprintf("namespace %d missing /NS", i),
			})
			continue
		}

		nsURI, err := document.ResolveString(ctx, doc, nsURIObj)
		if err != nil || string(nsURI) == "" {
			slog.Debug("compliance.accessibility: namespace /NS is empty", "index", i)
			results = append(results, Violation{
				Rule:    Rule_14289_2_8_2_4_t01,
				Context: fmt.Sprintf("namespace %d has empty /NS", i),
			})
		} else {
			slog.Debug("compliance.accessibility: namespace entry valid", "index", i, "ns", string(nsURI))
		}
	}

	return results
}

// --- 11b. Namespace role mapping (PDF/UA-2 §8.2.4) ---

// standardNamespaceURIs lists the namespace URIs that are "standard" per
// ISO 32000-2:2020, 14.8.6.
//
//nolint:gochecknoglobals // static lookup table
var standardNamespaceURIs = map[string]bool{
	"http://iso.org/pdf/ssn":             true, // PDF 1.7
	"http://iso.org/pdf2/ssn":            true, // PDF 2.0
	"http://www.w3.org/1998/Math/MathML": true, // MathML
}

// checkNamespaceRoleMapping validates PDF/UA-2 §8.2.4:
// - t1/t4: all structure elements must belong to or be role-mapped to a standard namespace
// - t3: within a namespace, types shall not be role-mapped to types in the same namespace
func checkNamespaceRoleMapping(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}

	// Collect namespace dicts and their URIs.
	nsArr, _ := document.ResolveArray(ctx, doc, treeRoot["Namespaces"])

	// Build a map of namespace URI → RoleMapNS dict for chain following.
	nsByURI := make(map[string]ast.PdfDict)
	for _, nsObj := range nsArr {
		nsDict, _ := document.ResolveDict(ctx, doc, nsObj)
		if nsDict == nil {
			continue
		}
		uri, _ := document.ResolveString(ctx, doc, nsDict["NS"])
		if len(uri) > 0 {
			rmns, _ := document.ResolveDict(ctx, doc, nsDict["RoleMapNS"])
			if rmns != nil {
				nsByURI[string(uri)] = rmns
			}
		}
	}

	// t3: check each namespace's RoleMapNS. Follow chains through intermediate
	// namespaces — if the final target resolves back to the source namespace
	// (directly or indirectly), it's a same-namespace mapping violation.
	var results []Violation
	for _, nsObj := range nsArr {
		nsDict, _ := document.ResolveDict(ctx, doc, nsObj)
		if nsDict == nil {
			continue
		}
		srcURI, _ := document.ResolveString(ctx, doc, nsDict["NS"])
		rmns, _ := document.ResolveDict(ctx, doc, nsDict["RoleMapNS"])
		if rmns == nil {
			continue
		}
		for typeName, mappingObj := range rmns {
			// t03: same-namespace circular mapping.
			if roleMapChainReturnsSameNS(ctx, doc, string(srcURI), string(typeName), nsByURI) {
				results = append(results, failf(Rule_14289_2_8_2_4_t03,
					"namespace %q: type %s maps back to same namespace through chain",
					string(srcURI), typeName)...)
			}
			// t04: standard type remapped to non-standard.
			if standardNamespaceURIs[string(srcURI)] && isStandardStructType(typeName, "PDF/UA-2") {
				// Check if the target is also in a standard namespace type.
				targetArr, _ := document.ResolveArray(ctx, doc, mappingObj)
				if len(targetArr) >= 2 { //nolint:mnd
					targetNSDict, _ := document.ResolveDict(ctx, doc, targetArr[1])
					if targetNSDict != nil {
						targetURI, _ := document.ResolveString(ctx, doc, targetNSDict["NS"])
						targetType, _ := document.ResolveName(ctx, doc, targetArr[0])
						if !standardNamespaceURIs[string(targetURI)] || !isStandardStructType(targetType, "PDF/UA-2") {
							results = append(results, failf(Rule_14289_2_8_2_4_t04,
								"standard type %s remapped to %s in namespace %q",
								typeName, targetType, string(targetURI))...)
						}
					}
				}
			}
		}
	}

	// t04: also check the StructTreeRoot's old-style /RoleMap for standard types
	// remapped to non-standard types. This applies when the PDF uses legacy RoleMap
	// entries alongside namespace-based structure.
	roleMap, _ := document.ResolveDict(ctx, doc, treeRoot["RoleMap"])
	for src, dstObj := range roleMap {
		if isStandardStructType(ast.PdfName(src), "PDF/UA-2") {
			dstName, _ := document.ResolveName(ctx, doc, dstObj)
			if dstName != "" && !isStandardStructType(dstName, "PDF/UA-2") {
				results = append(results, failf(Rule_14289_2_8_2_4_t04,
					"standard type %s remapped to non-standard type %s in StructTreeRoot /RoleMap",
					src, dstName)...)
			}
		}
	}

	// t1/t4: walk structure tree, check each element resolves to a standard namespace.
	// Elements with no explicit /NS use the first namespace in the array.
	defaultNSURI := ""
	if len(nsArr) > 0 {
		nsDict, _ := document.ResolveDict(ctx, doc, nsArr[0])
		if nsDict != nil {
			ns, _ := document.ResolveString(ctx, doc, nsDict["NS"])
			defaultNSURI = string(ns)
		}
	}

	walkNSCheck(ctx, doc, treeRoot, defaultNSURI, nsArr, &results)
	return results
}

// walkNSCheck recursively walks structure elements and checks namespace membership.
func walkNSCheck(ctx context.Context, doc *document.Document, node ast.PdfDict, defaultNSURI string, nsArr ast.PdfArray, results *[]Violation) {
	if ctx.Err() != nil {
		return
	}

	sType, _ := document.ResolveName(ctx, doc, node["S"])
	if sType != "" {
		// Determine the element's namespace URI.
		elemNSURI := defaultNSURI
		if nsObj, ok := node["NS"]; ok {
			nsDict, _ := document.ResolveDict(ctx, doc, nsObj)
			if nsDict != nil {
				ns, _ := document.ResolveString(ctx, doc, nsDict["NS"])
				if len(ns) > 0 {
					elemNSURI = string(ns)
				}
			}
		}

		// Check if the element's namespace (or role-mapped target) is standard.
		if !standardNamespaceURIs[elemNSURI] {
			// Check if the namespace has a RoleMapNS that maps to a standard NS.
			if !typeMapsToStandardNS(ctx, doc, string(sType), elemNSURI, nsArr) {
				*results = append(*results, failf(Rule_14289_2_8_2_4_t01,
					"structure type %s in namespace %q is not mapped to a standard namespace",
					sType, elemNSURI)...)
				return // One violation is enough per document.
			}
		}
	}

	kObj := node["K"]
	if kObj == nil {
		return
	}
	if arr, _ := document.ResolveArray(ctx, doc, kObj); len(arr) > 0 {
		for _, kid := range arr {
			d, _ := document.ResolveDict(ctx, doc, kid)
			if d != nil {
				walkNSCheck(ctx, doc, d, defaultNSURI, nsArr, results)
			}
		}
	} else if d, _ := document.ResolveDict(ctx, doc, kObj); d != nil {
		walkNSCheck(ctx, doc, d, defaultNSURI, nsArr, results)
	}
}

// typeMapsToStandardNS checks if a structure type in a given namespace has a
// RoleMapNS entry that ultimately resolves to a standard namespace.
func typeMapsToStandardNS(ctx context.Context, doc *document.Document, typeName, nsURI string, nsArr ast.PdfArray) bool {
	// Find the namespace dict for nsURI.
	for _, nsObj := range nsArr {
		nsDict, _ := document.ResolveDict(ctx, doc, nsObj)
		if nsDict == nil {
			continue
		}
		ns, _ := document.ResolveString(ctx, doc, nsDict["NS"])
		if string(ns) != nsURI {
			continue
		}
		rmns, _ := document.ResolveDict(ctx, doc, nsDict["RoleMapNS"])
		if rmns == nil {
			return false
		}
		mappingObj, ok := rmns[ast.PdfName(typeName)]
		if !ok {
			return false
		}
		arr, _ := document.ResolveArray(ctx, doc, mappingObj)
		if len(arr) < 2 { //nolint:mnd
			return false
		}
		targetNSDict, _ := document.ResolveDict(ctx, doc, arr[1])
		if targetNSDict == nil {
			return false
		}
		targetURI, _ := document.ResolveString(ctx, doc, targetNSDict["NS"])
		return standardNamespaceURIs[string(targetURI)]
	}
	return false
}

// roleMapChainReturnsSameNS follows a RoleMapNS chain starting from typeName
// in srcNSURI and returns true if the chain eventually maps back to the
// source namespace (directly or through intermediates).
func roleMapChainReturnsSameNS(ctx context.Context, doc *document.Document, srcNSURI string, typeName string, nsByURI map[string]ast.PdfDict) bool {
	currentType := typeName
	currentNS := srcNSURI
	visited := map[string]bool{currentNS + ":" + currentType: true}

	for range 10 { //nolint:mnd // max chain depth
		rmns, ok := nsByURI[currentNS]
		if !ok {
			return false
		}
		mappingObj, ok := rmns[ast.PdfName(currentType)]
		if !ok {
			return false
		}
		arr, _ := document.ResolveArray(ctx, doc, mappingObj)
		if len(arr) < 2 { //nolint:mnd
			return false
		}
		targetType, _ := document.ResolveName(ctx, doc, arr[0])
		targetNSDict, _ := document.ResolveDict(ctx, doc, arr[1])
		if targetNSDict == nil {
			return false
		}
		targetURI, _ := document.ResolveString(ctx, doc, targetNSDict["NS"])
		targetNS := string(targetURI)

		if targetNS == srcNSURI {
			return true // Chain resolves back to source namespace.
		}

		key := targetNS + ":" + string(targetType)
		if visited[key] {
			return false // Cycle without returning to source NS.
		}
		visited[key] = true
		currentType = string(targetType)
		currentNS = targetNS
	}
	return false
}

// --- 12. Link nesting (PDF/UA-2) ---

func checkLinkNesting(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	slog.Debug("compliance.accessibility: checking link nesting in structure tree")

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		slog.Debug("compliance.accessibility: no StructTreeRoot for link nesting check")
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
			slog.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
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
		slog.Debug("compliance.accessibility: no link annotations found")
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

	slog.Debug("compliance.accessibility: link annotation/structure counts",
		"annotations", linkAnnotCount, "structElements", linkStructCount)

	if linkStructCount == 0 && linkAnnotCount > 0 {
		return fail(Rule_14289_2_8_2_5_20_t01, "link annotations exist but no Link/Reference structure elements found")
	}

	return nil
}

// --- 13. Formula MathML (PDF/UA-2) ---

func checkFormulaMathML(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	slog.Debug("compliance.accessibility: checking Formula structure elements for /AF (MathML)")

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		slog.Debug("compliance.accessibility: no StructTreeRoot for Formula check")
		return nil
	}

	var results []Violation
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
				slog.Debug("compliance.accessibility: Formula has /Alt text", "path", path)
				return
			}
		}
		if atObj, hasAT := elem["ActualText"]; hasAT {
			atStr, err := document.ResolveString(ctx, doc, atObj)
			if err == nil && strings.TrimSpace(string(atStr)) != "" {
				slog.Debug("compliance.accessibility: Formula has /ActualText", "path", path)
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
				slog.Debug("compliance.accessibility: Formula has structural children", "path", path)
				return
			}
		}

		slog.Debug("compliance.accessibility: Formula missing /AF, /Alt, /ActualText, and structural children", "path", path)
		results = append(results, Violation{Rule: Rule_14289_2_8_2_5_29_t01, Context: path})
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

func checkInvisibleAnnotArtifact(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	slog.Debug("compliance.accessibility: checking invisible annotation artifact requirement")

	pages := collectPages(ctx, doc, catalog)
	var results []Violation

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
			slog.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
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
				slog.Debug("compliance.accessibility: cannot resolve annotation flags", "error", err)
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
				slog.Debug("compliance.accessibility: invisible annotation in structure tree", "loc", loc)
				results = append(results, Violation{Rule: Rule_14289_2_8_9_2_2_t01, Context: loc})
			}
		}
	}

	return results
}

// --- 15. File specification /F and /UF (PDF/UA-1 §7.11) ---

// checkPDFUAFileSpecFUF validates that file specification dictionaries for embedded
// files contain both /F and /UF keys (ISO 14289-1, §7.11 t01).
func checkPDFUAFileSpecFUF(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !isPDFUA(profile) {
		return nil
	}

	slog.Debug("compliance.accessibility: checking file specification /F and /UF")

	rule := Rule_14289_1_7_11_t01
	var results []Violation

	// Scan ALL file specification dicts with /EF key (embedded files) for
	// non-empty /F and /UF. This covers file specs referenced from the
	// EmbeddedFiles name tree, FileAttachment annotations, and other sources.
	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			return results
		}
		if entry.Free {
			continue
		}

		dict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || dict == nil {
			continue
		}

		typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
		if typeName != "Filespec" {
			continue
		}
		if _, hasEF := dict["EF"]; !hasEF {
			continue // Only check embedded file specs.
		}

		// PDF/UA-1 §7.11: /F and /UF must both be present and non-empty.
		if profile == "PDF/UA-1" {
			fStr, _ := document.ResolveString(ctx, doc, dict["F"])
			ufStr, _ := document.ResolveString(ctx, doc, dict["UF"])

			if len(fStr) == 0 || len(ufStr) == 0 {
				results = append(results, Violation{
					Rule:    rule,
					Context: fmt.Sprintf("object %d: file spec F=%v UF=%v", objNum, len(fStr) > 0, len(ufStr) > 0),
				})
			}
		}

		// PDF/UA-2 §8.14.1: /Desc must be present on embedded file specs.
		if profile == "PDF/UA-2" {
			descStr, _ := document.ResolveString(ctx, doc, dict["Desc"])
			if len(descStr) == 0 {
				results = append(results, Violation{
					Rule:    Rule_14289_2_8_14_1_t01,
					Context: fmt.Sprintf("object %d: embedded file spec missing /Desc", objNum),
				})
			}
		}
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

// hasStringKeyPresent checks if a dict has a key whose value resolves to a
// string (even an empty string). Returns false if the key is absent, the
// value is null, or it's not a string type.
func hasStringKeyPresent(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) bool {
	obj, ok := dict[key]
	if !ok {
		return false
	}
	_, err := document.ResolveString(ctx, doc, obj)
	return err == nil
}

// figureHasCaptionChild checks if a Figure structure element has a child
// element with /S /Caption. Used for PDF/UA-2 §8.2.5.28.2.
func figureHasCaptionChild(ctx context.Context, doc *document.Document, elem ast.PdfDict) bool {
	kObj, ok := elem["K"]
	if !ok {
		return false
	}
	// /K can be a single dict or an array.
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		// Try as single element.
		child, err := document.ResolveDict(ctx, doc, kObj)
		if err != nil || child == nil {
			return false
		}
		childType, _ := document.ResolveName(ctx, doc, child["S"])
		return childType == "Caption"
	}
	for _, item := range arr {
		child, err := document.ResolveDict(ctx, doc, item)
		if err != nil || child == nil {
			continue
		}
		childType, _ := document.ResolveName(ctx, doc, child["S"])
		if childType == "Caption" {
			return true
		}
	}
	return false
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
		slog.Debug("compliance.accessibility: failed to resolve /Metadata stream", "error", err)
		return nil, false
	}

	data, err := doc.DecodeStream(stream)
	if err != nil {
		// Fallback to raw bytes if decode fails (metadata streams are often uncompressed).
		slog.Debug("compliance.accessibility: doc.DecodeStream failed, trying raw", "error", err)
		data, err = stream.Bytes()
		if err != nil {
			slog.Debug("compliance.accessibility: failed to read raw metadata stream", "error", err)
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

// --- 20. Encryption permissions ---

// checkEncryptionPermissions validates PDF/UA-1 §7.16: if encrypted, the
// /Encrypt dictionary must contain a /P key whose 10th bit is true
// (accessibility content extraction permitted).
func checkEncryptionPermissions(doc *document.Document, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	if !doc.IsEncrypted {
		return nil
	}

	trailer := doc.Index.Trailer
	encObj, ok := trailer["Encrypt"]
	if !ok {
		return fail(Rule_14289_1_7_16_t01, "file is encrypted but no /Encrypt dictionary in trailer")
	}

	// Resolve the Encrypt dict (may be an indirect ref).
	var encDict ast.PdfDict
	switch v := encObj.(type) {
	case ast.PdfDict:
		encDict = v
	case ast.PdfIndirectRef:
		obj, err := doc.Resolve(context.Background(), v)
		if err != nil {
			return fail(Rule_14289_1_7_16_t01, "cannot resolve /Encrypt dictionary")
		}
		d, ok := obj.(ast.PdfDict)
		if !ok {
			return fail(Rule_14289_1_7_16_t01, "resolved /Encrypt is not a dictionary")
		}
		encDict = d
	default:
		return fail(Rule_14289_1_7_16_t01, "/Encrypt is not a dictionary")
	}

	pObj, hasP := encDict["P"]
	if !hasP {
		return fail(Rule_14289_1_7_16_t01, "encrypted file has no /P key in /Encrypt dictionary")
	}

	pVal, ok := pObj.(ast.PdfInt)
	if !ok {
		return fail(Rule_14289_1_7_16_t01, "/P is not an integer")
	}

	// Bit 10 (1-indexed) = bit 9 (0-indexed) = 0x200.
	const accessibilityBit = 1 << 9
	if int(pVal)&accessibilityBit == 0 {
		return failf(Rule_14289_1_7_16_t01,
			"10th bit of /P (%d) is false — accessibility content extraction not permitted", int(pVal))
	}

	return nil
}

// --- 21. Annotation structure nesting ---

// checkAnnotationNesting validates PDF/UA-1 §7.18.x annotation nesting rules:
//   - §7.18.1 t01: Non-exempt annotations must be nested within an Annot tag
//   - §7.18.2 t01: TrapNet annotations are forbidden
//   - §7.18.4 t01: Widget annotations must be nested within a Form tag
//   - §7.18.5 t01: Link annotations must be nested within a Link tag
//   - §7.18.8 t01: PrinterMark annotations must NOT be in the logical structure
func checkAnnotationNesting(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	// Build map: annotation objNum -> enclosing structure element standard type.
	structParentMap := buildAnnotStructTypeMap(ctx, doc, catalog, profile)

	pages := collectPages(ctx, doc, catalog)
	var results []Violation

	for _, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}

		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			slog.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
			continue
		}

		for _, annotObj := range annots {
			if ctx.Err() != nil {
				return results
			}

			annotObjNum := 0
			if ref, ok := annotObj.(ast.PdfIndirectRef); ok {
				annotObjNum = ref.ObjectNum
			}

			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}

			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])

			// Skip hidden or outside-crop-box annotations (all rules share these exemptions).
			if isAnnotHidden(ctx, doc, annot) || isAnnotOutsideCropBox(ctx, doc, annot, page) {
				continue
			}

			// Skip Popup annotations — they are auxiliary and don't have structure requirements.
			if subtype == "Popup" {
				continue
			}

			stdType := structParentMap[annotObjNum] // "" if not in structure tree

			switch subtype {
			case "TrapNet":
				// §7.18.2: TrapNet annotations are forbidden.
				results = append(results, fail(Rule_14289_1_7_18_2_t01,
					"TrapNet annotation exists")...)

			case "PrinterMark":
				// §7.18.8: PrinterMark must NOT be in logical structure.
				if stdType != "" {
					results = append(results, fail(Rule_14289_1_7_18_8_t01,
						"PrinterMark annotation is included in logical structure")...)
				}

			case "Widget":
				// §7.18.4: Widget must be nested within Form tag.
				if stdType != "Form" {
					results = append(results, failf(Rule_14289_1_7_18_4_t01,
						"Widget annotation nested within %s instead of Form", nestingDesc(stdType))...)
				}

			case "Link":
				// §7.18.5: Link must be nested within Link tag.
				if stdType != "Link" {
					results = append(results, failf(Rule_14289_1_7_18_5_t01,
						"Link annotation nested within %s instead of Link", nestingDesc(stdType))...)
				}

			default:
				// §7.18.1: All other annotations must be nested within Annot tag.
				if stdType != "Annot" {
					results = append(results, failf(Rule_14289_1_7_18_1_t01,
						"%s annotation nested within %s instead of Annot", subtype, nestingDesc(stdType))...)
				}
			}
		}
	}

	return results
}

// nestingDesc returns a human-readable description for the annotation's
// enclosing structure element standard type, or "Artifact" if not in structure.
func nestingDesc(stdType string) string {
	if stdType == "" {
		return "Artifact"
	}
	return stdType
}

// buildAnnotStructTypeMap walks the structure tree and returns a map from
// annotation object number to the standard type name of the enclosing structure
// element. Annotations not referenced by any structure element are absent.
func buildAnnotStructTypeMap(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) map[int]string {
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}

	// Load role map for resolving non-standard types to standard types.
	roleMap := loadRoleMap(ctx, doc, treeRoot)

	result := make(map[int]string)
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, _ string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		if typeName == "" {
			return
		}

		stdType := resolveStandardType(typeName, roleMap, profile)

		kObj, ok := elem["K"]
		if !ok {
			return
		}
		collectOBJRWithType(ctx, doc, kObj, string(stdType), result)
	})

	return result
}

// loadRoleMap loads the /RoleMap dictionary from the structure tree root.
func loadRoleMap(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict) map[ast.PdfName]ast.PdfName {
	rmObj, ok := structTreeRoot["RoleMap"]
	if !ok {
		return nil
	}
	rm, err := document.ResolveDict(ctx, doc, rmObj)
	if err != nil || rm == nil {
		return nil
	}
	mapping := make(map[ast.PdfName]ast.PdfName, len(rm))
	for key, val := range rm {
		targetName, err := document.ResolveName(ctx, doc, val)
		if err == nil && targetName != "" {
			mapping[key] = targetName
		}
	}
	return mapping
}

// resolveStandardType follows the role map chain to find the standard type.
// Returns the original name if already standard or if no mapping exists.
func resolveStandardType(name ast.PdfName, roleMap map[ast.PdfName]ast.PdfName, profile string) ast.PdfName {
	if isStandardStructType(name, profile) {
		return name
	}
	visited := map[ast.PdfName]bool{name: true}
	current := name
	for range 50 { //nolint:mnd // depth limit for role map chain
		target, ok := roleMap[current]
		if !ok {
			return current
		}
		if isStandardStructType(target, profile) {
			return target
		}
		if visited[target] {
			return current // circular
		}
		visited[target] = true
		current = target
	}
	return current
}

// collectOBJRWithType finds OBJR dicts in /K children and records the annotation
// object numbers with their enclosing element's standard type.
func collectOBJRWithType(ctx context.Context, doc *document.Document, kObj ast.PdfObject, stdType string, dest map[int]string) {
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		recordOBJRType(ctx, doc, kObj, stdType, dest)
		return
	}
	for _, item := range arr {
		recordOBJRType(ctx, doc, item, stdType, dest)
	}
}

// recordOBJRType checks if obj is an OBJR dict and records the annotation
// object number with the given standard type.
func recordOBJRType(ctx context.Context, doc *document.Document, obj ast.PdfObject, stdType string, dest map[int]string) {
	dict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || dict == nil {
		return
	}
	typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
	if typeName != "OBJR" {
		return
	}
	if ref, ok := dict["Obj"].(ast.PdfIndirectRef); ok {
		dest[ref.ObjectNum] = stdType
	}
}

// --- 22. Form XObject restrictions (PDF/UA-1 §7.20) ---

// checkFormXObjectRestrictions validates PDF/UA-1 §7.20:
//   - t01: Reference XObjects are forbidden (Form XObject with /Ref key)
//   - t02: Form XObjects with MCIDs must not be referenced more than once
func checkFormXObjectRestrictions(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	var results []Violation
	checkedForRef := make(map[int]bool)

	// Get inherited resources from the Pages tree root (fallback for pages
	// without their own /Resources).
	pagesDict, _ := document.ResolveDict(ctx, doc, catalog["Pages"])
	inheritedRes := getResources(ctx, doc, pagesDict)

	// t01: Scan XObject resources for /Ref key (reference XObjects forbidden).
	// t02: Count Do operator invocations per Form XObject across all pages.
	doRefCounts := make(map[int]int) // Form XObject objNum -> Do invocation count

	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		if ctx.Err() != nil {
			return results
		}
		res := getResources(ctx, doc, page)
		if res == nil {
			res = inheritedRes
		}
		if res == nil {
			continue
		}

		// t01: check for /Ref in Form XObjects (only once per object).
		scanFormXObjectsUA(ctx, doc, res, &results, checkedForRef, 0)

		// t02: parse content stream for Do operators, resolve names to objNums.
		xobjDict, _ := document.ResolveDict(ctx, doc, res["XObject"])
		if xobjDict == nil {
			continue
		}
		streamData, err := getPageContentBytes(ctx, doc, page)
		if err != nil || len(streamData) == 0 {
			continue
		}
		ops, err := content.ParseStreamData(streamData)
		if err != nil {
			slog.Debug("compliance.accessibility: cannot parse content stream", "error", err)
			continue
		}
		for _, op := range ops {
			if op.Operator != "Do" || len(op.Operands) < 1 {
				slog.Debug("compliance.accessibility: cannot parse content stream", "error", err)
				continue
			}
			name, ok := op.Operands[0].(ast.PdfName)
			if !ok {
				continue
			}
			xobjRef, ok := xobjDict[name].(ast.PdfIndirectRef)
			if !ok {
				continue
			}
			doRefCounts[xobjRef.ObjectNum]++
		}
	}

	// t02: Form XObjects with MCIDs must not be referenced more than once.
	for objNum, count := range doRefCounts {
		if count <= 1 {
			continue
		}
		// Check if this Form XObject contains MCIDs (has /StructParents).
		stream, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			slog.Debug("compliance.accessibility: cannot resolve form XObject stream", "error", err)
			continue
		}
		if _, hasSP := stream.Dict["StructParents"]; hasSP {
			results = append(results, failf(Rule_14289_1_7_20_t02,
				"Form XObject (obj %d) contains MCIDs and is referenced %d times", objNum, count)...)
		}
	}

	return results
}

// scanFormXObjectsUA scans XObject resources for PDF/UA-1 §7.20 t01: /Ref
// key forbidden. XObjects are streams, so we use ResolveStream.
// checkedForRef prevents reporting the same object twice.
func scanFormXObjectsUA(ctx context.Context, doc *document.Document, res ast.PdfDict, results *[]Violation, checkedForRef map[int]bool, depth int) {
	if depth > 10 || ctx.Err() != nil {
		return
	}

	xobjectsObj, ok := res["XObject"]
	if !ok {
		return
	}
	xobjects, err := document.ResolveDict(ctx, doc, xobjectsObj)
	if err != nil || xobjects == nil {
		return
	}

	for _, xobjObj := range xobjects {
		ref, isRef := xobjObj.(ast.PdfIndirectRef)
		if isRef && checkedForRef[ref.ObjectNum] {
			continue
		}

		stream, err := document.ResolveStream(ctx, doc, xobjObj)
		if err != nil {
			slog.Debug("compliance.accessibility: cannot resolve XObject stream", "error", err)
			continue
		}
		xobjDict := stream.Dict
		if xobjDict == nil {
			slog.Debug("compliance.accessibility: cannot resolve XObject stream", "error", err)
			continue
		}
		subtype, _ := document.ResolveName(ctx, doc, xobjDict["Subtype"])
		if subtype != "Form" {
			continue
		}

		if isRef {
			checkedForRef[ref.ObjectNum] = true
		}

		// t01: /Ref key forbidden (reference XObject).
		if _, hasRef := xobjDict["Ref"]; hasRef {
			*results = append(*results, fail(Rule_14289_1_7_20_t01,
				"Form XObject contains /Ref key (reference XObject)")...)
		}

		// Recurse into sub-resources.
		subRes, _ := document.ResolveDict(ctx, doc, xobjDict["Resources"])
		if subRes != nil {
			scanFormXObjectsUA(ctx, doc, subRes, results, checkedForRef, depth+1)
		}
	}
}

// --- 23. PDF/UA-2 form structure checks ---

// checkPDFUA2FormStructure validates:
//   - §8.10.1 t02: Form structure element contains at most one widget annotation.
//   - §8.10.2.3 t01: Widget without label needs /Contents entry.
func checkPDFUA2FormStructure(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	var results []Violation

	// §8.10.1 t02: walk structure tree for Form elements with >1 widget OBJR.
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err == nil && treeRoot != nil {
		walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
			typeName, _ := document.ResolveName(ctx, doc, elem["S"])
			if typeName != "Form" {
				return
			}
			// Count OBJR children pointing to widget annotations.
			widgetCount := countOBJRWidgets(ctx, doc, elem)
			if widgetCount > 1 {
				results = append(results, failf(Rule_14289_2_8_10_1_t02,
					"Form structure element at %s contains %d widget annotations", path, widgetCount)...)
			}
		})
	}

	// §8.10.2.3 t01: Widget annotations without label need /Contents.
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		annotsArr, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for _, annotObj := range annotsArr {
			annot, _ := document.ResolveDict(ctx, doc, annotObj)
			if annot == nil {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if subtype != "Widget" {
				continue
			}
			if isAnnotHidden(ctx, doc, annot) {
				continue
			}
			// Check if widget has a /Contents entry.
			if contentsObj, ok := annot["Contents"]; ok {
				contentsStr, err := document.ResolveString(ctx, doc, contentsObj)
				if err == nil && len(contentsStr) > 0 {
					continue // has Contents
				}
			}
			// Check if the widget's parent field has a label (via /TU or sibling text).
			// For simplicity, we only check /Contents presence on the widget itself.
			results = append(results, fail(Rule_14289_2_8_10_2_3_t01,
				"Widget annotation has no label and no /Contents entry")...)
		}
	}

	return results
}

// countOBJRWidgets counts OBJR children of a structure element that point to
// Widget annotations.
func countOBJRWidgets(ctx context.Context, doc *document.Document, elem ast.PdfDict) int {
	kObj, ok := elem["K"]
	if !ok {
		return 0
	}
	count := 0
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		// Single child.
		if isWidgetOBJR(ctx, doc, kObj) {
			return 1
		}
		return 0
	}
	for _, item := range arr {
		if isWidgetOBJR(ctx, doc, item) {
			count++
		}
	}
	return count
}

func isWidgetOBJR(ctx context.Context, doc *document.Document, obj ast.PdfObject) bool {
	d, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || d == nil {
		return false
	}
	typeName, _ := document.ResolveName(ctx, doc, d["Type"])
	if typeName != "OBJR" {
		return false
	}
	// Resolve the referenced object and check if it's a Widget.
	ref, ok := d["Obj"].(ast.PdfIndirectRef)
	if !ok {
		return false
	}
	annot, err := document.ResolveDict(ctx, doc, ref)
	if err != nil || annot == nil {
		return false
	}
	subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
	return subtype == "Widget"
}

// --- 24. Structure destinations (PDF/UA-2 §8.8) ---

// checkStructureDestinations validates PDF/UA-2 §8.8:
//   - t01: Destinations in outline items, OpenAction, and Link annotations
//     must be structure destinations (array starting with struct element ref).
//   - t02: GoTo action destinations must be structure destinations.
func checkStructureDestinations(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	var results []Violation

	// Check outline destinations (t01 for /Dest, t02 for /A GoTo).
	outlinesObj, ok := catalog["Outlines"]
	if ok {
		outlines, _ := document.ResolveDict(ctx, doc, outlinesObj)
		if outlines != nil {
			walkOutlineStructDest(ctx, doc, outlines["First"], &results, 0)
		}
	}

	// Check catalog /OpenAction (t01).
	if oaObj, ok := catalog["OpenAction"]; ok {
		if !isStructureDestination(ctx, doc, oaObj) {
			// Could be an action dict — check if GoTo.
			oaDict, _ := document.ResolveDict(ctx, doc, oaObj)
			if oaDict != nil {
				if s, _ := document.ResolveName(ctx, doc, oaDict["S"]); s == "GoTo" {
					if dObj, hasDest := oaDict["D"]; hasDest {
						if !isStructureDestination(ctx, doc, dObj) {
							results = append(results, fail(Rule_14289_2_8_8_t02,
								"OpenAction GoTo destination is not a structure destination")...)
						}
					}
				}
			} else {
				// Direct destination array.
				results = append(results, fail(Rule_14289_2_8_8_t01,
					"OpenAction destination is not a structure destination")...)
			}
		}
	}

	return results
}

// walkOutlineStructDest walks outline items checking destinations.
func walkOutlineStructDest(ctx context.Context, doc *document.Document, obj ast.PdfObject, results *[]Violation, depth int) {
	if depth > 200 || ctx.Err() != nil { //nolint:mnd
		return
	}
	item, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || item == nil {
		return
	}

	// Check /Dest on outline item (t01).
	if destObj, hasDest := item["Dest"]; hasDest {
		if !isStructureDestination(ctx, doc, destObj) {
			*results = append(*results, fail(Rule_14289_2_8_8_t01,
				"Outline item destination is not a structure destination")...)
		}
	}

	// Check /A action on outline item.
	if aObj, hasA := item["A"]; hasA {
		aDict, _ := document.ResolveDict(ctx, doc, aObj)
		if aDict != nil {
			s, _ := document.ResolveName(ctx, doc, aDict["S"])
			if s == "GoTo" {
				if dObj, hasDest := aDict["D"]; hasDest {
					if !isStructureDestination(ctx, doc, dObj) {
						*results = append(*results, fail(Rule_14289_2_8_8_t02,
							"Outline GoTo action destination is not a structure destination")...)
					}
				}
			}
		}
	}

	// Recurse: children then siblings.
	walkOutlineStructDest(ctx, doc, item["First"], results, depth+1)
	walkOutlineStructDest(ctx, doc, item["Next"], results, depth)
}

// isStructureDestination checks if a destination object is a "structure
// destination" per PDF 2.0 — an array whose first element references a
// structure element (has /Type /StructElem or /S key) rather than a page.
func isStructureDestination(ctx context.Context, doc *document.Document, destObj ast.PdfObject) bool {
	// Resolve named destination if it's a string/name.
	if nameObj, err := document.ResolveName(ctx, doc, destObj); err == nil && nameObj != "" {
		// Named destination — look up in /Names/Dests.
		// For simplicity, assume named destinations are not structure destinations.
		return false
	}

	arr, err := document.ResolveArray(ctx, doc, destObj)
	if err != nil || len(arr) == 0 {
		return false
	}

	// The first element of a structure destination is a reference to a
	// structure element dict (has /S key), not a page dict (has /Type /Page).
	firstDict, err := document.ResolveDict(ctx, doc, arr[0])
	if err != nil || firstDict == nil {
		return false
	}

	// Structure elements have /S key.
	if _, hasS := firstDict["S"]; hasS {
		return true
	}

	// Also check /Type /StructElem explicitly.
	typeName, _ := document.ResolveName(ctx, doc, firstDict["Type"])
	return typeName == "StructElem"
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

// --- 17. Link annotation Contents ---

// checkLinkContents validates PDF/UA-1 §7.18.5 t2: Link annotations must
// have a non-empty /Contents key providing an alternate description.
func checkLinkContents(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	pages := collectPages(ctx, doc, catalog)
	var results []Violation
	for _, page := range pages {
		if ctx.Err() != nil {
			return results
		}
		annotsArr, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for _, annotObj := range annotsArr {
			annot, _ := document.ResolveDict(ctx, doc, annotObj)
			if annot == nil {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if subtype != "Link" {
				continue
			}
			contents, _ := document.ResolveString(ctx, doc, annot["Contents"])
			if len(contents) == 0 {
				results = append(results, failf(Rule_14289_1_7_18_5_t02,
					"Link annotation without non-empty /Contents")...)
				return results
			}
		}
	}
	return results
}

// --- 18. Media clip data ---

// checkMediaClipData validates PDF/UA-1 §7.18.6.2: media clip data dicts
// must have /CT (content type, t01) and /Alt (alternative text, t02).
// Finds media clips via Screen annotation → Rendition action → clip → data.
func checkMediaClipData(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	var results []Violation
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		annotsArr, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for _, annotObj := range annotsArr {
			annot, _ := document.ResolveDict(ctx, doc, annotObj)
			if annot == nil {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if subtype != "Screen" {
				continue
			}
			// Find the media clip dict. Two structures are used:
			//   (a) annot /A → rendition action /R → /C (clip dict)
			//   (b) annot /A → rendition action /C (clip dict directly)
			aDict, _ := document.ResolveDict(ctx, doc, annot["A"])
			if aDict == nil {
				continue
			}
			clipDict := resolveMediaClipDict(ctx, doc, aDict)
			if clipDict == nil {
				continue
			}

			// t01: /CT key must be present on the clip dict.
			if _, hasCT := clipDict["CT"]; !hasCT {
				results = append(results, failf(Rule_14289_1_7_18_6_2_t01,
					"media clip missing /CT key")...)
			}
			// t02: /Alt key must be present with valid content.
			if !hasValidMediaClipAlt(ctx, doc, clipDict) {
				results = append(results, failf(Rule_14289_1_7_18_6_2_t02,
					"media clip missing or invalid /Alt key")...)
			}
		}
	}
	return results
}

// resolveMediaClipDict finds the media clip dictionary from a rendition action.
// Tries /R → /C first (rendition dict with clip), then /C directly on the action.
func resolveMediaClipDict(ctx context.Context, doc *document.Document, actionDict ast.PdfDict) ast.PdfDict {
	// Path (a): action /R (rendition dict) → /C (clip dict).
	if rDict, _ := document.ResolveDict(ctx, doc, actionDict["R"]); rDict != nil {
		if clipDict, _ := document.ResolveDict(ctx, doc, rDict["C"]); clipDict != nil {
			return clipDict
		}
	}
	// Path (b): action /C directly (clip dict on the action itself).
	if clipDict, _ := document.ResolveDict(ctx, doc, actionDict["C"]); clipDict != nil {
		return clipDict
	}
	return nil
}

// hasValidMediaClipAlt checks if the media clip dict has a valid /Alt entry.
// Per ISO 32000-1 Table 274, /Alt is an array of pairs [lang, text, ...].
// A missing key, empty array, or array with only empty strings is invalid.
func hasValidMediaClipAlt(ctx context.Context, doc *document.Document, clipDict ast.PdfDict) bool {
	altObj, hasAlt := clipDict["Alt"]
	if !hasAlt {
		return false
	}
	altArr, err := document.ResolveArray(ctx, doc, altObj)
	if err != nil {
		// Not an array — check as string.
		s, err := document.ResolveString(ctx, doc, altObj)
		return err == nil && strings.TrimSpace(string(s)) != ""
	}
	// Must have at least one lang+text pair with non-empty text.
	for i := 1; i < len(altArr); i += 2 {
		s, err := document.ResolveString(ctx, doc, altArr[i])
		if err == nil && strings.TrimSpace(string(s)) != "" {
			return true
		}
	}
	return false
}

// --- 19. Content marking ---

// markerKind tracks whether a BMC/BDC span is Artifact or tagged content.
type markerKind int

const (
	markerTagged     markerKind = iota // BMC/BDC with a structure tag, no MCID
	markerTaggedMCID                   // BDC with MCID (real structure element reference)
	markerArtifact                     // BMC /Artifact or BDC /Artifact
)

// checkContentMarking validates PDF/UA-1 §7.1:
//   - t1: Artifact content must not appear inside tagged content
//   - t2: Tagged content must not appear inside Artifact
//   - t3: All painting operators must be inside a marked content span
func checkContentMarking(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !isPDFUA(profile) {
		return nil
	}

	pages := collectPages(ctx, doc, catalog)
	var results []Violation

	for _, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		data, err := getPageContentBytes(ctx, doc, page)
		if err != nil || len(data) == 0 {
			continue
		}
		ops, err := content.ParseStreamData(data)
		if err != nil {
			slog.Debug("compliance.accessibility: cannot parse content stream", "error", err)
			continue
		}

		// Track BMC/BDC/EMC nesting with marker kind (Artifact vs tagged).
		var markerStack []markerKind
		hasUntaggedPainting := false
		allUntaggedIsTaggedDo := true
		reportedT1 := false
		reportedT2 := false
		resources := getResources(ctx, doc, page)

		classifyMarker := func(op content.Operation) markerKind {
			if len(op.Operands) < 1 {
				return markerTagged
			}
			tag, ok := op.Operands[0].(ast.PdfName)
			if !ok {
				return markerTagged
			}
			if tag == "Artifact" {
				return markerArtifact
			}
			// Check for MCID in the properties dict (second operand).
			if len(op.Operands) >= 2 { //nolint:mnd
				if d, ok := op.Operands[1].(ast.PdfDict); ok {
					if _, hasMCID := d["MCID"]; hasMCID {
						return markerTaggedMCID
					}
				}
			}
			return markerTagged
		}

		inArtifact := func() bool {
			return slices.Contains(markerStack, markerArtifact)
		}
		inTaggedMCID := func() bool {
			return slices.Contains(markerStack, markerTaggedMCID)
		}

		for _, op := range ops {
			switch op.Operator {
			case "BMC", "BDC":
				kind := classifyMarker(op)
				// t1: Artifact inside tagged content with MCID.
				if kind == markerArtifact && !reportedT1 && inTaggedMCID() {
					results = append(results, failf(Rule_14289_1_7_1_t01,
						"content marked as Artifact inside tagged content")...)
					reportedT1 = true
				}
				// t2: Tagged content with MCID inside Artifact.
				if kind == markerTaggedMCID && !reportedT2 && inArtifact() {
					results = append(results, failf(Rule_14289_1_7_1_t02,
						"tagged content inside content marked as Artifact")...)
					reportedT2 = true
				}
				markerStack = append(markerStack, kind)
			case "EMC":
				if len(markerStack) > 0 {
					markerStack = markerStack[:len(markerStack)-1]
				}
			case "Do":
				if len(markerStack) == 0 {
					hasUntaggedPainting = true
					if !doTargetIsTagged(ctx, doc, resources, op) {
						allUntaggedIsTaggedDo = false
					}
				}
			case "f", "F", "f*", "S", "s", "B", "B*", "b", "b*",
				"Tj", "TJ", "'", "\"", "sh":
				if len(markerStack) == 0 {
					hasUntaggedPainting = true
					allUntaggedIsTaggedDo = false
				}
			}
		}

		// t3: painting operators outside any marked content span.
		if hasUntaggedPainting && !allUntaggedIsTaggedDo {
			rule := Rule_14289_1_7_1_t03
			if profile == "PDF/UA-2" {
				rule = Rule_14289_2_8_2_2_t01
			}
			results = append(results, failf(rule,
				"page content not marked as Artifact or tagged")...)
			return results
		}
	}
	return results
}

// doTargetIsTagged checks if a Do operator's target XObject is a Form XObject
// with its own structure tagging (StructParents or BMC/BDC markers).
func doTargetIsTagged(ctx context.Context, doc *document.Document, resources ast.PdfDict, op content.Operation) bool {
	if resources == nil || len(op.Operands) < 1 {
		return false
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return false
	}
	xoDict, _ := document.ResolveDict(ctx, doc, resources["XObject"])
	if xoDict == nil {
		return false
	}
	stream, err := document.ResolveStream(ctx, doc, xoDict[name])
	if err != nil || stream.Dict == nil {
		return false
	}
	st, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
	if st != "Form" {
		return false
	}
	// Form XObject with StructParents is tagged via structure tree.
	if _, hasSP := stream.Dict["StructParents"]; hasSP {
		return true
	}
	if _, hasSP := stream.Dict["StructParent"]; hasSP {
		return true
	}
	return false
}

func ocConfigNameRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_7_t01
	}
	return Rule_14289_1_7_10_t01
}

// --- 16. Form field TU key ---

// checkFormFieldTU validates that form fields have a non-empty /TU key.
// Per PDF/UA-1 §7.18.1 test 3: "A form field shall have a TU key present
// or all its Widget annotations shall have alternative descriptions."
// This check validates /TU presence and non-emptiness. The alternative
// (Widget Alt in structure tree) is not checked due to complexity.
func checkFormFieldTU(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	acroForm, err := document.ResolveDict(ctx, doc, catalog["AcroForm"])
	if err != nil || acroForm == nil {
		return nil
	}

	fields, err := document.ResolveArray(ctx, doc, acroForm["Fields"])
	if err != nil {
		slog.Debug("compliance.accessibility: cannot resolve form fields array", "error", err)
		return nil
	}

	var results []Violation
	for _, fieldObj := range fields {
		results = append(results, checkFieldTURecursive(ctx, doc, fieldObj, 0)...)
	}
	return results
}

// annotRectOutsideCropBox checks if an annotation's /Rect is entirely outside
// the page's /CropBox (or /MediaBox if no CropBox). Returns true if the
// annotation is not visible on the page.
func annotRectOutsideCropBox(ctx context.Context, doc *document.Document, annot ast.PdfDict) bool {
	rect, err := document.ResolveArray(ctx, doc, annot["Rect"])
	if err != nil || len(rect) < 4 { //nolint:mnd
		return false
	}
	// Resolve the page to get CropBox/MediaBox.
	pageObj := annot["P"]
	if pageObj == nil {
		return false
	}
	page, err := document.ResolveDict(ctx, doc, pageObj)
	if err != nil || page == nil {
		return false
	}
	box, err := document.ResolveArray(ctx, doc, page["CropBox"])
	if err != nil || len(box) < 4 { //nolint:mnd
		box, err = document.ResolveArray(ctx, doc, page["MediaBox"])
		if err != nil || len(box) < 4 { //nolint:mnd
			return false
		}
	}
	// Parse rectangles.
	rx1, ry1 := document.GetFloat(rect[0]), document.GetFloat(rect[1])
	rx2, ry2 := document.GetFloat(rect[2]), document.GetFloat(rect[3])
	bx1, by1 := document.GetFloat(box[0]), document.GetFloat(box[1])
	bx2, by2 := document.GetFloat(box[2]), document.GetFloat(box[3])
	// Normalise: ensure min < max.
	if rx1 > rx2 {
		rx1, rx2 = rx2, rx1
	}
	if ry1 > ry2 {
		ry1, ry2 = ry2, ry1
	}
	if bx1 > bx2 {
		bx1, bx2 = bx2, bx1
	}
	if by1 > by2 {
		by1, by2 = by2, by1
	}
	// Check non-intersection.
	return rx2 <= bx1 || rx1 >= bx2 || ry2 <= by1 || ry1 >= by2
}

// formStructElementHasAlt walks the structure tree looking for a Form element
// with a non-empty /Alt entry. This is the alternative to /TU per §7.18.1 t3.
func formStructElementHasAlt(ctx context.Context, doc *document.Document, node ast.PdfDict, depth int) bool {
	if depth > 30 { //nolint:mnd
		return false
	}
	sType, _ := document.ResolveName(ctx, doc, node["S"])
	if sType == "Form" {
		alt, _ := document.ResolveString(ctx, doc, node["Alt"])
		if len(alt) > 0 {
			return true
		}
	}
	kObj := node["K"]
	if kObj == nil {
		return false
	}
	if arr, _ := document.ResolveArray(ctx, doc, kObj); len(arr) > 0 {
		for _, kid := range arr {
			kidDict, _ := document.ResolveDict(ctx, doc, kid)
			if kidDict != nil && formStructElementHasAlt(ctx, doc, kidDict, depth+1) {
				return true
			}
		}
	} else if kidDict, _ := document.ResolveDict(ctx, doc, kObj); kidDict != nil {
		return formStructElementHasAlt(ctx, doc, kidDict, depth+1)
	}
	return false
}

// checkFieldTURecursive checks a form field (and its children) for /TU.
func checkFieldTURecursive(ctx context.Context, doc *document.Document, fieldObj ast.PdfObject, depth int) []Violation {
	if depth > 10 { //nolint:mnd
		return nil
	}
	field, err := document.ResolveDict(ctx, doc, fieldObj)
	if err != nil || field == nil {
		return nil
	}

	// Check if this is a terminal field (has /FT or /Subtype Widget).
	_, hasFT := field["FT"]
	subtype, _ := document.ResolveName(ctx, doc, field["Subtype"])
	isTerminal := hasFT || subtype == "Widget"

	if isTerminal {
		// Hidden or invisible annotations are exempt.
		flags, _ := document.ResolveInt(ctx, doc, field["F"])
		const (
			invisBit  = 1 << 0 // bit 0
			hiddenBit = 1 << 1 // bit 1
			noViewBit = 1 << 5 // bit 5
		)
		if int(flags)&(hiddenBit|invisBit|noViewBit) != 0 {
			return nil
		}
		// Annotations with Rect outside the page CropBox are exempt.
		if annotRectOutsideCropBox(ctx, doc, field) {
			return nil
		}

		// Check /TU key: must be present and non-empty.
		tuStr, _ := document.ResolveString(ctx, doc, field["TU"])
		if len(tuStr) > 0 {
			return nil
		}
		// TU missing or empty — check if widget has Alt in structure tree.
		catalog, _ := document.GetCatalog(ctx, doc)
		treeRoot, _ := document.ResolveDict(ctx, doc, catalog.Dict["StructTreeRoot"])
		if treeRoot != nil && formStructElementHasAlt(ctx, doc, treeRoot, 0) {
			return nil
		}
		fieldName, _ := document.ResolveString(ctx, doc, field["T"])
		return failf(Rule_14289_1_7_18_1_t03,
			"form field %q: missing or empty /TU key and no Alt on enclosing structure element", string(fieldName))
	}

	// Non-terminal: recurse into /Kids.
	kids, err := document.ResolveArray(ctx, doc, field["Kids"])
	if err != nil {
		slog.Debug("compliance.accessibility: cannot resolve field kids array", "error", err)
		return nil
	}
	var results []Violation
	for _, kid := range kids {
		results = append(results, checkFieldTURecursive(ctx, doc, kid, depth+1)...)
	}
	return results
}
