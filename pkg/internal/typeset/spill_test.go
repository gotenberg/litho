package typeset

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/models"
)

// TestSpill_ChargesAStreamItsCapacity checks that a stream kept in memory
// costs the budget the buffer it holds, not the bytes it writes: a buffer
// grown past its length keeps its spare capacity alive.
func TestSpill_ChargesAStreamItsCapacity(t *testing.T) {
	t.Parallel()
	ctx, mem, cancel := limits.WithBudget(context.Background(), models.Limits{})
	defer cancel()
	w := newDocWriter(ctx, Options{Spool: -1}, Version17, false)
	w.alloc.Alloc(ast.NewStream(ast.PdfDict{}, make([]byte, 10, 4096)))
	if err := w.spill(ctx); err != nil {
		t.Fatal(err)
	}
	if got := mem.Held(); got != 4096 {
		t.Errorf("held %d bytes, want the 4096 of the stream's buffer", got)
	}
}
