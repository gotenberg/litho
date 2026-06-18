package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixAnnotations_NullsForbiddenObject verifies that removing a forbidden
// annotation also nulls its indirect object, not just its /Annots slot. A
// Rendition /Screen annotation carries its MediaClip embedded-file filespec
// inline, so an object left live still defines a reachable embedded file in
// the document body. Dropping the /Annots reference alone does not detach it,
// because conformance is assessed over all indirect objects, not only those
// reachable from a page. The embedded-file filespec is forbidden by
// ISO 19005-3 §6.8 / 19005-4 §6.9.
func TestFixAnnotations_NullsForbiddenObject(t *testing.T) {
	pdf := annotsPDF("[4 0 R 5 0 R]",
		"<< /Type /Annot /Subtype /Screen /Rect [0 0 10 10] /A << /S /Rendition /R << /Type /Rendition /C << /Type /MediaClip /D << /Type /Filespec /F (clip) /EF << /F 9 0 R >> >> >> >> >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixAnnotations(context.Background(), alloc, doc, Options{}, nil, "PDF/A-3a"); err != nil {
		t.Fatalf("fixAnnotations: %v", err)
	}

	// The Screen annotation object (4) must be nulled.
	v, ok := alloc.Pending()[4]
	if !ok {
		t.Fatal("Screen object 4 not registered; expected it nulled")
	}
	if _, isNull := v.(ast.PdfNull); !isNull {
		t.Errorf("Screen object 4 = %T, want ast.PdfNull", v)
	}

	// The Link object (5) must not be nulled.
	if w, queued := alloc.Pending()[5]; queued {
		if _, isNull := w.(ast.PdfNull); isNull {
			t.Error("Link object 5 must not be nulled")
		}
	}

	// The page /Annots must keep only the Link.
	page := pendingDict(t, alloc, 3)
	annots, _ := page["Annots"].(ast.PdfArray)
	if len(annots) != 1 {
		t.Fatalf("expected 1 surviving annotation, got %d", len(annots))
	}
	if ref, ok := annots[0].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 5 {
		t.Errorf("surviving annotation = %v, want Link (obj 5)", annots[0])
	}
}
