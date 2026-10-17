package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchRoleMap(v compliance.Violation) bool {
	return descriptionContains(v, "standard tags") && descriptionContains(v, "remapped") ||
		descriptionContains(v, "circular mapping") ||
		descriptionContains(v, "is remapped") ||
		descriptionContains(v, "standard type") && descriptionContains(v, "remapped") ||
		clausePrefix(v, "6.8.3.4") ||
		// ISO 14289-2 §8.2.4 t1: a type the legacy /RoleMap maps can still be
		// unmapped in a namespace that map does not govern
		// ([repairUngovernedRoles]).
		(v.Rule.Specification == "ISO_14289_2" && v.Rule.Clause == "8.2.4" && v.Rule.TestNumber == 1) ||
		// ISO 14289-2 §8.2.4 t3: a /RoleMapNS chain that maps a type to
		// another type of its own namespace ([repairNamespaceRoleLoops]).
		(v.Rule.Specification == "ISO_14289_2" && v.Rule.Clause == "8.2.4" && v.Rule.TestNumber == 3) ||
		// ISO 14289-2 §8.2.4 t4: a standard type the /RoleMap leads to no
		// standard type.
		(v.Rule.Specification == "ISO_14289_2" && v.Rule.Clause == "8.2.4" && v.Rule.TestNumber == 4)
}

// roleChainReachesNoStandard reports whether the /RoleMap chain from src, as
// keep still holds it, ends without meeting a type of the PDF 1.7 vocabulary,
// which is what every /RoleMap target belongs to (ISO 32000-2:2020, 14.8.6):
// the chain stops on a type nothing maps, or closes on a type already
// visited. targets holds each source's target name. The walk is bounded by the
// number of entries, since each step visits a new one.
func roleChainReachesNoStandard(src ast.PdfName, targets map[ast.PdfName]ast.PdfName, keep ast.PdfDict) bool {
	visited := map[ast.PdfName]bool{src: true}
	cur := targets[src]
	for range len(targets) + 1 {
		if visited[cur] {
			return true
		}
		if compliance.IsStandardStructTypeInNS(cur, "") {
			return false
		}
		visited[cur] = true
		if _, ok := keep[cur]; !ok {
			return true
		}
		cur = targets[cur]
	}
	return true
}

// standardMembers returns the members of a /RoleMap cycle that are standard
// structure types under profile, in the order the walk met them.
func standardMembers(members []ast.PdfName, profile string) []ast.PdfName {
	var out []ast.PdfName
	for _, m := range members {
		if compliance.IsStandardStructType(m, profile) {
			out = append(out, m)
		}
	}
	return out
}

// fixRoleMap cleans up the StructTreeRoot /RoleMap. It breaks every circular
// mapping (a self-loop or a multi-hop cycle, forbidden in PDF/A and PDF/UA):
// a cycle through standard types loses their entries, so no element is given a
// standard type its content never claimed, and a cycle of private types has
// the entry that closes it remapped to /P. It
// in non-namespaced documents also drops entries that remap a standard structure
// type (ISO 32000-1 §14.8.4, forbidden by PDF/A-2a §7.1). A namespaced
// (PDF 2.0 / PDF/UA-2) document is left to its namespace mechanism: a standard
// *name* there may belong to a custom namespace and still need its mapping, so
// the standard-type drop does not apply on name alone. Under ISO 14289-2 it
// gives a type the map maps, declared in a namespace the map does not govern,
// the type the map gives it ([repairUngovernedRoles]), and drops an entry that
// leads a standard type to no standard type (ISO 14289-2 §8.2.4 t4). It also
// breaks the /RoleMapNS loops a namespaced PDF/UA-2 document's elements close
// ([repairNamespaceRoleLoops]), by the same rule as a /RoleMap cycle.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-2__8.2.4-t04-fail-a: maps /H1 to a private type nothing
//     maps further; dropping the entry leaves both tools reporting nothing on
//     the PDF/UA-2 and WTPDF-1.0-Accessibility outputs, default and
//     aggressive, where both reported 8.2.4 t4 before.
//   - litho complex_2a: maps /Div to /Custom. With the entry gone its Div,
//     which holds a content item, passes through to the Document for ISO/TS
//     32005 Table 5, and both tools report Document-content in place of
//     8.2.4 t4: the private mapping hid the defect, it did not repair it.
//   - All corpora: 35 files map a standard type to no standard type; only
//     those two are namespaced when the fixer runs, and the other 33 keep the
//     residual rule set they had at PDF/UA-2 and WTPDF-1.0-Accessibility.
//   - verapdf 7.1-t05-fail-d: dropping a cycle-closing source left another
//     entry pointing at an unmapped name, failing ISO 14289-1 §7.1 t5.
//   - verapdf PDF_UA-2__8.2.4-t02-fail-a: its self-mapping /LI -> /LI became
//     /LI -> /P, which litho reported as a remapped standard type and which
//     turned every list item into a paragraph, so <L> then held <P> and <P>
//     held <LBody>.
//   - litho rolecycle_ua2: its /Sect -> /Book -> /Sect cycle was broken by
//     sending /Book to /P, so the Sect in no namespace read as a P at
//     PDF/UA-2; dropping the /Sect entry keeps it a Sect, and both tools
//     report no circular mapping on it.
//   - litho complex_ua1: repaired at PDF/UA-2, breaking /Bar -> /Foo by
//     deletion left /MyType -> /Foo -> /Bar dangling.
//   - verapdf PDF_UA-1__7.1-t07-fail-a.pdf: maps /Document to /Book, a
//     standard source mapped to a custom name.
//   - govdocs thread_300_300708.pdf: maps /H4 -> /P. Dropping it alone turned
//     paragraphs into the document's FIRST heading, an H4, which fails the
//     heading sequence of ISO 14289-1, 7.4.2 test 1.
func fixRoleMap(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	stObj, ok := catalogDict["StructTreeRoot"]
	if !ok {
		return nil, nil
	}
	var stRef ast.PdfIndirectRef
	if ref, ok := stObj.(ast.PdfIndirectRef); ok {
		stRef = ref
	}
	stDict := resolveFromAllocOrDoc(ctx, alloc, doc, stRef)
	if stDict == nil {
		if d, err := document.ResolveDict(ctx, doc, stObj); err == nil {
			stDict = d
		}
	}
	if stDict == nil {
		return nil, nil
	}

	part1 := compliance.PDFAPart(profile) == 1

	// A namespaced PDF/UA-2 document can close a role-map loop through its
	// namespaces' /RoleMapNS alone, with no legacy /RoleMap at all.
	_, namespaced := stDict["Namespaces"]
	nsLoops := namespaced && compliance.PDFUAPart(profile) == 2

	roleMap, _ := document.ResolveDict(ctx, doc, stDict["RoleMap"]) // Best-effort: nil means no existing map.
	if roleMap == nil && !part1 && !nsLoops {
		return nil, nil
	}

	// Resolve every target name once. The RoleMap is a functional graph: each
	// source name maps to a single target name.
	targets := make(map[ast.PdfName]ast.PdfName, len(roleMap))
	keep := make(ast.PdfDict, len(roleMap))
	for src, dstObj := range roleMap {
		// Best-effort: an unreadable target yields an empty name; it cannot
		// close a cycle and is left in place.
		dstName, _ := document.ResolveName(ctx, doc, dstObj)
		targets[src] = dstName
		keep[src] = dstObj
	}

	srcs := make([]ast.PdfName, 0, len(keep))
	for src := range keep {
		srcs = append(srcs, src)
	}
	slices.Sort(srcs)

	var cyclesBroken, standardsDropped int

	// Standard-type sources removed in the standard-type pass, against the
	// target each declared, so the elements that used them can be retagged
	// rather than left to resolve to a different type than the file meant.
	dropped := make(map[ast.PdfName]ast.PdfName)

	// The cycle pass breaks every circular mapping. Follow each source's chain;
	// if it returns to that source, the entry closes a cycle. In a
	// non-namespaced document remap that source to the nearest standard type
	// (P) rather than dropping it: dropping would leave any other entry that
	// targets this source pointing at an unmapped name, which fails the "maps
	// to a standard type" rule (ISO 14289-1 §7.1 t5). A namespaced (PDF 2.0)
	// document keeps the drop, since a standard NAME there may belong to a
	// custom namespace and P must not be assumed standard for it. Sorted
	// iteration makes the broken edge deterministic; the step bound caps the
	// walk.
	for _, src := range srcs {
		if _, ok := keep[src]; !ok {
			continue
		}
		cur := targets[src]
		members := []ast.PdfName{src}
		for steps := 0; steps < len(targets); steps++ {
			if cur == src {
				// A standard type in the cycle is dropped, not redirected.
				// Remapping a standard type is forbidden outright
				// (ISO 14289-1, 7.1 t7), and nothing is left dangling: a chain
				// aimed at this source now ends on the standard name itself,
				// which is exactly where it should end. Redirecting a
				// self-mapping such as /LI -> /LI to /P made litho report a
				// remapped standard type and turned every list item into a
				// paragraph. Every standard type of the cycle goes, wherever
				// the walk entered it: in /Sect -> /Book -> /Sect, breaking at
				// the private /Book by sending it to /P sent every Sect on to
				// /P, a type the content never claimed.
				if standard := standardMembers(members, profile); len(standard) > 0 {
					for _, m := range standard {
						delete(keep, m)
						delete(targets, m)
					}
					cyclesBroken++
					break
				}
				// Remap rather than delete, in a namespaced document too.
				// Deleting the entry that closes the cycle leaves every other
				// entry aimed at it pointing at an unmapped name, which trades
				// the circular-mapping violation for an
				// is-not-mapped-to-a-standard-type one (ISO 14289-2, 8.2.4 t1).
				keep[src] = ast.PdfName("P")
				targets[src] = "P"
				cyclesBroken++
				break
			}
			if _, ok := keep[cur]; !ok {
				break // chain terminates (standard type or unmapped): no cycle
			}
			members = append(members, cur)
			cur = targets[cur]
		}
	}

	// A namespaced PDF/UA-2 document keeps its standard-type mappings but one:
	// a standard type whose /RoleMap chain reaches no standard type is a
	// remapped standard type (ISO 14289-2 §8.2.4 t4). Dropping the entry is
	// the whole repair, as for /Document -> /Book below: every element it
	// governed reads as the standard type its /S names, and a private type
	// mapped to that source stopped there already, since a mapping ends on
	// the first standard type it meets (ISO 32000-2:2020, 14.8.6).
	deadEnds := 0
	if nsLoops {
		for _, src := range srcs {
			if _, ok := keep[src]; !ok {
				continue
			}
			if compliance.IsStandardStructTypeInNS(src, "") && roleChainReachesNoStandard(src, targets, keep) {
				delete(keep, src)
				delete(targets, src)
				deadEnds++
			}
		}
	}

	// The standard-type pass drops redundant standard-type sources only in
	// non-namespaced documents. Standardness is profile-aware and shared with
	// the compliance checker (compliance.IsStandardStructType): a PDF 2.0 type
	// such as Title is non-standard under PDF/A-1/2/3 and PDF/UA-1, so its
	// mapping to a standard type is a required repair there, not a forbidden
	// standard-to-standard remap.
	if !namespaced {
		for _, src := range srcs {
			if _, ok := keep[src]; !ok {
				continue
			}
			// Annot is a PDF 1.5 type: under PDF/A-1 (PDF 1.4) it is
			// NON-standard, so mapping it to a PDF 1.4 standard type is the
			// required repair, not a forbidden standard remap.
			if part1 && src == "Annot" {
				continue
			}
			if compliance.IsStandardStructType(src, profile) {
				// Retag only towards a target that is itself standard. A
				// standard source mapped to a CUSTOM name (/Document to /Book,
				// say) is already the standard type its /S names; rewriting it
				// to the custom name would leave an element of an unmapped type
				// and trade this violation for ISO 14289-1 7.1 test 5. Dropping
				// the mapping alone is the whole repair there.
				if compliance.IsStandardStructType(targets[src], profile) {
					dropped[src] = targets[src]
				}
				delete(keep, src)
				standardsDropped++
			}
		}
	}

	// Dropping the entry un-maps every element that used it, so the element now
	// resolves to the standard type its /S literally names. That silently
	// changes what the document says: veraPDF resolves a role through the
	// RoleMap even for a standard name, so before the drop those elements read
	// as the target and after it they read as the source. Retag them to the
	// target the producer declared, which keeps the meaning the file already
	// had and leaves only the forbidden mapping removed.
	//
	// Dropping a mapping such as /H4 -> /P alone turns paragraphs into
	// headings, which can break the heading sequence of ISO 14289-1, 7.4.2
	// test 1: a repair introducing a violation the input did not have.
	retagged := retagDroppedRoles(ctx, alloc, doc, dropped)

	// PDF/A-1 targets: every Annot structure element needs a role mapping
	// to a PDF 1.4 standard type. Link is the nearest functional
	// equivalent for the annotation wrappers litho's own tagging emits.
	annotMapped := 0
	if part1 && keep["Annot"] == nil && structTreeHasAnnotElement(ctx, doc) {
		keep["Annot"] = ast.PdfName("Link")
		annotMapped++
	}

	mapChanged := cyclesBroken > 0 || standardsDropped > 0 || deadEnds > 0 || annotMapped > 0
	newST := make(ast.PdfDict, len(stDict))
	maps.Copy(newST, stDict)
	if mapChanged {
		if len(keep) == 0 {
			delete(newST, "RoleMap")
		} else {
			newST["RoleMap"] = keep
		}
	}

	// A namespaced element the legacy map does not govern is judged by the map
	// as it stands after the passes above, so a broken cycle leads it where it
	// leads every governed element.
	stChanged := mapChanged
	var loopFixes []string
	if nsLoops {
		var withNS ast.PdfDict
		loopFixes, withNS = repairNamespaceRoleLoops(ctx, alloc, doc, stRef, newST)
		if withNS != nil {
			newST = withNS
			stChanged = true
		}
	}

	var ungovernedFixes []string
	if compliance.PDFUAPart(profile) == 2 && len(keep) > 0 {
		var withNS ast.PdfDict
		ungovernedFixes, withNS = repairUngovernedRoles(ctx, alloc, doc, stRef, newST)
		if withNS != nil {
			newST = withNS
			stChanged = true
		}
	}

	if !stChanged && len(ungovernedFixes) == 0 && len(loopFixes) == 0 {
		return nil, nil
	}
	if stChanged && stRef.ObjectNum > 0 {
		alloc.AllocAt(stRef.ObjectNum, newST)
	}

	var fixes []string
	if cyclesBroken > 0 {
		fixes = append(fixes, fmt.Sprintf("broke %d circular role mapping(s)", cyclesBroken))
	}
	if standardsDropped > 0 {
		fixes = append(fixes, fmt.Sprintf("removed %d standard-type role mapping(s)", standardsDropped))
	}
	if deadEnds > 0 {
		fixes = append(fixes, fmt.Sprintf("removed %d role mapping(s) that led a standard type to no standard type", deadEnds))
	}
	if retagged > 0 {
		fixes = append(fixes, fmt.Sprintf("retagged %d element(s) to the role their dropped mapping declared", retagged))
	}
	if annotMapped > 0 {
		fixes = append(fixes, "mapped Annot structure elements to Link for PDF/A-1")
	}
	fixes = append(fixes, loopFixes...)
	return append(fixes, ungovernedFixes...), nil
}

// repairUngovernedRoles repairs the structure elements ISO 14289-2 §8.2.4 t1
// reports only because they declare a namespace the structure tree root's
// legacy /RoleMap does not govern, while the map sends their type to a standard
// one ([compliance.UngovernedRoleReader]). It returns the fix descriptions and,
// when a repair had to list a namespace the root's /Namespaces lacked, the
// updated root dictionary; otherwise that result is nil.
//
// The legacy map governs a type in no namespace, or in the PDF 1.7 namespace
// when that namespace declares no /RoleMapNS (ISO 32000-2:2020, 14.8.6), so an
// element whose type it maps can still resolve to no standard type once it
// declares another namespace. The repair gives the element the type the map
// gives every element of that type it governs, the meaning the producer
// declared, in the one form that namespace admits:
//
//   - an element of a standard namespace that defines that type is renamed to
//     it, as [retagDroppedRoles] retags the elements of a dropped mapping. A
//     /RoleMapNS entry costs more there: it either maps a type within its own
//     namespace (§8.2.4 t3) or reads the element in a namespace other than the
//     one it declares, and in the PDF 1.7 namespace it joins the very map that
//     left the type unmapped;
//   - a type of any other namespace, whose vocabulary no standard defines, is
//     mapped through that namespace's /RoleMapNS to the type in a standard
//     namespace (ISO 32000-2:2020, 14.8.6), the PDF 2.0 one where it defines
//     the type and the PDF 1.7 one otherwise. The entry maps every element of
//     that type in the namespace, and no element is renamed.
//
// A type the map leads to no standard type, or to one the element's standard
// namespace does not define, is left to the residual: repairing it would
// invent a meaning the document never declared. So is an inline element, which
// has no object number to rename in place, and an element whose /NS is not an
// indirect reference to a namespace dictionary (ISO 32000-2:2020, Table 355).
//
// # Corpus evidence
//
//   - All corpora: no tagged file declares, on an element whose type only the
//     legacy map maps, a namespace that map does not govern, before or after
//     PDF/UA-2 remediation.
//   - litho rolemapscope_ua2: a Para in the PDF 2.0 namespace the map sends to
//     P, which veraPDF reported as 8.2.4 t1 after PDF/UA-2 remediation.
//   - litho rolemapns_ua2: one element per repair, in two custom namespaces,
//     the PDF 1.7 namespace carrying its own /RoleMapNS and the PDF 2.0
//     namespace. veraPDF reported 8.2.4 t1 on all five after PDF/UA-2 and
//     WTPDF remediation, default and aggressive, and passes every output now.
func repairUngovernedRoles(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, stRef ast.PdfIndirectRef, stDict ast.PdfDict) ([]string, ast.PdfDict) {
	reader := compliance.NewUngovernedRoleReader(ctx, doc, stDict)
	type reading struct {
		target ast.PdfName
		nsURI  string
	}
	// readings holds, in walk order, each element to repair beside what the
	// reader said of it.
	var elems []structNode
	var readings []reading
	visited := map[int]bool{stRef.ObjectNum: true}
	walkStructElemsWhere(ctx, alloc, doc, stDict["K"], func(elem ast.PdfDict) bool {
		target, nsURI, ok := reader.Target(ctx, doc, elem)
		if ok {
			readings = append(readings, reading{target: target, nsURI: nsURI})
		}
		return ok
	}, visited, 0, &elems)

	renamed := 0
	// entries holds, per namespace object number, the /RoleMapNS entries the
	// namespace receives: its type against the standard type the map gives it.
	entries := make(map[int]map[ast.PdfName]ast.PdfName)
	for i, elem := range elems {
		target, nsURI := readings[i].target, readings[i].nsURI
		if compliance.IsStandardNamespace(nsURI) {
			if !compliance.IsStandardStructTypeInNS(target, nsURI) {
				continue
			}
			newElem := make(ast.PdfDict, len(elem.dict))
			maps.Copy(newElem, elem.dict)
			newElem["S"] = target
			if alloc.AllocAt(elem.ref.ObjectNum, newElem) {
				renamed++
			}
			continue
		}
		nsRef, ok := elem.dict["NS"].(ast.PdfIndirectRef)
		if !ok || nsRef.ObjectNum <= 0 {
			continue
		}
		if entries[nsRef.ObjectNum] == nil {
			entries[nsRef.ObjectNum] = make(map[ast.PdfName]ast.PdfName)
		}
		entries[nsRef.ObjectNum][structType(ctx, doc, elem.dict)] = target
	}

	mapped, newST := mapUngovernedTypes(ctx, alloc, doc, stDict, entries)

	var fixes []string
	if renamed > 0 {
		fixes = append(fixes, fmt.Sprintf("retagged %d element(s) the /RoleMap maps in a namespace it does not govern as the standard type it maps them to", renamed))
	}
	if mapped > 0 {
		fixes = append(fixes, fmt.Sprintf("mapped %d type(s) the /RoleMap maps in a namespace it does not govern through that namespace's /RoleMapNS", mapped))
	}
	return fixes, newST
}

// mapUngovernedTypes writes the /RoleMapNS entries of [repairUngovernedRoles]:
// for each namespace object number, each type mapped to the standard type the
// legacy /RoleMap gives it, as the [name namespace] pair a /RoleMapNS value
// names a type of another namespace with (ISO 32000-2:2020, 14.8.6). It
// returns the number of entries written and, when a target namespace had to be
// added to the root's /Namespaces, the updated root dictionary; otherwise that
// result is nil.
//
// A namespace whose /RoleMapNS is present but no dictionary is left alone:
// writing one over it would replace a value the file wrote rather than extend
// it.
func mapUngovernedTypes(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, stDict ast.PdfDict, entries map[int]map[ast.PdfName]ast.PdfName) (int, ast.PdfDict) {
	nsNums := slices.Sorted(maps.Keys(entries))
	mapped := 0
	st := stDict
	var newST ast.PdfDict
	for _, num := range nsNums {
		if ctx.Err() != nil {
			break
		}
		added := 0
		editRoleMapNS(ctx, alloc, doc, num, func(rmns ast.PdfDict) bool {
			for _, typ := range slices.Sorted(maps.Keys(entries[num])) {
				if _, exists := rmns[typ]; exists {
					continue
				}
				target := entries[num][typ]
				targetURI := pdf1NamespaceURI
				if compliance.IsStandardStructTypeInNS(target, pdf2NamespaceURI) {
					targetURI = pdf2NamespaceURI
				}
				targetRef, withNS := ensureNamespace(ctx, alloc, doc, st, targetURI)
				if withNS != nil {
					st, newST = withNS, withNS
				}
				rmns[typ] = ast.PdfArray{target, targetRef}
				added++
			}
			return added > 0
		})
		mapped += added
	}
	return mapped, newST
}

// retagDroppedRoles rewrites /S on every element that used a role mapping the
// standard-type pass removed, setting it to the target that mapping declared.
// It returns the number of elements rewritten.
//
// Only a mapping whose target resolves to a non-empty name is followed: an
// unreadable target names no role to retag to, and rewriting /S to an empty name
// would destroy the element. A source equal to its target needs no rewrite, and
// would be a self-mapping the cycle pass has already broken.
func retagDroppedRoles(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, dropped map[ast.PdfName]ast.PdfName) int {
	srcs := make([]ast.PdfName, 0, len(dropped))
	for src, dst := range dropped {
		if dst == "" || dst == src {
			continue
		}
		srcs = append(srcs, src)
	}
	// Sorted so the walk order, and therefore the output, is deterministic.
	slices.Sort(srcs)

	retagged := 0
	for _, src := range srcs {
		if ctx.Err() != nil {
			return retagged
		}
		elems, err := collectStructElems(ctx, alloc, doc, src)
		if err != nil {
			doc.Logger.Debug("remediation.rolemap: cannot collect elements for dropped role",
				"role", string(src), "error", err)
			continue
		}
		for _, elem := range elems {
			if elem.ref.ObjectNum <= 0 {
				continue // inline element: nothing to write back at
			}
			newElem := make(ast.PdfDict, len(elem.dict))
			maps.Copy(newElem, elem.dict)
			newElem["S"] = dropped[src]
			if alloc.AllocAt(elem.ref.ObjectNum, newElem) {
				retagged++
			}
		}
	}
	return retagged
}

// structTreeHasAnnotElement reports whether any structure element in the
// document uses type Annot. The scan walks the object index like
// [collectFileSpecs]: structure elements are always indirect.
func structTreeHasAnnotElement(ctx context.Context, doc *document.Document) bool {
	for objNum, entry := range sweptObjects(doc) {
		if ctx.Err() != nil {
			return false
		}
		if entry.Free || objNum <= 0 {
			continue
		}
		d, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || d == nil {
			continue
		}
		if t, _ := document.ResolveName(ctx, doc, d["Type"]); t != "" && t != "StructElem" {
			continue
		}
		if sName, _ := document.ResolveName(ctx, doc, d["S"]); sName == "Annot" {
			if _, hasP := d["P"]; hasP {
				return true
			}
		}
	}
	return false
}
