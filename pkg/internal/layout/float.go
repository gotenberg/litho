package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// floatRect is the margin box of a placed float.
type floatRect struct {
	side           css.Float
	x0, y0, x1, y1 float64
}

// bfc is the state of one block formatting context: the floats placed in
// it (CSS 2.1, 9.4.1 and 9.5).
type bfc struct {
	floats []floatRect
	// cols and mcol, in the column flow of a container whose columns
	// differ in width, narrow the band of a column narrower than the flow
	// at its end edge.
	cols *colCtx
	mcol *mcolData
}

// overlaps reports whether f overlaps the band [y, y+h), a zero-height
// band being the line at y.
func (f floatRect) overlaps(y, h float64) bool {
	if h <= 1e-3 {
		return f.y0 <= y+eps && f.y1 > y+eps
	}
	return f.y0 < y+h-eps && f.y1 > y+eps
}

// band returns the left and right edges of the space beside the floats in
// the band [y, y+h), within the containing block edges left and right.
func (c *bfc) band(y, h, left, right float64) (float64, float64) {
	if c.cols != nil && c.mcol != nil && len(c.cols.frags) > 0 {
		f := c.cols.frags[min(c.cols.of(y+eps), len(c.cols.frags)-1)]
		right = max(right-c.mcol.shrink(f.col), left)
	}
	return c.floatBand(y, h, left, right)
}

// floatBand returns the left and right edges of the space beside the
// floats alone in the band [y, y+h), within left and right.
func (c *bfc) floatBand(y, h, left, right float64) (float64, float64) {
	l, r := left, right
	for _, f := range c.floats {
		if !f.overlaps(y, h) {
			continue
		}
		if f.side == css.FloatLeft {
			l = max(l, f.x1)
		} else {
			r = min(r, f.x0)
		}
	}
	return l, max(r, l)
}

// narrowed reports whether a float narrows the band [y, y+h) within left
// and right.
func (c *bfc) narrowed(y, h, left, right float64) bool {
	l, r := c.floatBand(y, h, left, right)
	return l > left+eps || r < right-eps
}

// below returns the lowest bottom of the floats overlapping the band
// [y, y+h) that lies below y, +Inf when none does: the next place content
// that does not fit beside them can try.
func (c *bfc) below(y, h float64) float64 {
	next := math.Inf(1)
	for _, f := range c.floats {
		if f.overlaps(y, h) && f.y1 > y+eps {
			next = min(next, f.y1)
		}
	}
	return next
}

// place returns where a float of margin box w by h goes in a containing
// block whose content spans left to right, no higher than minY, following
// the rules of CSS 2.1, 9.5.1: no higher than an earlier float, as high as
// possible, then as far left (or right) as possible, moving down past the
// floats until it fits or none is left beside it.
func (c *bfc) place(side css.Float, w, h, minY, left, right float64) (float64, float64) {
	y := minY
	for _, f := range c.floats {
		y = max(y, f.y0)
	}
	for range len(c.floats) + 1 {
		l, r := c.band(y, max(h, eps), left, right)
		x := l
		if side != css.FloatLeft {
			x = r - w
		}
		if w <= r-l+eps || (!c.narrowed(y, max(h, eps), left, right) && !c.collides(y, max(h, eps), x, x+w)) {
			return x, y
		}
		next := c.below(y, max(h, eps))
		if math.IsInf(next, 1) {
			break
		}
		y = next
	}
	if side == css.FloatLeft {
		return left, y
	}
	return right - w, y
}

// collides reports whether a float placed in the band [y, y+h) overlaps
// the horizontal span [x0, x1): a float of the formatting context outside
// the containing block still bars a wider float from overlapping it (CSS
// 2.1, 9.5.1, rules 2, 3 and 7).
func (c *bfc) collides(y, h, x0, x1 float64) bool {
	for _, f := range c.floats {
		if f.overlaps(y, h) && f.x0 < x1-eps && f.x1 > x0+eps {
			return true
		}
	}
	return false
}

// clearTo returns the position below the floats a clear value clears
// (CSS 2.1, 9.5.2), -Inf when it clears none.
func (c *bfc) clearTo(cl css.Clear) float64 {
	y := math.Inf(-1)
	for _, f := range c.floats {
		if cl == css.ClearBoth || (cl == css.ClearLeft && f.side == css.FloatLeft) || (cl == css.ClearRight && f.side == css.FloatRight) {
			y = max(y, f.y1)
		}
	}
	return y
}

// bottom returns the lowest float margin edge, -Inf without floats.
func (c *bfc) bottom() float64 {
	return c.clearTo(css.ClearBoth)
}
