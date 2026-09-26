package compliance

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// standardStructTypesPDF17 is the vocabulary of the PDF 1.7 standard structure
// namespace http://iso.org/pdf/ssn: the standard structure types of
// ISO 32000-1:2008, 14.8.4 (Table 333 onward), which ISO 32000-2:2020, 14.8.6
// designates as that namespace.
//
//nolint:gochecknoglobals // static spec table
var standardStructTypesPDF17 = map[ast.PdfName]bool{
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
}

// standardStructTypesPDF20 is the vocabulary of the PDF 2.0 standard structure
// namespace http://iso.org/pdf2/ssn: the standard structure types of
// ISO 32000-2:2020, 14.8.4 (Table 364), designated as that namespace by 14.8.6.
//
// It is not a superset of [standardStructTypesPDF17]. PDF 2.0 dropped Art,
// BlockQuote, TOC, TOCI, Index, Private, Quote, Note, Reference, BibEntry and
// Code, and added DocumentFragment, Aside, Title, Sub, Em, Strong, Artifact and
// FENote. Numbered headings are unbounded in this namespace
// (ISO 32000-2:2020, 14.8.4.6.2), so [iso32005HnRe] matches them instead of the
// table enumerating H1 to H6.
//
//nolint:gochecknoglobals // static spec table
var standardStructTypesPDF20 = map[ast.PdfName]bool{
	"Document": true, "DocumentFragment": true, "Part": true, "Sect": true,
	"Div": true, "Aside": true, "NonStruct": true, "Title": true, "Sub": true,
	"H": true, "P": true, "L": true, "LI": true, "Lbl": true, "LBody": true,
	"Table": true, "TR": true, "TH": true, "TD": true, "THead": true,
	"TBody": true, "TFoot": true, "Caption": true, "Span": true, "Link": true,
	"Annot": true, "Form": true, "Ruby": true, "RB": true, "RT": true,
	"RP": true, "Warichu": true, "WT": true, "WP": true, "FENote": true,
	"Figure": true, "Formula": true, "Artifact": true, "Em": true,
	"Strong": true,
}

// standardStructTypes lists every standard structure type of either standard
// structure namespace. It answers the profile-scoped question "does any version
// of the spec define this type", which is all a caller holding no namespace can
// ask. A caller that knows the namespace an element declares must use
// [isStandardStructTypeInNS] instead: the union accepts a type in a namespace
// that does not define it.
//
//nolint:gochecknoglobals // derived from the two spec tables above
var standardStructTypes = unionStructTypes(standardStructTypesPDF17, standardStructTypesPDF20)

// standardStructTypesPDF2Only lists types added in ISO 32000-2 that are NOT
// standard structure types in ISO 32000-1. For PDF/UA-1 and PDF/A-1/2/3,
// these are non-standard and CAN be legitimately remapped.
//
//nolint:gochecknoglobals // derived from the two spec tables above
var standardStructTypesPDF2Only = differenceStructTypes(standardStructTypesPDF20, standardStructTypesPDF17)

// unionStructTypes returns the names present in either table.
func unionStructTypes(a, b map[ast.PdfName]bool) map[ast.PdfName]bool {
	out := make(map[ast.PdfName]bool, len(a)+len(b))
	for name := range a {
		out[name] = true
	}
	for name := range b {
		out[name] = true
	}
	return out
}

// differenceStructTypes returns the names present in a but absent from b.
func differenceStructTypes(a, b map[ast.PdfName]bool) map[ast.PdfName]bool {
	out := make(map[ast.PdfName]bool, len(a))
	for name := range a {
		if !b[name] {
			out[name] = true
		}
	}
	return out
}

// mathMLNamespaceURI is the MathML namespace ISO 32000-2:2020, 14.8.6 admits
// as a standard structure namespace alongside the two PDF ones.
const mathMLNamespaceURI = "http://www.w3.org/1998/Math/MathML"

// isStandardStructTypeInNS reports whether name is a standard structure type of
// the standard structure namespace nsURI.
//
// The set is namespace-parameterised, not a union: ISO 32000-2:2020, 14.8.6
// gives the PDF 1.7 and PDF 2.0 namespaces different vocabularies, so
// BlockQuote is standard only in the former and FENote only in the latter.
// Judging either against the union accepts a type in a namespace that does not
// define it. The MathML namespace constrains no element name
// (ISO 32000-2:2020, 14.8.6), so every type in it is standard.
//
// An empty nsURI means the element declares no /NS and so belongs to no
// namespace, in which case ISO 32000-1:2008, 14.8.4 governs and the PDF 1.7
// vocabulary applies. A namespace litho does not recognise defines no standard
// type at all, so it answers false rather than falling back to a vocabulary.
func isStandardStructTypeInNS(name ast.PdfName, nsURI string) bool {
	switch nsURI {
	case pdf1NamespaceURI, "":
		return standardStructTypesPDF17[name]
	case pdf2NamespaceURI:
		return standardStructTypesPDF20[name] || iso32005HnRe.MatchString(string(name))
	case mathMLNamespaceURI:
		return true
	default:
		return false
	}
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

// IsStandardStructType reports whether name is a standard structure type for the
// given profile. It is the profile-aware authority the remediation role-map
// fixer shares so it does not remap or drop a type inconsistently with what the
// compliance checker considers standard.
func IsStandardStructType(name ast.PdfName, profile string) bool {
	return isStandardStructType(name, profile)
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
// It aliases [MaxStructTreeWalkDepth], which states the anti-DoS rationale.
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
			// ISO 19005-4 §6.2.10.8 is scoped to CosActualText; the profile
			// defines no CosAlt rule, so /Alt is not judged at PDF/A-4.
			results = append(results, checkActualTextPUA(ctx, doc, catalog, puaTreeRoot, Rule_19005_4_6_2_10_8_t01, Rule{})...)
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

	// PDF/UA-2 §8.6 t1: an outline item /Title, the catalog /Lang and a
	// marked-content /Lang are text strings outside the structure tree, so they
	// are judged whether or not the document carries one. veraPDF scopes the
	// clause to CosTextString and reports a PUA title on a file whose catalog
	// names no /StructTreeRoot; running the check inside the structure-tree
	// branch above left exactly those files unreported.
	if profile == "PDF/UA-2" {
		results = append(results, checkTextStringPUA(ctx, vctx)...)
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

	// Every /RoleMap rule is scoped to a structure ELEMENT, not to the map:
	// ISO 14289-1 7.1 t5/t6/t7, ISO 14289-2 8.2.4 t1/t2/t3 and ISO 19005-2
	// 6.7.3.4 t1/t2/t3 all carry veraPDF's SENonStandard or PDStructElem as
	// their object, and each judges an element whose type resolves through the
	// map. An entry no element uses has no element to answer for it, so a
	// circular or dangling chain sitting there is inert. Reporting it condemned
	// documents veraPDF passes, and on a cycle spanning several entries it
	// reported one violation per entry where veraPDF reports one per element.
	used := collectUsedStructTypes(ctx, doc, structTreeRoot)

	// Sorted source names keep RoleMap violation order deterministic
	// (CONTRIBUTING "Deterministic output").
	srcNames := make([]ast.PdfName, 0, len(mapping))
	for src := range mapping {
		if !used[src] {
			continue
		}
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
		if resolveToStandard(src, mapping, results, profile) {
			continue
		}
		// PDF/UA-2 is the one profile with no element walk for the
		// non-standard rule: checkUnmappedStructTypes is not called for it,
		// because its namespace-aware walk owns every element that declares a
		// namespace. That walk skips namespace-less types, which resolve
		// through the legacy /RoleMap, so they are answered here.
		if IsPDFUA(profile) && PDFUAPart(profile) == 2 &&
			classifyRoleChain(src, mapping, profile) == roleChainUnmapped {
			*results = append(*results, failf(roleMapNonStandardRule(profile),
				"non-standard type %s is not mapped to a standard type", src)...)
		}
	}
}

// collectUsedStructTypes returns every type a structure element in the tree
// carries as its /S. It is what scopes the /RoleMap rules to the elements they
// judge; see [checkRoleMap] for the clauses.
//
// The walk is bounded by [maxStructTreeDepth] and by an object-number visited
// set, so a tree that points back at itself terminates.
func collectUsedStructTypes(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict) map[ast.PdfName]bool {
	used := make(map[ast.PdfName]bool)
	seen := make(map[int]bool)

	var walk func(obj ast.PdfObject, depth int)
	walk = func(obj ast.PdfObject, depth int) {
		if ctx.Err() != nil || depth > maxStructTreeDepth {
			return
		}

		if ref, ok := obj.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				return
			}
			seen[ref.ObjectNum] = true
		}

		if arr, err := document.ResolveArray(ctx, doc, obj); err == nil {
			for _, item := range arr {
				walk(item, depth+1)
			}
			return
		}

		dict, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || dict == nil {
			return
		}
		if sObj, hasS := dict["S"]; hasS {
			if name, err := document.ResolveName(ctx, doc, sObj); err == nil {
				used[name] = true
			}
		}
		if kids, hasK := dict["K"]; hasK {
			walk(kids, depth+1)
		}
	}

	if kids, ok := structTreeRoot["K"]; ok {
		walk(kids, 0)
	}
	return used
}

// roleChainOutcome says where a role map chain ends.
type roleChainOutcome int

const (
	roleChainStandard roleChainOutcome = iota // terminates at a standard type
	roleChainUnmapped                         // terminates at a non-standard type
	roleChainCircular                         // revisits a type it already passed
	roleChainDepth                            // longer than [maxStructTreeDepth]
)

// classifyRoleChain follows the role map chain from src and says where it ends.
func classifyRoleChain(src ast.PdfName, mapping map[ast.PdfName]ast.PdfName, profile string) roleChainOutcome {
	visited := map[ast.PdfName]bool{src: true}
	current := src

	for range maxStructTreeDepth {
		target, ok := mapping[current]
		if !ok {
			// current is not in the mapping; check if it is a standard type.
			if isStandardStructType(current, profile) {
				return roleChainStandard
			}
			return roleChainUnmapped
		}

		if isStandardStructType(target, profile) {
			return roleChainStandard
		}

		if visited[target] {
			return roleChainCircular
		}
		visited[target] = true
		current = target
	}

	return roleChainDepth
}

// resolveToStandard follows the role map chain from src and returns true if it
// terminates at a standard type. It reports a circular chain, which every
// profile scopes to the map entry. It does NOT report a chain that ends at a
// non-standard type: that rule is scoped to the structure element, so
// [checkUnmappedStructTypes] owns it and reports once per element.
func resolveToStandard(src ast.PdfName, mapping map[ast.PdfName]ast.PdfName, results *[]Violation, profile string) bool {
	switch classifyRoleChain(src, mapping, profile) {
	case roleChainStandard:
		return true
	case roleChainCircular:
		*results = append(*results, failf(roleMapCircularRule(profile),
			"circular mapping for %s", src)...)
	case roleChainDepth:
		*results = append(*results, failf(roleMapCircularRule(profile),
			"role map chain for %s exceeds depth limit", src)...)
	case roleChainUnmapped:
	}
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
		classMap:       structClassMap(ctx, doc, structTreeRoot),
		iso32005Parent: "StructTreeRoot",
		ua1Parent:      ua1ParentRef{root: true},
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
		// or weakly structured (H), not both. The clause states the same
		// requirement once per object, t2 against SEH and t3 against SEHn, so a
		// document carrying both kinds fails both: each kind is the offender
		// from the other's point of view. Picking a single "mode" from the first
		// heading and reporting only the other kind reports half the defect.
		if state.hasGenericH && state.hasSpecificH {
			*results = append(*results, failf(headingMixedRule(profile),
				"document uses a generic H heading while also using H1-H6 headings")...)
			*results = append(*results, failf(Rule_14289_1_7_4_4_t03,
				"document uses numbered H1-H6 headings while also using generic H headings")...)
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

	// tableIrregular is true when the current enclosing Table failed the
	// regularity check (§8.2.5.26 / §7.2). Header determination (§8.2.5.26
	// t5/t6, §7.5) is only well-defined on a regular grid, where every row has
	// the same column count, so the per-TD header check skips an irregular
	// table whose row/column geometry cannot be resolved.
	tableIrregular bool

	// roleMap is the StructTreeRoot /RoleMap as a name index, used to
	// resolve custom structure types to standard ones where a check's
	// semantics (ISO/TS 32005 Table 5) are defined over standard types.
	roleMap map[ast.PdfName]ast.PdfName

	// classMap is the StructTreeRoot /ClassMap, which resolves the attribute
	// classes a structure element names through /C (ISO 32000-1, 14.7.5.2).
	classMap ast.PdfDict

	// iso32005Parent is the effective Table 5 key of the parent of the
	// elements currently being walked, already resolved through the role
	// map by [iso32005Key]. It differs from the walk's literal parentType
	// in two ways the standard-type parent must reflect: children of the
	// structure tree root carry the sentinel "StructTreeRoot", and the
	// wrappers in [iso32005PassThroughTypes] are transparent, so their kids
	// inherit the wrapper's own effective parent. Table 5 treats what those
	// wrappers contain as belonging to the enclosing element, so containment
	// is evaluated against that element. It is empty when the parent resolves
	// to no standard type, which leaves the child outside every Table 5
	// parent-child row.
	iso32005Parent string

	// seq numbers each structure element the walk enters, and ua1ParentSeq is
	// the number of the element whose children are being walked. The PDF/UA-1
	// list rules state their verdict against the PARENT element (veraPDF keys
	// §7.2 t19 on SEL and t20 on SELI, each testing the whole kidsStandardTypes
	// string at once), so litho, which visits children, needs the parent's
	// identity to state each finding once. listReported records the parents
	// already reported, keyed by [listRuleKey].
	seq          int
	ua1ParentSeq int
	listReported map[int]bool

	// ua1Parent is veraPDF's parentStandardType for the elements currently
	// being walked, which the PDF/UA-1 containment rules test. It reads through
	// the role map and past Div, Part, and NonStruct as iso32005Parent does, but
	// differs at the top of the tree: a child of the structure tree root has no
	// parent element, and the kids of a pass-through element directly under the
	// root see that element's own type, which is where veraPDF's climb stops.
	ua1Parent ua1ParentRef

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
		// Only process structure elements (skip marked content refs, object refs):
		// [isStructElemDict] keeps an element whose /S is missing.
		if isStructElemDict(ctx, state.doc, dict) {
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
		}
	}
}

// validateCaptionKidPositions checks ISO 14289-2 §8.2.5.27 t1: a Caption shall
// be the first or last child of the structure element that contains it.
//
// The requirement is about the standard type a kid stands for, so each kid is
// resolved as [iso32005Kids] resolves it: the /RoleMap applies first (ISO
// 32000-2:2020, 14.8.6), and a Div, Part, or NonStruct kid contributes its own
// kids in its place. veraPDF evaluates the rule over the same list
// (GFPDStructTreeNode.getkidsStandardTypes).
//
// # Corpus evidence
//
//   - govdocs thread_100_100184.pdf: its role map maps Caption to P, so its
//     mid-Sect Caption elements are paragraphs.
func validateCaptionKidPositions(ctx context.Context, state *walkState, elem ast.PdfDict) {
	kidKeys, _ := iso32005Kids(ctx, state, elem)
	for i := 1; i < len(kidKeys)-1; i++ {
		if kidKeys[i] == "Caption" {
			*state.results = append(*state.results,
				failf(Rule_14289_2_8_2_5_27_t01,
					"Caption at position %d (not first or last) of %d kids", i+1, len(kidKeys))...)
			return
		}
	}
}

// walkElement validates a single structure element and recurses into children.
//
// # Corpus evidence
//
//   - govdocs thread_000_000150.pdf: maps LI_Label to Lbl and LI_Title to
//     LBody, so comparing the PDF/UA-1 containment names as written reported
//     its list items' kids as foreign to LI.
//   - verapdf 8.2.5.29 fixtures: litho's reading of the math-in-Formula test
//     was measured against veraPDF on them and on crafted variants.
//   - govdocs thread_000_000838.pdf: its "Heading 2", which its role map maps
//     to H2, is a level-2 heading for ISO 14289-1 §7.4.2.
//   - govdocs thread_400_400934.pdf: writes its notes as /Footnote, mapped to
//     /Note, without /ID, and veraPDF reports ISO 14289-1, 7.9 t1 while
//     litho, reading /S, did not.
func walkElement(ctx context.Context, state *walkState, elem ast.PdfDict, parentType ast.PdfName, depth int, lastHeadingLevel int) {
	if ctx.Err() != nil {
		return
	}

	typeName, err := document.ResolveName(ctx, state.doc, elem["S"])
	if err != nil || typeName == "" {
		if isStructElemDict(ctx, state.doc, elem) {
			walkTypelessElement(ctx, state, elem, depth, lastHeadingLevel)
		}
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
		validateCaptionKidPositions(ctx, state, elem)
	}
	if state.profile == "PDF/UA-1" {
		validateUA1CaptionKidPositions(ctx, state, elem, typeName)
	}

	// Tag-containment rules (§7.2 parent/child tables) are PDF/UA-1 only.
	// PDF/UA-2 defines no general containment tables; it constrains structure
	// only through table regularity (§8.2.5.26, validateTableColumnConsistency)
	// and list semantics (§8.2.5.25, checked below).
	//
	// veraPDF judges every one of them by standard type: the element's own (its
	// SETR, SELI and the rest are built from it), its parent's (parentStandardType,
	// which looks through Div, Part, and NonStruct), and its kids'
	// (kidsStandardTypes). A role map can map a producer's own list label and
	// body types to Lbl and LBody, so comparing the names as written would
	// report those kids as foreign to LI.
	if state.profile == "PDF/UA-1" {
		selfType := ast.PdfName(iso32005Key(ctx, state, elem, typeName))
		parent := ua1ParentType(state.ua1Parent)
		validateTableContainment(state, selfType, parent)
		validateTableChildren(state, selfType, parent)
		validateListContainment(ctx, state, elem, selfType, parent)
	}

	// PDF/UA-2 §8.2.5.25: L with Lbl children must have ListNumbering attribute.
	// veraPDF scopes the rule to its SEL object, which it builds for an element
	// whose standard type is L, so the role map decides which elements are lists.
	if state.profile == "PDF/UA-2" && iso32005Key(ctx, state, elem, typeName) == "L" {
		validateListNumbering(ctx, state, elem)
	}

	// PDF/UA-2 §8.2.5.25 t2: real content within an LI shall be enclosed in Lbl
	// or LBody, never a direct content item of the LI. veraPDF scopes the rule
	// to its SELI object, built for an element whose standard type is LI, and
	// its hasContentItems looks through Div, Part, and NonStruct kids, as
	// [iso32005Kids] does.
	if state.profile == "PDF/UA-2" && iso32005Key(ctx, state, elem, typeName) == "LI" {
		if _, hasContent := iso32005Kids(ctx, state, elem); hasContent {
			*state.results = append(*state.results,
				failf(Rule_14289_2_8_2_5_25_t02, "LI element directly contains a content item")...)
		}
	}

	// PDF/UA-2 §8.2.5.14 t4: a FENote NoteType attribute, when present, shall be
	// Footnote, Endnote, or None. The requirement binds the structure type the
	// element stands for through the role map, as veraPDF's SEFENote does, and
	// reads NoteType as [feNoteType] does.
	if state.profile == "PDF/UA-2" && iso32005Key(ctx, state, elem, typeName) == "FENote" {
		if nt := feNoteType(ctx, state, elem); nt != "Footnote" && nt != "Endnote" && nt != "None" {
			*state.results = append(*state.results,
				failf(Rule_14289_2_8_2_5_14_t04, "FENote NoteType %q is not Footnote/Endnote/None", nt)...)
		}
	}

	// PDF/UA-2 §8.2.5.29 t1: the math structure type shall occur only as a child
	// of a Formula structure element. The validation profile's test binds every
	// element that stands for a MathML type through the role map (veraPDF builds
	// its SEMathMLStructElem from the standard type), so a /S of math outside
	// the MathML namespace is no math element. It relaxes "child" to any
	// ancestor that stands for Formula or for a MathML type
	// (hasParentFormulaOrMathML), so the kids of math pass and so does a math
	// wrapped inside its Formula; litho follows the test, measured against
	// veraPDF.
	if state.profile == "PDF/UA-2" && standsForMathML(ctx, state, elem, typeName) &&
		!hasFormulaOrMathMLAncestor(ctx, state, elem) {
		*state.results = append(*state.results,
			failf(Rule_14289_2_8_2_5_29_t01, "MathML element %s has no Formula ancestor", typeName)...)
	}

	// TOC/TOCI containment (§7.2 t26/t27) is PDF/UA-1 only. PDF/UA-2 imposes
	// no TOC/TOCI containment; its only TOCI requirement is §8.2.5.8 t1 (a /Ref
	// entry), checked below.
	if state.profile == "PDF/UA-1" {
		// Both rules read standard types, as the containment checks above do.
		selfType := ast.PdfName(iso32005Key(ctx, state, elem, typeName))
		parent := ua1ParentType(state.ua1Parent)

		// TOCI must be inside TOC.
		if selfType == "TOCI" && parent != "TOC" && parent != "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t26, "TOCI inside %s instead of TOC", parent)...)
		}

		// TOC may contain only TOC, TOCI, and Caption. A kid with no standard
		// type, or a Div, Part, or NonStruct kid, is not one of veraPDF's
		// kidsStandardTypes entries.
		if parent == "TOC" && ua1KidJudged(selfType) && selfType != "TOC" && selfType != "TOCI" && selfType != "Caption" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t27, "TOC contains %s instead of TOC/TOCI/Caption", selfType)...)
		}
	}

	// PDF/UA-2 §8.2.5.8 t1: a TOCI element shall identify its target with a /Ref
	// entry, on the TOCI itself or on one of its child structure elements. The
	// requirement binds the structure type the element stands for through the
	// role map, as veraPDF's SETOCI does, and accepts a /Ref anywhere below it,
	// as its containsRef does. Measured against veraPDF.
	if state.profile == "PDF/UA-2" && iso32005Key(ctx, state, elem, typeName) == "TOCI" {
		if !structElemContainsRef(ctx, state.doc, elem, 0, make(map[int]bool)) {
			*state.results = append(*state.results,
				failf(Rule_14289_2_8_2_5_8_t01, "TOCI element has no /Ref entry")...)
		}
	}

	// Track heading levels and generic/specific heading usage, by standard type:
	// veraPDF builds its SEHn and SEH objects from the standard type, so a custom
	// type the role map maps to H2 is a level-2 heading for ISO 14289-1 §7.4.2.
	headingType := iso32005StandardName(ctx, state, elem, typeName)
	if headingType == "H" {
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
	if hl := headingLevel(headingType); hl > 0 {
		state.headingLevels = append(state.headingLevels, hl)
		state.hasSpecificH = true
		if !state.firstHeadingSeen {
			state.firstHeadingSeen = true
			state.firstHeadingSpecific = true
		}
	}

	// The Formula and Note requirements bind the structure type an element stands
	// for, not the name it is written with: veraPDF judges its SEFormula and
	// SENote objects, which it builds from the standard type the /RoleMap maps
	// to (ISO 32000-1, 14.7.3). A custom type mapped to /Note is a Note for ISO
	// 14289-1, 7.9 t1. Measured against veraPDF with a mapped and an unmapped
	// name for each.
	standardType := iso32005Key(ctx, state, elem, typeName)

	// §7.7 t1: Formula elements must have non-empty /Alt, or /ActualText (presence suffices).
	if standardType == "Formula" && state.profile == "PDF/UA-1" {
		hasAlt := hasNonEmptyStringKey(ctx, state.doc, elem, "Alt")
		hasAT := actualTextPresent(ctx, state.doc, elem)
		if !hasAlt && !hasAT {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_7_t01, "Formula element without /Alt or /ActualText")...)
		}
	}

	// §7.9 t1/t2: Note elements must have /ID, and IDs must be unique. veraPDF
	// reads /ID as a decoded text string (GFSENote, getStringKey): an /ID holding
	// only a byte order mark is empty, and two /ID strings that decode to the
	// same text are the same ID whatever their encoding. Measured against
	// veraPDF.
	if standardType == "Note" && state.profile == "PDF/UA-1" {
		idVal, ok := decodedStringKey(ctx, state.doc, elem, "ID")
		if !ok || idVal == "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_9_t01, "Note element without /ID entry")...)
		} else {
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

	// When entering a Table, determine whether it is column-regular. The flag
	// gates the header determination run when the table is left, so compute it
	// here. Save and restore so nested tables don't leak state.
	prevTableIrregular := state.tableIrregular
	// veraPDF builds its SETable object for an element whose standard type is
	// Table (GFSEFactory.createTypedStructElem), so the role map decides.
	isTable := iso32005Key(ctx, state, elem, typeName) == "Table"
	if isTable {
		state.tableIrregular = !validateTableColumnConsistency(ctx, state, elem)
		// ISO 14289-1 §7.2 t15 is a per-cell rule, but a cell only intersects
		// relative to the table's grid, so the whole table is scanned once on
		// entry rather than per cell during the recursion below. ISO 14289-2
		// §8.2.5.26 t1 is the same rule: both scope to SETableCell and both
		// read "hasIntersection != true", so one scan serves both. The
		// THead/TBody/TFoot row-span limit belongs to §8.2.5.26 t2, which
		// scopes to SETable and is a different test.
		if state.profile == "PDF/UA-1" || state.profile == "PDF/UA-2" {
			validateTableCellIntersection(ctx, state, elem)
		}
	}

	// Recurse into children. The Table 5 effective parent follows the
	// element type, except through a pass-through wrapper, which is
	// transparent: its kids keep the current effective parent.
	if kObj, ok := elem["K"]; ok {
		prevISOParent, prevUA1Parent := state.iso32005Parent, state.ua1Parent
		prevUA1ParentSeq := state.ua1ParentSeq
		state.seq++
		selfSeq := state.seq
		selfKey := iso32005Key(ctx, state, elem, typeName)
		if !iso32005PassThroughTypes[selfKey] {
			state.iso32005Parent = selfKey
			state.ua1Parent = ua1ParentRef{key: selfKey}
			state.ua1ParentSeq = selfSeq
		} else if state.ua1Parent.root {
			// veraPDF's climb through pass-through parents stops at the root
			// and reports the type of the last one it passed.
			state.ua1Parent = ua1ParentRef{key: selfKey}
			state.ua1ParentSeq = selfSeq
		}
		walkChildren(ctx, state, kObj, typeName, depth+1, lastHeadingLevel)
		state.iso32005Parent, state.ua1Parent = prevISOParent, prevUA1Parent
		state.ua1ParentSeq = prevUA1ParentSeq
	}

	if isTable {
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
		state.tableIrregular = prevTableIrregular
	}
}

// validateListContainment checks PDF/UA list structure rules.
//
// # Corpus evidence
//
//   - govdocs thread_400_400037.pdf: holds 21 LI elements with exactly two
//     offending children each, and litho reported 42 findings where veraPDF
//     reports 21, which is why §7.2 t20 is stated once per LI.
func validateListContainment(_ context.Context, state *walkState, _ ast.PdfDict, typeName ast.PdfName, parentType ast.PdfName) {
	// L must contain only LI (and optionally Caption). Stated once for the L,
	// however many of its children offend: see [walkState.listReported].
	if parentType == "L" && ua1KidJudged(typeName) && typeName != "LI" && typeName != "Caption" && typeName != "L" {
		if !listParentReported(state, 19) {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t19, "L contains %s instead of L/LI/Caption", typeName)...)
		}
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
	if parentType == "LI" && ua1KidJudged(typeName) {
		switch typeName {
		case "Lbl", "LBody":
			// OK
		default:
			// Stated once for the LI, however many of its children offend,
			// as veraPDF states it.
			if !listParentReported(state, 20) {
				*state.results = append(*state.results,
					failf(Rule_14289_1_7_2_t20, "LI contains %s instead of Lbl/LBody", typeName)...)
			}
		}
	}
}

// listParentReported reports whether the PDF/UA-1 list rule test has already
// been stated for the element whose children are being walked, marking it when
// it has not. veraPDF states §7.2 t19 against the SEL and t20 against the SELI,
// each testing that element's whole kid list, so one offending parent is one
// finding whatever the number of offending children.
func listParentReported(state *walkState, test int) bool {
	if state.ua1ParentSeq == 0 {
		return false
	}
	if state.listReported == nil {
		state.listReported = make(map[int]bool)
	}
	key := state.ua1ParentSeq*100 + test
	if state.listReported[key] {
		return true
	}
	state.listReported[key] = true
	return false
}

// walkTypelessElement walks the kids of a structure element whose /S is missing,
// null or no name. veraPDF keeps such an element in the tree with no standard
// type (TaggedPDFHelper.isStructElem), so its kids are judged against a parent of
// no type: a TR under it stands outside any table (ISO 14289-1, 7.2 t4), and two
// H kids are two H in one node (7.4.4 t1). Measured against veraPDF.
func walkTypelessElement(ctx context.Context, state *walkState, elem ast.PdfDict, depth int, lastHeadingLevel int) {
	kObj, ok := elem["K"]
	if !ok {
		return
	}
	prevISOParent, prevUA1Parent := state.iso32005Parent, state.ua1Parent
	state.iso32005Parent, state.ua1Parent = "", ua1ParentRef{}
	walkChildren(ctx, state, kObj, "", depth+1, lastHeadingLevel)
	state.iso32005Parent, state.ua1Parent = prevISOParent, prevUA1Parent
}

// ua1ParentRef is the parent the PDF/UA-1 containment rules see for the
// elements being walked ([walkState.ua1Parent]). The structure tree root is a
// flag beside the type, not a reserved type string: ISO 32000-1, 7.3.5 forbids
// the #00 escape, but litho's lexer decodes /#00root to a name holding a NUL,
// as veraPDF and PDF.js do, so no string is out of a structure type's reach.
type ua1ParentRef struct {
	// key is the parent's standard type, keyed as [iso32005Key] keys it, or ""
	// when the parent has no standard type.
	key string
	// root reports the children of the structure tree root, which have no
	// parent element.
	root bool
}

// ua1ParentType maps the walk's [walkState.ua1Parent] to the parent type the
// PDF/UA-1 containment checks compare against. veraPDF's parentStandardType is
// null both for a child of the structure tree root and for an element whose
// parent has no standard type, and null matches none of the parents the rules
// allow: on one-element fixtures veraPDF fails §7.2 t17 for a list item at the
// root. Both are named by a placeholder no rule allows.
func ua1ParentType(parent ua1ParentRef) ast.PdfName {
	if parent.root || parent.key == "" {
		return "(none)"
	}
	return ast.PdfName(parent.key)
}

// ua1KidJudged reports whether a kid of the given standard type is an entry of
// veraPDF's kidsStandardTypes. A kid with no standard type drops out, and a Div,
// Part, or NonStruct kid contributes its own kids in its place, which the walk
// judges against the same parent.
func ua1KidJudged(t ast.PdfName) bool {
	return t != "" && !iso32005PassThroughTypes[string(t)]
}

// validateUA1CaptionKidPositions checks where ISO 14289-1, 7.2 lets a Caption
// sit among the kids of a Table (first or last, t16), a TOC (first, t28), or an
// L (first, t40); 7.2 states no Caption position for any other parent. veraPDF
// tests each on the parent, the SETable, SETOC, or SEL it builds from the
// standard type, over kidsStandardTypes: the kids resolved as [iso32005Kids]
// resolves them, where the role map decides each type and a Div, Part, or
// NonStruct kid contributes its own kids. So a name mapped to Caption counts, a
// Caption mapped to P does not, and a parent fails once however many Captions
// are misplaced. Measured against veraPDF on mapped, wrapped, and repeated
// variants of each.
func validateUA1CaptionKidPositions(ctx context.Context, state *walkState, elem ast.PdfDict, typeName ast.PdfName) {
	var rule Rule
	lastAllowed := false
	switch iso32005Key(ctx, state, elem, typeName) {
	case "Table":
		rule, lastAllowed = Rule_14289_1_7_2_t16, true
	case "TOC":
		rule = Rule_14289_1_7_2_t28
	case "L":
		rule = Rule_14289_1_7_2_t40
	default:
		return
	}
	kidKeys, _ := iso32005Kids(ctx, state, elem)
	for i := 1; i < len(kidKeys); i++ {
		if kidKeys[i] == "Caption" && (!lastAllowed || i < len(kidKeys)-1) {
			*state.results = append(*state.results,
				failf(rule, "Caption at position %d of the %d kids of %s", i+1, len(kidKeys), typeName)...)
			return
		}
	}
}

// linkAnnotDest returns the identity of the structure destination a Link
// annotation targets, or "" when it targets none.
//
// ISO 14289-2 §8.2.5.20 t2 asks whether the Link annotations grouped under one
// element point at the same place, and ISO 32000-2 §12.3.2.3 makes a structure
// destination the thing being pointed at: a destination whose target is a
// structure element. A page destination, a URI action, and a destination naming
// no structure element leave nothing to compare and so return "". Only the
// target's identity decides the answer; the view parameters that follow it in
// the destination array do not, so two links onto the same element with
// different zoom agree.
//
// ISO 32000-2:2020, Table 176 forbids /Dest beside /A, so when both appear the
// action governs, as veraPDF's GFPDLinkAnnot.getStructureDestinationObject
// reads it: an /A that is no GoTo action targets nothing.
func linkAnnotDest(ctx context.Context, doc *document.Document, annot ast.PdfDict) string {
	dest := annot["Dest"]
	if aObj, hasA := annot["A"]; hasA {
		aDict, _ := document.ResolveDict(ctx, doc, aObj)
		if aDict == nil {
			return ""
		}
		if aType, _ := document.ResolveName(ctx, doc, aDict["S"]); aType != "GoTo" {
			return ""
		}
		// ISO 32000-2 §12.6.4.2: /SD takes precedence over /D when both appear.
		if dest = aDict["SD"]; dest == nil {
			dest = aDict["D"]
		}
	}
	if dest == nil {
		return ""
	}
	return structDestTarget(ctx, doc, dest)
}

// structDestTarget resolves a destination to the structure element it names and
// returns that element's identity, or "" when it names none.
func structDestTarget(ctx context.Context, doc *document.Document, dest ast.PdfObject) string {
	// A name or string destination stands for the array it maps to, through
	// either the /Names /Dests name tree or the legacy catalog /Dests.
	switch named := dest.(type) {
	case ast.PdfName:
		arr, err := document.ResolveNamedDest(ctx, doc, string(named))
		if err != nil {
			doc.Logger.Debug("compliance.structure: named destination unresolved", "name", string(named), "error", err)
			return ""
		}
		dest = arr
	case ast.PdfString:
		name := document.DecodePdfString(named)
		arr, err := document.ResolveNamedDest(ctx, doc, name)
		if err != nil {
			doc.Logger.Debug("compliance.structure: named destination unresolved", "name", name, "error", err)
			return ""
		}
		dest = arr
	}

	resolved, _ := document.ResolveObject(ctx, doc, dest)

	// A destination dictionary carries its structure destination under /SD.
	if d, ok := resolved.(ast.PdfDict); ok {
		return structElemIdentity(ctx, doc, d["SD"])
	}
	arr, ok := resolved.(ast.PdfArray)
	if !ok || len(arr) == 0 {
		return ""
	}
	return structElemIdentity(ctx, doc, arr[0])
}

// structElemIdentity returns the object identity of a reference to a structure
// element, or "" when obj is not one.
//
// A structure element is what carries /S; a page dictionary, which is what a
// page destination names, does not. Identity is the indirect reference, so a
// direct object never matches: it has no identity to compare.
func structElemIdentity(ctx context.Context, doc *document.Document, obj ast.PdfObject) string {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok {
		return ""
	}
	elem, _ := document.ResolveDict(ctx, doc, ref)
	if elem == nil {
		return ""
	}
	if _, isStructElem := elem["S"]; !isStructElem {
		return ""
	}
	return fmt.Sprintf("%d %d", ref.ObjectNum, ref.GenNum)
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

// validateListNumbering checks PDF/UA-2 §8.2.5.25 t1: if an LI of the L has a
// Lbl child, the L element must have a ListNumbering attribute other than None.
func validateListNumbering(ctx context.Context, state *walkState, lElem ast.PdfDict) {
	if !listContainsLabels(ctx, state, lElem, 0) {
		return
	}

	if listNum := listNumbering(ctx, state, lElem); listNum == "None" {
		*state.results = append(*state.results, failf(Rule_14289_2_8_2_5_25_t01,
			"L element with Lbl children has ListNumbering=%q", listNum)...)
	}
}

// listNumbering returns the ListNumbering attribute that applies to a list
// element, or None, the value ISO 32000-1, 14.8.5.5 gives an absent entry.
//
// The attribute belongs to the List owner and is inheritable (14.8.5.5), so it
// is read from the element's /A first, then from the attribute classes its /C
// names, which /A overrides (14.7.5.2), then from each ancestor in turn.
// veraPDF resolves it the same way (AttributeHelper.getListNumbering).
//
// # Corpus evidence
//
//   - pdfjs bug1937438_mml_from_latex.pdf: states ListNumbering only through
//     a class its role-mapped lists name.
func listNumbering(ctx context.Context, state *walkState, elem ast.PdfDict) string {
	cur := elem
	for range maxStructTreeDepth {
		if v := ownedAttributeName(ctx, state.doc, cur["A"], "List", "ListNumbering"); v != "" {
			return v
		}
		if v := classAttributeName(ctx, state, cur["C"], "List", "ListNumbering"); v != "" {
			return v
		}
		parent, err := document.ResolveDict(ctx, state.doc, cur["P"])
		if err != nil || parent == nil {
			break
		}
		cur = parent
	}
	return "None"
}

// ownedAttributeName returns the name value of key in the first attribute
// object whose /O names owner and that gives key a name. obj is a single
// attribute object or an array of them, possibly interleaved with revision
// numbers (ISO 32000-1, 14.7.5.1).
func ownedAttributeName(ctx context.Context, doc *document.Document, obj ast.PdfObject, owner, key ast.PdfName) string {
	if obj == nil {
		return ""
	}
	items, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		items = ast.PdfArray{obj}
	}
	for _, item := range items {
		d, derr := document.ResolveDict(ctx, doc, item)
		if derr != nil || d == nil {
			continue
		}
		if o, _ := document.ResolveName(ctx, doc, d["O"]); o != owner {
			continue
		}
		if v, verr := document.ResolveName(ctx, doc, d[key]); verr == nil && v != "" {
			return string(v)
		}
	}
	return ""
}

// classAttributeName resolves key through the attribute classes a structure
// element's /C names, a single class name or an array of them, against the
// StructTreeRoot /ClassMap (ISO 32000-1, 14.7.5.2).
func classAttributeName(ctx context.Context, state *walkState, cObj ast.PdfObject, owner, key ast.PdfName) string {
	if cObj == nil || state.classMap == nil {
		return ""
	}
	names, err := document.ResolveArray(ctx, state.doc, cObj)
	if err != nil {
		names = ast.PdfArray{cObj}
	}
	for _, item := range names {
		class, nerr := document.ResolveName(ctx, state.doc, item)
		if nerr != nil || class == "" {
			continue
		}
		if v := ownedAttributeName(ctx, state.doc, state.classMap[class], owner, key); v != "" {
			return v
		}
	}
	return ""
}

// ownedAttributeInt returns the integer value of key in the first attribute
// object whose /O names owner and that gives key a positive integer. It is the
// numeric counterpart of [ownedAttributeName]: ISO 32000-1, Table 349 types
// /ColSpan and /RowSpan as integers, which ResolveName cannot read.
func ownedAttributeInt(ctx context.Context, doc *document.Document, obj ast.PdfObject, owner, key ast.PdfName) (int, bool) {
	if obj == nil {
		return 0, false
	}
	items, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		items = ast.PdfArray{obj}
	}
	for _, item := range items {
		d, derr := document.ResolveDict(ctx, doc, item)
		if derr != nil || d == nil {
			continue
		}
		if o, _ := document.ResolveName(ctx, doc, d["O"]); o != owner {
			continue
		}
		if v, verr := document.ResolveInt(ctx, doc, d[key]); verr == nil && v > 0 {
			return int(v), true
		}
	}
	return 0, false
}

// classAttributeInt resolves an integer key through the attribute classes a
// structure element's /C names, against the StructTreeRoot /ClassMap
// (ISO 32000-1, 14.7.5.2). Numeric counterpart of [classAttributeName].
func classAttributeInt(ctx context.Context, state *walkState, cObj ast.PdfObject, owner, key ast.PdfName) (int, bool) {
	if cObj == nil || state.classMap == nil {
		return 0, false
	}
	names, err := document.ResolveArray(ctx, state.doc, cObj)
	if err != nil {
		names = ast.PdfArray{cObj}
	}
	for _, item := range names {
		class, nerr := document.ResolveName(ctx, state.doc, item)
		if nerr != nil || class == "" {
			continue
		}
		if v, ok := ownedAttributeInt(ctx, state.doc, state.classMap[class], owner, key); ok {
			return v, true
		}
	}
	return 0, false
}

// feNoteType returns the NoteType attribute that applies to an FENote element,
// or None when it states none. veraPDF reads it only from attribute objects the
// FENote owner holds, in the element's /A and then in the classes its /C names,
// without inheritance (AttributeHelper.getNoteType), so a NoteType stated under
// another owner does not count for ISO 14289-2, 8.2.5.14 t4.
func feNoteType(ctx context.Context, state *walkState, elem ast.PdfDict) string {
	if v := ownedAttributeName(ctx, state.doc, elem["A"], "FENote", "NoteType"); v != "" {
		return v
	}
	if v := classAttributeName(ctx, state, elem["C"], "FENote", "NoteType"); v != "" {
		return v
	}
	return "None"
}

// structElemContainsRef reports whether elem, or a structure element below it,
// carries a /Ref entry. veraPDF's SETOCI.containsRef descends through every
// structure-element kid, so a /Ref on a child identifies a TOCI's target as well
// as one on the TOCI itself. A kid whose /Type names something other than
// StructElem (an MCR or OBJR) is no structure element, as isStructElem reads it.
// Recursion is bounded by [maxStructTreeWalkDepth]; visited guards cycles.
func structElemContainsRef(ctx context.Context, doc *document.Document, elem ast.PdfDict, depth int, visited map[int]bool) bool {
	if _, ok := elem["Ref"]; ok {
		return true
	}
	if depth >= maxStructTreeWalkDepth || ctx.Err() != nil {
		return false
	}
	kObj, ok := elem["K"]
	if !ok {
		return false
	}
	kids, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		kids = ast.PdfArray{kObj}
	}
	for _, kid := range kids {
		if ref, isRef := kid.(ast.PdfIndirectRef); isRef {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
		}
		d, derr := document.ResolveDict(ctx, doc, kid)
		if derr != nil || d == nil {
			continue
		}
		if t, _ := document.ResolveName(ctx, doc, d["Type"]); t != "" && t != "StructElem" {
			continue
		}
		if structElemContainsRef(ctx, doc, d, depth+1, visited) {
			return true
		}
	}
	return false
}

// structClassMap resolves the StructTreeRoot /ClassMap, or nil when the tree
// declares none.
func structClassMap(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict) ast.PdfDict {
	classMap, err := document.ResolveDict(ctx, doc, structTreeRoot["ClassMap"])
	if err != nil {
		return nil
	}
	return classMap
}

// listContainsLabels reports whether an LI among a list's kids has a Lbl among
// its own kids. Both levels are read by standard type, as [iso32005Kids] reads
// them: the /RoleMap applies first (ISO 32000-2:2020, 14.8.6), and a Div, Part,
// or NonStruct kid contributes its own kids in its place. veraPDF decides
// containsLabels the same way (GFSEL.getcontainsLabels).
//
// # Corpus evidence
//
//   - govdocs thread_300_300035.pdf: labels its items with LI_Label, which
//     its role map maps to Lbl.
func listContainsLabels(ctx context.Context, state *walkState, elem ast.PdfDict, depth int) bool {
	kObj, ok := elem["K"]
	if !ok {
		return false
	}
	items, err := document.ResolveArray(ctx, state.doc, kObj)
	if err != nil {
		items = ast.PdfArray{kObj}
	}
	for _, item := range items {
		d, derr := document.ResolveDict(ctx, state.doc, item)
		if derr != nil || d == nil {
			continue
		}
		s, serr := document.ResolveName(ctx, state.doc, d["S"])
		if serr != nil || s == "" {
			continue
		}
		switch key := iso32005Key(ctx, state, d, s); {
		case key == "LI":
			if kidKeys, _ := iso32005Kids(ctx, state, d); slices.Contains(kidKeys, "Lbl") {
				return true
			}
		case iso32005PassThroughTypes[key] && depth < maxISO32005PassThroughDepth:
			if listContainsLabels(ctx, state, d, depth+1) {
				return true
			}
		}
	}
	return false
}

// structParentPresenceRule selects the structure-element /P presence rule by UA
// profile: ISO 14289-1 §7.1 t12 and ISO 14289-2 §8.2.1 t2.
func structParentPresenceRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_2_1_t02
	}
	return Rule_14289_1_7_1_t12
}

// checkActualTextPUA validates that /ActualText and /Alt entries do not contain
// Unicode PUA values (U+E000-U+F8FF, U+F0000-U+FFFFD, U+100000-U+10FFFD).
//
// Which keys are in scope differs by profile, so the caller supplies a rule per
// key and passes the zero [Rule] for a key the profile does not judge. PDF/UA-2
// splits ActualText (§8.4.3 t02) from Alt (§8.4.3 t03); ISO 19005-4 §6.2.10.8
// covers ActualText ALONE, so /Alt is out of scope there and firing the same
// rule for it reported a violation veraPDF does not.
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
			if keyRule(key).Specification == "" {
				continue
			}
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

// containsPUARunes checks if a decoded string contains any Unicode PUA code
// points: the private-use characters, General_Category Co, which the Unicode
// Standard, 23.5 places at U+E000 to U+F8FF and in planes 15 and 16 up to
// U+FFFFD and U+10FFFD. The last two code points of each plane are
// noncharacters (23.7), not private use, and veraPDF's PUAHelper.containPUA
// stops at the same bounds.
func containsPUARunes(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Co, r) {
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

// standsForMathML reports whether a structure element stands, through the role
// map, for a type of the MathML namespace (ISO 32000-2:2020, 14.8.6). The role
// map applies first, as for any type, and the chain stops at the first standard
// type, a MathML one included.
func standsForMathML(ctx context.Context, state *walkState, elem ast.PdfDict, typeName ast.PdfName) bool {
	std, ok := resolveStandardRef(ctx, state, iso32005RefOf(ctx, state, elem, typeName), anyStandardRef)
	return ok && std.uri == mathMLNamespaceURI
}

// anyStandardRef reports whether ref names a standard type of any standard
// structure namespace, the MathML one included (ISO 32000-2:2020, 14.8.6).
func anyStandardRef(ref iso32005Ref) bool {
	return isStandardStructTypeInNS(ref.name, ref.uri)
}

// hasFormulaOrMathMLAncestor reports whether a structure element above elem,
// reached by climbing /P, stands for Formula or for a MathML type, which is
// how the PDF/UA-2 validation profile tests ISO 14289-2, 8.2.5.29 t1. A /P
// that loops back or does not resolve ends the climb.
func hasFormulaOrMathMLAncestor(ctx context.Context, state *walkState, elem ast.PdfDict) bool {
	cur, ok := elem["P"]
	if !ok {
		return false
	}
	visited := make(map[int]bool)
	for range maxStructTreeWalkDepth {
		if ctx.Err() != nil {
			return false
		}
		if ref, isRef := cur.(ast.PdfIndirectRef); isRef {
			if visited[ref.ObjectNum] {
				return false
			}
			visited[ref.ObjectNum] = true
		}
		d, err := document.ResolveDict(ctx, state.doc, cur)
		if err != nil || d == nil {
			return false
		}
		if s, serr := document.ResolveName(ctx, state.doc, d["S"]); serr == nil && s != "" {
			std, sok := resolveStandardRef(ctx, state, iso32005RefOf(ctx, state, d, s), anyStandardRef)
			if sok && (std.name == "Formula" || std.uri == mathMLNamespaceURI) {
				return true
			}
		}
		if cur, ok = d["P"]; !ok {
			return false
		}
	}
	return false
}

// checkUnmappedStructTypes walks the structure tree and flags every element
// whose type reaches no standard type, whether the /RoleMap omits it or maps it
// to another non-standard type.
//
// The rule is scoped to the element, not to the type name: ISO 14289-1 7.1 t5,
// ISO 14289-2 8.2.4 t1 and ISO 19005-1/2/3 6.8.3.4/6.7.3.4 t1 all carry
// veraPDF's SENonStandard as their object. Nine elements sharing one unmapped
// type are nine violations, and deduplicating by name reported one.
func checkUnmappedStructTypes(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict, profile string, results *[]Violation) {
	roleMap := LoadRoleMap(ctx, doc, structTreeRoot)
	rule := roleMapNonStandardRule(profile)
	if rule.Specification == "" {
		return
	}

	walkStructureElements(ctx, doc, structTreeRoot, func(elem ast.PdfDict, _ string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		if typeName == "" {
			// An element whose /S is missing, null or no name has no structure
			// type (ISO 32000-1, Table 323 makes /S required), so it reaches no
			// standard type: veraPDF's SENonStandard reports it. Measured.
			*results = append(*results, failf(rule, "structure element has no structure type")...)
			return
		}
		if isStandardStructType(typeName, profile) {
			return
		}
		// A circular chain is the circular rule's business, and it is scoped to
		// the map entry, so an element on one answers there instead.
		if classifyRoleChain(typeName, roleMap, profile) != roleChainUnmapped {
			return
		}
		*results = append(*results, failf(rule,
			"non-standard structure type %s is not mapped to a standard type", typeName)...)
	})
}

// checkSingleDocumentRoot validates ISO 14289-2, 8.2.5.2: the structure tree
// root shall contain a single Document structure element as its only child
// (t1), and that element shall be declared in the PDF 2.0 standard structure
// namespace (t2).
//
// Both tests are decided on each child's effective standard structure type
// (ISO 32000-2:2020, 14.7.4 role mapping), never on the raw /S name. A child
// whose /S is role-mapped to Document therefore satisfies t1 and is judged by
// t2, and a child literally named Document that resolves to no standard type
// fails t1 rather than t2. That is what the two rules constrain: t1 tests the
// resolved kid types, t2 tests only the namespace of an otherwise conforming
// single Document.
func checkSingleDocumentRoot(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict, results *[]Violation) {
	r := newNSRoleResolver(ctx, doc, structTreeRoot)
	kids := r.significantChildren(ctx, doc, structTreeRoot, 0)

	// A child that resolves to no standard type contributes no type to the t1
	// comparison, so a root holding only such children reads as empty.
	types := make([]string, 0, len(kids))
	for _, kid := range kids {
		if kid.stdType != "" {
			types = append(types, kid.stdType)
		}
	}

	if len(types) != 1 || types[0] != "Document" {
		*results = append(*results, failf(Rule_14289_2_8_2_5_2_t01,
			"StructTreeRoot children resolve to [%s] instead of a single Document",
			strings.Join(types, ", "))...)
		return
	}

	// t2 scopes to the first structurally significant child, which the t1
	// verdict above has established is the single Document.
	if kids[0].stdNS != pdf2NamespaceURI {
		*results = append(*results, failf(Rule_14289_2_8_2_5_2_t02,
			"Document structure element is in namespace %q, not the PDF 2.0 namespace",
			kids[0].stdNS)...)
	}
}

// pdf2NamespaceURI is the PDF 2.0 standard structure namespace (ISO 32000-2
// §14.8.6).
const pdf2NamespaceURI = "http://iso.org/pdf2/ssn"

// checkPDFALangValid validates ISO 19005-1, 6.8.4 and ISO 19005-2/3, 6.7.4:
// a /Lang entry in the catalog, in a structure element or in a marked-content
// property list must be a language tag of the part's grammar.
func checkPDFALangValid(ctx context.Context, doc *document.Document, catalog ast.PdfDict, _ string, part int) []Violation {
	rule := pdfaLangValidRule(part)

	var results []Violation

	// Check catalog /Lang. [pdfaLangTagInvalid] carries the grammar each part
	// requires and judges the bytes the file wrote.
	if langObj, ok := catalog["Lang"]; ok {
		langStr, err := document.ResolveString(ctx, doc, langObj)
		if err == nil && pdfaLangTagInvalid(langStr, part) {
			results = append(results, Violation{
				Rule:    rule,
				Context: fmt.Sprintf("catalog lang=%s", strings.TrimSpace(document.DecodePdfString(langStr))),
			})
		}
	}

	// Check structure element /Lang values. A file with no structure tree has
	// none, and its marked content below is judged all the same: the clause
	// names every property list, whatever the tree.
	if treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"]); err == nil && treeRoot != nil {
		walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
			langObj, ok := elem["Lang"]
			if !ok {
				return
			}
			langStr, err := document.ResolveString(ctx, doc, langObj)
			if err != nil {
				return
			}
			if pdfaLangTagInvalid(langStr, part) {
				results = append(results, Violation{
					Rule:    rule,
					Context: fmt.Sprintf("lang=%s, %s", strings.TrimSpace(document.DecodePdfString(langStr)), path),
				})
			}
		})
	}

	results = append(results, pdfaMarkedContentLang(ctx, doc, catalog, part, rule)...)

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

// checkTextStringPUA validates ISO 14289-2, 8.6 t1: a text string a consumer
// reads out shall carry no Private Use Area code point, because nothing outside
// the producing system knows what one means.
//
// The clause is scoped to veraPDF's CosTextString, whose only instances in its
// model are an outline item's /Title (GFPDOutline.getTitle) and every CosLang,
// the one subtype: the catalog /Lang (GFPDDocument.getLang), a structure
// element's /Lang (GFPDStructElem.getLang), the /Lang of the element an
// annotation or a form field names by its /StructParent (GFPDAnnot.getLang,
// GFPDFormField.getLang), and the /Lang of a marked-content property list
// (GFOpMarkedContent.getLang). [typedValues] marks the dictionary strings
// among them, and [forEachContentLang] reaches the property lists. A string
// read as a CosActualText or a CosAlt is a CosString and answers ISO 14289-2,
// 8.4.3 instead, and a document information /Title or an annotation's own
// /Contents or /Lang is no typed string at all. Measured on veraPDF 1.30.2
// with one crafted file per position, an annotation's own /Lang and a form
// field's own /Lang silent among them. The code points are those of
// PUAHelper.containPUA ([containsPUARunes]).
//
// # Corpus evidence
//
//   - verapdf PDF_UA-2__8.6-t01-fail-a: its catalog /Lang is <FEFFF00C>, and
//     veraPDF reports it at PDF/UA-2 and both WTPDF levels, which litho,
//     reading outline titles only, did not.
func checkTextStringPUA(ctx context.Context, vctx *validationCtx) []Violation {
	var results []Violation
	for _, v := range typedValues(ctx, vctx) {
		if !v.textString {
			continue
		}
		if s, ok := v.value.(ast.PdfString); ok && containsPUARunes(document.DecodePdfString(s)) {
			results = append(results, failf(Rule_14289_2_8_6_t01, "%s contains a Private Use Area code point", v.where)...)
		}
	}
	forEachContentLang(ctx, vctx.doc, vctx.catalog, func(pageNum int, langObj ast.PdfObject, origin string) {
		// A /Lang that is no string is no CosLang for veraPDF.
		lang, err := document.ResolveString(ctx, vctx.doc, langObj)
		if err != nil {
			return
		}
		if containsPUARunes(document.DecodePdfString(lang)) {
			results = append(results, failf(Rule_14289_2_8_6_t01,
				"page %d, %s: marked-content /Lang contains a Private Use Area code point", pageNum, origin)...)
		}
	})
	return results
}
