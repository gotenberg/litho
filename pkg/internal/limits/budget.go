package limits

import (
	"context"
	"fmt"
	"image/color"
	"sync"
	"sync/atomic"
)

// Budget is the memory one conversion may hold, as litho accounts it: each
// site that takes memory a document asks for charges what it takes before
// it takes it, and the site that gives it back releases it. A charge past
// the limit is refused with an error wrapping [ErrMemoryBudget] that names
// what was being held, and the refusal sticks: every later charge fails
// too, and the context [WithBudget] returned is cancelled with the same
// error, so every checkpoint of the conversion stops at its next
// ctx.Err().
//
// The account is an estimate, not a measure of the heap: each site charges
// a cost per unit it documents (a byte of source, a decoded pixel, a box
// and its computed style), and the garbage collector holds memory a while
// after it is released. A process converting one document peaks at about
// twice what the account holds at its peak.
//
// A Budget is safe for the goroutines of one conversion. The nil *Budget
// is unlimited: its charges always succeed, so code a conversion did not
// give a budget runs as before.
type Budget struct {
	limits Limits
	limit  int64
	held   atomic.Int64
	peak   atomic.Int64

	mu     sync.Mutex
	err    error
	cancel context.CancelCauseFunc
}

// budgetKey addresses the Budget [WithBudget] installs on a context.
type budgetKey struct{}

// WithBudget returns a context that carries a new Budget of
// l.MaxConvertMemoryBytes bytes (its default when zero) and the budgets l
// sets, which [Budget.Limits] returns to the code the conversion runs; the
// Budget; and the function that releases the context's resources, which
// the caller defers. The context is cancelled, with the refusal as its
// cause, when a charge passes the limit.
func WithBudget(ctx context.Context, l Limits) (context.Context, *Budget, func()) {
	l = l.WithDefaults()
	ctx, cancel := context.WithCancelCause(ctx)
	b := &Budget{limits: l, limit: l.MaxConvertMemoryBytes, cancel: cancel}
	ctx = context.WithValue(ctx, budgetKey{}, b)
	return ctx, b, func() { cancel(nil) }
}

// BudgetFrom returns the Budget ctx carries, nil when it carries none.
func BudgetFrom(ctx context.Context) *Budget {
	b, _ := ctx.Value(budgetKey{}).(*Budget) //nolint:errcheck // a type assertion, nil when absent
	return b
}

// Limits returns the budgets of the conversion b accounts, every field
// filled in, or the defaults for a nil b.
func (b *Budget) Limits() Limits {
	if b == nil {
		return Default()
	}
	return b.limits
}

// CheckPicture checks a picture of w by h pixels against
// [Limits.MaxImagePixels], failing with an error wrapping
// [ErrPictureTooLarge] past it or for a side that is not positive. A
// picture written as it is (a JPEG) is checked and costs nothing more; a
// decoded one is charged by [Budget.ChargePicture].
func (b *Budget) CheckPicture(w, h int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("%w: a %d by %d picture", ErrPictureTooLarge, w, h)
	}
	if capPx := b.Limits().MaxImagePixels; int64(w)*int64(h) > capPx {
		return fmt.Errorf("%w: a %d by %d picture, past %d pixels", ErrPictureTooLarge, w, h, capPx)
	}
	return nil
}

// ChargePicture checks a picture of w by h pixels of colour model m, the
// model its header names, against [Limits.MaxImagePixels], failing with an
// error wrapping [ErrPictureTooLarge] past it or for a side that is not
// positive, and charges its decoded pixels ([PixelCost] each) before it is
// decoded. It returns what it charged, for the caller to release once the
// decoded picture is dropped.
func (b *Budget) ChargePicture(w, h int, m color.Model) (int64, error) {
	if err := b.CheckPicture(w, h); err != nil {
		return 0, err
	}
	px := int64(w) * int64(h)
	cost := PixelCost(m)
	if err := b.ChargeUnits(px, cost, "decoded pictures"); err != nil {
		return 0, err
	}
	if b == nil {
		return 0, nil
	}
	return px * cost, nil
}

// Charge accounts n more bytes held for what, a short name of the memory
// ("decoded pictures", "boxes"), and fails with an error wrapping
// [ErrMemoryBudget] when the account would pass the limit or an earlier
// charge already failed. A refused charge is not accounted. A charge of
// zero or less bytes accounts nothing and fails only after a refusal.
func (b *Budget) Charge(n int64, what string) error {
	if b == nil {
		return nil
	}
	if err := b.Err(); err != nil {
		return err
	}
	if n <= 0 {
		return nil
	}
	held := b.held.Add(n)
	if held > b.limit || held < 0 {
		b.held.Add(-n)
		return b.refuse(fmt.Errorf("%w: holding %d more bytes of %s would take the conversion past its memory limit of %d bytes", ErrMemoryBudget, n, what, b.limit))
	}
	for {
		p := b.peak.Load()
		if held <= p || b.peak.CompareAndSwap(p, held) {
			return nil
		}
	}
}

// ChargeUnits charges count units of cost bytes each, what naming them,
// saturating the product instead of overflowing.
func (b *Budget) ChargeUnits(count, cost int64, what string) error {
	if b == nil || count <= 0 || cost <= 0 {
		return b.Charge(0, what)
	}
	if count > b.limit/cost {
		return b.Charge(b.limit+1, what)
	}
	return b.Charge(count*cost, what)
}

// Take is [Budget.ChargeUnits] for a site that returns no error: it
// reports whether the charge was accounted. A refusal sticks and cancels
// the conversion's context, so the conversion stops at its next
// checkpoint and reports the refusal.
func (b *Budget) Take(count, cost int64, what string) bool {
	return b.ChargeUnits(count, cost, what) == nil
}

// Release gives back n bytes an earlier charge accounted.
func (b *Budget) Release(n int64) {
	if b == nil || n <= 0 {
		return
	}
	b.held.Add(-n)
}

// Err returns the refusal that stopped the budget, nil while charges
// succeed.
func (b *Budget) Err() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

// Held returns the bytes the account holds now.
func (b *Budget) Held() int64 {
	if b == nil {
		return 0
	}
	return b.held.Load()
}

// Peak returns the most bytes the account has held at once.
func (b *Budget) Peak() int64 {
	if b == nil {
		return 0
	}
	return b.peak.Load()
}

// refuse records err as the budget's refusal, unless one is recorded
// already, cancels the conversion's context with it and returns the
// recorded refusal.
func (b *Budget) refuse(err error) error {
	b.mu.Lock()
	if b.err == nil {
		b.err = err
	}
	err = b.err
	b.mu.Unlock()
	b.cancel(err)
	return err
}
