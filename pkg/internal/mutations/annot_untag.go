package mutations

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// RemovedAnnotation is an annotation leaving its page: Obj is its /Annots
// entry, a reference or the dictionary written in place, and Dict its
// dictionary as it stood before the removal.
type RemovedAnnotation struct {
	Obj  ast.PdfObject
	Dict ast.PdfDict
}

// UntagRemovedAnnotations takes annotations that leave their pages out of the
// document's structure tree, the cleanup [FlattenForm] makes for a widget it
// drops without drawing (see flattenTags): every object reference to one
// leaves the element that holds it (ISO 32000-1, 14.7.4.3), its /StructParent
// key leaves the /ParentTree (14.7.4.4), and an element left holding nothing
// leaves its parent and the parent tree. It does nothing on a document with no
// structure tree, and nothing when the parent tree cannot be read whole.
//
// Objects are read from alloc first, then doc, and every edit is queued in
// alloc.
func UntagRemovedAnnotations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogRef ast.PdfIndirectRef, annots []RemovedAnnotation) error {
	if len(annots) == 0 {
		return nil
	}
	catalogDict, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return fmt.Errorf("untagging removed annotations: %w", err)
	}
	tags := newFlattenTags(ctx, alloc, doc, catalogRef, catalogDict)
	for _, annot := range annots {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("untagging removed annotations: %w", err)
		}
		tags.drop(ctx, alloc, doc, annot.Obj, annot.Dict)
	}
	if err := tags.commit(ctx, alloc, doc); err != nil {
		return fmt.Errorf("untagging removed annotations: %w", err)
	}
	return nil
}
