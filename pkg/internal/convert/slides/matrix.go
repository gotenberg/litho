package slides

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// then returns the transform that applies a, then b.
func then(a, b compose.Matrix) compose.Matrix {
	return compose.Matrix{
		A: b.A*a.A + b.C*a.B,
		B: b.B*a.A + b.D*a.B,
		C: b.A*a.C + b.C*a.D,
		D: b.B*a.C + b.D*a.D,
		E: b.A*a.E + b.C*a.F + b.E,
		F: b.B*a.E + b.D*a.F + b.F,
	}
}

// apply maps the point (x, y) through m.
func apply(m compose.Matrix, x, y float64) (float64, float64) {
	return m.A*x + m.C*y + m.E, m.B*x + m.D*y + m.F
}

// transformRect returns the bounding box of r mapped through m.
func transformRect(m compose.Matrix, r compose.Rect) compose.Rect {
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	for _, p := range [4][2]float64{{r.X, r.Y}, {r.X + r.Width, r.Y}, {r.X, r.Y + r.Height}, {r.X + r.Width, r.Y + r.Height}} {
		x, y := apply(m, p[0], p[1])
		x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
	}
	return compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// translate returns the translation by (x, y).
func translate(x, y float64) compose.Matrix {
	return compose.Matrix{A: 1, D: 1, E: x, F: y}
}

// aboutCentre returns the transform that mirrors, then turns clockwise by
// deg degrees, about the centre of r (ECMA-376 Part 1, 20.1.7.6 xfrm: the
// flips apply before the rotation).
func aboutCentre(r compose.Rect, deg float64, flipH, flipV bool) compose.Matrix {
	deg = math.Mod(deg, 360)
	if math.IsNaN(deg) {
		deg = 0
	}
	if deg == 0 && !flipH && !flipV {
		return compose.Identity
	}
	cx, cy := r.X+r.Width/2, r.Y+r.Height/2
	f := compose.Identity
	if flipH {
		f.A = -1
	}
	if flipV {
		f.D = -1
	}
	// In page coordinates, y down, a clockwise turn maps the x axis onto
	// (cos, sin).
	s, c := math.Sincos(deg * math.Pi / 180)
	rot := compose.Matrix{A: c, B: s, C: -s, D: c}
	return then(then(then(translate(-cx, -cy), f), rot), translate(cx, cy))
}

// rectOps returns the path of the rectangle r.
func rectOps(r compose.Rect) []compose.PathOp {
	pt := func(x, y float64) [3]compose.Point { return [3]compose.Point{{X: x, Y: y}} }
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: pt(r.X, r.Y)},
		{Kind: compose.LineTo, P: pt(r.X+r.Width, r.Y)},
		{Kind: compose.LineTo, P: pt(r.X+r.Width, r.Y+r.Height)},
		{Kind: compose.LineTo, P: pt(r.X, r.Y+r.Height)},
		{Kind: compose.ClosePath},
	}
}

// lineOps returns the path of the segment from (x1, y1) to (x2, y2).
func lineOps(x1, y1, x2, y2 float64) []compose.PathOp {
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: x1, Y: y1}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x2, Y: y2}}},
	}
}

// group wraps frags in a group under m, or returns them as they are under
// the identity.
func group(m compose.Matrix, frags []compose.Fragment) []compose.Fragment {
	if len(frags) == 0 {
		return nil
	}
	if m == compose.Identity {
		return frags
	}
	return []compose.Fragment{&compose.Group{Transform: m, Opacity: 1, Content: frags}}
}

// finite reports whether every side of r is a finite number, its size not
// negative.
func finite(r compose.Rect) bool {
	for _, v := range [4]float64{r.X, r.Y, r.Width, r.Height} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > maxCoordinate {
			return false
		}
	}
	return r.Width >= 0 && r.Height >= 0
}

// maxCoordinate bounds the coordinates of a box drawn, in points.
// Anti-DoS guard, not spec-mandated: ten times the largest PDF page side
// holds every shape a slide shows, and keeps the arithmetic finite.
const maxCoordinate = 10 * 14400
