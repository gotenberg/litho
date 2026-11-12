package html

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// Content a clip hides wholly is left out of the page rather than written
// under its clip: an overflow: hidden box's clipped lines (CSS Overflow 3,
// 3.1), the content past the page area of a fitted page, the part of a
// placed page outside its source page. Written, it would cost bytes and
// reach text extraction, which ignores clipping. A fragment that crosses
// its clip, or that the bounds below cannot place, stays.

// cullDepth caps the group nesting cullPage walks. Anti-DoS guard, not
// spec-mandated: the composition model refuses pages nested deeper than
// 24 groups; deeper content is kept as it is.
const cullDepth = 32

// area is an axis-aligned rectangle in page coordinates, by its edges.
type area struct {
	x0, y0, x1, y1 float64
}

// hasExtent reports whether a is wider and taller than nothing. An area
// with a NaN edge has no extent.
func (a area) hasExtent() bool { return a.x1 > a.x0 && a.y1 > a.y0 }

// meets reports whether a and b overlap or touch.
func (a area) meets(b area) bool {
	return a.x0 <= b.x1 && b.x0 <= a.x1 && a.y0 <= b.y1 && b.y0 <= a.y1
}

// cut returns the intersection of a and b.
func (a area) cut(b area) area {
	return area{max(a.x0, b.x0), max(a.y0, b.y0), min(a.x1, b.x1), min(a.y1, b.y1)}
}

// cullPage drops from pg the fragments wholly outside the clips they are
// painted under, the page's own bounds included, and the groups culling
// left empty. It reports whether it dropped anything.
func cullPage(pg *compose.Page) bool {
	page := area{0, 0, pg.Width, pg.Height}
	out, dropped := cullFrags(pg.Content, compose.Identity, page, 0)
	pg.Content = out
	return dropped
}

// cullFrags returns frags without the fragments wholly outside clip, a
// page-space area, m mapping their coordinates to the page's.
func cullFrags(frags []compose.Fragment, m compose.Matrix, clip area, depth int) ([]compose.Fragment, bool) {
	dropped := false
	out := frags[:0:0]
	for _, f := range frags {
		keep := f
		switch v := f.(type) {
		case *compose.Group:
			g, d := cullGroup(v, m, clip, depth)
			dropped = dropped || d
			if g == nil {
				dropped = true
				continue
			}
			keep = g
		default:
			if b, ok := fragmentArea(f, m); ok && !b.meets(clip) {
				dropped = true
				continue
			}
		}
		out = append(out, keep)
	}
	if !dropped {
		return frags, false
	}
	return out, true
}

// cullGroup returns g culled against clip, a copy when its content
// changed, nil when nothing of it is left.
func cullGroup(g *compose.Group, m compose.Matrix, clip area, depth int) (*compose.Group, bool) {
	if depth >= cullDepth {
		return g, false
	}
	t := g.Transform
	if t == (compose.Matrix{}) {
		t = compose.Identity
	}
	gm := then(t, m)
	inner := clip
	if g.Clip != nil {
		if b, ok := opsArea(g.Clip.Ops, gm, 0); ok {
			inner = clip.cut(b)
			if !inner.hasExtent() && len(g.Content) > 0 {
				return nil, true
			}
		}
	}
	content, dropped := cullFrags(g.Content, gm, inner, depth+1)
	if !dropped {
		return g, false
	}
	if len(content) == 0 {
		return nil, true
	}
	c := *g
	c.Content = content
	return &c, true
}

// then returns the transform that applies a, then b.
func then(a, b compose.Matrix) compose.Matrix {
	return compose.Matrix{
		A: a.A*b.A + a.B*b.C, B: a.A*b.B + a.B*b.D,
		C: a.C*b.A + a.D*b.C, D: a.C*b.B + a.D*b.D,
		E: a.E*b.A + a.F*b.C + b.E, F: a.E*b.B + a.F*b.D + b.F,
	}
}

// fragmentArea returns a page-space area that holds everything the leaf
// fragment f paints, m mapping its coordinates to the page's, and false
// when it cannot bound it.
func fragmentArea(f compose.Fragment, m compose.Matrix) (area, bool) {
	switch v := f.(type) {
	case *compose.Image:
		r := v.Rect
		return pointsArea([]compose.Point{{X: r.X, Y: r.Y}, {X: r.X + r.Width, Y: r.Y}, {X: r.X, Y: r.Y + r.Height}, {X: r.X + r.Width, Y: r.Y + r.Height}}, m, 0)
	case *compose.Path:
		pad := 0.0
		if s := v.Stroke; s != nil {
			// A miter reaches at most half the miter limit times the line
			// width past the path (ISO 32000-1, 8.4.3.5); a square cap
			// half the width times the square root of two.
			miter := s.Miter
			if miter == 0 {
				miter = 10
			}
			pad = s.Width / 2 * max(miter, math.Sqrt2)
		}
		return opsArea(v.Ops, m, pad)
	case *compose.Text:
		return textArea(v, m)
	}
	return area{}, false
}

// textArea bounds a glyph run: from each glyph origin, a box reaching two
// ems before and after it and two ems above and below the baseline, which
// holds any glyph a text face draws, swashes and stacked marks included,
// its slant and stroke too.
func textArea(t *compose.Text, m compose.Matrix) (area, bool) {
	if len(t.Glyphs) == 0 {
		return area{}, false
	}
	x0, x1 := math.Inf(1), math.Inf(-1)
	y0, y1 := math.Inf(1), math.Inf(-1)
	for _, g := range t.Glyphs {
		x0, x1 = min(x0, g.X), max(x1, g.X)
		y0, y1 = min(y0, g.Y), max(y1, g.Y)
	}
	em := 2*t.Size + t.Stroke
	ox, oy := t.Origin.X, t.Origin.Y
	return pointsArea([]compose.Point{
		{X: ox + x0 - em, Y: oy + y0 - em},
		{X: ox + x1 + em, Y: oy + y0 - em},
		{X: ox + x0 - em, Y: oy + y1 + em},
		{X: ox + x1 + em, Y: oy + y1 + em},
	}, m, 0)
}

// opsArea bounds a path by its points, curve control points included,
// which hold the curves (a Bézier lies in the hull of its control points),
// grown by pad in its own coordinates.
func opsArea(ops []compose.PathOp, m compose.Matrix, pad float64) (area, bool) {
	var pts []compose.Point
	for _, op := range ops {
		switch op.Kind {
		case compose.MoveTo, compose.LineTo:
			pts = append(pts, op.P[0])
		case compose.CurveTo:
			pts = append(pts, op.P[:]...)
		case compose.ClosePath:
		}
	}
	return pointsArea(pts, m, pad)
}

// pointsArea returns the page-space bounds of pts grown by pad, each
// corner of the grown box mapped through m, false for no point or a value
// that is not finite.
func pointsArea(pts []compose.Point, m compose.Matrix, pad float64) (area, bool) {
	if len(pts) == 0 {
		return area{}, false
	}
	x0, x1 := math.Inf(1), math.Inf(-1)
	y0, y1 := math.Inf(1), math.Inf(-1)
	for _, p := range pts {
		x0, x1 = min(x0, p.X), max(x1, p.X)
		y0, y1 = min(y0, p.Y), max(y1, p.Y)
	}
	x0, y0, x1, y1 = x0-pad, y0-pad, x1+pad, y1+pad
	b := area{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, c := range [4][2]float64{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}} {
		x := m.A*c[0] + m.C*c[1] + m.E
		y := m.B*c[0] + m.D*c[1] + m.F
		b.x0, b.x1 = min(b.x0, x), max(b.x1, x)
		b.y0, b.y1 = min(b.y0, y), max(b.y1, y)
	}
	for _, v := range [4]float64{b.x0, b.y0, b.x1, b.y1} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return area{}, false
		}
	}
	return b, true
}
