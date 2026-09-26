package writer

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestObjAllocator_Reserve pins that Reserve moves the next object number past
// the number it is given and never back.
func TestObjAllocator_Reserve(t *testing.T) {
	t.Parallel()

	a := NewObjAllocator(5)
	a.Reserve(7)
	if ref := a.Alloc(ast.PdfNull{}); ref.ObjectNum != 8 {
		t.Errorf("Alloc after Reserve(7) = %d, want 8", ref.ObjectNum)
	}
	a.Reserve(3)
	if ref := a.Alloc(ast.PdfNull{}); ref.ObjectNum != 9 {
		t.Errorf("Alloc after Reserve(3) = %d, want 9: Reserve must not lower the next number", ref.ObjectNum)
	}
}
