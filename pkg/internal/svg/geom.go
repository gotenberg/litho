package svg

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// identity is the transform that changes nothing.
var identity = compose.Matrix{A: 1, D: 1}

// mul returns the transform that applies b, then a: a∘b. With b mapping an
// element's coordinates to its parent's and a its parent's to the
// grandparent's, the result maps the element's to the grandparent's.
func mul(a, b compose.Matrix) compose.Matrix {
	return compose.Matrix{
		A: a.A*b.A + a.C*b.B,
		B: a.B*b.A + a.D*b.B,
		C: a.A*b.C + a.C*b.D,
		D: a.B*b.C + a.D*b.D,
		E: a.A*b.E + a.C*b.F + a.E,
		F: a.B*b.E + a.D*b.F + a.F,
	}
}

// apply maps p through m.
func apply(m compose.Matrix, p compose.Point) compose.Point {
	return compose.Point{X: m.A*p.X + m.C*p.Y + m.E, Y: m.B*p.X + m.D*p.Y + m.F}
}

// translate returns the translation by (x, y).
func translate(x, y float64) compose.Matrix { return compose.Matrix{A: 1, D: 1, E: x, F: y} }

// scale returns the scaling by (x, y).
func scale(x, y float64) compose.Matrix { return compose.Matrix{A: x, D: y} }

// isIdentity reports whether m changes nothing.
func isIdentity(m compose.Matrix) bool { return m == identity }

// invertible reports whether m has an inverse and finite entries: a
// singular transform collapses what it draws to a line or a point, which
// paints nothing (SVG 1.1, 7.5).
func invertible(m compose.Matrix) bool {
	det := m.A*m.D - m.B*m.C
	return det != 0 && finite(det) && finite(m.A) && finite(m.B) && finite(m.C) && finite(m.D) && finite(m.E) && finite(m.F)
}

// invert returns the inverse of m, false when it has none.
func invert(m compose.Matrix) (compose.Matrix, bool) {
	det := m.A*m.D - m.B*m.C
	if det == 0 || !finite(det) {
		return identity, false
	}
	inv := compose.Matrix{
		A: m.D / det, B: -m.B / det, C: -m.C / det, D: m.A / det,
		E: (m.C*m.F - m.D*m.E) / det, F: (m.B*m.E - m.A*m.F) / det,
	}
	return inv, invertible(inv)
}

// finite reports whether v is neither NaN nor infinite.
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// clampCoord bounds a coordinate to [maxCoordinate], NaN to 0.
func clampCoord(v float64) float64 {
	switch {
	case math.IsNaN(v):
		return 0
	case v > maxCoordinate:
		return maxCoordinate
	case v < -maxCoordinate:
		return -maxCoordinate
	}
	return v
}

// bbox is an axis-aligned bounding box in user units.
type bbox struct {
	x0, y0, x1, y1 float64
	ok             bool
}

// add grows b to hold p.
func (b *bbox) add(p compose.Point) {
	if !b.ok {
		b.x0, b.y0, b.x1, b.y1, b.ok = p.X, p.Y, p.X, p.Y, true
		return
	}
	b.x0, b.y0 = min(b.x0, p.X), min(b.y0, p.Y)
	b.x1, b.y1 = max(b.x1, p.X), max(b.y1, p.Y)
}

// opsBBox returns the bounding box of a path, its curves bounded by their
// extrema (SVG 1.1, 7.11: the bounding box is tight, not the hull of the
// control points).
func opsBBox(ops []compose.PathOp) bbox {
	var b bbox
	var cur compose.Point
	for _, op := range ops {
		switch op.Kind {
		case compose.MoveTo, compose.LineTo:
			b.add(op.P[0])
			cur = op.P[0]
		case compose.CurveTo:
			cubicExtrema(cur, op.P[0], op.P[1], op.P[2], b.add)
			cur = op.P[2]
		case compose.ClosePath:
		}
	}
	return b
}

// cubicExtrema calls add with the end point and every extremum of the
// cubic Bézier p0 p1 p2 p3 along each axis.
func cubicExtrema(p0, p1, p2, p3 compose.Point, add func(compose.Point)) {
	add(p0)
	add(p3)
	at := func(t float64) compose.Point {
		u := 1 - t
		return compose.Point{
			X: u*u*u*p0.X + 3*u*u*t*p1.X + 3*u*t*t*p2.X + t*t*t*p3.X,
			Y: u*u*u*p0.Y + 3*u*u*t*p1.Y + 3*u*t*t*p2.Y + t*t*t*p3.Y,
		}
	}
	for axis := range 2 {
		v := [4]float64{p0.X, p1.X, p2.X, p3.X}
		if axis == 1 {
			v = [4]float64{p0.Y, p1.Y, p2.Y, p3.Y}
		}
		// The derivative is a quadratic a t^2 + b t + c.
		a := -v[0] + 3*v[1] - 3*v[2] + v[3]
		bq := 2 * (v[0] - 2*v[1] + v[2])
		c := v[1] - v[0]
		for _, t := range quadRoots(a, bq, c) {
			if t > 0 && t < 1 {
				add(at(t))
			}
		}
	}
}

// quadRoots returns the real roots of a t^2 + b t + c.
func quadRoots(a, b, c float64) []float64 {
	const eps = 1e-12
	if math.Abs(a) < eps {
		if math.Abs(b) < eps {
			return nil
		}
		return []float64{-c / b}
	}
	d := b*b - 4*a*c
	if d < 0 {
		return nil
	}
	s := math.Sqrt(d)
	return []float64{(-b + s) / (2 * a), (-b - s) / (2 * a)}
}

// transformOps maps every point of ops through m into a new slice.
func transformOps(ops []compose.PathOp, m compose.Matrix) []compose.PathOp {
	out := make([]compose.PathOp, len(ops))
	for i, op := range ops {
		out[i].Kind = op.Kind
		for j := range op.P {
			out[i].P[j] = apply(m, op.P[j])
		}
	}
	return out
}
