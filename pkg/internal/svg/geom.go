package svg

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// identity is the transform that changes nothing.
var identity = typeset.Matrix{A: 1, D: 1}

// mul returns the transform that applies b, then a: a∘b. With b mapping an
// element's coordinates to its parent's and a its parent's to the
// grandparent's, the result maps the element's to the grandparent's.
func mul(a, b typeset.Matrix) typeset.Matrix {
	return typeset.Matrix{
		A: float64(a.A*b.A) + float64(a.C*b.B),
		B: float64(a.B*b.A) + float64(a.D*b.B),
		C: float64(a.A*b.C) + float64(a.C*b.D),
		D: float64(a.B*b.C) + float64(a.D*b.D),
		E: float64(a.A*b.E) + float64(a.C*b.F) + a.E,
		F: float64(a.B*b.E) + float64(a.D*b.F) + a.F,
	}
}

// apply maps p through m.
func apply(m typeset.Matrix, p typeset.Point) typeset.Point {
	return typeset.Point{X: float64(m.A*p.X) + float64(m.C*p.Y) + m.E, Y: float64(m.B*p.X) + float64(m.D*p.Y) + m.F}
}

// translate returns the translation by (x, y).
func translate(x, y float64) typeset.Matrix { return typeset.Matrix{A: 1, D: 1, E: x, F: y} }

// scale returns the scaling by (x, y).
func scale(x, y float64) typeset.Matrix { return typeset.Matrix{A: x, D: y} }

// isIdentity reports whether m changes nothing.
func isIdentity(m typeset.Matrix) bool { return m == identity }

// invertible reports whether m has an inverse and finite entries: a
// singular transform collapses what it draws to a line or a point, which
// paints nothing (SVG 1.1, 7.5).
func invertible(m typeset.Matrix) bool {
	det := float64(m.A*m.D) - float64(m.B*m.C)
	return det != 0 && numeric.Finite(det) && numeric.Finite(m.A) && numeric.Finite(m.B) && numeric.Finite(m.C) && numeric.Finite(m.D) && numeric.Finite(m.E) && numeric.Finite(m.F)
}

// invert returns the inverse of m, false when it has none.
func invert(m typeset.Matrix) (typeset.Matrix, bool) {
	det := float64(m.A*m.D) - float64(m.B*m.C)
	if det == 0 || !numeric.Finite(det) {
		return identity, false
	}
	inv := typeset.Matrix{
		A: m.D / det, B: -m.B / det, C: -m.C / det, D: m.A / det,
		E: (float64(m.C*m.F) - float64(m.D*m.E)) / det, F: (float64(m.B*m.E) - float64(m.A*m.F)) / det,
	}
	return inv, invertible(inv)
}

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

// hasArea reports whether b holds a box of positive width and height. A
// box with a NaN edge has none.
func (b bbox) hasArea() bool { return b.ok && b.x1 > b.x0 && b.y1 > b.y0 }

// add grows b to hold p.
func (b *bbox) add(p typeset.Point) {
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
func opsBBox(ops []typeset.PathOp) bbox {
	var b bbox
	var cur typeset.Point
	for _, op := range ops {
		switch op.Kind {
		case typeset.MoveTo, typeset.LineTo:
			b.add(op.P[0])
			cur = op.P[0]
		case typeset.CurveTo:
			cubicExtrema(cur, op.P[0], op.P[1], op.P[2], b.add)
			cur = op.P[2]
		case typeset.ClosePath:
		}
	}
	return b
}

// cubicExtrema calls add with the end point and every extremum of the
// cubic Bézier p0 p1 p2 p3 along each axis.
func cubicExtrema(p0, p1, p2, p3 typeset.Point, add func(typeset.Point)) {
	add(p0)
	add(p3)
	at := func(t float64) typeset.Point {
		u := 1 - t
		return typeset.Point{
			X: float64(u*u*u*p0.X) + float64(3*u*u*t*p1.X) + float64(3*u*t*t*p2.X) + float64(t*t*t*p3.X),
			Y: float64(u*u*u*p0.Y) + float64(3*u*u*t*p1.Y) + float64(3*u*t*t*p2.Y) + float64(t*t*t*p3.Y),
		}
	}
	for axis := range 2 {
		v := [4]float64{p0.X, p1.X, p2.X, p3.X}
		if axis == 1 {
			v = [4]float64{p0.Y, p1.Y, p2.Y, p3.Y}
		}
		// The derivative is a quadratic a t^2 + b t + c.
		a := -v[0] + float64(3*v[1]) - float64(3*v[2]) + v[3]
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
	d := float64(b*b) - float64(4*a*c)
	if d < 0 {
		return nil
	}
	s := math.Sqrt(d)
	return []float64{(-b + s) / (2 * a), (-b - s) / (2 * a)}
}

// transformOps maps every point of ops through m into a new slice.
func transformOps(ops []typeset.PathOp, m typeset.Matrix) []typeset.PathOp {
	out := make([]typeset.PathOp, len(ops))
	for i, op := range ops {
		out[i].Kind = op.Kind
		for j := range op.P {
			out[i].P[j] = apply(m, op.P[j])
		}
	}
	return out
}
