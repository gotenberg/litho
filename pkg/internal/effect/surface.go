package effect

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// Format is the pixel layout of a [Surface].
type Format uint8

const (
	// Coverage holds one byte a pixel: 0 uncovered, 255 fully covered. It
	// is the alpha of an effect drawn in one colour, a shadow or a glow.
	Coverage Format = 1
	// RGBA holds four bytes a pixel, red, green, blue and alpha,
	// premultiplied by alpha: no colour byte exceeds its alpha.
	RGBA Format = 4
)

// MaxPixels caps the pixels of one surface. Anti-DoS guard, not
// spec-mandated: 16,777,216 pixels (4096 by 4096, 64 MiB in RGBA) hold an
// A4 page at 300 pixels an inch with room to spare, more than [Plan]
// ever asks for, and bound what one effect costs on a hostile size.
const MaxPixels = 1 << 24

// Grid places a surface's pixels on the page: the top-left corner of
// pixel (0, 0) at (X, Y) in points, Scale pixels a point along both axes.
// The engine carries it and never reads it: every primitive works in
// pixels.
type Grid struct {
	X, Y  float64
	Scale float64
}

// Surface is an 8-bit image an effect is drawn into and filtered: W by H
// pixels of [Format] F, row after row, no padding between rows. Pixels
// outside the surface read as transparent black unless a primitive
// states otherwise.
//
// A Surface made by [New] charges its bytes to the conversion's memory
// budget; [Surface.Release] gives them back. A Surface is not safe for
// concurrent use; distinct surfaces are.
type Surface struct {
	F    Format
	W, H int
	Pix  []uint8
	Grid Grid

	budget  *limits.Budget
	charged int64
}

// New returns a transparent surface of w by h pixels of format f placed
// by g, its bytes charged to the budget ctx carries. It fails with an
// error wrapping [ErrTooLarge] for a side that is not positive or past
// [MaxPixels] pixels, with [ErrFormat] for an unknown format, and with the
// budget's refusal when the budget cannot hold it.
func New(ctx context.Context, f Format, w, h int, g Grid) (*Surface, error) {
	if f != Coverage && f != RGBA {
		return nil, fmt.Errorf("new surface of format %d: %w", f, ErrFormat)
	}
	if w <= 0 || h <= 0 || w > MaxPixels || h > MaxPixels || w > MaxPixels/h {
		return nil, fmt.Errorf("%w: %d by %d pixels", ErrTooLarge, w, h)
	}
	n := int64(w) * int64(h) * int64(f)
	b := limits.BudgetFrom(ctx)
	if err := b.Charge(n, "effect surfaces"); err != nil {
		return nil, err
	}
	return &Surface{F: f, W: w, H: h, Pix: make([]uint8, n), Grid: g, budget: b, charged: n}, nil
}

// Release gives the surface's bytes back to the budget [New] charged and
// drops its pixels. The surface is unusable after; a second Release does
// nothing.
func (s *Surface) Release() {
	s.budget.Release(s.charged)
	s.charged = 0
	s.Pix = nil
	s.W, s.H = 0, 0
}

// Clone returns a copy of s, charged to the budget ctx carries.
func (s *Surface) Clone(ctx context.Context) (*Surface, error) {
	c, err := New(ctx, s.F, s.W, s.H, s.Grid)
	if err != nil {
		return nil, err
	}
	copy(c.Pix, s.Pix)
	return c, nil
}

// stride returns the bytes of one row.
func (s *Surface) stride() int { return s.W * int(s.F) }

// same reports an error wrapping [ErrFormat] naming op unless a and b
// share format and size.
func same(op string, a, b *Surface) error {
	if a.F != b.F || a.W != b.W || a.H != b.H {
		return fmt.Errorf("%s: %w", op, ErrFormat)
	}
	return nil
}

// want reports an error wrapping [ErrFormat] naming op unless s has
// format f.
func want(op string, s *Surface, f Format) error {
	if s.F != f {
		return fmt.Errorf("%s: %w", op, ErrFormat)
	}
	return nil
}

// scratch is memory a primitive holds for the length of one call, charged
// to the budget and given back by release.
type scratch struct {
	budget *limits.Budget
	n      int64
}

// take charges n more bytes of scratch memory.
func (sc *scratch) take(ctx context.Context, n int64) error {
	if sc.budget == nil {
		sc.budget = limits.BudgetFrom(ctx)
	}
	if err := sc.budget.Charge(n, "effect scratch"); err != nil {
		return err
	}
	sc.n += n
	return nil
}

// release gives back every byte taken.
func (sc *scratch) release() {
	sc.budget.Release(sc.n)
	sc.n = 0
}
