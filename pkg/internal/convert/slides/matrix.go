package slides

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// then returns the transform that applies a, then b. Each product is
// rounded on its own (the Go specification lets a compiler fuse x*y + z
// into one rounding, which arm64 does and amd64 does not), so that every
// architecture computes the same matrix.
func then(a, b typeset.Matrix) typeset.Matrix {
	return typeset.Matrix{
		A: float64(b.A*a.A) + float64(b.C*a.B),
		B: float64(b.B*a.A) + float64(b.D*a.B),
		C: float64(b.A*a.C) + float64(b.C*a.D),
		D: float64(b.B*a.C) + float64(b.D*a.D),
		E: float64(b.A*a.E) + float64(b.C*a.F) + b.E,
		F: float64(b.B*a.E) + float64(b.D*a.F) + b.F,
	}
}

// apply maps the point (x, y) through m, each product rounded on its own
// as in [then].
func apply(m typeset.Matrix, x, y float64) (float64, float64) {
	return float64(m.A*x) + float64(m.C*y) + m.E, float64(m.B*x) + float64(m.D*y) + m.F
}

// transformRect returns the bounding box of r mapped through m.
func transformRect(m typeset.Matrix, r typeset.Rect) typeset.Rect {
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	for _, p := range [4][2]float64{{r.X, r.Y}, {r.X + r.Width, r.Y}, {r.X, r.Y + r.Height}, {r.X + r.Width, r.Y + r.Height}} {
		x, y := apply(m, p[0], p[1])
		x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
	}
	return typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// translate returns the translation by (x, y).
func translate(x, y float64) typeset.Matrix {
	return typeset.Matrix{A: 1, D: 1, E: x, F: y}
}

// skewedAboutCentre returns the transform that mirrors, then shears by
// skew degrees, the top edge moving right for a positive angle, then
// turns clockwise by deg degrees, about the centre of r (ECMA-376 Part 1,
// 20.1.7.6 xfrm: the flips apply before the rotation; ODF 1.3 Part 3,
// 19.228 draw:transform, whose skewX applies before its rotate).
func skewedAboutCentre(r typeset.Rect, deg, skew float64, flipH, flipV bool) typeset.Matrix {
	deg = math.Mod(deg, 360)
	if math.IsNaN(deg) {
		deg = 0
	}
	if math.IsNaN(skew) || math.Abs(skew) > maxSkew {
		skew = 0
	}
	if deg == 0 && skew == 0 && !flipH && !flipV {
		return typeset.Identity
	}
	cx, cy := r.X+r.Width/2, r.Y+r.Height/2
	f := typeset.Identity
	if flipH {
		f.A = -1
	}
	if flipV {
		f.D = -1
	}
	// With y down, a point above the centre has a negative y: the shear
	// moves it right for a positive angle.
	sh := typeset.Matrix{A: 1, C: -math.Tan(skew * math.Pi / 180), D: 1}
	// In page coordinates, y down, a clockwise turn maps the x axis onto
	// (cos, sin).
	s, c := math.Sincos(deg * math.Pi / 180)
	rot := typeset.Matrix{A: c, B: s, C: -s, D: c}
	return then(then(then(then(translate(-cx, -cy), f), sh), rot), translate(cx, cy))
}

// maxSkew bounds a shear's angle, in degrees: a shear of 90 degrees
// flattens the shape to a line and has no finite matrix.
const maxSkew = 89

// rectOps returns the path of the rectangle r.
func rectOps(r typeset.Rect) []typeset.PathOp {
	pt := func(x, y float64) [3]typeset.Point { return [3]typeset.Point{{X: x, Y: y}} }
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: pt(r.X, r.Y)},
		{Kind: typeset.LineTo, P: pt(r.X+r.Width, r.Y)},
		{Kind: typeset.LineTo, P: pt(r.X+r.Width, r.Y+r.Height)},
		{Kind: typeset.LineTo, P: pt(r.X, r.Y+r.Height)},
		{Kind: typeset.ClosePath},
	}
}

// lineOps returns the path of the segment from (x1, y1) to (x2, y2).
func lineOps(x1, y1, x2, y2 float64) []typeset.PathOp {
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x1, Y: y1}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x2, Y: y2}}},
	}
}

// group wraps frags in a group under m, or returns them as they are under
// the identity.
func group(m typeset.Matrix, frags []typeset.Fragment) []typeset.Fragment {
	if len(frags) == 0 {
		return nil
	}
	if m == typeset.Identity {
		return frags
	}
	return []typeset.Fragment{&typeset.Group{Transform: m, Opacity: 1, Content: frags}}
}

// finite reports whether every side of r is a finite number, its size not
// negative.
func finite(r typeset.Rect) bool {
	for _, v := range [4]float64{r.X, r.Y, r.Width, r.Height} {
		if !numeric.Finite(v) || math.Abs(v) > maxCoordinate {
			return false
		}
	}
	return r.Width >= 0 && r.Height >= 0
}

// maxCoordinate bounds the coordinates of a box drawn, in points.
// Anti-DoS guard, not spec-mandated: ten times the largest PDF page side
// holds every shape a slide shows, and keeps the arithmetic finite.
const maxCoordinate = 10 * 14400
