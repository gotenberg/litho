package writer

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestObjAllocator_Writes pins that Writes counts every object queued, a
// replacement of a pending one included, skips a refused number, and goes
// back with Restore.
func TestObjAllocator_Writes(t *testing.T) {
	t.Parallel()

	a := NewObjAllocator(5)
	a.AllocAt(3, ast.PdfNull{})
	snap := a.Snapshot()
	if got := a.Writes(); got != 1 {
		t.Fatalf("Writes after one AllocAt = %d, want 1", got)
	}

	a.AllocAt(3, ast.PdfInt(1))
	a.Alloc(ast.PdfNull{})
	a.AllocAt(0, ast.PdfNull{})
	if got := a.Writes(); got != 3 {
		t.Errorf("Writes after a replacement, an Alloc and a refused AllocAt = %d, want 3", got)
	}

	a.Restore(snap)
	if got := a.Writes(); got != 1 {
		t.Errorf("Writes after Restore = %d, want the snapshot's 1", got)
	}
}
