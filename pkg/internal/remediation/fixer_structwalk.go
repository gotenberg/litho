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

// structNode pairs a structure element with its indirect reference.
type structNode struct {
	ref  ast.PdfIndirectRef
	dict ast.PdfDict
}

// structType returns a structure element's /S role name. An unresolvable /S
// yields the empty name, which matches no role, so the caller treats the element
// as one to leave in place. The resolve error carries no extra signal beyond
// that empty result, so it is dropped.
func structType(ctx context.Context, doc *document.Document, elem ast.PdfDict) ast.PdfName {
	s, _ := document.ResolveName(ctx, doc, elem["S"])
	return s
}

// resolveStructKids returns a structure element's /K as an array. Per ISO
// 32000-1 §14.7.2 /K may be a single element or an array; a single element is
// wrapped into a one-item array so callers handle both forms uniformly, the same
// way the validator's walk does (compliance walkChildren). The original /K item
// is kept (not the resolved object), so a caller still sees an indirect
// reference where one was given. Returns nil when /K is absent or resolves to
// neither an array nor a dictionary.
func resolveStructKids(ctx context.Context, doc *document.Document, kObj ast.PdfObject) ast.PdfArray {
	if arr, err := document.ResolveArray(ctx, doc, kObj); err == nil {
		return arr
	}
	if d, err := document.ResolveDict(ctx, doc, kObj); err == nil && d != nil {
		return ast.PdfArray{kObj}
	}
	return nil
}

// collectStructElems walks the structure tree reachable from the catalog's
// StructTreeRoot and returns every element whose /S role equals role. It returns
// (nil, nil) when the document has no StructTreeRoot. Reads are allocator-aware,
// so a fixer sees the pending edits of earlier passes.
func collectStructElems(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, role ast.PdfName) ([]structNode, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
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

	var out []structNode
	visited := map[int]bool{stRef.ObjectNum: true}
	walkStructElems(ctx, alloc, doc, stDict["K"], role, visited, 0, &out)
	return out, nil
}

// walkStructElems recursively gathers role-matching elements from kObj. A
// visited set breaks reference cycles and the depth bound caps a pathological
// tree.
func walkStructElems(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, kObj ast.PdfObject, role ast.PdfName, visited map[int]bool, depth int, out *[]structNode) {
	if depth > compliance.MaxStructTreeWalkDepth || ctx.Err() != nil {
		return
	}
	switch v := kObj.(type) {
	case ast.PdfArray:
		for _, item := range v {
			walkStructElems(ctx, alloc, doc, item, role, visited, depth, out)
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
		if structType(ctx, doc, elem) == role {
			*out = append(*out, structNode{ref: v, dict: elem})
		}
		walkStructElems(ctx, alloc, doc, elem["K"], role, visited, depth+1, out)
	case ast.PdfDict:
		walkStructElems(ctx, alloc, doc, v["K"], role, visited, depth+1, out)
	}
}

// childWrapper repairs one structure element in place, returning the number of
// children it moved (0 when no repair applied).
type childWrapper func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, node structNode) int

// wrapStructChildren runs wrap over every structure element of the given role and
// reports one message per element repaired. It is the shared driver behind the
// table-section and list-body fixers.
func wrapStructChildren(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, role ast.PdfName, wrap childWrapper, msgf string) ([]string, error) {
	nodes, err := collectStructElems(ctx, alloc, doc, role)
	if err != nil {
		return nil, err
	}
	var fixes []string
	for _, n := range nodes {
		if c := wrap(ctx, alloc, doc, n); c > 0 {
			fixes = append(fixes, fmt.Sprintf(msgf, c))
		}
	}
	if len(fixes) == 0 {
		return nil, nil
	}
	return fixes, nil
}

// movedNode is a child that resolved and so can be re-parented under a new
// container.
type movedNode struct {
	ref  ast.PdfIndirectRef
	dict ast.PdfDict
}

// reparentIntoContainer moves the given child references of parent into a new
// container structure element of role containerRole. It allocates the container,
// re-parents each resolvable child onto it (updating /P), and rebuilds parent's
// /K (passed as parentK) with the container in the slot of the first moved child,
// dropping the other moved children and preserving the order of everything else.
// It returns the number of children moved, or 0 when none resolve. The
// /ParentTree is untouched: moved objects keep their object numbers, so only the
// /P pointers and parent's /K change. Children that fail to resolve are dropped
// from the move so the container /K, the re-parented /P pointers, and the removal
// set stay derived from one consistent list.
func reparentIntoContainer(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, parent structNode, parentK ast.PdfArray, moveRefs []ast.PdfIndirectRef, containerRole ast.PdfName) int {
	var moved []movedNode
	for _, r := range moveRefs {
		d := resolveFromAllocOrDoc(ctx, alloc, doc, r)
		if d == nil {
			continue
		}
		cp := make(ast.PdfDict, len(d))
		maps.Copy(cp, d)
		moved = append(moved, movedNode{ref: r, dict: cp})
	}
	if len(moved) == 0 {
		return 0
	}

	items := make(ast.PdfArray, len(moved))
	movedSet := make(map[int]bool, len(moved))
	for i, m := range moved {
		items[i] = m.ref
		movedSet[m.ref.ObjectNum] = true
	}
	containerRef := alloc.Alloc(ast.PdfDict{
		"Type": ast.PdfName("StructElem"),
		"S":    containerRole,
		"P":    parent.ref,
		"K":    items,
	})

	for _, m := range moved {
		m.dict["P"] = containerRef
		alloc.AllocAt(m.ref.ObjectNum, m.dict)
	}

	newK := make(ast.PdfArray, 0, len(parentK))
	inserted := false
	for _, item := range parentK {
		if ref, ok := item.(ast.PdfIndirectRef); ok && movedSet[ref.ObjectNum] {
			if !inserted {
				newK = append(newK, containerRef)
				inserted = true
			}
			continue
		}
		newK = append(newK, item)
	}
	newParent := make(ast.PdfDict, len(parent.dict))
	maps.Copy(newParent, parent.dict)
	newParent["K"] = newK
	alloc.AllocAt(parent.ref.ObjectNum, newParent)

	return len(moved)
}
