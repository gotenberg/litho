package limits_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// TestBudget_RefusesPastTheLimitAndSticks pins that a charge past the limit
// is refused before it is accounted, names what was held, cancels the
// conversion's context with the refusal, and that every later charge fails.
func TestBudget_RefusesPastTheLimitAndSticks(t *testing.T) {
	t.Parallel()

	ctx, b, done := limits.WithBudget(context.Background(), limits.Limits{MaxConvertMemoryBytes: 100})
	defer done()
	if limits.BudgetFrom(ctx) != b {
		t.Fatal("the context does not carry the budget")
	}
	if err := b.Charge(60, "boxes"); err != nil {
		t.Fatalf("charge under the limit: %v", err)
	}
	b.Release(20)
	if err := b.Charge(60, "boxes"); err != nil {
		t.Fatalf("charge after a release: %v", err)
	}
	if b.Held() != 100 || b.Peak() != 100 {
		t.Errorf("held %d, peak %d, want 100 and 100", b.Held(), b.Peak())
	}
	err := b.Charge(1, "decoded pictures")
	if !errors.Is(err, limits.ErrMemoryBudget) || !strings.Contains(err.Error(), "decoded pictures") {
		t.Fatalf("charge past the limit = %v", err)
	}
	if b.Held() != 100 {
		t.Errorf("a refused charge was accounted: held %d", b.Held())
	}
	if !errors.Is(context.Cause(ctx), limits.ErrMemoryBudget) {
		t.Errorf("context cause = %v, want the refusal", context.Cause(ctx))
	}
	b.Release(100)
	if err := b.Charge(1, "cells"); !errors.Is(err, limits.ErrMemoryBudget) {
		t.Errorf("a charge after a refusal = %v, want the refusal", err)
	}
	if err := b.ChargeUnits(1<<40, 1<<40, "cells"); !errors.Is(err, limits.ErrMemoryBudget) {
		t.Errorf("an overflowing charge = %v", err)
	}
}

// TestBudget_Nil pins that code run without a budget charges nothing,
// never fails and reads the default limits.
func TestBudget_Nil(t *testing.T) {
	t.Parallel()

	var b *limits.Budget
	if limits.BudgetFrom(context.Background()) != nil {
		t.Fatal("a bare context carries a budget")
	}
	if b.Limits() != limits.Default() {
		t.Errorf("nil budget limits %+v", b.Limits())
	}
	if n, err := b.ChargePicture(1<<13, 1<<13, true); err != nil || n != 0 {
		t.Errorf("nil budget picture: %d, %v", n, err)
	}
	if err := b.ChargeUnits(1<<40, 1<<40, "x"); err != nil || b.Err() != nil || b.Held() != 0 || b.Peak() != 0 {
		t.Errorf("nil budget: %v", err)
	}
	b.Release(1)
}

// TestBudget_ConcurrentCharges pins that the account is exact under the
// goroutines of one conversion.
func TestBudget_ConcurrentCharges(t *testing.T) {
	t.Parallel()

	ctx, b, done := limits.WithBudget(context.Background(), limits.Limits{})
	if b.Limits().MaxConvertMemoryBytes != limits.DefaultMaxConvertMemoryBytes || limits.BudgetFrom(ctx) != b {
		t.Fatalf("default budget %+v", b.Limits())
	}
	defer done()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 1000 {
				if err := b.Charge(3, "glyph runs"); err != nil {
					t.Error(err)
					return
				}
				b.Release(1)
			}
		})
	}
	wg.Wait()
	if b.Held() != 8*1000*2 {
		t.Errorf("held %d, want %d", b.Held(), 8*1000*2)
	}
}

// TestBudget_ChargePicture pins the pixel cap of one picture and the cost
// of its decoded pixels.
func TestBudget_ChargePicture(t *testing.T) {
	t.Parallel()

	_, b, done := limits.WithBudget(context.Background(), limits.Limits{MaxImagePixels: 100, MaxConvertMemoryBytes: 1000})
	defer done()
	if n, err := b.ChargePicture(10, 10, false); err != nil || n != 400 {
		t.Errorf("10 by 10: %d, %v", n, err)
	}
	if _, err := b.ChargePicture(11, 10, false); !errors.Is(err, limits.ErrPictureTooLarge) || b.Err() != nil {
		t.Errorf("past the pixel cap: %v, budget %v", err, b.Err())
	}
	if _, err := b.ChargePicture(0, 10, false); !errors.Is(err, limits.ErrPictureTooLarge) {
		t.Errorf("empty: %v", err)
	}
	if _, err := b.ChargePicture(10, 10, true); !errors.Is(err, limits.ErrMemoryBudget) {
		t.Errorf("deep past the budget: %v", err)
	}
}
