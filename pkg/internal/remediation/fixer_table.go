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

// matchTableSections matches the "Table has THead/TFoot but no TBody" violations
// (ISO 14289-1 §7.2 t13/t14). It keys on the exact test numbers rather than the
// clause prefix, which §7.2 shares with the /Lang checks.
func matchTableSections(v compliance.Violation) bool {
	return v.Rule.Clause == "7.2" && (v.Rule.TestNumber == 13 || v.Rule.TestNumber == 14)
}

// tableNode pairs a Table structure element with its indirect reference.
type tableNode struct {
	ref  ast.PdfIndirectRef
	dict ast.PdfDict
}

// fixTableSections wraps a Table's orphan body rows in a TBody. A Table that
// carries a THead or TFoot but no TBody, with one or more TR children directly
// under the Table, is non-compliant (ISO 14289-1 §7.2 t13/t14): the body rows
// belong in a TBody section. The fixer allocates a TBody, moves those TR
// elements into it (updating each TR's /P), and replaces them in the Table's /K
// with the new TBody. Only TR elements given as indirect references are moved,
// so the rebuild never has to rewrite an inline subtree. The /ParentTree is
// left untouched: the TR objects and the leaf cells that own the marked-content
// IDs keep their object numbers, so only the /P pointers and the Table's /K
// change. It does not re-nest cells or reorder sections; those are separate
// repairs.
func fixTableSections(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalog == nil {
		return nil, fmt.Errorf("resolving catalog")
	}
	stRef, ok := catalog["StructTreeRoot"].(ast.PdfIndirectRef)
	if !ok {
		return nil, nil
	}
	stDict := resolveFromAllocOrDoc(ctx, alloc, doc, stRef)
	if stDict == nil {
		return nil, nil
	}

	var tables []tableNode
	visited := map[int]bool{stRef.ObjectNum: true}
	collectTables(ctx, alloc, doc, stDict["K"], visited, 0, &tables)

	var fixes []string
	for _, t := range tables {
		if n := wrapBodyRows(ctx, alloc, doc, t); n > 0 {
			fixes = append(fixes, fmt.Sprintf("wrapped %d table body row(s) in a TBody", n))
		}
	}
	if len(fixes) == 0 {
		return nil, nil
	}
	return fixes, nil
}

// collectTables walks the structure tree and gathers every Table element
// reachable from kObj. A visited set breaks reference cycles and the depth bound
// caps a pathological tree, mirroring the /Lang repair walk.
func collectTables(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, kObj ast.PdfObject, visited map[int]bool, depth int, out *[]tableNode) {
	if depth > compliance.MaxStructTreeWalkDepth || ctx.Err() != nil {
		return
	}
	switch v := kObj.(type) {
	case ast.PdfArray:
		for _, item := range v {
			collectTables(ctx, alloc, doc, item, visited, depth, out)
		}
	case ast.PdfIndirectRef:
		if v.ObjectNum == 0 || visited[v.ObjectNum] {
			return
		}
		visited[v.ObjectNum] = true
		elem := resolveFromAllocOrDoc(ctx, alloc, doc, v)
		if elem == nil {
			return
		}
		if structType(ctx, doc, elem) == "Table" {
			*out = append(*out, tableNode{ref: v, dict: elem})
		}
		collectTables(ctx, alloc, doc, elem["K"], visited, depth+1, out)
	case ast.PdfDict:
		collectTables(ctx, alloc, doc, v["K"], visited, depth+1, out)
	}
}

// structType returns a structure element's /S role name. An unresolvable /S
// yields the empty name, which matches no table role, so the caller treats the
// element as a non-section, non-row child and leaves it in place. The resolve
// error carries no extra signal beyond that empty result, so it is dropped.
func structType(ctx context.Context, doc *document.Document, elem ast.PdfDict) ast.PdfName {
	s, _ := document.ResolveName(ctx, doc, elem["S"])
	return s
}

// movedRow is a body TR that resolved and so can be re-parented under the TBody.
type movedRow struct {
	ref  ast.PdfIndirectRef
	dict ast.PdfDict
}

// wrapBodyRows wraps a Table's direct-child TR elements in a new TBody when the
// Table carries a THead or TFoot but no TBody. It returns the number of rows
// wrapped, or 0 when no repair applies. Only indirect TR children are wrapped;
// a Table that mixes in an inline TR is left untouched for safety. Body rows
// need not be contiguous in /K: every direct-child TR moves into the TBody
// regardless of position, and the TBody takes the slot of the first body row.
func wrapBodyRows(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, t tableNode) int {
	arr, err := document.ResolveArray(ctx, doc, t.dict["K"])
	if err != nil || len(arr) == 0 {
		return 0
	}

	var hasSection, hasTBody, inlineTR bool
	var trRefs []ast.PdfIndirectRef
	for _, item := range arr {
		d, err := document.ResolveDict(ctx, doc, item)
		if err != nil || d == nil {
			continue
		}
		switch structType(ctx, doc, d) {
		case "THead", "TFoot":
			hasSection = true
		case "TBody":
			hasTBody = true
		case "TR":
			if ref, ok := item.(ast.PdfIndirectRef); ok {
				trRefs = append(trRefs, ref)
			} else {
				inlineTR = true
			}
		}
	}

	if !hasSection || hasTBody || len(trRefs) == 0 || inlineTR {
		return 0
	}

	// Resolve every body row up front. Only rows that resolve move, so the
	// TBody /K, the re-parented /P pointers, and the Table /K removal set are
	// derived from one list and stay consistent even if a row fails to resolve.
	var moved []movedRow
	for _, r := range trRefs {
		tr := resolveFromAllocOrDoc(ctx, alloc, doc, r)
		if tr == nil {
			continue
		}
		cp := make(ast.PdfDict, len(tr))
		maps.Copy(cp, tr)
		moved = append(moved, movedRow{ref: r, dict: cp})
	}
	if len(moved) == 0 {
		return 0
	}

	trItems := make(ast.PdfArray, len(moved))
	movedSet := make(map[int]bool, len(moved))
	for i, m := range moved {
		trItems[i] = m.ref
		movedSet[m.ref.ObjectNum] = true
	}
	tbodyRef := alloc.Alloc(ast.PdfDict{
		"Type": ast.PdfName("StructElem"),
		"S":    ast.PdfName("TBody"),
		"P":    t.ref,
		"K":    trItems,
	})

	// Re-parent each moved TR onto the TBody.
	for _, m := range moved {
		m.dict["P"] = tbodyRef
		alloc.AllocAt(m.ref.ObjectNum, m.dict)
	}

	// Rebuild the Table /K: drop every moved body TR and put the single TBody in
	// the slot of the first one, keeping THead/TFoot/Caption and the order.
	newK := make(ast.PdfArray, 0, len(arr))
	inserted := false
	for _, item := range arr {
		if ref, ok := item.(ast.PdfIndirectRef); ok && movedSet[ref.ObjectNum] {
			if !inserted {
				newK = append(newK, tbodyRef)
				inserted = true
			}
			continue
		}
		newK = append(newK, item)
	}
	newTable := make(ast.PdfDict, len(t.dict))
	maps.Copy(newTable, t.dict)
	newTable["K"] = newK
	alloc.AllocAt(t.ref.ObjectNum, newTable)

	return len(moved)
}
