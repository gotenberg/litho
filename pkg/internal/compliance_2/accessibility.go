package compliance2

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
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
	langValidRule := langValidRule(profile)

	langObj, ok := catalog["Lang"]
	if !ok {
		slog.Debug("compliance_2/accessibility: catalog missing /Lang key")
		return fail(langRule, "catalog")
	}

	langStr, err := document.ResolveString(ctx, doc, langObj)
	if err != nil {
		slog.Debug("compliance_2/accessibility: /Lang is unresolvable", "error", err)
		return fail(langRule, "catalog")
	}

	trimmed := strings.TrimSpace(string(langStr))
	if trimmed == "" {
		// Empty or whitespace-only /Lang is treated as "not determined" — this is a
		// missing-lang violation (t34), not an invalid-format violation (t29).
		slog.Debug("compliance_2/accessibility: /Lang is empty/whitespace, treating as absent")
		return fail(langRule, "catalog")
	}

	slog.Debug("compliance_2/accessibility: catalog /Lang value", "lang", trimmed)

	if !isValidLangTag(trimmed) {
		slog.Debug("compliance_2/accessibility: catalog /Lang is invalid BCP 47", "lang", trimmed)
		return failf(langValidRule, "lang=%s", trimmed)
	}

	slog.Debug("compliance_2/accessibility: catalog /Lang is valid, checking structure elements")

	// Also validate /Lang values in structure elements.
	results := validateStructureLangs(ctx, doc, catalog, langValidRule)

	return results
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
		trimmed := strings.TrimSpace(string(langStr))
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

// isValidLangTag validates a language tag against BCP 47 / RFC 3066 basic format.
// An empty or whitespace-only tag is treated as "not present" rather than invalid —
// callers should skip validation instead of reporting an invalid-tag violation.
//
// Primary subtag: 2-8 alpha characters, OR single-character "i" (IANA) / "x" (private use).
// Subsequent subtags: 1-8 alphanumeric characters, separated by "-".
func isValidLangTag(tag string) bool {
	trimmed := strings.TrimSpace(tag)
	if trimmed == "" {
		// Empty or whitespace-only is "absent", not "invalid".
		slog.Debug("compliance_2/accessibility: lang tag is empty/whitespace, treating as absent", "raw", tag)
		return true
	}

	parts := strings.Split(trimmed, "-")
	primary := parts[0]

	if len(primary) < 1 {
		slog.Debug("compliance_2/accessibility: lang tag has empty primary subtag", "tag", tag)
		return false
	}

	// Primary subtag must be all alpha.
	for _, c := range primary {
		if !isAlpha(c) {
			slog.Debug("compliance_2/accessibility: lang tag primary subtag contains non-alpha character",
				"tag", tag, "char", string(c))
			return false
		}
	}

	// Single-character primary subtags: only "i" (grandfathered/IANA) and "x" (private use) are valid.
	if len(primary) == 1 {
		lower := strings.ToLower(primary)
		if lower != "i" && lower != "x" {
			slog.Debug("compliance_2/accessibility: lang tag single-char primary subtag is not i/x",
				"tag", tag, "primary", primary)
			return false
		}
	} else if len(primary) > 8 {
		// BCP 47: primary language subtag is 2-8 alpha characters.
		slog.Debug("compliance_2/accessibility: lang tag primary subtag exceeds 8 characters",
			"tag", tag, "primary", primary, "len", len(primary))
		return false
	}

	// Subsequent subtags: 1-8 alphanumeric characters each.
	for _, sub := range parts[1:] {
		if len(sub) < 1 || len(sub) > 8 {
			slog.Debug("compliance_2/accessibility: lang subtag has invalid length",
				"tag", tag, "subtag", sub, "len", len(sub))
			return false
		}
		for _, c := range sub {
			if !isAlphaNum(c) {
				slog.Debug("compliance_2/accessibility: lang subtag contains invalid character",
					"tag", tag, "subtag", sub, "char", string(c))
				return false
			}
		}
	}

	slog.Debug("compliance_2/accessibility: lang tag is valid", "tag", tag)
	return true
}

func isAlpha(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isAlphaNum(c rune) bool {
	return isAlpha(c) || (c >= '0' && c <= '9')
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
				// PDF/UA-1: every page that has annotations must have /Tabs = /S.
				if hasAnnots {
					slog.Debug("compliance_2/accessibility: page missing /Tabs", "page", pageIdx)
					results = append(results, Result{Rule: rule, Context: fmt.Sprintf("page %d, Tabs missing", pageIdx)})
				}
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

		if isPDFUA1 && hasAnnots && tabsName != "S" {
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

		// Check for /Alt or /ActualText.
		_, hasAlt := elem["Alt"]
		_, hasActualText := elem["ActualText"]

		if !hasAlt && !hasActualText {
			slog.Debug("compliance_2/accessibility: Figure missing /Alt", "path", path)
			results = append(results, Result{Rule: rule, Context: path})
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

			// Link, Widget, and Popup annotations are excluded from this check.
			// Popup annotations are explicitly exempt — they inherit content
			// from their parent annotation and do not need /Contents or /Alt.
			if subtype == "Link" || subtype == "Widget" || subtype == "Popup" {
				slog.Debug("compliance_2/accessibility: skipping exempt annotation subtype",
					"page", pageIdx, "annot", annotIdx, "subtype", string(subtype))
				continue
			}

			// Check if annotation is hidden.
			if isAnnotHidden(ctx, doc, annot) {
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

			// Annotation lacks /Contents — would need to check enclosing structure
			// element for /Alt, but that requires full structure tree → annotation
			// mapping. For now, flag annotations without /Contents.
			loc := fmt.Sprintf("page %d, annot %d (%s)", pageIdx, annotIdx, subtype)
			slog.Debug("compliance_2/accessibility: annotation missing /Contents", "loc", loc)
			results = append(results, Result{Rule: rule, Context: loc})
		}
	}

	return results
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

		// Formula elements should have /AF (associated files) with MathML content,
		// OR non-empty /Alt text, OR non-empty /ActualText as alternative text
		// representations. Any of these provide accessible representation of the formula.
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

		afObj, ok := elem["AF"]
		if !ok {
			slog.Debug("compliance_2/accessibility: Formula missing /AF, /Alt, and /ActualText", "path", path)
			results = append(results, Result{Rule: Rule_14289_2_8_2_5_29_t01, Context: path})
			return
		}

		// /AF can be a single file spec dict or an array; verify it is present and non-empty.
		afArr, err := document.ResolveArray(ctx, doc, afObj)
		if err != nil {
			// Might be a single dict, which is acceptable.
			afDict, err := document.ResolveDict(ctx, doc, afObj)
			if err != nil || afDict == nil {
				slog.Debug("compliance_2/accessibility: Formula /AF unresolvable", "path", path, "error", err)
				results = append(results, Result{Rule: Rule_14289_2_8_2_5_29_t01, Context: path})
			}
			return
		}

		if len(afArr) == 0 {
			slog.Debug("compliance_2/accessibility: Formula /AF array is empty", "path", path)
			results = append(results, Result{Rule: Rule_14289_2_8_2_5_29_t01, Context: path})
		}
	})

	return results
}

// --- 14. Invisible annotations in structure (PDF/UA-2) ---

// checkInvisibleAnnotArtifact validates that annotations with the Invisible flag
// set are not included in the logical structure tree (they must be artifacts).
// Rule: ISO 14289-2, 8.9.2.2, test 1.
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

	data, err := filter.DecodeStream(stream)
	if err != nil {
		// Fallback to raw bytes if decode fails (metadata streams are often uncompressed).
		slog.Debug("compliance_2/accessibility: filter.DecodeStream failed, trying raw", "error", err)
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
