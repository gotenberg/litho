package remediation

import (
	"context"
	"fmt"
	"maps"

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

// fixRoleMap cleans up the StructTreeRoot /RoleMap by removing entries
// that map a standard structure type (ISO 32000-1 §14.8.4) or that form
// a circular mapping (source == target).
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

	newRoleMap := make(ast.PdfDict, len(roleMap))
	modified := false
	for src, dstObj := range roleMap {
		// Best-effort: an unreadable mapping target yields an empty name,
		// skipped by the guard below.
		dstName, _ := document.ResolveName(ctx, doc, dstObj)

		// Drop entries for standard types (forbidden by PDF/A-2a §7.1).
		if standardStructTypes[src] {
			modified = true
			continue
		}
		// Drop circular mappings (src → src).
		if dstName == src {
			modified = true
			continue
		}
		newRoleMap[src] = dstObj
	}

	if !modified {
		return nil, nil
	}

	newST := make(ast.PdfDict, len(stDict))
	maps.Copy(newST, stDict)
	if len(newRoleMap) == 0 {
		delete(newST, "RoleMap")
	} else {
		newST["RoleMap"] = newRoleMap
	}
	if stRef.ObjectNum > 0 {
		alloc.AllocAt(stRef.ObjectNum, newST)
	}

	return []string{fmt.Sprintf("cleaned StructTreeRoot /RoleMap (%d entries remaining)", len(newRoleMap))}, nil
}
