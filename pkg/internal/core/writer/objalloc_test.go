package writer

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestObjAllocator_Alloc(t *testing.T) {
	a := NewObjAllocator(10)
	ref := a.Alloc(ast.PdfInt(42))
	if ref.ObjectNum != 11 {
		t.Errorf("first alloc: got obj %d, want 11", ref.ObjectNum)
	}
	ref2 := a.Alloc(ast.PdfInt(99))
	if ref2.ObjectNum != 12 {
		t.Errorf("second alloc: got obj %d, want 12", ref2.ObjectNum)
	}
	if a.NextObjNum() != 13 {
		t.Errorf("next obj num: got %d, want 13", a.NextObjNum())
	}
}

func TestObjAllocator_AllocAt(t *testing.T) {
	a := NewObjAllocator(10)
	a.AllocAt(5, ast.PdfInt(42))
	pending := a.Pending()
	if _, ok := pending[5]; !ok {
		t.Error("expected object 5 in pending")
	}
	// AllocAt with a number >= nextObjNum should bump it.
	a.AllocAt(20, ast.PdfInt(99))
	if a.NextObjNum() != 21 {
		t.Errorf("next obj num after AllocAt(20): got %d, want 21", a.NextObjNum())
	}
}

func TestObjAllocator_Pending(t *testing.T) {
	a := NewObjAllocator(0)
	a.Alloc(ast.PdfInt(1))
	a.Alloc(ast.PdfInt(2))
	a.AllocAt(5, ast.PdfInt(3))
	pending := a.Pending()
	if len(pending) != 3 {
		t.Errorf("pending count: got %d, want 3", len(pending))
	}
}
