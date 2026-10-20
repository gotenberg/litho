package geom

import "math"

// Point is a position in some coordinate space; the caller knows which.
type Point struct {
	X, Y float64
}

// Matrix is an affine transform [a b c d e f] (ISO 32000-1, 8.3.3).
type Matrix [6]float64

// Identity is the transform that maps every point to itself.
var Identity = Matrix{1, 0, 0, 1, 0, 0}

// Apply maps p through m.
func (m Matrix) Apply(p Point) Point {
	return Point{X: m[0]*p.X + m[2]*p.Y + m[4], Y: m[1]*p.X + m[3]*p.Y + m[5]}
}

// ApplyVector maps the direction v through m, ignoring the translation.
func (m Matrix) ApplyVector(v Point) Point {
	return Point{X: m[0]*v.X + m[2]*v.Y, Y: m[1]*v.X + m[3]*v.Y}
}

// Mul returns the transform that applies m, then n. In the content stream,
// "m cm" with current matrix n leaves m.Mul(n) in force (ISO 32000-1, 8.4.4).
func (m Matrix) Mul(n Matrix) Matrix {
	return Matrix{
		m[0]*n[0] + m[1]*n[2],
		m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2],
		m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4],
		m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

// Det returns the determinant of the linear part of m.
func (m Matrix) Det() float64 {
	return m[0]*m[3] - m[1]*m[2]
}

// Invert returns the inverse of m, and false when m is singular or holds a
// value that is not finite.
func (m Matrix) Invert() (Matrix, bool) {
	det := m.Det()
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) {
		return Matrix{}, false
	}
	inv := Matrix{
		m[3] / det,
		-m[1] / det,
		-m[2] / det,
		m[0] / det,
		(m[2]*m[5] - m[3]*m[4]) / det,
		(m[1]*m[4] - m[0]*m[5]) / det,
	}
	for _, v := range inv {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return Matrix{}, false
		}
	}
	return inv, true
}

// Op is one path construction step.
type Op uint8

// The path operations. MoveTo and LineTo take one point, CubicTo three (two
// control points, then the end point), and Close none. They mirror m, l, c
// and h of ISO 32000-1, 8.5.2; v, y and re are written as these.
const (
	MoveTo Op = iota
	LineTo
	CubicTo
	Close
)

// Path is a sequence of subpaths. Ops lists the steps and Pts the points
// they consume, in order.
type Path struct {
	Ops []Op
	Pts []Point
}

// MoveTo starts a new subpath at p.
func (p *Path) MoveTo(pt Point) {
	p.Ops = append(p.Ops, MoveTo)
	p.Pts = append(p.Pts, pt)
}

// LineTo adds a straight segment to pt.
func (p *Path) LineTo(pt Point) {
	p.Ops = append(p.Ops, LineTo)
	p.Pts = append(p.Pts, pt)
}

// CubicTo adds a cubic Bézier segment with control points c1 and c2 ending
// at end.
func (p *Path) CubicTo(c1, c2, end Point) {
	p.Ops = append(p.Ops, CubicTo)
	p.Pts = append(p.Pts, c1, c2, end)
}

// Close closes the current subpath with a straight segment to its start.
func (p *Path) Close() {
	p.Ops = append(p.Ops, Close)
}

// Empty reports whether the path holds no step.
func (p *Path) Empty() bool {
	return len(p.Ops) == 0
}

// Transform returns a copy of p with every point mapped through m. An affine
// map sends a Bézier's control points to the control points of its image, so
// mapping the points maps the curve.
func (p Path) Transform(m Matrix) Path {
	out := Path{Ops: append([]Op(nil), p.Ops...), Pts: make([]Point, len(p.Pts))}
	for i, pt := range p.Pts {
		out.Pts[i] = m.Apply(pt)
	}
	return out
}

// Rect is an axis-aligned rectangle with Min ≤ Max on both axes.
type Rect struct {
	Min, Max Point
}

// Bounds returns the rectangle holding every point of p, control points
// included, and false for a path with no point. A Bézier lies inside the hull
// of its control points, so the box contains the curve.
func (p Path) Bounds() (Rect, bool) {
	if len(p.Pts) == 0 {
		return Rect{}, false
	}
	r := Rect{Min: p.Pts[0], Max: p.Pts[0]}
	for _, pt := range p.Pts[1:] {
		r.Min.X = min(r.Min.X, pt.X)
		r.Min.Y = min(r.Min.Y, pt.Y)
		r.Max.X = max(r.Max.X, pt.X)
		r.Max.Y = max(r.Max.Y, pt.Y)
	}
	return r, true
}
