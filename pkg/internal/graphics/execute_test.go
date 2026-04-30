package graphics

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func TestMachineExecute_RespectsContextCancel(t *testing.T) {
	// Build a list comfortably above the per-100-op poll interval so
	// the cancellation check fires before the loop completes.
	ops := make([]content.Operation, 1000)
	for i := range ops {
		// "q" / "Q" are no-op-ish state push/pop operators that don't
		// touch external state. Either is fine here.
		ops[i] = content.Operation{Operator: "q"}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := NewMachine([4]float64{0, 0, 612, 792})
	if err := m.Execute(ctx, ops); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
