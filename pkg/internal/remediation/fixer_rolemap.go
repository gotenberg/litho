package remediation

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// standardStructTypes lists structure types considered standard per
// ISO 32000-1 §14.8.4 and ISO 32000-2 §14.8.4. Mappings of these types
// in the StructTreeRoot /RoleMap are either redundant (standard to
// standard) or forbidden by PDF/A-2a §7.1 (standard remapped at all).
//
//nolint:gochecknoglobals // static lookup table
var standardStructTypes = map[ast.PdfName]bool{
	"Document": true, "Part": true, "Art": true, "Sect": true, "Div": true,
	"BlockQuote": true, "Caption": true, "TOC": true, "TOCI": true, "Index": true,
	"NonStruct": true, "Private": true, "P": true, "H": true,
	"H1": true, "H2": true, "H3": true, "H4": true, "H5": true, "H6": true,
	"L": true, "LI": true, "Lbl": true, "LBody": true,
	"Table": true, "TR": true, "TH": true, "TD": true, "THead": true, "TBody": true, "TFoot": true,
	"Span": true, "Quote": true, "Note": true, "Reference": true, "BibEntry": true, "Code": true,
	"Link": true, "Annot": true,
	"Ruby": true, "RB": true, "RT": true, "RP": true,
	"Warichu": true, "WT": true, "WP": true,
	"Figure": true, "Formula": true, "Form": true,
	"DocumentFragment": true, "Aside": true, "Title": true, "FENote": true, "Sub": true, "Em": true, "Strong": true,
}

func matchRoleMap(v compliance.Violation) bool {
	return descriptionContains(v, "standard tags") && descriptionContains(v, "remapped") ||
		descriptionContains(v, "circular mapping") ||
		descriptionContains(v, "is remapped") ||
		descriptionContains(v, "standard type") && descriptionContains(v, "remapped")
}

// fixRoleMap cleans up the StructTreeRoot /RoleMap. It breaks every circular
// mapping (a self-loop or a multi-hop cycle, forbidden in PDF/A and PDF/UA), and
// in non-namespaced documents also drops entries that remap a standard structure
// type (ISO 32000-1 §14.8.4, forbidden by PDF/A-2a §7.1). A namespaced
// (PDF 2.0 / PDF/UA-2) document is left to its namespace mechanism: a standard
// *name* there may belong to a custom namespace and still need its mapping, so
// the standard-type drop does not apply on name alone.
func fixRoleMap(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
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

	roleMapObj, ok := stDict["RoleMap"]
	if !ok {
		return nil, nil
	}
	roleMap, err := document.ResolveDict(ctx, doc, roleMapObj)
	if err != nil || roleMap == nil {
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
	sort.Slice(srcs, func(i, j int) bool { return srcs[i] < srcs[j] })

	var cyclesBroken, standardsDropped int

	// Pass 1: break every circular mapping. Follow each source's chain; if it
	// returns to that source, the entry closes a cycle, so drop it. Sorted
	// iteration makes the dropped edge deterministic, and the step bound caps
	// the walk on already-cyclic input.
	for _, src := range srcs {
		if _, ok := keep[src]; !ok {
			continue
		}
		cur := targets[src]
		for steps := 0; steps < len(targets); steps++ {
			if cur == src {
				delete(keep, src)
				cyclesBroken++
				break
			}
			if _, ok := keep[cur]; !ok {
				break // chain terminates (standard type or unmapped): no cycle
			}
			cur = targets[cur]
		}
	}

	// Pass 2: drop standard-type sources only in non-namespaced documents.
	if !namespaced {
		for _, src := range srcs {
			if _, ok := keep[src]; !ok {
				continue
			}
			if standardStructTypes[src] {
				delete(keep, src)
				standardsDropped++
			}
		}
	}

	if cyclesBroken == 0 && standardsDropped == 0 {
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
	return fixes, nil
}
