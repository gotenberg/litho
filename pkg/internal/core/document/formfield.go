package document

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// ResolveFieldParent follows one hop up a form field's /Parent chain, returning
// the parent object and its dictionary, and reporting whether the walk may
// continue. It stops at a field that states no /Parent, at a parent that does
// not resolve to a dictionary, and at a parent already visited.
//
// seen records the object numbers visited so far and is updated in place. The
// caller owns the depth bound and the loop; this call owns only the hop, so a
// walk keeps whatever limit its own package documents.
//
// A depth bound alone does not make a /Parent walk safe. It stops a cycle
// without detecting one, so the walk runs to the bound and then reports
// whichever node it happened to halt on as though that node were the answer.
// Cycles are real: one corpus file carries 16 of them, while the deepest
// acyclic chain measured across 5171 files is 7, so every bound in use sits far
// above anything genuine and only the visited set does real work.
//
// [Document.Resolve] cannot stand in for seen. Its own visited set is scoped to
// a single call and unwinds on return, and a second visit to the same object
// hits the object cache and returns before that check runs at all.
//
// Only an indirect /Parent can close a cycle, since a dictionary written
// directly into its child cannot refer back to it, so a direct parent is
// followed without being recorded.
func ResolveFieldParent(
	ctx context.Context,
	doc *Document,
	dict ast.PdfDict,
	seen map[int]bool,
) (ast.PdfObject, ast.PdfDict, bool) {
	parentObj, ok := dict["Parent"]
	if !ok {
		return nil, nil, false
	}
	if ref, isRef := parentObj.(ast.PdfIndirectRef); isRef {
		if seen[ref.ObjectNum] {
			return nil, nil, false
		}
		seen[ref.ObjectNum] = true
	}
	parent, err := ResolveDict(ctx, doc, parentObj)
	if err != nil || parent == nil {
		return nil, nil, false
	}
	return parentObj, parent, true
}
