package compliance

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// standardStructTypes lists all standard structure types across both
// ISO 32000-1:2008 and ISO 32000-2:2020. Used for general lookups.
var standardStructTypes = map[ast.PdfName]bool{ //nolint:gochecknoglobals // constant set.
	// ISO 32000-1 types (14.8.4).
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

// standardStructTypesPDF2Only lists types added in ISO 32000-2 that are NOT
// standard structure types in ISO 32000-1. For PDF/UA-1 and PDF/A-1/2/3,
// these are non-standard and CAN be legitimately remapped.
//
//nolint:gochecknoglobals // constant set.
var standardStructTypesPDF2Only = map[ast.PdfName]bool{
	"DocumentFragment": true, "Aside": true, "Title": true, "Sub": true,
	"Em": true, "Strong": true, "Artifact": true, "FENote": true,
}

// isStandardStructType returns true if the name is a standard structure type
// for the given profile. ISO 32000-2 types (Title, Artifact, Em, Strong, etc.)
// are only standard for PDF/A-4 and PDF/UA-2.
func isStandardStructType(name ast.PdfName, profile string) bool {
	if !standardStructTypes[name] {
		return false
	}
	if standardStructTypesPDF2Only[name] {
		return pdfaPart(profile) == 4 || profile == "PDF/UA-2"
	}
	return true
}

// maxStructTreeDepth is the recursion limit for walking the structure tree.
const maxStructTreeDepth = 200

// checkStructureGroup validates structure tree requirements for PDF/A and
// PDF/UA profiles. For PDF/A level 'a' it checks MarkInfo and StructTreeRoot.
// For PDF/UA it additionally validates tag containment, role maps, heading
// hierarchy, table structure, list structure, captions, and TH scope.
func checkStructureGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	part := pdfaPart(profile)
	level := pdfaLevel(profile)
	ua := isPDFUA(profile)

	slog.Debug("compliance.structure: starting structure checks",
		"profile", profile, "part", part, "level", level, "ua", ua,
	)

	var results []Violation

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

	// PDF/A-4 §6.2.10.8: ActualText shall not contain PUA values.
	// Resolve StructTreeRoot independently when the level-based check returned nil.
	if part == 4 { //nolint:mnd
		puaTreeRoot := structTreeRoot
		if puaTreeRoot == nil {
			puaTreeRoot, _ = document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
		}
		if puaTreeRoot != nil {
			results = append(results, checkActualTextPUA(ctx, doc, catalog, puaTreeRoot)...)
		}
	}

	// PDF/A level 'a': validate /RoleMap (§6.7.3.4 / §6.8.3.4).
	if level == 'a' && !ua && structTreeRoot != nil {
		checkRoleMap(ctx, doc, structTreeRoot, profile, &results)
		checkUnmappedStructTypes(ctx, doc, structTreeRoot, profile, &results)
	}

	// PDF/UA-specific structure tree validation.
	if ua && structTreeRoot != nil {
		if ctx.Err() != nil {
			return results
		}
		// Check /RoleMap for both PDF/UA-1 and PDF/UA-2 (legacy RoleMap
		// can still appear in PDF/UA-2 files and must not have cycles).
		checkRoleMap(ctx, doc, structTreeRoot, profile, &results)
		walkStructureTree(ctx, doc, structTreeRoot, profile, &results)

		// PDF/UA-2 §8.2.5.2: single Document root child.
		if profile == "PDF/UA-2" {
			checkSingleDocumentRoot(ctx, doc, structTreeRoot, &results)
		}
	}

	slog.Debug("compliance.structure: structure checks complete", "violations", len(results))
	return results
}

// checkMarkInfo validates that the catalog contains /MarkInfo with /Marked=true.
func checkMarkInfo(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int, results *[]Violation) {
	slog.Debug("compliance.structure: checking MarkInfo")

	markInfoObj, ok := catalog["MarkInfo"]
	if !ok {
		slog.Debug("compliance.structure: catalog missing /MarkInfo")
		*results = append(*results, fail(markInfoRule(profile, part), "catalog missing /MarkInfo")...)
		return
	}

	markInfo, err := document.ResolveDict(ctx, doc, markInfoObj)
	if err != nil || markInfo == nil {
		slog.Debug("compliance.structure: cannot resolve /MarkInfo", "error", err)
		*results = append(*results, fail(markInfoRule(profile, part), "cannot resolve /MarkInfo")...)
		return
	}

	markedObj, ok := markInfo["Marked"]
	if !ok {
		slog.Debug("compliance.structure: /MarkInfo missing /Marked entry")
		*results = append(*results, fail(markInfoRule(profile, part), "/MarkInfo missing /Marked entry")...)
		return
	}

	marked, err := document.ResolveBool(ctx, doc, markedObj)
	if err != nil || !bool(marked) {
		slog.Debug("compliance.structure: /Marked is not true", "marked", marked, "error", err)
		*results = append(*results, fail(markInfoRule(profile, part), "/Marked is not true")...)
	}
}

// checkStructTreeRoot validates the presence of /StructTreeRoot in the catalog.
// Returns the resolved StructTreeRoot dict, or nil if absent.
func checkStructTreeRoot(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int, level byte, ua bool, results *[]Violation) ast.PdfDict {
	if level != 'a' && !ua {
		return nil
	}

	slog.Debug("compliance.structure: checking StructTreeRoot")

	stObj, ok := catalog["StructTreeRoot"]
	if !ok {
		slog.Debug("compliance.structure: catalog missing /StructTreeRoot")
		*results = append(*results, fail(structTreeRootRule(profile, part), "catalog missing /StructTreeRoot")...)
		return nil
	}

	st, err := document.ResolveDict(ctx, doc, stObj)
	if err != nil || st == nil {
		slog.Debug("compliance.structure: cannot resolve /StructTreeRoot", "error", err)
		*results = append(*results, fail(structTreeRootRule(profile, part), "cannot resolve /StructTreeRoot")...)
		return nil
	}

	slog.Debug("compliance.structure: /StructTreeRoot found")
	return st
}

// checkRoleMap validates the /RoleMap dictionary in the structure tree root.
// Non-standard tags must map to standard types, standard tags must not be
// remapped, and no circular mappings may exist.
func checkRoleMap(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict, profile string, results *[]Violation) {
	slog.Debug("compliance.structure: checking RoleMap")

	rmObj, ok := structTreeRoot["RoleMap"]
	if !ok {
		slog.Debug("compliance.structure: no /RoleMap present")
		return
	}

	roleMap, err := document.ResolveDict(ctx, doc, rmObj)
	if err != nil || roleMap == nil {
		slog.Debug("compliance.structure: cannot resolve /RoleMap", "error", err)
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
			slog.Debug("compliance.structure: cannot resolve RoleMap target", "key", key, "error", err)
			continue
		}
		mapping[key] = target
	}

	// Check standard types are not remapped (PDF/UA-1 only; PDF/UA-2 allows some).
	if !isPDFUA(profile) || profile == "PDF/UA-1" {
		for src := range mapping {
			if isStandardStructType(src, profile) {
				slog.Debug("compliance.structure: standard type remapped", "type", src)
				*results = append(*results, failf(roleMapStandardRemapRule(profile), "standard type %s is remapped", src)...)
			}
		}
	}

	// Check for self-mappings (A→A) which are always circular.
	for src, target := range mapping {
		if src == target {
			slog.Debug("compliance.structure: self-mapping detected", "type", src)
			*results = append(*results, failf(roleMapCircularRule(profile),
				"circular mapping for %s", src)...)
		}
	}

	// Check non-standard types resolve to standard type (no circularity).
	for src, target := range mapping {
		if ctx.Err() != nil {
			return
		}
		if isStandardStructType(src, profile) {
			continue
		}
		if !resolveToStandard(src, mapping, results, profile) {
			slog.Debug("compliance.structure: non-standard type not mapped to standard",
				"type", src, "target", target,
			)
		}
	}
}

// resolveToStandard follows the role map chain from src and returns true if it
// terminates at a standard type. Reports circular mappings and unmapped types.
func resolveToStandard(src ast.PdfName, mapping map[ast.PdfName]ast.PdfName, results *[]Violation, profile string) bool {
	visited := map[ast.PdfName]bool{src: true}
	current := src

	for range maxStructTreeDepth {
		target, ok := mapping[current]
		if !ok {
			// current is not in the mapping; check if it is a standard type.
			if isStandardStructType(current, profile) {
				return true
			}
			*results = append(*results, failf(roleMapNonStandardRule(profile),
				"non-standard type %s is not mapped to a standard type", src)...)
			return false
		}

		if isStandardStructType(target, profile) {
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
func walkStructureTree(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict, profile string, results *[]Violation) {
	slog.Debug("compliance.structure: walking structure tree")

	kidsObj, ok := structTreeRoot["K"]
	if !ok {
		slog.Debug("compliance.structure: StructTreeRoot has no /K entry")
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

	// §7.4.4 t2/t3: document must be either strongly structured (H1-H6)
	// or weakly structured (H), not both.
	if state.hasGenericH && state.hasSpecificH {
		*results = append(*results, failf(headingMixedRule(profile),
			"document uses both H (generic) and H1-H6 (specific) headings — must use one or the other")...)
	}
}

// walkState holds mutable state accumulated during the structure tree walk.
type walkState struct {
	doc     *document.Document
	profile string
	results *[]Violation

	// headingLevels tracks all heading levels encountered in document order.
	headingLevels []int
	// hasGenericH is true when any H (generic heading) element is found.
	hasGenericH bool
	// hasSpecificH is true when any H1-H6 element is found.
	hasSpecificH bool
	// noteIDs tracks Note element /ID values for uniqueness (§7.9 t2).
	noteIDs map[string]bool

	// tableUsesHeaderIDs is true when the current enclosing Table uses
	// the Headers+IDs association model (any TD has /Headers in its
	// attributes). When true, TH elements do not need /Scope.
	tableUsesHeaderIDs bool
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

	// Count only structural children (elements with /S key) for caption
	// position validation. Non-structural items (MCR integers, OBJR dicts)
	// must not affect the index.
	totalStructChildren := 0
	for _, item := range arr {
		d, err := document.ResolveDict(ctx, state.doc, item)
		if err == nil && d != nil {
			if _, hasS := d["S"]; hasS {
				totalStructChildren++
			}
		}
	}

	structIdx := 0
	hCount := 0 // Count of H (generic heading) children for §7.4.4 t1.
	for _, item := range arr {
		if ctx.Err() != nil {
			return
		}
		dict, err := document.ResolveDict(ctx, state.doc, item)
		if err != nil || dict == nil {
			continue
		}
		// Only process structure elements (skip marked content refs, object refs).
		if _, hasS := dict["S"]; hasS {
			// Check for H (generic heading) children — §7.4.4 t1.
			childType, _ := document.ResolveName(ctx, state.doc, dict["S"])
			if childType == "H" {
				hCount++
				if hCount > 1 {
					*state.results = append(*state.results,
						failf(Rule_14289_1_7_4_4_t01, "node contains %d H children (max 1)", hCount)...)
				}
			}

			walkElement(ctx, state, dict, parentType, depth, lastHeadingLevel)
			validateCaptionPosition(state, dict, parentType, structIdx, totalStructChildren)
			structIdx++
		}
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

	slog.Debug("compliance.structure: visiting element", "type", typeName, "parent", parentType, "depth", depth)

	// Table and list structure validation.
	validateTableContainment(state, typeName, parentType)
	validateTableChildren(state, typeName, parentType)
	validateListContainment(ctx, state, elem, typeName, parentType)

	// PDF/UA-2 §8.2.5.25: L with Lbl children must have ListNumbering attribute.
	if typeName == "L" && state.profile == "PDF/UA-2" {
		validateListNumbering(ctx, state, elem)
	}

	// PDF/UA-2 §8.2.5.29: math structure type must only appear inside Formula.
	if state.profile == "PDF/UA-2" && isMathMLType(typeName) && parentType != "Formula" {
		*state.results = append(*state.results,
			failf(Rule_14289_2_8_2_5_29_t01, "math type nested within %s instead of Formula", parentType)...)
	}

	// TOC/TOCI containment: TOCI must be inside TOC.
	if typeName == "TOCI" && parentType != "TOC" && parentType != "" {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t26, "TOCI inside %s instead of TOC", parentType)...)
	}

	// TOC children: TOC may contain only TOC, TOCI, and Caption.
	if parentType == "TOC" && typeName != "TOC" && typeName != "TOCI" && typeName != "Caption" {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t27, "TOC contains %s instead of TOC/TOCI/Caption", typeName)...)
	}

	// Track heading levels and generic/specific heading usage.
	if typeName == "H" {
		state.hasGenericH = true
		// PDF/UA-2 §8.2.5.12: H structure type is forbidden entirely.
		if state.profile == "PDF/UA-2" {
			*state.results = append(*state.results,
				fail(Rule_14289_2_8_2_5_12_t01, "document uses H structure type")...)
		}
	}
	if hl := headingLevel(typeName); hl > 0 {
		state.headingLevels = append(state.headingLevels, hl)
		state.hasSpecificH = true
	}

	// TH scope validation.
	if typeName == "TH" {
		validateTHScope(ctx, state, elem)
	}

	// §7.7 t1: Formula elements must have non-empty /Alt, or /ActualText (presence suffices).
	if typeName == "Formula" && state.profile == "PDF/UA-1" {
		hasAlt := hasNonEmptyStringKey(ctx, state.doc, elem, "Alt")
		hasAT := hasStringKeyPresent(ctx, state.doc, elem, "ActualText")
		if !hasAlt && !hasAT {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_7_t01, "Formula element without /Alt or /ActualText")...)
		}
	}

	// §7.9 t1/t2: Note elements must have /ID, and IDs must be unique.
	if typeName == "Note" && state.profile == "PDF/UA-1" {
		idStr, _ := document.ResolveString(ctx, state.doc, elem["ID"])
		if len(idStr) == 0 {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_9_t01, "Note element without /ID entry")...)
		} else {
			idVal := string(idStr)
			if state.noteIDs == nil {
				state.noteIDs = make(map[string]bool)
			}
			if state.noteIDs[idVal] {
				*state.results = append(*state.results,
					failf(Rule_14289_1_7_9_t02, "Note element has duplicate /ID %q", idVal)...)
			}
			state.noteIDs[idVal] = true
		}
	}

	// §8.2.5.20 t2: Link annotations in same Link/Reference element must target same destination.
	if (typeName == "Link" || typeName == "Reference") && state.profile == "PDF/UA-2" {
		validateLinkDestConsistency(ctx, state, elem)
	}

	// When entering a Table, determine if it uses the Headers+IDs model.
	// Save and restore the flag so nested tables don't leak state.
	prevTableUsesHeaderIDs := state.tableUsesHeaderIDs
	if typeName == "Table" {
		state.tableUsesHeaderIDs = tableHasHeaderIDs(ctx, state.doc, elem)
	}

	// Recurse into children.
	if kObj, ok := elem["K"]; ok {
		walkChildren(ctx, state, kObj, typeName, depth+1, lastHeadingLevel)
	}

	// Table section count validation (§7.2 t11-t14).
	if typeName == "Table" {
		validateTableSectionCounts(ctx, state, elem)
		validateTableColumnConsistency(ctx, state, elem)
		state.tableUsesHeaderIDs = prevTableUsesHeaderIDs
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
}

// validateTableColumnConsistency checks that all TR rows in a Table have the
// same number of columns, accounting for ColSpan attributes on TD/TH cells.
// PDF/UA-1 §7.2 t43 and PDF/UA-2 §8.2.5.26 t3/t4.
func validateTableColumnConsistency(ctx context.Context, state *walkState, tableElem ast.PdfDict) {
	rows := collectTableRows(ctx, state.doc, tableElem)
	if len(rows) < 2 { //nolint:mnd
		return
	}

	// Build effective column count per row using a proper grid model.
	// RowSpan causes a cell to occupy column positions in subsequent rows.
	// We track which columns are "occupied" by RowSpan carry-over.
	effectiveCols := computeEffectiveColumns(ctx, state.doc, rows)
	if len(effectiveCols) < 2 || effectiveCols[0] == 0 { //nolint:mnd
		return
	}

	for i := 1; i < len(effectiveCols); i++ {
		if effectiveCols[i] != effectiveCols[0] {
			rule := tableColumnRule(state.profile)
			*state.results = append(*state.results,
				failf(rule, "Table row 1 has %d columns, row %d has %d columns",
					effectiveCols[0], i+1, effectiveCols[i])...)
			return
		}
	}
}

// computeEffectiveColumns builds the effective column count for each row,
// accounting for both ColSpan and RowSpan. Uses a grid model where
// carry[col] tracks how many additional rows a column position is occupied.
func computeEffectiveColumns(ctx context.Context, doc *document.Document, rows []ast.PdfDict) []int {
	result := make([]int, len(rows))
	// carry[col] = remaining rows this column is occupied by a previous RowSpan.
	carry := make(map[int]int)

	for ri, row := range rows {
		colPos := 0 // Current column position in the grid.

		// Get cells for this row.
		cells := getRowCells(ctx, doc, row)

		for _, cell := range cells {
			// Skip columns occupied by RowSpan from previous rows.
			for carry[colPos] > 0 {
				colPos++
			}

			cs := cellColSpan(ctx, doc, cell)
			rs := cellRowSpan(ctx, doc, cell)

			// This cell occupies colPos..colPos+cs-1. Set carry to rs
			// (not rs-1) because the end-of-row decrement will subtract 1.
			if rs > 1 {
				for c := range cs {
					carry[colPos+c] = rs
				}
			}
			colPos += cs
		}

		// Count remaining carried columns after all explicit cells.
		for carry[colPos] > 0 {
			colPos++
		}

		result[ri] = colPos

		// Decrement carry counters for next row.
		for k, v := range carry {
			if v <= 1 {
				delete(carry, k)
			} else {
				carry[k] = v - 1
			}
		}
	}
	return result
}

// getRowCells extracts TD/TH cell dicts from a TR element.
func getRowCells(ctx context.Context, doc *document.Document, tr ast.PdfDict) []ast.PdfDict {
	kObj := tr["K"]
	if kObj == nil {
		return nil
	}
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		d, _ := document.ResolveDict(ctx, doc, kObj)
		if d != nil {
			return []ast.PdfDict{d}
		}
		return nil
	}
	var cells []ast.PdfDict
	for _, item := range arr {
		d, _ := document.ResolveDict(ctx, doc, item)
		if d == nil {
			continue
		}
		typeName, _ := document.ResolveName(ctx, doc, d["S"])
		if typeName == "TD" || typeName == "TH" {
			cells = append(cells, d)
		}
	}
	return cells
}

// collectTableRows extracts all TR elements from a Table, including those
// nested inside THead, TBody, and TFoot sections.
func collectTableRows(ctx context.Context, doc *document.Document, tableElem ast.PdfDict) []ast.PdfDict {
	var rows []ast.PdfDict
	kObj := tableElem["K"]
	if kObj == nil {
		return nil
	}

	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		// Single child.
		dict, err := document.ResolveDict(ctx, doc, kObj)
		if err == nil && dict != nil {
			collectRowsFrom(ctx, doc, dict, &rows)
		}
		return rows
	}

	for _, item := range arr {
		dict, err := document.ResolveDict(ctx, doc, item)
		if err != nil || dict == nil {
			continue
		}
		collectRowsFrom(ctx, doc, dict, &rows)
	}
	return rows
}

func collectRowsFrom(ctx context.Context, doc *document.Document, elem ast.PdfDict, rows *[]ast.PdfDict) {
	typeName, _ := document.ResolveName(ctx, doc, elem["S"])
	if typeName == "TR" {
		*rows = append(*rows, elem)
		return
	}
	// THead, TBody, TFoot contain TR children.
	if typeName == "THead" || typeName == "TBody" || typeName == "TFoot" {
		kObj := elem["K"]
		if kObj == nil {
			return
		}
		arr, err := document.ResolveArray(ctx, doc, kObj)
		if err != nil {
			dict, _ := document.ResolveDict(ctx, doc, kObj)
			if dict != nil {
				collectRowsFrom(ctx, doc, dict, rows)
			}
			return
		}
		for _, item := range arr {
			dict, _ := document.ResolveDict(ctx, doc, item)
			if dict != nil {
				collectRowsFrom(ctx, doc, dict, rows)
			}
		}
	}
}

// cellColSpan returns the ColSpan value for a TD/TH cell (default 1).
func cellColSpan(ctx context.Context, doc *document.Document, cell ast.PdfDict) int {
	// ColSpan is in the cell's attribute dict /A or table attribute array.
	aObj := cell["A"]
	if aObj == nil {
		return 1
	}
	// /A can be a dict or array of dicts.
	aDict, err := document.ResolveDict(ctx, doc, aObj)
	if err == nil && aDict != nil {
		if cs, _ := document.ResolveInt(ctx, doc, aDict["ColSpan"]); cs > 0 {
			return int(cs)
		}
		return 1
	}
	aArr, err := document.ResolveArray(ctx, doc, aObj)
	if err != nil {
		return 1
	}
	for _, item := range aArr {
		d, _ := document.ResolveDict(ctx, doc, item)
		if d == nil {
			continue
		}
		if cs, _ := document.ResolveInt(ctx, doc, d["ColSpan"]); cs > 0 {
			return int(cs)
		}
	}
	return 1
}

// cellRowSpan returns the RowSpan value for a TD/TH cell (default 1).
func cellRowSpan(ctx context.Context, doc *document.Document, cell ast.PdfDict) int {
	aObj := cell["A"]
	if aObj == nil {
		return 1
	}
	aDict, err := document.ResolveDict(ctx, doc, aObj)
	if err == nil && aDict != nil {
		if rs, _ := document.ResolveInt(ctx, doc, aDict["RowSpan"]); rs > 0 {
			return int(rs)
		}
		return 1
	}
	aArr, err := document.ResolveArray(ctx, doc, aObj)
	if err != nil {
		return 1
	}
	for _, item := range aArr {
		d, _ := document.ResolveDict(ctx, doc, item)
		if d == nil {
			continue
		}
		if rs, _ := document.ResolveInt(ctx, doc, d["RowSpan"]); rs > 0 {
			return int(rs)
		}
	}
	return 1
}

func tableColumnRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_2_5_26_t04
	}
	return Rule_14289_1_7_2_t43
}

// validateTableChildren checks Table children containment. Split from
// validateTableContainment for readability.
func validateTableChildren(state *walkState, typeName ast.PdfName, parentType ast.PdfName) {
	// Table children: Table may contain only TR, THead, TBody, TFoot, Caption.
	// THead/TBody/TFoot must contain only TR.
	// TR must contain only TH and TD.
	switch parentType {
	case "Table":
		switch typeName {
		case "TR", "THead", "TBody", "TFoot", "Caption":
			// Valid Table children.
		default:
			slog.Debug("compliance.structure: invalid child of Table",
				"parent", parentType, "child", typeName,
			)
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t03, "Table contains %s instead of TR/THead/TBody/TFoot/Caption", typeName)...)
		}
	case "THead", "TBody", "TFoot":
		if typeName != "TR" && typeName != "Caption" {
			slog.Debug("compliance.structure: invalid child of table section",
				"parent", parentType, "child", typeName,
			)
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t03, "%s contains %s instead of TR", parentType, typeName)...)
		}
	case "TR":
		switch typeName {
		case "TH", "TD":
			// Valid TR children.
		default:
			slog.Debug("compliance.structure: invalid child of TR",
				"parent", parentType, "child", typeName,
			)
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t10, "TR contains %s instead of TH/TD", typeName)...)
		}
	}
}

// validateTableSectionCounts checks that a Table has at most 1 THead, 1 TFoot,
// and that TFoot/THead require TBody (§7.2 t11-t14).
func validateTableSectionCounts(ctx context.Context, state *walkState, tableElem ast.PdfDict) {
	kObj, ok := tableElem["K"]
	if !ok {
		return
	}
	arr, err := document.ResolveArray(ctx, state.doc, kObj)
	if err != nil {
		return
	}

	var theadCount, tfootCount, tbodyCount, captionCount int
	for _, item := range arr {
		d, err := document.ResolveDict(ctx, state.doc, item)
		if err != nil || d == nil {
			continue
		}
		childType, _ := document.ResolveName(ctx, state.doc, d["S"])
		switch childType {
		case "THead":
			theadCount++
		case "TFoot":
			tfootCount++
		case "TBody":
			tbodyCount++
		case "Caption":
			captionCount++
		}
	}

	if theadCount > 1 {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t11, "Table has %d THead children (max 1)", theadCount)...)
	}
	if tfootCount > 1 {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t12, "Table has %d TFoot children (max 1)", tfootCount)...)
	}
	if tfootCount > 0 && tbodyCount == 0 {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t13, "Table has TFoot but no TBody")...)
	}
	if theadCount > 0 && tbodyCount == 0 {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t14, "Table has THead but no TBody")...)
	}
	if captionCount > 1 {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t39, "Table has %d Caption children (max 1)", captionCount)...)
	}
}

// validateListContainment checks PDF/UA list structure rules.
func validateListContainment(ctx context.Context, state *walkState, elem ast.PdfDict, typeName ast.PdfName, parentType ast.PdfName) {
	// L must contain only LI (and optionally Caption).
	if parentType == "L" && typeName != "LI" && typeName != "Caption" && typeName != "L" {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t09, "L contains %s instead of LI", typeName)...)
	}

	// LI must be inside L.
	if typeName == "LI" && parentType != "L" && parentType != "" {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t17, "LI inside %s instead of L", parentType)...)
	}

	// LBody must be inside LI.
	if typeName == "LBody" && parentType != "LI" && parentType != "" {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t18, "LBody inside %s instead of LI", parentType)...)
	}

	// LI must contain Lbl and/or LBody.
	if parentType == "LI" {
		switch typeName {
		case "Lbl", "LBody":
			// OK
		default:
			slog.Debug("compliance.structure: LI contains unexpected child", "child", typeName)
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
		}
	case "L":
		// L allows Caption only as first child.
		if childIdx != 0 {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t40,
					"Caption at position %d (not first) in L", childIdx+1)...)
		}
	case "TOC":
		// TOC allows Caption only as first child.
		if childIdx != 0 {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t28,
					"Caption at position %d (not first) in TOC", childIdx+1)...)
		}
	}
}

// validateLinkDestConsistency checks that all Link annotations within a
// Link/Reference structure element target the same destination.
// PDF/UA-2 §8.2.5.20 t2.
func validateLinkDestConsistency(ctx context.Context, state *walkState, elem ast.PdfDict) {
	kObj := elem["K"]
	if kObj == nil {
		return
	}

	arr, _ := document.ResolveArray(ctx, state.doc, kObj)
	if len(arr) == 0 {
		arr = ast.PdfArray{kObj}
	}

	var firstDest string
	found := false

	for _, kid := range arr {
		d, _ := document.ResolveDict(ctx, state.doc, kid)
		if d == nil {
			continue
		}
		typeName, _ := document.ResolveName(ctx, state.doc, d["Type"])
		if typeName != "OBJR" {
			continue
		}
		annot, _ := document.ResolveDict(ctx, state.doc, d["Obj"])
		if annot == nil {
			continue
		}
		subtype, _ := document.ResolveName(ctx, state.doc, annot["Subtype"])
		if subtype != "Link" {
			continue
		}

		dest := linkAnnotDest(ctx, state.doc, annot)
		if dest == "" {
			continue
		}

		if !found {
			firstDest = dest
			found = true
			continue
		}
		if dest != firstDest {
			*state.results = append(*state.results,
				failf(Rule_14289_2_8_2_5_20_t02,
					"Link/Reference element contains annotations targeting different destinations")...)
			return
		}
	}
}

// linkAnnotDest extracts a comparable destination string from a Link annotation.
// Handles both /Dest and /A (GoTo action with /D) forms.
func linkAnnotDest(ctx context.Context, doc *document.Document, annot ast.PdfDict) string {
	// Direct /Dest
	if dest := annot["Dest"]; dest != nil {
		return fmt.Sprintf("%v", dest)
	}
	// Action /A with /S=GoTo and /D
	aDict, _ := document.ResolveDict(ctx, doc, annot["A"])
	if aDict == nil {
		return ""
	}
	aType, _ := document.ResolveName(ctx, doc, aDict["S"])
	if aType == "GoTo" {
		// PDF 2.0 §12.6.4.2: /SD (structured destination) takes precedence
		// over /D when both are present.
		d := aDict["SD"]
		if d == nil {
			d = aDict["D"]
		}
		if d == nil {
			return ""
		}
		// Resolve indirect refs to get the actual destination array.
		resolved, _ := document.ResolveObject(ctx, doc, d)
		if resolved != nil {
			return fmt.Sprintf("%v", resolved)
		}
		return fmt.Sprintf("%v", d)
	}
	// URI action
	if aType == "URI" {
		uri, _ := document.ResolveString(ctx, doc, aDict["URI"])
		return "URI:" + string(uri)
	}
	return ""
}

func headingMixedRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_4_4_t02
	}
	return Rule{}
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

	slog.Debug("compliance.structure: checking heading hierarchy",
		"count", len(state.headingLevels),
	)

	maxSeen := 0
	for _, level := range state.headingLevels {
		// If the level jumps forward by more than 1, a level was skipped.
		// When maxSeen == 0 (first heading), the first heading must be H1.
		if level > maxSeen+1 {
			slog.Debug("compliance.structure: heading level skipped",
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

// validateListNumbering checks PDF/UA-2 §8.2.5.25: if any LI child of L has
// a Lbl descendant, the L element must have a ListNumbering attribute not "None".
func validateListNumbering(ctx context.Context, state *walkState, lElem ast.PdfDict) {
	// Check if any child LI has a Lbl child.
	kObj, ok := lElem["K"]
	if !ok {
		return
	}
	arr, err := document.ResolveArray(ctx, state.doc, kObj)
	if err != nil {
		return
	}

	hasLbl := false
	for _, item := range arr {
		d, err := document.ResolveDict(ctx, state.doc, item)
		if err != nil || d == nil {
			continue
		}
		childType, _ := document.ResolveName(ctx, state.doc, d["S"])
		if childType != "LI" {
			continue
		}
		if liHasLblChild(ctx, state.doc, d) {
			hasLbl = true
			break
		}
	}

	if !hasLbl {
		return
	}

	// Check for ListNumbering attribute on the L element.
	listNum := resolveAttributeValue(ctx, state.doc, lElem, "ListNumbering")
	if listNum == "" || listNum == "None" {
		*state.results = append(*state.results, failf(Rule_14289_2_8_2_5_25_t01,
			"L element with Lbl children has ListNumbering=%q", listNum)...)
	}
}

// liHasLblChild checks if an LI element has a direct Lbl child.
func liHasLblChild(ctx context.Context, doc *document.Document, liElem ast.PdfDict) bool {
	kObj, ok := liElem["K"]
	if !ok {
		return false
	}
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		// Single child.
		d, err := document.ResolveDict(ctx, doc, kObj)
		if err != nil || d == nil {
			return false
		}
		t, _ := document.ResolveName(ctx, doc, d["S"])
		return t == "Lbl"
	}
	for _, item := range arr {
		d, err := document.ResolveDict(ctx, doc, item)
		if err != nil || d == nil {
			continue
		}
		t, _ := document.ResolveName(ctx, doc, d["S"])
		if t == "Lbl" {
			return true
		}
	}
	return false
}

// resolveAttributeValue looks for a named attribute in the element's /A dict
// or /A array of attribute dicts, returning the string value or "".
func resolveAttributeValue(ctx context.Context, doc *document.Document, elem ast.PdfDict, attrName ast.PdfName) string {
	aObj, ok := elem["A"]
	if !ok {
		return ""
	}
	// /A can be a single dict or an array of dicts.
	if d, err := document.ResolveDict(ctx, doc, aObj); err == nil && d != nil {
		if v, _ := document.ResolveName(ctx, doc, d[attrName]); v != "" {
			return string(v)
		}
	}
	if arr, err := document.ResolveArray(ctx, doc, aObj); err == nil {
		for _, item := range arr {
			d, err := document.ResolveDict(ctx, doc, item)
			if err != nil || d == nil {
				continue
			}
			if v, _ := document.ResolveName(ctx, doc, d[attrName]); v != "" {
				return string(v)
			}
		}
	}
	return ""
}

// checkActualTextPUA validates PDF/A-4 §6.2.10.8: ActualText entries shall not
// contain Unicode PUA values (U+E000-U+F8FF, U+F0000-U+FFFFD, U+100000-U+10FFFD).
func checkActualTextPUA(ctx context.Context, doc *document.Document, catalog ast.PdfDict, treeRoot ast.PdfDict) []Violation {
	var results []Violation

	// Check structure element ActualText and Alt.
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		for _, key := range []ast.PdfName{"ActualText", "Alt"} {
			atObj, ok := elem[key]
			if !ok {
				continue
			}
			atStr, err := document.ResolveString(ctx, doc, atObj)
			if err != nil {
				slog.Debug("compliance.structure: cannot resolve attribute string", "error", err)
				continue
			}
			decoded := document.DecodePdfString(atStr)
			if containsPUARunes(decoded) || containsPUABytes([]byte(atStr)) {
				results = append(results, failf(Rule_19005_4_6_2_10_8_t01,
					"structure element %s contains PUA at %s", key, path)...)
				return
			}
		}
	})

	// Check content stream BDC ActualText spans.
	pages := collectPages(ctx, doc, catalog)
	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}
		streamData, err := getPageContentBytes(ctx, doc, page)
		if err != nil || len(streamData) == 0 {
			continue
		}
		// Scan for /ActualText in BDC operands.
		// BDC dict operands contain /ActualText as a key.
		if containsPUAInContentStreamActualText(streamData) {
			results = append(results, failf(Rule_19005_4_6_2_10_8_t01,
				"page %d: content stream ActualText contains PUA", pageIdx+1)...)
		}
	}

	return results
}

// containsPUABytes checks raw PDF string bytes (possibly UTF-16BE) for PUA code points.
// Handles both UTF-16BE (with or without BOM) and raw byte strings.
func containsPUABytes(data []byte) bool {
	// Check for UTF-16BE format (with BOM).
	start := 0
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		start = 2
	}
	// Scan as UTF-16BE pairs.
	for i := start; i+1 < len(data); i += 2 {
		hi := data[i]
		lo := data[i+1]
		cp := uint32(hi)<<8 | uint32(lo)
		// BMP PUA: U+E000-U+F8FF.
		if cp >= 0xE000 && cp <= 0xF8FF {
			return true
		}
	}
	return false
}

// containsPUARunes checks if a decoded string contains any Unicode PUA code points.
func containsPUARunes(s string) bool {
	for _, r := range s {
		if (r >= 0xE000 && r <= 0xF8FF) || // BMP PUA
			(r >= 0xF0000 && r <= 0xFFFFF) || // Supplementary PUA-A
			(r >= 0x100000 && r <= 0x10FFFF) { // Supplementary PUA-B
			return true
		}
	}
	return false
}

// containsPUAInContentStreamActualText scans raw content stream bytes for
// /ActualText entries in BDC property dicts that contain PUA code points.
func containsPUAInContentStreamActualText(data []byte) bool {
	// Look for /ActualText followed by a string value containing PUA.
	s := string(data)
	for {
		idx := strings.Index(s, "/ActualText")
		if idx < 0 {
			return false
		}
		s = s[idx+11:] // skip "/ActualText"
		rest := strings.TrimLeft(s, " \t\r\n")
		if len(rest) == 0 {
			return false
		}
		// Extract string value (parenthesized or hex).
		switch rest[0] {
		case '(':
			// Find matching close paren (handle nesting).
			depth := 1
			i := 1
			for i < len(rest) && depth > 0 {
				switch rest[i] {
				case '(':
					depth++
				case ')':
					depth--
				case '\\':
					i++ // skip escaped char
				}
				i++
			}
			strVal := rest[1 : i-1]
			// Decode as PDFDocEncoding or UTF-16BE.
			decoded := document.DecodePdfString(ast.PdfString(strVal))
			if containsPUARunes(decoded) {
				return true
			}
		case '<':
			// Hex string — find closing >.
			end := strings.Index(rest, ">")
			if end > 0 {
				hexStr := rest[1:end]
				decoded := document.DecodePdfString(ast.PdfString(decodeHexString(hexStr)))
				if containsPUARunes(decoded) {
					return true
				}
			}
		}
	}
}

// decodeHexString converts a hex string to bytes.
func decodeHexString(hex string) string {
	var buf []byte
	hex = strings.ReplaceAll(hex, " ", "")
	hex = strings.ReplaceAll(hex, "\n", "")
	hex = strings.ReplaceAll(hex, "\r", "")
	for i := 0; i+1 < len(hex); i += 2 {
		b := hexByte(hex[i])<<4 | hexByte(hex[i+1])
		buf = append(buf, b)
	}
	return string(buf)
}

func hexByte(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}

// isMathMLType checks if a structure element type name belongs to the MathML namespace.
// In PDF 2.0 structure trees, MathML elements use types like "math", "mi", "mo", etc.
func isMathMLType(typeName ast.PdfName) bool {
	// The top-level MathML element is "math" (case-sensitive per MathML spec).
	// Other MathML elements (mi, mo, mn, etc.) would also need this check,
	// but the test specifically checks the top-level "math" type.
	return typeName == "math" || typeName == "Math"
}

// checkUnmappedStructTypes walks the structure tree and flags non-standard
// types that are not in the /RoleMap. This catches types used in the document
// but never declared in the role mapping.
func checkUnmappedStructTypes(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict, profile string, results *[]Violation) {
	roleMap := loadRoleMap(ctx, doc, structTreeRoot)
	rule := roleMapNonStandardRule(profile)
	if rule.Specification == "" {
		return
	}

	reported := make(map[ast.PdfName]bool)
	walkStructureElements(ctx, doc, structTreeRoot, func(elem ast.PdfDict, _ string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		if typeName == "" || reported[typeName] {
			return
		}
		if isStandardStructType(typeName, profile) {
			return
		}
		// Check if the type is in the RoleMap.
		if _, inMap := roleMap[typeName]; inMap {
			return
		}
		reported[typeName] = true
		*results = append(*results, failf(rule,
			"non-standard structure type %s is not mapped to a standard type", typeName)...)
	})
}

// checkSingleDocumentRoot validates PDF/UA-2 §8.2.5.2: StructTreeRoot must
// contain exactly one child with type Document.
func checkSingleDocumentRoot(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict, results *[]Violation) {
	kObj, ok := structTreeRoot["K"]
	if !ok {
		*results = append(*results, fail(Rule_14289_2_8_2_5_2_t01,
			"StructTreeRoot has no /K entry")...)
		return
	}

	// /K can be a single element or an array.
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		// Single element — check if it's a Document.
		d, err := document.ResolveDict(ctx, doc, kObj)
		if err != nil || d == nil {
			return
		}
		typeName, _ := document.ResolveName(ctx, doc, d["S"])
		if typeName != "Document" {
			*results = append(*results, failf(Rule_14289_2_8_2_5_2_t01,
				"StructTreeRoot child is %s instead of Document", typeName)...)
		}
		return
	}

	// Count Document children.
	docCount := 0
	for _, item := range arr {
		d, err := document.ResolveDict(ctx, doc, item)
		if err != nil || d == nil {
			continue
		}
		typeName, _ := document.ResolveName(ctx, doc, d["S"])
		if typeName == "Document" {
			docCount++
		}
	}

	if docCount != 1 || len(arr) != 1 {
		*results = append(*results, failf(Rule_14289_2_8_2_5_2_t01,
			"StructTreeRoot has %d children (%d Document) instead of single Document", len(arr), docCount)...)
	}
}

// validateTHScope checks that TH elements have a /Scope attribute.
// When the table uses ID-based header association (TH has /ID attribute),
// /Scope is not required per ISO 14289-1, 7.5.
func validateTHScope(ctx context.Context, state *walkState, elem ast.PdfDict) {
	slog.Debug("compliance.structure: checking TH scope")

	// If the enclosing table uses the Headers+IDs association model
	// (any TD references TH via /Headers), /Scope is not required on any TH.
	if state.tableUsesHeaderIDs {
		slog.Debug("compliance.structure: table uses Headers+IDs, skipping /Scope requirement")
		return
	}

	// If TH has an /ID attribute, the table may use ID-based header
	// association (TD elements reference TH via /Headers). In that case,
	// /Scope is not required.
	if _, hasID := elem["ID"]; hasID {
		slog.Debug("compliance.structure: TH has /ID, skipping /Scope requirement (ID-based header association)")
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

// tableHasHeaderIDs scans a Table structure element's subtree to determine
// whether any TD cell uses the /Headers attribute (referencing TH IDs).
// When true, the table uses the Headers+IDs association model and TH elements
// do not need /Scope.
func tableHasHeaderIDs(ctx context.Context, doc *document.Document, tableElem ast.PdfDict) bool {
	kObj, ok := tableElem["K"]
	if !ok {
		return false
	}
	return scanForHeaders(ctx, doc, kObj, 0)
}

// scanForHeaders recursively searches structure element children for TD/TH
// elements that have /Headers in their attribute dict or directly on the element.
func scanForHeaders(ctx context.Context, doc *document.Document, kObj ast.PdfObject, depth int) bool {
	if ctx.Err() != nil || depth > maxStructTreeDepth {
		return false
	}

	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		// Single element.
		dict, err := document.ResolveDict(ctx, doc, kObj)
		if err != nil || dict == nil {
			return false
		}
		return elemHasHeaders(ctx, doc, dict, depth)
	}

	for _, item := range arr {
		dict, err := document.ResolveDict(ctx, doc, item)
		if err != nil || dict == nil {
			continue
		}
		if elemHasHeaders(ctx, doc, dict, depth) {
			return true
		}
	}
	return false
}

// elemHasHeaders checks if a structure element is a TD/TH with /Headers,
// or recurses into its children.
func elemHasHeaders(ctx context.Context, doc *document.Document, elem ast.PdfDict, depth int) bool {
	typeName, _ := document.ResolveName(ctx, doc, elem["S"])
	if typeName == "" {
		return false
	}

	// Check if this TD or TH has /Headers directly or in /A attributes.
	if typeName == "TD" || typeName == "TH" {
		if _, ok := elem["Headers"]; ok {
			return true
		}
		if findHeadersInAttributes(ctx, doc, elem["A"]) {
			return true
		}
	}

	// Recurse into children (TR, THead, TBody, TFoot, etc.).
	if kObj, ok := elem["K"]; ok {
		return scanForHeaders(ctx, doc, kObj, depth+1)
	}
	return false
}

// findHeadersInAttributes checks whether /Headers exists in attribute dict(s).
func findHeadersInAttributes(ctx context.Context, doc *document.Document, aObj ast.PdfObject) bool {
	if aObj == nil {
		return false
	}
	dict, err := document.ResolveDict(ctx, doc, aObj)
	if err == nil && dict != nil {
		if _, ok := dict["Headers"]; ok {
			return true
		}
	}
	arr, err := document.ResolveArray(ctx, doc, aObj)
	if err != nil {
		return false
	}
	for _, item := range arr {
		d, err := document.ResolveDict(ctx, doc, item)
		if err != nil || d == nil {
			continue
		}
		if _, ok := d["Headers"]; ok {
			return true
		}
	}
	return false
}

// checkPDFALangValid validates §6.8.4 (PDF/A-1) / §6.7.3.4 (PDF/A-2/3):
// if a /Lang entry is present in the catalog or structure elements, its value
// must be a valid RFC 3066 language tag.
func checkPDFALangValid(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int) []Violation {
	slog.Debug("compliance.structure: checking /Lang validity for PDF/A level 'a'", "profile", profile)

	rule := pdfaLangValidRule(part)

	var results []Violation

	// Check catalog /Lang.
	if langObj, ok := catalog["Lang"]; ok {
		langStr, err := document.ResolveString(ctx, doc, langObj)
		if err == nil {
			trimmed := strings.TrimSpace(document.DecodePdfString(langStr))
			if trimmed != "" && !isValidLangTag(trimmed) {
				slog.Debug("compliance.structure: catalog /Lang is invalid", "lang", trimmed)
				results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("catalog lang=%s", trimmed)})
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
		trimmed := strings.TrimSpace(document.DecodePdfString(langStr))
		if trimmed != "" && !isValidLangTag(trimmed) {
			slog.Debug("compliance.structure: invalid /Lang in structure element",
				"lang", trimmed, "path", path)
			results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("lang=%s, %s", trimmed, path)})
		}
	})

	// Also scan page /Properties resource dicts for /Lang values (used by BDC operators).
	pagesDict, _ := document.ResolveDict(ctx, doc, catalog["Pages"])
	inheritedRes := getResources(ctx, doc, pagesDict)
	pages := collectPages(ctx, doc, catalog)
	for pageIdx, page := range pages {
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
		propsDict, _ := document.ResolveDict(ctx, doc, res["Properties"])

		// Scan raw content stream for inline /Lang in BDC dicts.
		streamData, _ := getPageContentBytes(ctx, doc, page)
		if len(streamData) > 0 {
			s := string(streamData)
			searchStr := s
			for {
				idx := strings.Index(searchStr, "/Lang")
				if idx < 0 {
					break
				}
				searchStr = searchStr[idx+5:]
				rest := strings.TrimLeft(searchStr, " \t\r\n")
				if len(rest) == 0 {
					break
				}
				var langVal string
				if rest[0] == '(' {
					depth := 1
					i := 1
					for i < len(rest) && depth > 0 {
						switch rest[i] {
						case '(':
							depth++
						case ')':
							depth--
						case '\\':
							i++
						}
						i++
					}
					if i > 1 {
						langVal = document.DecodePdfString(ast.PdfString(rest[1 : i-1]))
					}
				} else if rest[0] == '<' && rest[0] != '<' || (len(rest) > 1 && rest[0] == '<' && rest[1] != '<') {
					// Hex string.
					end := strings.Index(rest[1:], ">")
					if end > 0 {
						langVal = document.DecodePdfString(ast.PdfString(decodeHexString(rest[1 : end+1])))
					}
				}
				trimmed := strings.TrimSpace(langVal)
				if trimmed != "" && !isValidLangTag(trimmed) {
					results = append(results, Violation{
						Rule:    rule,
						Context: fmt.Sprintf("lang=%s, page %d inline BDC", trimmed, pageIdx+1),
					})
					break
				}
			}
		}

		for propName, propObj := range propsDict {
			propDict, err := document.ResolveDict(ctx, doc, propObj)
			if err != nil || propDict == nil {
				continue
			}
			langObj, ok := propDict["Lang"]
			if !ok {
				continue
			}
			langStr, err := document.ResolveString(ctx, doc, langObj)
			if err != nil {
				slog.Debug("compliance.structure: cannot resolve Lang string", "error", err)
				continue
			}
			trimmed := strings.TrimSpace(document.DecodePdfString(langStr))
			if trimmed != "" && !isValidLangTag(trimmed) {
				results = append(results, Violation{
					Rule:    rule,
					Context: fmt.Sprintf("lang=%s, page %d property %s", trimmed, pageIdx+1, propName),
				})
			}
		}
	}

	if len(results) == 0 {
		slog.Debug("compliance.structure: all /Lang values valid for PDF/A level 'a'")
	}
	return results
}

// pdfaLangValidRule returns the Lang validity rule for PDF/A level 'a' profiles.
func pdfaLangValidRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_8_4_t01
	case 2: //nolint:mnd
		return Rule_19005_2_6_7_4_t01
	case 3: //nolint:mnd
		return Rule_19005_3_6_7_4_t01
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
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_8_3_4_t01
	case 2: //nolint:mnd
		return Rule_19005_2_6_7_3_4_t01
	case 3: //nolint:mnd
		return Rule_19005_3_6_7_3_4_t01
	}
	return Rule{}
}

// roleMapCircularRule returns the rule for circular role mappings.
func roleMapCircularRule(profile string) Rule {
	if isPDFUA(profile) {
		if len(profile) >= 7 && profile[6] == '2' {
			return Rule_14289_2_8_2_4_t02
		}
		return Rule_14289_1_7_1_t06
	}
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_8_3_4_t02
	case 2: //nolint:mnd
		return Rule_19005_2_6_7_3_4_t02
	case 3: //nolint:mnd
		return Rule_19005_3_6_7_3_4_t02
	}
	return Rule{}
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
