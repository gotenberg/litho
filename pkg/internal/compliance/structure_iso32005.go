package compliance

import (
	"context"
	"regexp"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// ISO/TS 32005 Table 5 inclusion checks for PDF/UA-2. veraPDF's ua2
// flavour validates the combined PDFUA-2-ISO32005 profile, so litho runs
// the generated [iso32005Entries] table during the UA-2 structure walk.
// The entries are mechanical: forbidden parent-child pairings, per-parent
// kid-count constraints, and direct-content prohibitions.

// iso32005HnRe matches the numbered heading types the profile abbreviates
// as "Hn" (ISO 32000-2 §14.8.4.6.2 allows unbounded levels in PDF 2.0).
var iso32005HnRe = regexp.MustCompile(`^H[1-9][0-9]*$`)

// iso32005GroupingTypes lists the grouping structure types per
// ISO/TS 32005 (ISO 32000-2:2020 Table 364 grouping level, plus Art,
// which ISO 32000-1 Table 333 defines as grouping and role maps carry
// into PDF 2.0 documents). The Table 5 isGrouping tests key on this set.
//
//nolint:gochecknoglobals // static spec table
var iso32005GroupingTypes = map[string]bool{
	"Document": true, "DocumentFragment": true, "Part": true,
	"Sect": true, "Div": true, "Aside": true,
	"NonStruct": true, "Private": true, "Art": true,
}

//nolint:gochecknoglobals // lazily built indexes over the generated table
var (
	iso32005Once             sync.Once
	iso32005ForbiddenParents map[string]map[string]Rule
	iso32005ParentRules      map[string][]iso32005Entry
)

// iso32005BuildIndexes splits the generated entry table into the two
// lookup directions the walk needs: child → forbidden parents, and
// parent → kid/content constraints.
func iso32005BuildIndexes() {
	iso32005ForbiddenParents = make(map[string]map[string]Rule)
	iso32005ParentRules = make(map[string][]iso32005Entry)
	for _, e := range iso32005Entries {
		switch e.Kind {
		case "forbiddenParent", "forbiddenParentHn":
			child := e.Child
			if iso32005ForbiddenParents[child] == nil {
				iso32005ForbiddenParents[child] = make(map[string]Rule)
			}
			iso32005ForbiddenParents[child][e.Parent] = e.Rule
		default:
			iso32005ParentRules[e.Parent] = append(iso32005ParentRules[e.Parent], e)
		}
	}
}

// maxRoleMapHops bounds RoleMap chain resolution. Anti-livelock guard,
// not spec-mandated: real role maps chain once or twice, and the RoleMap
// cycle rule is checked separately, so eight hops is generous.
const maxRoleMapHops = 8

// iso32005Key normalizes a structure type for table lookups, following
// the walk's RoleMap index until a standard type is reached: veraPDF
// evaluates the Table 5 rules over role-map-resolved standard types.
// Numbered headings collapse to the profile's "Hn" key. Returns "" when
// no standard type is reachable, which skips the checks rather than
// guessing.
func iso32005Key(state *walkState, t string) string {
	name := ast.PdfName(t)
	for range maxRoleMapHops {
		if iso32005HnRe.MatchString(string(name)) {
			return "Hn"
		}
		if isStandardStructType(name, "PDF/UA-2") {
			return string(name)
		}
		next, ok := state.roleMap[name]
		if !ok {
			return ""
		}
		name = next
	}
	return ""
}

// iso32005Kids resolves an element's direct /K children into resolved
// standard-type names and reports whether any direct content items
// (marked-content MCID integers or MCR dictionaries) are present. OBJR
// children are neither: veraPDF counts only marked content as content
// items.
func iso32005Kids(ctx context.Context, state *walkState, elem ast.PdfDict) (kidTypes []string, hasContent bool) {
	kObj, ok := elem["K"]
	if !ok {
		return nil, false
	}
	items, err := document.ResolveArray(ctx, state.doc, kObj)
	if err != nil {
		items = ast.PdfArray{kObj}
	}
	for _, item := range items {
		resolved, rerr := document.ResolveObject(ctx, state.doc, item)
		if rerr != nil || resolved == nil {
			continue
		}
		switch v := resolved.(type) {
		case ast.PdfInt:
			hasContent = true
		case ast.PdfDict:
			typeName, _ := document.ResolveName(ctx, state.doc, v["Type"])
			switch typeName {
			case "MCR":
				hasContent = true
			case "OBJR":
				// Object references are neither content items nor
				// structure kids for Table 5 purposes.
			default:
				if s, serr := document.ResolveName(ctx, state.doc, v["S"]); serr == nil && s != "" {
					kidTypes = append(kidTypes, string(s))
				}
			}
		}
	}
	return kidTypes, hasContent
}

// validateISO32005 evaluates the Table 5 entries for one element of the
// PDF/UA-2 structure walk. typeName and parentType are the element's and
// parent's structure types as written (role-mapped or namespaced types
// that do not resolve to a standard type are skipped, never guessed).
func validateISO32005(ctx context.Context, state *walkState, elem ast.PdfDict, typeName, parentType ast.PdfName) {
	iso32005Once.Do(iso32005BuildIndexes)

	selfKey := iso32005Key(state, string(typeName))
	if selfKey == "" {
		return
	}

	if parentKey := iso32005Key(state, string(parentType)); parentKey != "" {
		if rule, forbidden := iso32005ForbiddenParents[selfKey][parentKey]; forbidden {
			*state.results = append(*state.results,
				failf(rule, "%s inside %s", typeName, parentType)...)
		}
	}

	constraints := iso32005ParentRules[selfKey]
	if len(constraints) == 0 {
		return
	}
	kidTypes, hasContent := iso32005Kids(ctx, state, elem)
	counts := make(map[string]int, len(kidTypes))
	for _, k := range kidTypes {
		if key := iso32005Key(state, k); key != "" {
			counts[key]++
		}
	}
	grouping := iso32005GroupingTypes[selfKey]

	for _, e := range constraints {
		violated := false
		switch e.Kind {
		case "noContent":
			violated = hasContent
		case "kidMaxOne":
			violated = counts[e.Child] > 1
		case "kidZeroUnlessGrouping":
			violated = !grouping && counts[e.Child] > 0
		case "kidMaxOneIfGrouping":
			violated = grouping && counts[e.Child] > 1
		case "kidZeroUnlessGroupingHn":
			violated = !grouping && counts["Hn"] > 0
		case "kidExactlyOne":
			// Evaluated at the structure tree root by
			// [validateISO32005Root]; the root has no walkElement visit.
		}
		if violated {
			*state.results = append(*state.results,
				failf(e.Rule, "%s violates Table 5 inclusion for %s", typeName, e.Child)...)
		}
	}
}

// validateISO32005Root evaluates the StructTreeRoot-scoped Table 5
// entries (kidExactlyOne: the root shall contain exactly one Document).
func validateISO32005Root(ctx context.Context, state *walkState, structTreeRoot ast.PdfDict) {
	iso32005Once.Do(iso32005BuildIndexes)
	kidTypes, _ := iso32005Kids(ctx, state, structTreeRoot)
	counts := make(map[string]int, len(kidTypes))
	for _, k := range kidTypes {
		if key := iso32005Key(state, k); key != "" {
			counts[key]++
		}
	}
	for _, e := range iso32005ParentRules["StructTreeRoot"] {
		if e.Kind == "kidExactlyOne" && counts[e.Child] != 1 {
			*state.results = append(*state.results,
				failf(e.Rule, "StructTreeRoot contains %d %s elements, want exactly 1", counts[e.Child], e.Child)...)
		}
	}
}
