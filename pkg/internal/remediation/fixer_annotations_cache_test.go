package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestFixAnnotations_LeavesTheCachedAnnotsAlone pins that removing a forbidden
// annotation queues the page's rebuilt /Annots and leaves the array the
// document caches as the file has it. The fixer used to null the removed slot
// in the cached array itself, so every later read of the document, a
// validation or a full rewrite copying the page, saw the removal whether or not
// the allocator kept it.
func TestFixAnnotations_LeavesTheCachedAnnotsAlone(t *testing.T) {
	pdf := annotsPDF("[4 0 R 5 0 R]",
		"<< /Type /Annot /Subtype /Movie /Rect [0 0 10 10] >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] >>",
	)
	doc, alloc := newTestDoc(t, pdf)
	ctx := context.Background()
	page := ast.PdfIndirectRef{ObjectNum: 3}
	if _, err := doc.Resolve(ctx, page); err != nil {
		t.Fatalf("resolving the page: %v", err)
	}

	if _, err := fixAnnotations(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixAnnotations: %v", err)
	}
	if queued := pendingDict(t, alloc, 3); len(queued["Annots"].(ast.PdfArray)) != 1 {
		t.Fatalf("queued /Annots = %v, want the Link alone", queued["Annots"])
	}

	cached, err := document.ResolveDict(ctx, doc, page)
	if err != nil {
		t.Fatalf("resolving the page again: %v", err)
	}
	annots, _ := cached["Annots"].(ast.PdfArray)
	want := ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 4}, ast.PdfIndirectRef{ObjectNum: 5}}
	if len(annots) != len(want) {
		t.Fatalf("cached /Annots = %v, want %v", annots, want)
	}
	for i := range want {
		if annots[i] != want[i] {
			t.Errorf("cached /Annots[%d] = %v, want %v", i, annots[i], want[i])
		}
	}
}
