package compliance

import (
	"context"
	"regexp"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// ISO/TS 32005 Table 5 inclusion checks for PDF/UA-2. Table 5 is
// normative for PDF/UA-2 structure, so litho runs the generated
// [iso32005Entries] table during the UA-2 structure walk. The entries
// are mechanical: forbidden parent-child pairings, per-parent kid-count
// constraints, and direct-content prohibitions.

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

// iso32005PassThroughTypes lists the structure types Table 5 looks through
// when it determines an element's parent, kids, and direct content. Table 5
// declares no row for Part, Div, or NonStruct, so none of the three constrains
// what it contains; ISO 32000-2:2020, 14.8.4.4 defines all three as pure
// grouping wrappers whose descendants carry the structural meaning. Table 5
// therefore judges a wrapped element against the nearest enclosing element the
// matrix does constrain, and judges a wrapper's kids as kids of that element.
// veraPDF 1.30 encodes the identical set in PDStructElem.isPassThroughTag and
// applies it to parentStandardType, kidsStandardTypes, and hasContentItems.
// Private is deliberately absent: Table 5 constrains it no more than the other
// three, but its contents are private to one producer, so they are not
// reattributed to the enclosing element.
//
//nolint:gochecknoglobals // static spec table
var iso32005PassThroughTypes = map[string]bool{
	"NonStruct": true, "Div": true, "Part": true,
}

// maxISO32005PassThroughDepth bounds how far the Table 5 kid lookup descends
// through pass-through wrappers. Anti-DoS guard, not spec-mandated: the lookup
// runs once per element, so an unbounded chain of nested Div elements would
// make the structure walk quadratic. Real documents stack grouping wrappers a
// handful of levels deep, so 32 leaves ample headroom. Beyond the cap the
// wrapper contributes no kid keys, which cannot raise a kid count and so
// cannot manufacture a violation.
const maxISO32005PassThroughDepth = 32

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

// iso32005NSPDF17 is the PDF 1.7 standard structure namespace URI
// (ISO 32000-2:2020, 14.8.6). It is the one explicit namespace whose
// elements still resolve through the structure tree root's legacy /RoleMap.
const iso32005NSPDF17 = "http://iso.org/pdf/ssn"

// iso32005Ref is a structure type together with the namespace it was
// declared in. ns is the resolved /NS dictionary and is nil for a legacy
// element that carries no /NS entry; uri caches the namespace's /NS string.
type iso32005Ref struct {
	name ast.PdfName
	ns   ast.PdfDict
	uri  string
}

// iso32005Key resolves an element's structure type to its ISO/TS 32005
// Table 5 key. Table 5 governs standard structure types, so a type is first
// mapped to the standard type it stands for (ISO 32000-2:2020, 14.8.6).
// Numbered headings collapse to the profile's "Hn" key. Returns "" when the
// type resolves to no standard type, which skips the Table 5 checks rather
// than guessing: a non-standard element is outside the matrix.
func iso32005Key(ctx context.Context, state *walkState, elem ast.PdfDict, typeName ast.PdfName) string {
	return iso32005Resolve(ctx, state, iso32005RefOf(ctx, state, elem, typeName))
}

// iso32005RefOf pairs a structure type with the namespace dictionary its
// element declares through /NS (ISO 32000-2:2020, 14.8.6).
func iso32005RefOf(ctx context.Context, state *walkState, elem ast.PdfDict, typeName ast.PdfName) iso32005Ref {
	ref := iso32005Ref{name: typeName}
	nsObj, ok := elem["NS"]
	if !ok {
		return ref
	}
	ns, err := document.ResolveDict(ctx, state.doc, nsObj)
	if err != nil || ns == nil {
		state.doc.Logger.Debug("compliance.structure: cannot resolve structure element /NS", "error", err)
		return ref
	}
	ref.ns = ns
	if uri, uerr := document.ResolveString(ctx, state.doc, ns["NS"]); uerr == nil {
		ref.uri = string(uri)
	}
	return ref
}

// iso32005Resolve walks the role map to the standard type a structure type
// stands for, mirroring ISO 32000-2:2020, 14.8.6: the mapping is applied
// first, and only a type with no applicable mapping (or one whose chain
// immediately loops back on itself) stands for itself. A standard type that
// IS remapped therefore no longer denotes that standard type; ISO 14289-2
// §8.2.4 t4 reports the forbidden remapping separately, so Table 5 must not
// also judge the element as if the mapping were absent.
func iso32005Resolve(ctx context.Context, state *walkState, ref iso32005Ref) string {
	visited := map[string]bool{ref.visitKey(): true}

	next, ok := iso32005Equivalent(ctx, state, ref)
	if !ok || visited[next.visitKey()] {
		return iso32005StandardKey(ref)
	}
	for range maxRoleMapHops {
		if key := iso32005StandardKey(next); key != "" {
			return key
		}
		visited[next.visitKey()] = true
		after, aok := iso32005Equivalent(ctx, state, next)
		if !aok || visited[after.visitKey()] {
			return ""
		}
		next = after
	}
	return ""
}

// visitKey identifies a type within its namespace for cycle detection.
func (r iso32005Ref) visitKey() string { return string(r.name) + "\x00" + r.uri }

// iso32005Equivalent returns the role-map target of one structure type.
// Per ISO 32000-2:2020, 14.8.6, an element that declares a namespace is
// remapped through that namespace's /RoleMapNS, and the structure tree
// root's legacy /RoleMap applies only to elements in the default namespace
// or in the PDF 1.7 namespace. A namespace that declares a /RoleMapNS
// without an entry for the type therefore has no equivalent at all: the
// legacy /RoleMap does not fill the gap.
func iso32005Equivalent(ctx context.Context, state *walkState, ref iso32005Ref) (iso32005Ref, bool) {
	if ref.ns != nil {
		rmns, err := document.ResolveDict(ctx, state.doc, ref.ns["RoleMapNS"])
		if err != nil || rmns == nil {
			if ref.uri != iso32005NSPDF17 {
				return iso32005Ref{}, false
			}
		} else {
			return iso32005RoleMapNSTarget(ctx, state, rmns[ref.name])
		}
	}
	target, ok := state.roleMap[ref.name]
	if !ok {
		return iso32005Ref{}, false
	}
	return iso32005Ref{name: target}, true
}

// iso32005RoleMapNSTarget reads one /RoleMapNS value, which is either a bare
// name (the target sits in the default namespace) or a [name namespace] pair
// (ISO 32000-2:2020, Table 353).
func iso32005RoleMapNSTarget(ctx context.Context, state *walkState, entry ast.PdfObject) (iso32005Ref, bool) {
	if entry == nil {
		return iso32005Ref{}, false
	}
	if arr, err := document.ResolveArray(ctx, state.doc, entry); err == nil && len(arr) >= 2 {
		name, nerr := document.ResolveName(ctx, state.doc, arr[0])
		ns, derr := document.ResolveDict(ctx, state.doc, arr[1])
		if nerr != nil || name == "" || derr != nil || ns == nil {
			return iso32005Ref{}, false
		}
		out := iso32005Ref{name: name, ns: ns}
		if uri, uerr := document.ResolveString(ctx, state.doc, ns["NS"]); uerr == nil {
			out.uri = string(uri)
		}
		return out, true
	}
	name, err := document.ResolveName(ctx, state.doc, entry)
	if err != nil || name == "" {
		return iso32005Ref{}, false
	}
	return iso32005Ref{name: name}, true
}

// iso32005StandardKey maps a standard structure type to its Table 5 key,
// collapsing numbered headings onto "Hn" (ISO 32000-2 §14.8.4.6.2).
// Returns "" for a non-standard type.
//
// A type is standard only within the namespace that defines it
// (ISO 32000-2:2020, 14.8.6), so the namespace the reference carries selects
// the vocabulary: TOC keys as TOC in the PDF 1.7 namespace and as nothing in
// the PDF 2.0 one. Table 5 covers the two PDF standard structure namespaces
// alone, so a MathML type keys as nothing even though 14.8.6 makes it standard.
func iso32005StandardKey(ref iso32005Ref) string {
	if ref.uri == mathMLNamespaceURI || !isStandardStructTypeInNS(ref.name, ref.uri) {
		return ""
	}
	if iso32005HnRe.MatchString(string(ref.name)) {
		return "Hn"
	}
	return string(ref.name)
}

// iso32005Kids resolves an element's /K children into Table 5 keys and reports
// whether any content items are present: marked-content MCID integers, MCR
// dictionaries, and OBJR object references all count. An OBJR is a reference to
// a content-bearing object, so it is itself a content item: a Document whose
// only kid is an OBJR contains content directly and fails Table 5's
// Document-content rule (golden PDF_UA-1__7.18.4-t01-fail-a is that single-OBJR
// shape). Each kid resolves in its own namespace, so the kid dictionary drives
// the lookup rather than the type name alone.
//
// A kid whose own key is in [iso32005PassThroughTypes] contributes its kids and
// its content items instead of itself, recursively: Table 5 attributes what a
// Part, Div, or NonStruct wraps to the enclosing element.
func iso32005Kids(ctx context.Context, state *walkState, elem ast.PdfDict) (kidKeys []string, hasContent bool) {
	return iso32005KidsAt(ctx, state, elem, 0)
}

// iso32005KidsAt is [iso32005Kids] carrying the pass-through nesting depth.
func iso32005KidsAt(ctx context.Context, state *walkState, elem ast.PdfDict, depth int) (kidKeys []string, hasContent bool) {
	if ctx.Err() != nil {
		return nil, false
	}
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
			case "MCR", "OBJR":
				hasContent = true
			default:
				s, serr := document.ResolveName(ctx, state.doc, v["S"])
				if serr != nil || s == "" {
					continue
				}
				key := iso32005Key(ctx, state, v, s)
				switch {
				case key == "":
				case iso32005PassThroughTypes[key]:
					if depth >= maxISO32005PassThroughDepth {
						continue
					}
					nested, nestedContent := iso32005KidsAt(ctx, state, v, depth+1)
					kidKeys = append(kidKeys, nested...)
					hasContent = hasContent || nestedContent
				default:
					kidKeys = append(kidKeys, key)
				}
			}
		}
	}
	return kidKeys, hasContent
}

// validateISO32005 evaluates the Table 5 entries for one element of the
// PDF/UA-2 structure walk. typeName is the element's structure type as
// written; parentKey is the parent's already-resolved Table 5 key, or the
// "StructTreeRoot" sentinel for a root child. Role-mapped or namespaced
// types that do not resolve to a standard type are skipped, never guessed.
func validateISO32005(ctx context.Context, state *walkState, elem ast.PdfDict, typeName ast.PdfName, parentKey string) {
	iso32005Once.Do(iso32005BuildIndexes)

	selfKey := iso32005Key(ctx, state, elem, typeName)
	if selfKey == "" {
		return
	}

	if parentKey != "" {
		if rule, forbidden := iso32005ForbiddenParents[selfKey][parentKey]; forbidden {
			*state.results = append(*state.results,
				failf(rule, "%s inside %s", typeName, parentKey)...)
		}
	}

	constraints := iso32005ParentRules[selfKey]
	if len(constraints) == 0 {
		return
	}
	kidKeys, hasContent := iso32005Kids(ctx, state, elem)
	counts := make(map[string]int, len(kidKeys))
	for _, k := range kidKeys {
		counts[k]++
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
	kidKeys, _ := iso32005Kids(ctx, state, structTreeRoot)
	counts := make(map[string]int, len(kidKeys))
	for _, k := range kidKeys {
		counts[k]++
	}
	for _, e := range iso32005ParentRules["StructTreeRoot"] {
		if e.Kind == "kidExactlyOne" && counts[e.Child] != 1 {
			*state.results = append(*state.results,
				failf(e.Rule, "StructTreeRoot contains %d %s elements, want exactly 1", counts[e.Child], e.Child)...)
		}
	}
}
