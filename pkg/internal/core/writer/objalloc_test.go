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

func TestObjAllocator_SnapshotRestore(t *testing.T) {
	a := NewObjAllocator(10)
	a.AllocAt(5, ast.PdfInt(1))
	a.Alloc(ast.PdfInt(2)) // obj 11
	snap := a.Snapshot()

	a.Alloc(ast.PdfInt(3))       // obj 12
	a.AllocAt(5, ast.PdfInt(99)) // overwrite obj 5
	a.AllocAt(20, ast.PdfInt(4)) // obj 20

	if len(a.Pending()) != 4 {
		t.Fatalf("pre-restore pending: got %d, want 4", len(a.Pending()))
	}

	a.Restore(snap)

	pending := a.Pending()
	if len(pending) != 2 {
		t.Fatalf("post-restore pending: got %d, want 2", len(pending))
	}
	v, ok := pending[5].(ast.PdfInt)
	if !ok || v != 1 {
		t.Errorf("post-restore obj 5: got %v, want PdfInt(1)", pending[5])
	}
	if _, ok := pending[20]; ok {
		t.Error("post-restore obj 20 should be gone")
	}
	if a.NextObjNum() != 12 {
		t.Errorf("post-restore next obj num: got %d, want 12", a.NextObjNum())
	}

	// Mutating the snapshot-backed map after restore must not leak into
	// later allocations.
	a.AllocAt(99, ast.PdfInt(42))
	if len(snap.pending) != 2 {
		t.Errorf("snapshot mutated by later Alloc: got %d entries", len(snap.pending))
	}
}

// Callers that capture a pointer to the pending map before a restore must
// continue to see a coherent view afterwards. Writer.remediate captures the
// map reference to feed a queued mutation and needs rollback to update that
// same map in place.
func TestObjAllocator_Restore_MutatesInPlace(t *testing.T) {
	a := NewObjAllocator(10)
	a.AllocAt(5, ast.PdfInt(1))
	snap := a.Snapshot()

	captured := a.Pending()
	a.AllocAt(6, ast.PdfInt(2))
	a.AllocAt(7, ast.PdfInt(3))
	if len(captured) != 3 {
		t.Fatalf("pre-restore captured view: got %d, want 3", len(captured))
	}

	a.Restore(snap)

	if len(captured) != 1 {
		t.Fatalf("captured view after restore: got %d, want 1", len(captured))
	}
	if _, ok := captured[5]; !ok {
		t.Error("captured view lost obj 5 after restore")
	}
}
