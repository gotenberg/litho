package remediation

import (
	"context"

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
	return wrapStructChildren(ctx, alloc, doc, "Table", wrapBodyRows, "wrapped %d table body row(s) in a TBody")
}

// wrapBodyRows wraps a Table's direct-child TR elements in a new TBody when the
// Table carries a THead or TFoot but no TBody. It returns the number of rows
// wrapped, or 0 when no repair applies. Only indirect TR children are wrapped;
// a Table that mixes in an inline TR is left untouched for safety. Body rows
// need not be contiguous in /K: every direct-child TR moves into the TBody
// regardless of position, and the TBody takes the slot of the first body row.
func wrapBodyRows(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, t structNode, _ map[ast.PdfName]ast.PdfName) int {
	arr := resolveStructKids(ctx, doc, t.dict["K"])
	if len(arr) == 0 {
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

	return reparentIntoContainer(ctx, alloc, doc, t, arr, trRefs, "TBody")
}
