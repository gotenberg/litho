package compliance2

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// standardStructTypes lists all standard structure types defined in
// ISO 32000-1:2008 (14.8.4) and ISO 32000-2:2020 (14.8.4). Non-standard
// types must be mapped to one of these via the /RoleMap or /Namespaces dict.
var standardStructTypes = map[ast.PdfName]bool{ //nolint:gochecknoglobals // constant set.
	// ISO 32000-1 types.
	"Document": true, "Part": true, "Art": true, "Sect": true, "Div": true,
	"BlockQuote": true, "Caption": true, "TOC": true, "TOCI": true, "Index": true,
	"NonStruct": true, "Private": true, "H": true, "H1": true, "H2": true,
	"H3": true, "H4": true, "H5": true, "H6": true, "P": true, "L": true,
	"LI": true, "Lbl": true, "LBody": true, "Table": true, "TR": true,
	"TH": true, "TD": true, "THead": true, "TBody": true, "TFoot": true,
	"Span": true, "Quote": true, "Note": true, "Reference": true, "BibEntry": true,
	"Code": true, "Link": true, "Annot": true, "Ruby": true, "Warichu": true,
	"RB": true, "RT": true, "RP": true, "WT": true, "WP": true,
	"Figure": true, "Formula": true, "Form": true,
	// ISO 32000-2 additional types (PDF 2.0 / PDF/UA-2).
	"DocumentFragment": true, "Aside": true, "Title": true, "Sub": true,
	"Em": true, "Strong": true, "Artifact": true, "FENote": true,
}

// maxStructTreeDepth is the recursion limit for walking the structure tree.
const maxStructTreeDepth = 200

// checkStructureGroup validates structure tree requirements for PDF/A and
// PDF/UA profiles. For PDF/A level 'a' it checks MarkInfo and StructTreeRoot.
// For PDF/UA it additionally validates tag containment, role maps, heading
// hierarchy, table structure, list structure, captions, and TH scope.
func checkStructureGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	part := pdfaPart(profile)
	level := pdfaLevel(profile)
	ua := isPDFUA(profile)

	slog.Debug("compliance_2/structure: starting structure checks",
		"profile", profile, "part", part, "level", level, "ua", ua,
	)

	var results []Result

	// MarkInfo is required for PDF/A level 'a' and all PDF/UA profiles.
	if level == 'a' || ua {
		checkMarkInfo(ctx, doc, catalog, profile, part, &results)
	}

	// StructTreeRoot is required for PDF/A level 'a' and all PDF/UA profiles.
	structTreeRoot := checkStructTreeRoot(ctx, doc, catalog, profile, part, level, ua, &results)

	// PDF/A level 'a': validate catalog /Lang is a valid language tag (§6.8.4).
	if level == 'a' && !ua {
		results = append(results, checkPDFALangValid(ctx, doc, catalog, profile, part)...)
	}

	// PDF/UA-specific structure tree validation.
	if ua && structTreeRoot != nil {
		if ctx.Err() != nil {
			return results
		}
		// PDF/UA-2 uses /Namespaces for type mapping instead of /RoleMap;
		// only check RoleMap for PDF/UA-1.
		if profile == "PDF/UA-1" {
			checkRoleMap(ctx, doc, structTreeRoot, profile, &results)
		}
		walkStructureTree(ctx, doc, structTreeRoot, profile, &results)
	}

	slog.Debug("compliance_2/structure: structure checks complete", "violations", len(results))
	return results
}

// checkMarkInfo validates that the catalog contains /MarkInfo with /Marked=true.
func checkMarkInfo(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int, results *[]Result) {
	slog.Debug("compliance_2/structure: checking MarkInfo")

	markInfoObj, ok := catalog["MarkInfo"]
	if !ok {
		slog.Debug("compliance_2/structure: catalog missing /MarkInfo")
		*results = append(*results, fail(markInfoRule(profile, part), "catalog missing /MarkInfo")...)
		return
	}

	markInfo, err := document.ResolveDict(ctx, doc, markInfoObj)
	if err != nil || markInfo == nil {
		slog.Debug("compliance_2/structure: cannot resolve /MarkInfo", "error", err)
		*results = append(*results, fail(markInfoRule(profile, part), "cannot resolve /MarkInfo")...)
		return
	}

	markedObj, ok := markInfo["Marked"]
	if !ok {
		slog.Debug("compliance_2/structure: /MarkInfo missing /Marked entry")
		*results = append(*results, fail(markInfoRule(profile, part), "/MarkInfo missing /Marked entry")...)
		return
	}

	marked, err := document.ResolveBool(ctx, doc, markedObj)
	if err != nil || !bool(marked) {
		slog.Debug("compliance_2/structure: /Marked is not true", "marked", marked, "error", err)
		*results = append(*results, fail(markInfoRule(profile, part), "/Marked is not true")...)
	}
}

// checkStructTreeRoot validates the presence of /StructTreeRoot in the catalog.
// Returns the resolved StructTreeRoot dict, or nil if absent.
func checkStructTreeRoot(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int, level byte, ua bool, results *[]Result) ast.PdfDict {
	if level != 'a' && !ua {
		return nil
	}

	slog.Debug("compliance_2/structure: checking StructTreeRoot")

	stObj, ok := catalog["StructTreeRoot"]
	if !ok {
		slog.Debug("compliance_2/structure: catalog missing /StructTreeRoot")
		*results = append(*results, fail(structTreeRootRule(profile, part), "catalog missing /StructTreeRoot")...)
		return nil
	}

	st, err := document.ResolveDict(ctx, doc, stObj)
	if err != nil || st == nil {
		slog.Debug("compliance_2/structure: cannot resolve /StructTreeRoot", "error", err)
		*results = append(*results, fail(structTreeRootRule(profile, part), "cannot resolve /StructTreeRoot")...)
		return nil
	}

	slog.Debug("compliance_2/structure: /StructTreeRoot found")
	return st
}

// checkRoleMap validates the /RoleMap dictionary in the structure tree root.
// Non-standard tags must map to standard types, standard tags must not be
// remapped, and no circular mappings may exist.
func checkRoleMap(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict, profile string, results *[]Result) {
	slog.Debug("compliance_2/structure: checking RoleMap")

	rmObj, ok := structTreeRoot["RoleMap"]
	if !ok {
		slog.Debug("compliance_2/structure: no /RoleMap present")
		return
	}

	roleMap, err := document.ResolveDict(ctx, doc, rmObj)
	if err != nil || roleMap == nil {
		slog.Debug("compliance_2/structure: cannot resolve /RoleMap", "error", err)
		return
	}

	// Build a name→name mapping for cycle and standard type checks.
	mapping := make(map[ast.PdfName]ast.PdfName, len(roleMap))
	for key, valObj := range roleMap {
		if ctx.Err() != nil {
			return
		}
		target, err := document.ResolveName(ctx, doc, valObj)
		if err != nil {
			slog.Debug("compliance_2/structure: cannot resolve RoleMap target", "key", key, "error", err)
			continue
		}
		mapping[key] = target
	}

	// Check standard types are not remapped.
	for src := range mapping {
		if standardStructTypes[src] {
			slog.Debug("compliance_2/structure: standard type remapped", "type", src)
			*results = append(*results, failf(roleMapStandardRemapRule(profile), "standard type %s is remapped", src)...)
		}
	}

	// Check non-standard types resolve to standard type (no circularity).
	for src, target := range mapping {
		if ctx.Err() != nil {
			return
		}
		if standardStructTypes[src] {
			continue // already reported above
		}
		if !resolveToStandard(src, mapping, results, profile) {
			slog.Debug("compliance_2/structure: non-standard type not mapped to standard",
				"type", src, "target", target,
			)
		}
	}
}

// resolveToStandard follows the role map chain from src and returns true if it
// terminates at a standard type. Reports circular mappings and unmapped types.
func resolveToStandard(src ast.PdfName, mapping map[ast.PdfName]ast.PdfName, results *[]Result, profile string) bool {
	visited := map[ast.PdfName]bool{src: true}
	current := src

	for range maxStructTreeDepth {
		target, ok := mapping[current]
		if !ok {
			// current is not in the mapping; check if it is a standard type.
			if standardStructTypes[current] {
				return true
			}
			*results = append(*results, failf(roleMapNonStandardRule(profile),
				"non-standard type %s is not mapped to a standard type", src)...)
			return false
		}

		if standardStructTypes[target] {
			return true
		}

		if visited[target] {
			*results = append(*results, failf(roleMapCircularRule(profile),
				"circular mapping for %s", src)...)
			return false
		}
		visited[target] = true
		current = target
	}

	// Depth limit exceeded — treat as circular.
	*results = append(*results, failf(roleMapCircularRule(profile),
		"role map chain for %s exceeds depth limit", src)...)
	return false
}

// walkStructureTree recursively walks the structure tree to validate element
// containment, heading hierarchy, table structure, list structure, captions,
// and TH scope attributes.
func walkStructureTree(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict, profile string, results *[]Result) {
	slog.Debug("compliance_2/structure: walking structure tree")

	kidsObj, ok := structTreeRoot["K"]
	if !ok {
		slog.Debug("compliance_2/structure: StructTreeRoot has no /K entry")
		return
	}

	state := &walkState{
		doc:     doc,
		profile: profile,
		results: results,
	}

	walkChildren(ctx, state, kidsObj, "", 0, 0)

	// Check heading hierarchy: levels must not be skipped.
	checkHeadingHierarchy(state)
}

// walkState holds mutable state accumulated during the structure tree walk.
type walkState struct {
	doc     *document.Document
	profile string
	results *[]Result

	// headingLevels tracks all heading levels encountered in document order.
	headingLevels []int
}

// walkChildren processes the /K entry of a structure element (may be a single
// element or an array).
func walkChildren(ctx context.Context, state *walkState, kObj ast.PdfObject, parentType ast.PdfName, depth int, lastHeadingLevel int) {
	if ctx.Err() != nil || depth > maxStructTreeDepth {
		return
	}

	// /K can be a single dict or an array.
	arr, err := document.ResolveArray(ctx, state.doc, kObj)
	if err != nil {
		// Try as a single element.
		dict, err := document.ResolveDict(ctx, state.doc, kObj)
		if err != nil || dict == nil {
			return
		}
		walkElement(ctx, state, dict, parentType, depth, lastHeadingLevel)
		return
	}

	childIdx := 0
	for _, item := range arr {
		if ctx.Err() != nil {
			return
		}
		dict, err := document.ResolveDict(ctx, state.doc, item)
		if err != nil || dict == nil {
			childIdx++
			continue
		}
		// Only process structure elements (skip marked content refs, object refs).
		if _, hasS := dict["S"]; hasS {
			walkElement(ctx, state, dict, parentType, depth, lastHeadingLevel)
			validateCaptionPosition(state, dict, parentType, childIdx, len(arr))
		}
		childIdx++
	}
}

// walkElement validates a single structure element and recurses into children.
func walkElement(ctx context.Context, state *walkState, elem ast.PdfDict, parentType ast.PdfName, depth int, lastHeadingLevel int) {
	if ctx.Err() != nil {
		return
	}

	typeName, err := document.ResolveName(ctx, state.doc, elem["S"])
	if err != nil || typeName == "" {
		return
	}

	slog.Debug("compliance_2/structure: visiting element", "type", typeName, "parent", parentType, "depth", depth)

	// Table and list structure validation.
	validateTableContainment(state, typeName, parentType)
	validateListContainment(ctx, state, elem, typeName, parentType)

	// Track heading levels.
	if hl := headingLevel(typeName); hl > 0 {
		state.headingLevels = append(state.headingLevels, hl)
	}

	// TH scope validation.
	if typeName == "TH" {
		validateTHScope(ctx, state, elem)
	}

	// Recurse into children.
	if kObj, ok := elem["K"]; ok {
		walkChildren(ctx, state, kObj, typeName, depth+1, lastHeadingLevel)
	}
}

// validateTableContainment checks PDF/UA table containment rules.
func validateTableContainment(state *walkState, typeName ast.PdfName, parentType ast.PdfName) {
	switch typeName {
	case "TR":
		// TR must be inside Table, THead, TBody, or TFoot.
		switch parentType {
		case "Table", "THead", "TBody", "TFoot":
			// OK
		default:
			if parentType != "" {
				*state.results = append(*state.results,
					failf(Rule_14289_1_7_2_t05, "TR inside %s", parentType)...)
			}
		}
	case "THead":
		// THead must be inside Table.
		if parentType != "Table" && parentType != "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t06, "THead inside %s", parentType)...)
		}
	case "TBody":
		// TBody must be inside Table.
		if parentType != "Table" && parentType != "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t07, "TBody inside %s", parentType)...)
		}
	case "TFoot":
		// TFoot must be inside Table.
		if parentType != "Table" && parentType != "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t08, "TFoot inside %s", parentType)...)
		}
	case "TD", "TH":
		// TD/TH must be inside TR.
		if parentType != "TR" && parentType != "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t04, "%s inside %s instead of TR", typeName, parentType)...)
		}
	}

	// Table children: THead/TBody/TFoot must contain only TR.
	switch parentType {
	case "THead", "TBody", "TFoot":
		if typeName != "TR" && typeName != "Caption" {
			slog.Debug("compliance_2/structure: invalid child of table section",
				"parent", parentType, "child", typeName,
			)
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t03, "%s contains %s instead of TR", parentType, typeName)...)
		}
	}
}

// validateListContainment checks PDF/UA list structure rules.
func validateListContainment(ctx context.Context, state *walkState, elem ast.PdfDict, typeName ast.PdfName, parentType ast.PdfName) {
	// L must contain only LI (and optionally Caption).
	if parentType == "L" && typeName != "LI" && typeName != "Caption" && typeName != "L" {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t09, "L contains %s instead of LI", typeName)...)
	}

	// LI must contain Lbl and/or LBody.
	if parentType == "LI" {
		switch typeName {
		case "Lbl", "LBody":
			// OK
		default:
			slog.Debug("compliance_2/structure: LI contains unexpected child", "child", typeName)
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t10, "LI contains %s instead of Lbl/LBody", typeName)...)
		}
	}
}

// validateCaptionPosition checks that Caption is the first child of Table,
// Figure, or Form.
func validateCaptionPosition(state *walkState, elem ast.PdfDict, parentType ast.PdfName, childIdx int, totalChildren int) {
	typeName, _ := document.ResolveName(context.Background(), state.doc, elem["S"])
	if typeName != "Caption" {
		return
	}

	switch parentType {
	case "Table":
		// Table allows Caption as first or last child.
		if childIdx != 0 && childIdx != totalChildren-1 {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t11,
					"Caption at position %d (not first or last) in Table", childIdx+1)...)
		}
	case "Figure", "Form":
		// Figure/Form allow Caption as first or last child.
		if childIdx != 0 && childIdx != totalChildren-1 {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t12,
					"Caption at position %d (not first or last) in %s", childIdx+1, parentType)...)
		} else {
			slog.Debug("compliance_2/structure: Caption position OK in Figure/Form",
				"parent", string(parentType), "position", childIdx, "total", totalChildren)
		}
	}
}

// headingLevel returns the numeric heading level (1-6) for H1-H6 tags, or 0
// if the tag is not a heading.
func headingLevel(typeName ast.PdfName) int {
	switch typeName {
	case "H1":
		return 1
	case "H2":
		return 2
	case "H3":
		return 3
	case "H4":
		return 4
	case "H5":
		return 5
	case "H6":
		return 6
	default:
		return 0
	}
}

// checkHeadingHierarchy validates that heading levels are not skipped in a
// descending sequence.
func checkHeadingHierarchy(state *walkState) {
	if len(state.headingLevels) == 0 {
		return
	}

	slog.Debug("compliance_2/structure: checking heading hierarchy",
		"count", len(state.headingLevels),
	)

	maxSeen := 0
	for _, level := range state.headingLevels {
		// If the level jumps forward by more than 1, a level was skipped.
		if level > maxSeen+1 && maxSeen > 0 {
			slog.Debug("compliance_2/structure: heading level skipped",
				"expected", maxSeen+1, "got", level,
			)
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_4_2_t01,
					"heading level H%d skipped (previous max H%d)", level, maxSeen)...)
		}
		if level > maxSeen {
			maxSeen = level
		}
	}
}

// validateTHScope checks that TH elements have a /Scope attribute.
// When the table uses ID-based header association (TH has /ID attribute),
// /Scope is not required per ISO 14289-1, 7.5.
func validateTHScope(ctx context.Context, state *walkState, elem ast.PdfDict) {
	slog.Debug("compliance_2/structure: checking TH scope")

	// If TH has an /ID attribute, the table may use ID-based header
	// association (TD elements reference TH via /Headers). In that case,
	// /Scope is not required.
	if _, hasID := elem["ID"]; hasID {
		slog.Debug("compliance_2/structure: TH has /ID, skipping /Scope requirement (ID-based header association)")
		return
	}

	// Check /A attribute dict for Scope.
	aObj, ok := elem["A"]
	if !ok {
		*state.results = append(*state.results,
			fail(Rule_14289_1_7_5_t01, "TH element missing /Scope attribute")...)
		return
	}

	// /A can be a dict or an array of dicts.
	if hasScope := findScopeInAttributes(ctx, state.doc, aObj); !hasScope {
		*state.results = append(*state.results,
			fail(Rule_14289_1_7_5_t01, "TH element missing /Scope attribute")...)
	}
}

// findScopeInAttributes checks whether a /Scope key exists in the attribute
// object(s) attached to a structure element.
func findScopeInAttributes(ctx context.Context, doc *document.Document, aObj ast.PdfObject) bool {
	// Try as a single dict.
	dict, err := document.ResolveDict(ctx, doc, aObj)
	if err == nil && dict != nil {
		if _, ok := dict["Scope"]; ok {
			return true
		}
	}

	// Try as an array of attribute dicts.
	arr, err := document.ResolveArray(ctx, doc, aObj)
	if err != nil {
		return false
	}
	for _, item := range arr {
		if ctx.Err() != nil {
			return false
		}
		d, err := document.ResolveDict(ctx, doc, item)
		if err != nil || d == nil {
			continue
		}
		if _, ok := d["Scope"]; ok {
			return true
		}
	}
	return false
}

// checkPDFALangValid validates §6.8.4 (PDF/A-1) / §6.7.3.4 (PDF/A-2/3):
// if a /Lang entry is present in the catalog or structure elements, its value
// must be a valid RFC 3066 language tag.
func checkPDFALangValid(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int) []Result {
	slog.Debug("compliance_2/structure: checking /Lang validity for PDF/A level 'a'", "profile", profile)

	rule := pdfaLangValidRule(part)

	var results []Result

	// Check catalog /Lang.
	if langObj, ok := catalog["Lang"]; ok {
		langStr, err := document.ResolveString(ctx, doc, langObj)
		if err == nil {
			trimmed := strings.TrimSpace(string(langStr))
			if trimmed != "" && !isValidLangTag(trimmed) {
				slog.Debug("compliance_2/structure: catalog /Lang is invalid", "lang", trimmed)
				results = append(results, Result{Rule: rule, Context: fmt.Sprintf("catalog lang=%s", trimmed)})
			}
		}
	}

	// Check structure element /Lang values.
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return results
	}

	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		langObj, ok := elem["Lang"]
		if !ok {
			return
		}
		langStr, err := document.ResolveString(ctx, doc, langObj)
		if err != nil {
			return
		}
		trimmed := strings.TrimSpace(string(langStr))
		if trimmed != "" && !isValidLangTag(trimmed) {
			slog.Debug("compliance_2/structure: invalid /Lang in structure element",
				"lang", trimmed, "path", path)
			results = append(results, Result{Rule: rule, Context: fmt.Sprintf("lang=%s, %s", trimmed, path)})
		}
	})

	if len(results) == 0 {
		slog.Debug("compliance_2/structure: all /Lang values valid for PDF/A level 'a'")
	}
	return results
}

// pdfaLangValidRule returns the Lang validity rule for PDF/A level 'a' profiles.
func pdfaLangValidRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_8_4_t01
	case 2:
		return Rule_19005_2_6_7_3_4_t01
	case 3:
		return Rule_19005_3_6_7_3_4_t01
	default:
		return Rule_19005_1_6_8_4_t01
	}
}

// --- Rule selectors ---

// markInfoRule selects the MarkInfo rule based on profile.
func markInfoRule(profile string, part int) Rule {
	if isPDFUA(profile) {
		if len(profile) >= 7 && profile[6] == '2' {
			return Rule_14289_2_6_2_t01
		}
		return Rule_14289_1_6_2_t01
	}
	switch part {
	case 1:
		return Rule_19005_1_6_8_2_2_t01
	case 2:
		return Rule_19005_2_6_7_2_2_t01
	case 3:
		return Rule_19005_3_6_7_2_2_t01
	default:
		return Rule_19005_1_6_8_2_2_t01
	}
}

// structTreeRootRule selects the StructTreeRoot rule based on profile.
func structTreeRootRule(profile string, part int) Rule {
	if isPDFUA(profile) {
		if len(profile) >= 7 && profile[6] == '2' {
			return Rule_14289_2_8_2_1_t01
		}
		return Rule_14289_1_7_1_t11
	}
	switch part {
	case 1:
		return Rule_19005_1_6_8_3_3_t01
	case 2:
		return Rule_19005_2_6_7_3_3_t01
	case 3:
		return Rule_19005_3_6_7_3_3_t01
	default:
		return Rule_19005_1_6_8_3_3_t01
	}
}

// roleMapNonStandardRule returns the rule for non-standard types not mapped.
func roleMapNonStandardRule(profile string) Rule {
	if isPDFUA(profile) {
		if len(profile) >= 7 && profile[6] == '2' {
			return Rule_14289_2_8_2_4_t01
		}
		return Rule_14289_1_7_1_t05
	}
	return Rule_14289_1_7_1_t05
}

// roleMapCircularRule returns the rule for circular role mappings.
func roleMapCircularRule(profile string) Rule {
	if isPDFUA(profile) {
		if len(profile) >= 7 && profile[6] == '2' {
			return Rule_14289_2_8_2_4_t02
		}
		return Rule_14289_1_7_1_t06
	}
	return Rule_14289_1_7_1_t06
}

// roleMapStandardRemapRule returns the rule for standard types being remapped.
func roleMapStandardRemapRule(profile string) Rule {
	if isPDFUA(profile) {
		if len(profile) >= 7 && profile[6] == '2' {
			return Rule_14289_2_8_2_4_t03
		}
		return Rule_14289_1_7_1_t07
	}
	return Rule_14289_1_7_1_t07
}
