package remediation

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchTableCells matches the misplaced-table-cell violation (ISO 14289-1 §7.2
// t04: "TD/TH inside X instead of TR"). It keys on the exact clause and test
// number; t04 has a single emission site, so no context disambiguation is needed.
func matchTableCells(v compliance.Violation) bool {
	return v.Rule.Clause == "7.2" && v.Rule.TestNumber == 4
}

// cellContainerRoles are the table containers under which a TR is a valid child.
// A TD/TH directly under one of these is wrapped in a TR; a cell under any other
// element is left alone, because a TR placed there would itself be misplaced.
var cellContainerRoles = []ast.PdfName{"Table", "THead", "TBody", "TFoot"}

// fixTableCells wraps a table cell that sits directly under a Table, THead,
// TBody, or TFoot in a new TR. ISO 14289-1 §7.2 requires every TD/TH to be a
// child of a TR; a cell placed straight under a row group is non-compliant. The
// fixer allocates a TR, moves the orphan cells into it (updating each cell's /P),
// and replaces them in the container's /K with the TR. It repairs only cells
// whose parent is a row-group container, because a TR is a valid child only
// there; a cell under any other element is left for manual repair, since wrapping
// it would merely move the violation onto the new TR. The repair is structure
// only: moved cells keep their object numbers, so only /P pointers and the
// container's /K change.
func fixTableCells(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	var fixes []string
	for _, role := range cellContainerRoles {
		msgs, err := wrapStructChildren(ctx, alloc, doc, role, wrapCellsInRow, "wrapped %d table cell(s) in a TR")
		if err != nil {
			return nil, err
		}
		fixes = append(fixes, msgs...)
	}
	if len(fixes) == 0 {
		return nil, nil
	}
	return fixes, nil
}

// wrapCellsInRow wraps a container's direct-child TD/TH elements in a new TR. It
// returns the number of cells wrapped, or 0 when no repair applies. Only indirect
// cells are moved; a container that mixes in an inline cell is left untouched for
// safety, mirroring the table-section and list-body fixers. Existing TR children
// and any non-cell children are left in place.
func wrapCellsInRow(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, container structNode) int {
	arr := resolveStructKids(ctx, doc, container.dict["K"])
	if len(arr) == 0 {
		return 0
	}

	var inlineCell bool
	var cellRefs []ast.PdfIndirectRef
	for _, item := range arr {
		d, err := document.ResolveDict(ctx, doc, item)
		if err != nil || d == nil {
			continue
		}
		switch structType(ctx, doc, d) {
		case "TD", "TH":
			if ref, ok := item.(ast.PdfIndirectRef); ok {
				cellRefs = append(cellRefs, ref)
			} else {
				inlineCell = true
			}
		}
	}

	if len(cellRefs) == 0 || inlineCell {
		return 0
	}

	return reparentIntoContainer(ctx, alloc, doc, container, arr, cellRefs, "TR")
}
