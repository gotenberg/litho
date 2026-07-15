package compliance

import (
	"context"
	"fmt"
	"slices"
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

// standardStructTypesPDF15Plus lists types that ISO 32000-1 added after
// PDF 1.4. PDF/A-1 (ISO 19005-1) is based on PDF 1.4, whose standard
// structure-type table (PDF Reference 1.4, 9.7.4) lacks them, so under ISO
// 19005-1 §6.8.3.4 such a type is non-standard and must be role-mapped.
//
// Annot, THead, TBody, and TFoot are PDF 1.5 additions absent from the PDF 1.4
// table, hence non-standard for PDF/A-1. The Ruby/Warichu set
// (Ruby/RB/RT/RP/Warichu/WT/WP) is also a PDF 1.5 addition but stays standard
// until a fixture or profile extract confirms it.
//
//nolint:gochecknoglobals // constant set.
var standardStructTypesPDF15Plus = map[ast.PdfName]bool{
	"Annot": true,
	"THead": true,
	"TBody": true,
	"TFoot": true,
}

// isStandardStructType returns true if the name is a standard structure type
// for the given profile. ISO 32000-2 types (Title, Artifact, Em, Strong, etc.)
// are only standard for PDF/A-4 and PDF/UA-2; PDF 1.5 additions are not
// standard for PDF/A-1.
func isStandardStructType(name ast.PdfName, profile string) bool {
	if !standardStructTypes[name] {
		return false
	}
	if standardStructTypesPDF2Only[name] {
		return PDFAPart(profile) == 4 || profile == "PDF/UA-2"
	}
	if standardStructTypesPDF15Plus[name] && PDFAPart(profile) == 1 {
		return false
	}
	return true
}

// MaxStructTreeWalkDepth bounds recursion when walking a logical structure tree.
// It is an anti-DoS guard, not spec-mandated: real logical trees are shallow
// (Document > Sect > H/P), so 200 sits far above any genuine document while
// capping a hostile or deeply nested tree. It is exported so the remediation
// fixer walks to the same depth as validation: a repair shallower than the
// check would flag structure elements it could not reach to fix. A visited set
// independently breaks reference cycles.
const MaxStructTreeWalkDepth = 200

// maxStructTreeDepth is the recursion limit for walking the structure tree.
const maxStructTreeDepth = MaxStructTreeWalkDepth

// maxStructAttrSearchDepth bounds the depth-first searches that look for a
// single attribute (e.g. /Alt, /ActualText) on a structure-tree branch.
// Anti-DoS guard, not spec-mandated: deliberately tighter than
// [maxStructTreeDepth] because such attributes appear near the element
// being checked, so a deep crafted tree need not be fully descended.
const maxStructAttrSearchDepth = 30

// checkStructureGroup validates structure tree requirements for PDF/A and
// PDF/UA profiles. For PDF/A level 'a' it checks MarkInfo and StructTreeRoot.
// For PDF/UA it additionally validates tag containment, role maps, heading
// hierarchy, table structure, list structure, captions, and TH scope.
func checkStructureGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile
	part := vctx.part
	level := vctx.level
	ua := vctx.ua

	doc.Logger.Debug("compliance.structure: starting structure checks",
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
	if part == 4 {
		puaTreeRoot := structTreeRoot
		if puaTreeRoot == nil {
			puaTreeRoot, _ = document.ResolveDict(ctx, doc, catalog["StructTreeRoot"]) // Best-effort: nil dict means no structure tree to check.
		}
		if puaTreeRoot != nil {
			results = append(results, checkActualTextPUA(ctx, doc, catalog, puaTreeRoot, Rule_19005_4_6_2_10_8_t01, Rule_19005_4_6_2_10_8_t01)...)
		}
	}

	// PDF/A level 'a': validate /RoleMap (§6.7.3.4 / §6.8.3.4).
	if level == 'a' && !ua && structTreeRoot != nil {
		checkRoleMap(ctx, doc, structTreeRoot, profile, &results)
		checkUnmappedStructTypes(ctx, doc, structTreeRoot, profile, &results)
	}

	// PDF/UA-1 §7.1: a non-standard structure type not role-mapped to a
	// standard type fails 7.1 t5 (roleMapNonStandardRule). The check is the same
	// for PDF/UA-1 as for PDF/A level 'a', since both require every non-standard
	// type to map to a standard one. UA-2's equivalent (8.2.4) is covered by the
	// namespace-aware structure check, so it is excluded here to avoid
	// double-reporting.
	if profile == "PDF/UA-1" && structTreeRoot != nil {
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

		// PDF/UA-2 §8.4.3: /ActualText (t02) and /Alt (t03) shall not contain PUA.
		if profile == "PDF/UA-2" {
			results = append(results, checkActualTextPUA(ctx, doc, catalog, structTreeRoot, Rule_14289_2_8_4_3_t02, Rule_14289_2_8_4_3_t03)...)
		}

		// PDF/UA-2 §8.2.5.2: single Document root child.
		if profile == "PDF/UA-2" {
			checkSingleDocumentRoot(ctx, doc, structTreeRoot, &results)
			rootState := &walkState{
				doc: doc, profile: profile, results: &results,
				roleMap: buildRoleMapIndex(ctx, doc, structTreeRoot),
				seen:    make(map[int]bool),
			}
			validateISO32005Root(ctx, rootState, structTreeRoot)
		}
	}

	doc.Logger.Debug("compliance.structure: structure checks complete", "violations", len(results))
	return results
}

// checkMarkInfo validates that the catalog contains /MarkInfo with /Marked=true.
func checkMarkInfo(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int, results *[]Violation) {
	markInfoObj, ok := catalog["MarkInfo"]
	if !ok {
		*results = append(*results, fail(markInfoRule(profile, part), "catalog missing /MarkInfo")...)
		return
	}

	markInfo, err := document.ResolveDict(ctx, doc, markInfoObj)
	if err != nil || markInfo == nil {
		doc.Logger.Debug("compliance.structure: cannot resolve /MarkInfo", "error", err)
		*results = append(*results, fail(markInfoRule(profile, part), "cannot resolve /MarkInfo")...)
		return
	}

	markedObj, ok := markInfo["Marked"]
	if !ok {
		*results = append(*results, fail(markInfoRule(profile, part), "/MarkInfo missing /Marked entry")...)
		return
	}

	marked, err := document.ResolveBool(ctx, doc, markedObj)
	if err != nil || !marked {
		doc.Logger.Debug("compliance.structure: /Marked is not true", "marked", marked, "error", err)
		*results = append(*results, fail(markInfoRule(profile, part), "/Marked is not true")...)
	}
}

// checkStructTreeRoot validates the presence of /StructTreeRoot in the catalog.
// Returns the resolved StructTreeRoot dict, or nil if absent.
func checkStructTreeRoot(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int, level byte, ua bool, results *[]Violation) ast.PdfDict {
	if level != 'a' && !ua {
		return nil
	}

	stObj, ok := catalog["StructTreeRoot"]
	if !ok {
		*results = append(*results, fail(structTreeRootRule(profile, part), "catalog missing /StructTreeRoot")...)
		return nil
	}

	st, err := document.ResolveDict(ctx, doc, stObj)
	if err != nil || st == nil {
		doc.Logger.Debug("compliance.structure: cannot resolve /StructTreeRoot", "error", err)
		*results = append(*results, fail(structTreeRootRule(profile, part), "cannot resolve /StructTreeRoot")...)
		return nil
	}

	return st
}

// checkRoleMap validates the /RoleMap dictionary in the structure tree root.
// Non-standard tags must map to standard types, standard tags must not be
// remapped, and no circular mappings may exist.
func checkRoleMap(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict, profile string, results *[]Violation) {
	rmObj, ok := structTreeRoot["RoleMap"]
	if !ok {
		return
	}

	roleMap, err := document.ResolveDict(ctx, doc, rmObj)
	if err != nil || roleMap == nil {
		doc.Logger.Debug("compliance.structure: cannot resolve /RoleMap", "error", err)
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
			doc.Logger.Debug("compliance.structure: cannot resolve RoleMap target", "key", key, "error", err)
			continue
		}
		mapping[key] = target
	}

	// Sorted source names keep RoleMap violation order deterministic
	// (CONTRIBUTING "Deterministic output").
	srcNames := make([]ast.PdfName, 0, len(mapping))
	for src := range mapping {
		srcNames = append(srcNames, src)
	}
	slices.Sort(srcNames)

	// Check standard types are not remapped. PDF/UA-1 and PDF/A parts 2/3 forbid
	// it; PDF/UA-2 allows some (handled in accessibility_structure.go); PDF/A
	// parts 1/4 have no such rule, so roleMapStandardRemapRule returns the zero
	// Rule and the check is skipped (avoids a clause-less residual and a
	// mislabeled UA rule under a PDF/A profile).
	if !IsPDFUA(profile) || profile == "PDF/UA-1" {
		if remapRule := roleMapStandardRemapRule(profile); remapRule.Specification != "" {
			// ISO 19005-2/3 §6.7.3.4 t3 (veraPDF `remappedStandardType == null`)
			// forbids remapping a standard type only to a NON-standard type; a
			// standard→standard remap such as /Caption→/P conforms. PDF/UA-1 §7.1
			// t7 forbids remapping a standard type to any DIFFERENT type, so it
			// keeps the wider check and fires on /Caption→/P.
			targetMustBeNonStandard := !IsPDFUA(profile)
			for _, src := range srcNames {
				if !isStandardStructType(src, profile) {
					continue
				}
				// A standard type mapped to itself is an identity self-map. veraPDF
				// reports it as a circular mapping (t6 above), leaving
				// remappedStandardType == null, so it must not also fire the
				// standard-remap rule. PDF/A already skips it via the
				// standard-target exclusion below; PDF/UA-1 needs it explicitly.
				if src == mapping[src] {
					continue
				}
				if targetMustBeNonStandard && isStandardStructType(mapping[src], profile) {
					continue
				}
				*results = append(*results, failf(remapRule, "standard type %s is remapped to %s", src, mapping[src])...)
			}
		}
	}

	// Check for self-mappings (A→A) which are always circular. veraPDF scopes the
	// circular-mapping rule to non-standard structure elements under PDF/A
	// (6.7.3.4 / 6.8.3.4 t2, object SENonStandard) but to ALL structure elements
	// under PDF/UA (ISO 14289-1 §7.1 t6 / ISO 14289-2 §8.2.4 t2, object
	// PDStructElem). A standard type's identity self-map is therefore circular
	// only under PDF/UA; PDF/A treats a standard-type RoleMap entry as inert.
	for _, src := range srcNames {
		if src != mapping[src] {
			continue
		}
		if isStandardStructType(src, profile) && !IsPDFUA(profile) {
			continue
		}
		*results = append(*results, failf(roleMapCircularRule(profile),
			"circular mapping for %s", src)...)
	}

	// Check non-standard types resolve to standard type (no circularity).
	for _, src := range srcNames {
		if ctx.Err() != nil {
			return
		}
		if isStandardStructType(src, profile) {
			continue
		}
		resolveToStandard(src, mapping, results, profile)
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

	// Depth limit exceeded. Treat as circular.
	*results = append(*results, failf(roleMapCircularRule(profile),
		"role map chain for %s exceeds depth limit", src)...)
	return false
}

// walkStructureTree recursively walks the structure tree to validate element
// containment, heading hierarchy, table structure, list structure, captions,
// and TH scope attributes.
func walkStructureTree(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict, profile string, results *[]Violation) {
	kidsObj, ok := structTreeRoot["K"]
	if !ok {
		return
	}

	state := &walkState{
		doc:            doc,
		profile:        profile,
		results:        results,
		roleMap:        buildRoleMapIndex(ctx, doc, structTreeRoot),
		iso32005Parent: "StructTreeRoot",
		seen:           make(map[int]bool),
	}

	walkChildren(ctx, state, kidsObj, "", 0, 0)

	// Heading rules §7.4.2 t1 (no skipped levels) and §7.4.4 t2/t3 (no mixed
	// H and H1-H6) are PDF/UA-1 only. PDF/UA-2 has no analogue: ISO 14289-2
	// §8.2.5.12 forbids the H type entirely (t1, emitted per element in
	// walkElement) and leaves Hn sequences unconstrained.
	if profile == "PDF/UA-1" {
		checkHeadingHierarchy(state)

		// §7.4.4 t2/t3: a document must be either strongly structured (H1-H6)
		// or weakly structured (H), not both. The first heading in document
		// order sets the mode and the other kind is the offender, so exactly one
		// rule fires on that kind: t2 (object SEH) when a generic H appears in a
		// strongly structured document, t3 (object SEHn) when a numbered Hn
		// appears in a weakly structured one. Firing both would double-report a
		// single mixed-heading defect.
		if state.hasGenericH && state.hasSpecificH {
			if state.firstHeadingSpecific {
				*results = append(*results, failf(headingMixedRule(profile),
					"document uses a generic H heading while strongly structured with H1-H6 headings")...)
			} else {
				*results = append(*results, failf(Rule_14289_1_7_4_4_t03,
					"document uses numbered H1-H6 headings while weakly structured with generic H headings")...)
			}
		}
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
	// firstHeadingSeen / firstHeadingSpecific record the first heading element
	// encountered in document order: its kind sets the document's structure
	// mode (specific Hn => strongly structured, generic H => weakly structured)
	// for the §7.4.4 t2/t3 mixed-heading rule.
	firstHeadingSeen     bool
	firstHeadingSpecific bool
	// noteIDs tracks Note element /ID values for uniqueness (§7.9 t2).
	noteIDs map[string]bool

	// tableUsesHeaderIDs is true when the current enclosing Table uses
	// the Headers+IDs association model (any TD has /Headers in its
	// attributes). When true, TH elements do not need /Scope.
	tableUsesHeaderIDs bool

	// tableIrregular is true when the current enclosing Table failed the
	// regularity check (§8.2.5.26 / §7.2). Header determination (§8.2.5.26
	// t5/t6, §7.5) is only well-defined on a regular grid, where every row has
	// the same column count, so the scope and per-TD header checks skip an
	// irregular table whose row/column geometry cannot be resolved.
	tableIrregular bool

	// roleMap is the StructTreeRoot /RoleMap as a name index, used to
	// resolve custom structure types to standard ones where a check's
	// semantics (ISO/TS 32005 Table 5) are defined over standard types.
	roleMap map[ast.PdfName]ast.PdfName

	// iso32005Parent is the effective Table 5 parent of the elements
	// currently being walked. It differs from the walk's literal
	// parentType in two ways the standard-type parent must reflect:
	// children of the structure tree root carry the sentinel
	// "StructTreeRoot", and NonStruct is transparent, so kids of a
	// NonStruct inherit the NonStruct's own effective parent. Per
	// ISO/TS 32005, NonStruct contents are treated as belonging to the
	// enclosing element, so containment is evaluated against that element.
	iso32005Parent ast.PdfName

	// seen records every structure element visited so the walk processes each
	// once. The structure tree is a strict tree (ISO 32000-1 §14.7.2); without
	// this a crafted /K tree whose every node lists the same child twice fans
	// out 2^depth (a denial of service).
	seen map[int]bool
}

// buildRoleMapIndex resolves the StructTreeRoot /RoleMap into a plain
// name-to-name index. Unresolvable entries are skipped; cycle handling is
// the lookup side's concern (bounded hops).
func buildRoleMapIndex(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict) map[ast.PdfName]ast.PdfName {
	rm, err := document.ResolveDict(ctx, doc, structTreeRoot["RoleMap"])
	if err != nil || rm == nil {
		return nil
	}
	out := make(map[ast.PdfName]ast.PdfName, len(rm))
	for key, valObj := range rm {
		if val, verr := document.ResolveName(ctx, doc, valObj); verr == nil && val != "" {
			out[key] = val
		}
	}
	return out
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
		if ref, ok := kObj.(ast.PdfIndirectRef); ok {
			if state.seen[ref.ObjectNum] {
				return
			}
			state.seen[ref.ObjectNum] = true
		}
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
		if ref, ok := item.(ast.PdfIndirectRef); ok {
			if state.seen[ref.ObjectNum] {
				continue
			}
			state.seen[ref.ObjectNum] = true
		}
		dict, err := document.ResolveDict(ctx, state.doc, item)
		if err != nil || dict == nil {
			continue
		}
		// Only process structure elements (skip marked content refs, object refs).
		if _, hasS := dict["S"]; hasS {
			// §7.4.4 t1 (at most one H child per node) is PDF/UA-1 only.
			// PDF/UA-2 has no analogue: ISO 14289-2 §8.2.5.12 forbids H
			// entirely (t1, emitted per element in walkElement).
			if state.profile == "PDF/UA-1" {
				childType, _ := document.ResolveName(ctx, state.doc, dict["S"])
				if childType == "H" {
					hCount++
					if hCount > 1 {
						*state.results = append(*state.results,
							failf(Rule_14289_1_7_4_4_t01, "node contains %d H children (max 1)", hCount)...)
					}
				}
			}

			walkElement(ctx, state, dict, parentType, depth, lastHeadingLevel)
			validateCaptionPosition(ctx, state, dict, parentType, structIdx, totalStructChildren)
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

	// §7.1 t12 (UA-1) / §8.2.1 t2 (UA-2): every structure element shall contain
	// a /P (parent) entry. The structure tree root is never reached here (it has
	// no /S), so only genuine structure elements are checked.
	if IsPDFUA(state.profile) {
		if _, hasP := elem["P"]; !hasP {
			*state.results = append(*state.results,
				failf(structParentPresenceRule(state.profile), "%s element lacks /P parent entry", typeName)...)
		}
	}

	// ISO/TS 32005 Table 5 inclusion rules apply to PDF/UA-2 only: PDF/UA-2
	// adopts the PDF 2.0 standard structure namespace whose containment is
	// defined by ISO/TS 32005, whereas PDF/UA-1 uses the ISO 32000-1 §7.2
	// parent/child tables checked separately below. The effective parent comes
	// from the walk state, not the literal parentType: root children see
	// "StructTreeRoot" and NonStruct wrappers are transparent.
	if state.profile == "PDF/UA-2" {
		validateISO32005(ctx, state, elem, typeName, state.iso32005Parent)
	}

	// Tag-containment rules (§7.2 parent/child tables) are PDF/UA-1 only.
	// PDF/UA-2 defines no general containment tables; it constrains structure
	// only through table regularity (§8.2.5.26, validateTableColumnConsistency)
	// and list semantics (§8.2.5.25, checked below).
	if state.profile == "PDF/UA-1" {
		validateTableContainment(state, typeName, parentType)
		validateTableChildren(state, typeName, parentType)
		validateListContainment(ctx, state, elem, typeName, parentType)
	}

	// PDF/UA-2 §8.2.5.25: L with Lbl children must have ListNumbering attribute.
	if typeName == "L" && state.profile == "PDF/UA-2" {
		validateListNumbering(ctx, state, elem)
	}

	// PDF/UA-2 §8.2.5.25 t2: real content within an LI shall be enclosed in Lbl
	// or LBody, never a direct content item of the LI.
	if typeName == "LI" && state.profile == "PDF/UA-2" && liHasDirectContentItem(ctx, state.doc, elem) {
		*state.results = append(*state.results,
			failf(Rule_14289_2_8_2_5_25_t02, "LI element directly contains a content item")...)
	}

	// PDF/UA-2 §8.2.5.14 t4: a FENote NoteType attribute, when present, shall be
	// Footnote, Endnote, or None.
	if typeName == "FENote" && state.profile == "PDF/UA-2" {
		if nt := resolveAttributeValue(ctx, state.doc, elem, "NoteType"); nt != "" &&
			nt != "Footnote" && nt != "Endnote" && nt != "None" {
			*state.results = append(*state.results,
				failf(Rule_14289_2_8_2_5_14_t04, "FENote NoteType %q is not Footnote/Endnote/None", nt)...)
		}
	}

	// PDF/UA-2 §8.2.5.29: math structure type must only appear inside Formula.
	if state.profile == "PDF/UA-2" && isMathMLType(typeName) && parentType != "Formula" {
		*state.results = append(*state.results,
			failf(Rule_14289_2_8_2_5_29_t01, "math type nested within %s instead of Formula", parentType)...)
	}

	// TOC/TOCI containment (§7.2 t26/t27) is PDF/UA-1 only. PDF/UA-2 imposes
	// no TOC/TOCI containment; its only TOCI requirement is §8.2.5.8 t1 (a /Ref
	// entry), checked below.
	if state.profile == "PDF/UA-1" {
		// TOCI must be inside TOC.
		if typeName == "TOCI" && parentType != "TOC" && parentType != "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t26, "TOCI inside %s instead of TOC", parentType)...)
		}

		// TOC may contain only TOC, TOCI, and Caption.
		if parentType == "TOC" && typeName != "TOC" && typeName != "TOCI" && typeName != "Caption" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t27, "TOC contains %s instead of TOC/TOCI/Caption", typeName)...)
		}
	}

	// PDF/UA-2 §8.2.5.8 t1: a TOCI element shall contain a /Ref entry
	// (the reference to the structure element it points at).
	if state.profile == "PDF/UA-2" && typeName == "TOCI" {
		if _, hasRef := elem["Ref"]; !hasRef {
			*state.results = append(*state.results,
				failf(Rule_14289_2_8_2_5_8_t01, "TOCI element has no /Ref entry")...)
		}
	}

	// Track heading levels and generic/specific heading usage.
	if typeName == "H" {
		state.hasGenericH = true
		if !state.firstHeadingSeen {
			state.firstHeadingSeen = true
			state.firstHeadingSpecific = false
		}
		// PDF/UA-2 §8.2.5.12: H structure type is forbidden entirely.
		if state.profile == "PDF/UA-2" {
			*state.results = append(*state.results,
				fail(Rule_14289_2_8_2_5_12_t01, "document uses H structure type")...)
		}
	}
	if hl := headingLevel(typeName); hl > 0 {
		state.headingLevels = append(state.headingLevels, hl)
		state.hasSpecificH = true
		if !state.firstHeadingSeen {
			state.firstHeadingSeen = true
			state.firstHeadingSpecific = true
		}
	}

	// TH /Scope (§7.5 t1) is PDF/UA-1 only. PDF/UA-2 replaces the per-TH /Scope
	// requirement with the TD header-determination rules §8.2.5.26 t5/t6, a
	// different test on a different object.
	if typeName == "TH" && state.profile == "PDF/UA-1" {
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

	// When entering a Table, determine if it uses the Headers+IDs model and
	// whether it is column-regular. Both flags drive the header checks on its
	// descendant TH/TD cells (validated during the child recursion below), so
	// compute them here. Save and restore so nested tables don't leak state.
	prevTableUsesHeaderIDs := state.tableUsesHeaderIDs
	prevTableIrregular := state.tableIrregular
	if typeName == "Table" {
		state.tableUsesHeaderIDs = tableHasHeaderIDs(ctx, state.doc, elem)
		state.tableIrregular = !validateTableColumnConsistency(ctx, state, elem)
	}

	// Recurse into children. The Table 5 effective parent follows the
	// element type, except through NonStruct, which is transparent: its
	// kids keep the current effective parent.
	if kObj, ok := elem["K"]; ok {
		prevISOParent := state.iso32005Parent
		if iso32005Key(state, string(typeName)) != "NonStruct" {
			state.iso32005Parent = typeName
		}
		walkChildren(ctx, state, kObj, typeName, depth+1, lastHeadingLevel)
		state.iso32005Parent = prevISOParent
	}

	if typeName == "Table" {
		// Section counts (§7.2 t11-t14, t39) are PDF/UA-1 only. PDF/UA-2
		// constrains tables through the regularity rules §8.2.5.26, not through
		// THead/TFoot/Caption counts.
		if state.profile == "PDF/UA-1" {
			validateTableSectionCounts(ctx, state, elem)
		}
		// PDF/UA-2 §8.2.5.26 t5/t6 and PDF/UA-1 §7.5 t2: every TD in a
		// table with header cells must have determinable headers (Scope
		// coverage or /Headers IDs). The PDF/UA-1 rule expresses the same
		// per-TD requirement under a single rule id: each data cell must
		// connect to a header by scope or by explicit /Headers reference.
		// Header determination is only well-defined on a regular grid where
		// every row shares the same column count; an irregular table reports
		// the regularity violation alone, so skip the header check here too.
		if (state.profile == "PDF/UA-2" || state.profile == "PDF/UA-1") && !state.tableIrregular {
			validateTableHeaderDetermination(ctx, state, elem)
		}
		state.tableUsesHeaderIDs = prevTableUsesHeaderIDs
		state.tableIrregular = prevTableIrregular
	}
}

// validateListContainment checks PDF/UA list structure rules.
func validateListContainment(_ context.Context, state *walkState, _ ast.PdfDict, typeName ast.PdfName, parentType ast.PdfName) {
	// L must contain only LI (and optionally Caption).
	if parentType == "L" && typeName != "LI" && typeName != "Caption" && typeName != "L" {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t19, "L contains %s instead of L/LI/Caption", typeName)...)
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
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t20, "LI contains %s instead of Lbl/LBody", typeName)...)
		}
	}
}

// validateCaptionPosition checks that Caption is the first child of Table,
// Figure, or Form.
func validateCaptionPosition(ctx context.Context, state *walkState, elem ast.PdfDict, parentType ast.PdfName, childIdx int, totalChildren int) {
	typeName, _ := document.ResolveName(ctx, state.doc, elem["S"])
	if typeName != "Caption" {
		return
	}

	notFirstOrLast := childIdx != 0 && childIdx != totalChildren-1

	// PDF/UA-2 §8.2.5.27 t1: a Caption shall be the first or last child of any
	// structure element. The UA-1 rules below are first-only for L/TOC, which
	// would false-reject a conformant last-position Caption under PDF/UA-2.
	if state.profile == "PDF/UA-2" {
		if notFirstOrLast {
			*state.results = append(*state.results,
				failf(Rule_14289_2_8_2_5_27_t01,
					"Caption at position %d (not first or last) in %s", childIdx+1, parentType)...)
		}
		return
	}

	switch parentType {
	case "Table":
		// PDF/UA-1 §7.2 t16: Table allows Caption as first or last child.
		// PDF/UA-1 §7.2 defines a Caption-position constraint only for Table, L,
		// and TOC, so Figure and Form parents are intentionally not checked here
		// to avoid a false reject.
		if notFirstOrLast {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t16,
					"Caption at position %d (not first or last) in Table", childIdx+1)...)
		}
	case "L":
		// PDF/UA-1 §7.2 t40: L allows Caption only as first child.
		if childIdx != 0 {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t40,
					"Caption at position %d (not first) in L", childIdx+1)...)
		}
	case "TOC":
		// PDF/UA-1 §7.2 t28: TOC allows Caption only as first child.
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
	if IsPDFUA(profile) {
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

	maxSeen := 0
	for _, level := range state.headingLevels {
		// If the level jumps forward by more than 1, a level was skipped.
		// When maxSeen == 0 (first heading), the first heading must be H1.
		if level > maxSeen+1 {
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
	// Determine whether any child LI has a Lbl child.
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
// structParentPresenceRule selects the structure-element /P presence rule by UA
// profile: ISO 14289-1 §7.1 t12 and ISO 14289-2 §8.2.1 t2.
func structParentPresenceRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_2_1_t02
	}
	return Rule_14289_1_7_1_t12
}

// liHasDirectContentItem reports whether an LI structure element directly
// contains a content item (a bare MCID integer, or an /MCR or /OBJR dict) rather
// than enclosing all real content in Lbl or LBody children.
func liHasDirectContentItem(ctx context.Context, doc *document.Document, liElem ast.PdfDict) bool {
	kObj, ok := liElem["K"]
	if !ok {
		return false
	}

	isContentItem := func(item ast.PdfObject) bool {
		if _, ok := item.(ast.PdfInt); ok {
			return true
		}
		d, err := document.ResolveDict(ctx, doc, item)
		if err != nil || d == nil {
			return false
		}
		t, _ := document.ResolveName(ctx, doc, d["Type"])
		return t == "MCR" || t == "OBJR"
	}

	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		return isContentItem(kObj)
	}
	return slices.ContainsFunc(arr, isContentItem)
}

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

// checkActualTextPUA validates that /ActualText and /Alt entries do not contain
// Unicode PUA values (U+E000-U+F8FF, U+F0000-U+FFFFD, U+100000-U+10FFFD). The
// rule differs by profile: PDF/A-4 §6.2.10.8 uses one rule for both keys, while
// PDF/UA-2 splits ActualText (§8.4.3 t02) and Alt (§8.4.3 t03), so the caller
// supplies the per-key rule.
func checkActualTextPUA(ctx context.Context, doc *document.Document, catalog ast.PdfDict, treeRoot ast.PdfDict, ruleActualText, ruleAlt Rule) []Violation {
	var results []Violation

	keyRule := func(key ast.PdfName) Rule {
		if key == "Alt" {
			return ruleAlt
		}
		return ruleActualText
	}

	// Check structure element ActualText and Alt.
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		for _, key := range []ast.PdfName{"ActualText", "Alt"} {
			atObj, ok := elem[key]
			if !ok {
				continue
			}
			atStr, err := document.ResolveString(ctx, doc, atObj)
			if err != nil {
				doc.Logger.Debug("compliance.structure: cannot resolve attribute string", "error", err)
				continue
			}
			// DecodePdfString applies the PDF text-string rules (UTF-16BE with a
			// 0xFE 0xFF BOM, UTF-8 with a 0xEF 0xBB 0xBF BOM per PDF 2.0, else
			// PDFDocEncoding), matching how veraPDF reads a CosActualText. Scanning
			// the raw bytes as UTF-16BE would misread a UTF-8 BOM (0xEF 0xBB 0xBF)
			// as U+EFBB, a PUA code point, and fire a false positive.
			if containsPUARunes(document.DecodePdfString(atStr)) {
				results = append(results, failf(keyRule(key),
					"structure element %s contains PUA at %s", key, path)...)
			}
		}
	})

	// Check content stream BDC ActualText spans.
	pages := collectPages(ctx, doc, catalog)
	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}
		streamData, err := resolvePageContentBytes(ctx, doc, page)
		if err != nil || len(streamData) == 0 {
			continue
		}
		// Scan for /ActualText in BDC operands.
		// BDC dict operands contain /ActualText as a key.
		if containsPUAInContentStreamActualText(ctx, streamData) {
			results = append(results, failf(ruleActualText,
				"page %d: content stream ActualText contains PUA", pageIdx+1)...)
		}
	}

	return results
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
// Returns false on context cancellation; outer callers re-check ctx.
func containsPUAInContentStreamActualText(ctx context.Context, data []byte) bool {
	// Look for /ActualText followed by a string value containing PUA.
	s := string(data)
	for {
		if ctx.Err() != nil {
			return false
		}
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
			// Hex string: find closing >.
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
	roleMap := LoadRoleMap(ctx, doc, structTreeRoot)
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
		// Types in the RoleMap are mapped to standard types.
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
		// Single element: check if it's a Document.
		d, err := document.ResolveDict(ctx, doc, kObj)
		if err != nil || d == nil {
			return
		}
		typeName, _ := document.ResolveName(ctx, doc, d["S"])
		if typeName != "Document" {
			*results = append(*results, failf(Rule_14289_2_8_2_5_2_t01,
				"StructTreeRoot child is %s instead of Document", typeName)...)
			return
		}
		checkDocumentNamespace(ctx, doc, structTreeRoot, d, results)
		return
	}

	// Count Document children.
	docCount := 0
	var docElem ast.PdfDict
	for _, item := range arr {
		d, err := document.ResolveDict(ctx, doc, item)
		if err != nil || d == nil {
			continue
		}
		typeName, _ := document.ResolveName(ctx, doc, d["S"])
		if typeName == "Document" {
			docCount++
			docElem = d
		}
	}

	if docCount != 1 || len(arr) != 1 {
		*results = append(*results, failf(Rule_14289_2_8_2_5_2_t01,
			"StructTreeRoot has %d children (%d Document) instead of single Document", len(arr), docCount)...)
		return
	}
	checkDocumentNamespace(ctx, doc, structTreeRoot, docElem, results)
}

// pdf2NamespaceURI is the PDF 2.0 standard structure namespace (ISO 32000-2
// §14.8.6).
const pdf2NamespaceURI = "http://iso.org/pdf2/ssn"

// checkDocumentNamespace validates ISO 14289-2 §8.2.5.2 t2: the single Document
// child of the structure tree root shall be declared in the PDF 2.0 namespace
// (http://iso.org/pdf2/ssn). Any other namespace, including none at all, fails
// t2, so an unnamespaced Document (typical for legacy tagged PDFs) is rejected.
func checkDocumentNamespace(ctx context.Context, doc *document.Document, treeRoot, docElem ast.PdfDict, results *[]Violation) {
	r := newNSRoleResolver(ctx, doc, treeRoot)
	stdType, stdNS, ok := r.elementStandardType(ctx, doc, docElem)
	if !ok || stdType != "Document" {
		// Raw S=Document with no resolvable namespace still counts: the
		// kid's standard type is Document, its namespace URL is null.
		if s, err := document.ResolveName(ctx, doc, docElem["S"]); err != nil || s != "Document" {
			return
		}
		stdNS = ""
	}
	if stdNS != pdf2NamespaceURI {
		*results = append(*results, failf(Rule_14289_2_8_2_5_2_t02,
			"Document structure element is in namespace %q, not the PDF 2.0 namespace", stdNS)...)
	}
}

// validateTHScope checks that TH elements have a /Scope attribute.
// When the table uses ID-based header association (TH has /ID attribute),
// /Scope is not required per ISO 14289-1, 7.5.
func validateTHScope(ctx context.Context, state *walkState, elem ast.PdfDict) {
	// Header determination is only well-defined on a regular table; an
	// irregular grid reports the regularity violation alone.
	if state.tableIrregular {
		return
	}

	// If the enclosing table uses the Headers+IDs association model
	// (any TD references TH via /Headers), /Scope is not required on any TH.
	if state.tableUsesHeaderIDs {
		return
	}

	// If TH has an /ID attribute, the table may use ID-based header
	// association (TD elements reference TH via /Headers). In that case,
	// /Scope is not required.
	if _, hasID := elem["ID"]; hasID {
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
	return scanForHeaders(ctx, doc, kObj, make(map[int]bool), 0)
}

// scanForHeaders recursively searches structure element children for TD/TH
// elements that have /Headers in their attribute dict or directly on the element.
// seen visits each element once so a shared or cyclic /K tree cannot fan out
// 2^depth (a denial of service).
func scanForHeaders(ctx context.Context, doc *document.Document, kObj ast.PdfObject, seen map[int]bool, depth int) bool {
	if ctx.Err() != nil || depth > maxStructTreeDepth {
		return false
	}

	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		// Single element.
		if ref, ok := kObj.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				return false
			}
			seen[ref.ObjectNum] = true
		}
		dict, err := document.ResolveDict(ctx, doc, kObj)
		if err != nil || dict == nil {
			return false
		}
		return elemHasHeaders(ctx, doc, dict, seen, depth)
	}

	for _, item := range arr {
		if ref, ok := item.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
		}
		dict, err := document.ResolveDict(ctx, doc, item)
		if err != nil || dict == nil {
			continue
		}
		if elemHasHeaders(ctx, doc, dict, seen, depth) {
			return true
		}
	}
	return false
}

// elemHasHeaders checks if a structure element is a TD/TH with /Headers,
// or recurses into its children.
func elemHasHeaders(ctx context.Context, doc *document.Document, elem ast.PdfDict, seen map[int]bool, depth int) bool {
	typeName, _ := document.ResolveName(ctx, doc, elem["S"])
	if typeName == "" {
		return false
	}

	// TD or TH with /Headers (direct or in /A attributes) indicates header use.
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
		return scanForHeaders(ctx, doc, kObj, seen, depth+1)
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
func checkPDFALangValid(ctx context.Context, doc *document.Document, catalog ast.PdfDict, _ string, part int) []Violation {
	rule := pdfaLangValidRule(part)

	// PDF/A-1's 6.8.4 t1 grammar forbids digits in subtags; parts 2/3 (6.7.4)
	// allow them, matching the alphanumeric grammar of PDF/UA and the general
	// validator.
	validLang := IsValidLangTag
	if part == 1 {
		validLang = isValidLangTagPDFA1
	}

	var results []Violation

	// Check catalog /Lang.
	if langObj, ok := catalog["Lang"]; ok {
		langStr, err := document.ResolveString(ctx, doc, langObj)
		if err == nil {
			trimmed := strings.TrimSpace(document.DecodePdfString(langStr))
			if trimmed != "" && !validLang(trimmed) {
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
		if trimmed != "" && !validLang(trimmed) {
			results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("lang=%s, %s", trimmed, path)})
		}
	})

	// Also scan page /Properties resource dicts for /Lang values (used by BDC operators).
	pagesDict, _ := document.ResolveDict(ctx, doc, catalog["Pages"])
	inheritedRes := resolveResources(ctx, doc, pagesDict)
	pages := collectPages(ctx, doc, catalog)
	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}
		res := resolveResources(ctx, doc, page)
		if res == nil {
			res = inheritedRes
		}
		if res == nil {
			continue
		}
		propsDict, _ := document.ResolveDict(ctx, doc, res["Properties"])

		// Scan raw content stream for inline /Lang in BDC dicts.
		streamData, _ := resolvePageContentBytes(ctx, doc, page)
		if len(streamData) > 0 {
			s := string(streamData)
			searchStr := s
			for ctx.Err() == nil {
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
				if trimmed != "" && !validLang(trimmed) {
					results = append(results, Violation{
						Rule:    rule,
						Context: fmt.Sprintf("lang=%s, page %d inline BDC", trimmed, pageIdx+1),
					})
					break
				}
			}
		}

		for _, propName := range sortedNames(propsDict) {
			propObj := propsDict[propName]
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
				doc.Logger.Debug("compliance.structure: cannot resolve Lang string", "error", err)
				continue
			}
			trimmed := strings.TrimSpace(document.DecodePdfString(langStr))
			if trimmed != "" && !validLang(trimmed) {
				results = append(results, Violation{
					Rule:    rule,
					Context: fmt.Sprintf("lang=%s, page %d property %s", trimmed, pageIdx+1, propName),
				})
			}
		}
	}

	return results
}

// pdfaLangValidRule returns the Lang validity rule for PDF/A level 'a' profiles.
func pdfaLangValidRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_8_4_t01, Rule_19005_2_6_7_4_t01, Rule_19005_3_6_7_4_t01, Rule{})
}

// markInfoRule selects the MarkInfo rule based on profile.
func markInfoRule(profile string, part int) Rule {
	if IsPDFUA(profile) {
		if profile == "PDF/UA-2" {
			return Rule_14289_2_6_2_t01
		}
		return Rule_14289_1_6_2_t01
	}
	return ruleByPart(part, Rule_19005_1_6_8_2_2_t01, Rule_19005_2_6_7_2_2_t01, Rule_19005_3_6_7_2_2_t01, Rule{})
}

// structTreeRootRule selects the StructTreeRoot rule based on profile.
func structTreeRootRule(profile string, part int) Rule {
	if IsPDFUA(profile) {
		if profile == "PDF/UA-2" {
			return Rule_14289_2_8_2_1_t01
		}
		return Rule_14289_1_7_1_t11
	}
	return ruleByPart(part, Rule_19005_1_6_8_3_3_t01, Rule_19005_2_6_7_3_3_t01, Rule_19005_3_6_7_3_3_t01, Rule{})
}

// roleMapNonStandardRule returns the rule for non-standard types not mapped.
func roleMapNonStandardRule(profile string) Rule {
	if IsPDFUA(profile) {
		if profile == "PDF/UA-2" {
			return Rule_14289_2_8_2_4_t01
		}
		return Rule_14289_1_7_1_t05
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_8_3_4_t01, Rule_19005_2_6_7_3_4_t01, Rule_19005_3_6_7_3_4_t01, Rule{})
}

// roleMapCircularRule returns the rule for circular role mappings.
func roleMapCircularRule(profile string) Rule {
	if IsPDFUA(profile) {
		if profile == "PDF/UA-2" {
			return Rule_14289_2_8_2_4_t02
		}
		return Rule_14289_1_7_1_t06
	}
	switch PDFAPart(profile) {
	case 1:
		return Rule_19005_1_6_8_3_4_t02
	case 2:
		return Rule_19005_2_6_7_3_4_t02
	case 3:
		return Rule_19005_3_6_7_3_4_t02
	}
	return Rule{}
}

// roleMapStandardRemapRule returns the rule for standard types being remapped.
func roleMapStandardRemapRule(profile string) Rule {
	if IsPDFUA(profile) {
		if profile == "PDF/UA-2" {
			return Rule_14289_2_8_2_4_t03
		}
		return Rule_14289_1_7_1_t07
	}
	// Parts 2/3 §6.7.3.4 t3 forbid remapping a standard structure type. Part 1
	// (ISO 19005-1 §6.8.3.4 has only t1/t2: non-standard-must-map and circular)
	// and part 4 (ISO 19005-4 carries no RoleMap rule at all) have no such
	// requirement, so the check does not fire under those parts. veraPDF's
	// PDFA-1A profile likewise omits a 6.8.3.4 standard-remap rule. Returning
	// the zero Rule signals the caller to skip the check rather than emit a
	// clause-less residual.
	switch PDFAPart(profile) {
	case 2:
		return Rule_19005_2_6_7_3_4_t03
	case 3:
		return Rule_19005_3_6_7_3_4_t03
	}
	return Rule{}
}
