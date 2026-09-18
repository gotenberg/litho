package document

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// fieldParentCyclePDF builds a form whose field and widget name each other as
// /Parent, so the chain 5 -> 4 -> 5 never reaches a root. Object 6 is a field
// that states no /Parent at all.
func fieldParentCyclePDF() []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>\nendobj\n" +
		"4 0 obj\n<< /FT /Tx /T (name) /Parent 5 0 R >>\nendobj\n" +
		"5 0 obj\n<< /Type /Annot /Subtype /Widget /Parent 4 0 R >>\nendobj\n" +
		"6 0 obj\n<< /FT /Tx /T (rootless) >>\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")
}

func fieldParentCycleDoc(t *testing.T) (context.Context, *Document) {
	t.Helper()
	data := fieldParentCyclePDF()
	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return ctx, doc
}

// TestResolveFieldParent_RefusesARepeat pins the protection a depth bound
// cannot give. The third hop revisits object 4, and a walk that followed it
// would circle until its bound ran out and then report whichever node it
// halted on as the root.
func TestResolveFieldParent_RefusesARepeat(t *testing.T) {
	ctx, doc := fieldParentCycleDoc(t)

	widget, err := ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5})
	if err != nil || widget == nil {
		t.Fatalf("resolving the widget: %v", err)
	}

	seen := make(map[int]bool)

	_, field, ok := ResolveFieldParent(ctx, doc, widget, seen)
	if !ok {
		t.Fatal("first hop refused a parent that resolves")
	}
	if !seen[4] {
		t.Error("the first hop did not record object 4 in seen")
	}

	_, back, ok := ResolveFieldParent(ctx, doc, field, seen)
	if !ok {
		t.Fatal("second hop refused a parent that resolves")
	}
	if !seen[5] {
		t.Error("the second hop did not record object 5 in seen")
	}

	if _, _, ok := ResolveFieldParent(ctx, doc, back, seen); ok {
		t.Error("the third hop revisited object 4 instead of ending the walk")
	}
}

// TestResolveFieldParent_StopsWithoutAParent pins that a field stating no
// /Parent ends the walk rather than reporting a hop it did not make, and that
// a refused hop records nothing.
func TestResolveFieldParent_StopsWithoutAParent(t *testing.T) {
	ctx, doc := fieldParentCycleDoc(t)

	rootless, err := ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 6})
	if err != nil || rootless == nil {
		t.Fatalf("resolving the rootless field: %v", err)
	}

	seen := make(map[int]bool)
	obj, dict, ok := ResolveFieldParent(ctx, doc, rootless, seen)
	if ok {
		t.Error("a field with no /Parent reported a parent")
	}
	if obj != nil || dict != nil {
		t.Errorf("refused hop returned obj=%v dict=%v, want both nil", obj, dict)
	}
	if len(seen) != 0 {
		t.Errorf("a refused hop recorded %d entries in seen, want 0", len(seen))
	}
}
