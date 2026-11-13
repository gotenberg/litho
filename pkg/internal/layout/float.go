package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// floatRect is the margin box of a placed float.
type floatRect struct {
	side           css.Float
	x0, y0, x1, y1 float64
	// wrap, when not css.FrameWrapThrough, makes the rectangle the
	// exclusion of a wrapped page-anchored box (framewrap.go): it narrows
	// the lines beside it by its mode, and floats and clearance ignore it.
	wrap css.FrameWrap
	// shape, when not nil, is the contour a float of
	// css.Office.WrapPolygon wraps along within the rectangle.
	shape *wordShape
	// word marks the exclusion of a float of css.Office.WordAnchor;
	// border is its border box, left, top, right and bottom.
	word   bool
	border [4]float64
	// initial marks the margin box of an initial letter
	// (initialletter.go).
	initial bool
}

// wordBandTrim is how much of the bottom of a line's band an exclusion of
// css.Office.WordAnchor may overlap without wrapping the line: Word
// measures a line against a drawing over its height less about three
// quarters of a point.
//
// # Corpus evidence
//
//   - litho/docx images-positioning: a line of 14.5 pt whose bottom
//     overlaps a picture by 0.2 pt runs beside it unwrapped.
//   - litho/docx images-wrap: beside the contours of wrapTight and
//     wrapThrough, the pieces of lines over a contour's upper half end
//     where the contour lies 2.5 to 3.1 pt below the line, its distB and
//     distT being 3.6 pt; over its lower half, 3.5 to 3.9 pt above.
const wordBandTrim = 0.75

// wordBand returns the band [y, y+h) a line of height h at y is measured
// over against the exclusion f.
func (f floatRect) wordBand(y, h float64) (float64, float64) {
	if f.word && h > wordBandTrim {
		return y, h - wordBandTrim
	}
	return y, h
}

// extentOver returns the left and right edges of the exclusion f over the
// band [y, y+h): its rectangle's, or its contour's part over the band;
// false when its contour leaves the band free.
func (f floatRect) extentOver(y, h float64) (float64, float64, bool) {
	if f.shape == nil {
		return f.x0, f.x1, true
	}
	return f.shape.extent(y, h)
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

// bandFrom returns the left and right edges of the space beside the
// floats in the band [y, y+h), within left and right, that ends past
// from: a piece of a band the floats of css.FrameWrapBoth cut.
func (c *bfc) bandFrom(y, h, left, right, from float64) (float64, float64) {
	if c.cols != nil && c.mcol != nil && len(c.cols.frags) > 0 {
		f := c.cols.frags[min(c.cols.of(y+eps), len(c.cols.frags)-1)]
		right = max(right-c.mcol.shrink(f.col), left)
	}
	return c.floatBandFrom(y, h, left, right, from)
}

// hasBoth reports whether a float of css.FrameWrapBoth is among c's.
func (c *bfc) hasBoth() bool {
	for _, f := range c.floats {
		if f.wrap == css.FrameWrapBoth {
			return true
		}
	}
	return false
}

// floatBand returns the left and right edges of the space beside the
// floats alone in the band [y, y+h), within left and right.
func (c *bfc) floatBand(y, h, left, right float64) (float64, float64) {
	return c.floatBandFrom(y, h, left, right, math.Inf(-1))
}

// floatBandFrom returns the left and right edges of the space beside the
// floats in the band [y, y+h), within left and right, that ends past
// from: the floats of css.FrameWrapBoth cut the band into pieces, the
// first ending past from taken. Without a piece left, both edges are at
// the right edge of the band.
func (c *bfc) floatBandFrom(y, h, left, right, from float64) (float64, float64) {
	l, r := left, right
	both := false
	for _, f := range c.floats {
		if by, bh := f.wordBand(y, h); !f.overlaps(by, bh) {
			continue
		}
		if f.wrap == css.FrameWrapBoth {
			both = true
			continue
		}
		if f.wrap != css.FrameWrapThrough {
			x0, x1, ok := f.extentOver(f.wordBand(y, h))
			if !ok {
				continue
			}
			g := f
			g.x0, g.x1 = x0, x1
			l, r = g.wrapBand(l, r)
			continue
		}
		if f.side == css.FloatLeft {
			l = max(l, f.x1)
		} else {
			r = min(r, f.x0)
		}
	}
	r = max(r, l)
	if both {
		if pl, pr, ok := c.split(y, h, l, r, from); ok {
			return pl, pr
		}
		return r, r
	}
	return l, r
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

// hasOfficeExclusion reports whether c holds the exclusion of a float
// of css.Office.WordAnchor or of a wrapped page-anchored box. A paragraph
// such an exclusion pushes down starts where its first line lands, not
// where CSS would put its block: ECMA-376 Part 1, 17.3.1 and 20.4 say
// nothing of where a wrapped paragraph's box begins, and Word places the
// paragraph, its heading bookmark's destination included, at the line.
//
// # Corpus evidence
//
//   - litho/docx images-wrap: in Word's reference, the outline entry of
//     the heading "wrapSquare largest", beside a wrapSquare drawing,
//     points at x 172, y 400 (PDF space), the top of the heading's first
//     line beside the drawing (text at x 174.6, top 397.4), not the
//     left margin at x 72.
func (c *bfc) hasOfficeExclusion() bool {
	for _, f := range c.floats {
		if f.word || f.wrap != css.FrameWrapThrough {
			return true
		}
	}
	return false
}

// place returns where a float of margin box w by h goes in a containing
// block whose content spans left to right, no higher than minY, following
// the rules of CSS 2.1, 9.5.1: no higher than an earlier float, as high as
// possible, then as far left (or right) as possible, moving down past the
// floats until it fits or none is left beside it.
func (c *bfc) place(side css.Float, w, h, minY, left, right float64) (float64, float64) {
	y := minY
	for _, f := range c.floats {
		if f.wrap == css.FrameWrapThrough && !f.initial {
			y = max(y, f.y0)
		}
	}
	return c.placeBeside(side, w, h, y, left, right)
}

// placeBeside returns where a box of margin box w by h goes beside the
// floats, no higher than y, as [bfc.place] places a float but for the rule
// that keeps a float no higher than an earlier one: an initial letter is
// no float (CSS Inline 3, 4.9.3).
func (c *bfc) placeBeside(side css.Float, w, h, y, left, right float64) (float64, float64) {
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
		if f.wrap != css.FrameWrapThrough {
			continue
		}
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
