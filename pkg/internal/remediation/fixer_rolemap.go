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
		clausePrefix(v, "6.8.3.4")
}

// fixRoleMap cleans up the StructTreeRoot /RoleMap. It breaks every circular
// mapping (a self-loop or a multi-hop cycle, forbidden in PDF/A and PDF/UA), and
// in non-namespaced documents also drops entries that remap a standard structure
// type (ISO 32000-1 §14.8.4, forbidden by PDF/A-2a §7.1). A namespaced
// (PDF 2.0 / PDF/UA-2) document is left to its namespace mechanism: a standard
// *name* there may belong to a custom namespace and still need its mapping, so
// the standard-type drop does not apply on name alone.
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

	roleMap, _ := document.ResolveDict(ctx, doc, stDict["RoleMap"]) // Best-effort: nil means no existing map.
	if roleMap == nil && !part1 {
		return nil, nil
	}

	// Resolve every target name once. The RoleMap is a functional graph: each
	// source name maps to a single target name.
	_, namespaced := stDict["Namespaces"]
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

	// Pass 1: break every circular mapping. Follow each source's chain; if it
	// returns to that source, the entry closes a cycle. In a non-namespaced
	// document remap that source to the nearest standard type (P) rather than
	// dropping it: dropping would leave any other entry that targets this source
	// pointing at an unmapped name, which fails the "maps to a standard type"
	// rule (ISO 14289-1 §7.1 t5, qpdf-corpus 7.1-t05-fail-d). A namespaced
	// (PDF 2.0) document keeps the drop, since a standard NAME there may belong
	// to a custom namespace and P must not be assumed standard for it. Sorted
	// iteration makes the broken edge deterministic; the step bound caps the walk.
	for _, src := range srcs {
		if _, ok := keep[src]; !ok {
			continue
		}
		cur := targets[src]
		for steps := 0; steps < len(targets); steps++ {
			if cur == src {
				// A standard type in the cycle is dropped, not redirected.
				// Remapping a standard type is forbidden outright
				// (ISO 14289-1, 7.1 t7), and nothing is left dangling: a chain
				// aimed at this source now ends on the standard name itself,
				// which is exactly where it should end. The self-mapping
				// /LI -> /LI of PDF_UA-2__8.2.4-t02-fail-a became /LI -> /P,
				// which litho reported as a remapped standard type and which
				// turned every list item into a paragraph, so <L> then held
				// <P> and <P> held <LBody>.
				if compliance.IsStandardStructType(src, profile) {
					delete(keep, src)
					delete(targets, src)
					cyclesBroken++
					break
				}
				// Remap rather than delete, in a namespaced document too.
				// Deleting the entry that closes the cycle leaves every other
				// entry aimed at it pointing at an unmapped name, which trades
				// the circular-mapping violation for an
				// is-not-mapped-to-a-standard-type one (ISO 14289-2, 8.2.4 t1).
				// Measured on complex_ua1 repaired at PDF/UA-2, where breaking
				// /Bar -> /Foo left /MyType -> /Foo -> /Bar dangling.
				keep[src] = ast.PdfName("P")
				targets[src] = "P"
				cyclesBroken++
				break
			}
			if _, ok := keep[cur]; !ok {
				break // chain terminates (standard type or unmapped): no cycle
			}
			cur = targets[cur]
		}
	}

	// Pass 2: drop redundant standard-type sources only in non-namespaced
	// documents. Standardness is profile-aware and shared with the compliance
	// checker (compliance.IsStandardStructType): a PDF 2.0 type such as Title is
	// non-standard under PDF/A-1/2/3 and PDF/UA-1, so its mapping to a standard
	// type is a required repair there, not a forbidden standard-to-standard remap.
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
				delete(keep, src)
				standardsDropped++
			}
		}
	}

	// PDF/A-1 targets: every Annot structure element needs a role mapping
	// to a PDF 1.4 standard type. Link is the nearest functional
	// equivalent for the annotation wrappers litho's own tagging emits.
	annotMapped := 0
	if part1 && keep["Annot"] == nil && structTreeHasAnnotElement(ctx, doc) {
		keep["Annot"] = ast.PdfName("Link")
		annotMapped++
	}

	if cyclesBroken == 0 && standardsDropped == 0 && annotMapped == 0 {
		return nil, nil
	}

	newST := make(ast.PdfDict, len(stDict))
	maps.Copy(newST, stDict)
	if len(keep) == 0 {
		delete(newST, "RoleMap")
	} else {
		newST["RoleMap"] = keep
	}
	if stRef.ObjectNum > 0 {
		alloc.AllocAt(stRef.ObjectNum, newST)
	}

	var fixes []string
	if cyclesBroken > 0 {
		fixes = append(fixes, fmt.Sprintf("broke %d circular role mapping(s)", cyclesBroken))
	}
	if standardsDropped > 0 {
		fixes = append(fixes, fmt.Sprintf("removed %d standard-type role mapping(s)", standardsDropped))
	}
	if annotMapped > 0 {
		fixes = append(fixes, "mapped Annot structure elements to Link for PDF/A-1")
	}
	return fixes, nil
}

// structTreeHasAnnotElement reports whether any structure element in the
// document uses type Annot. The scan walks the object index like
// [collectFileSpecs]: structure elements are always indirect.
func structTreeHasAnnotElement(ctx context.Context, doc *document.Document) bool {
	for objNum, entry := range doc.Index.Entries {
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
