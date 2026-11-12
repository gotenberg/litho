package raster

import (
	"image"
	"image/draw"
	"math"

	"golang.org/x/image/vector"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// Path is a path to cover: its construction steps in path units, filled
// when Width is 0, else stroked with that width in path units, with
// round joins and caps.
type Path struct {
	Ops   []compose.PathOp
	Width float64
}

// maxStrokeVertices caps the polyline vertices of one stroked path, each
// drawn as a round join. Anti-DoS guard, not spec-mandated: a path is
// flattened into at most maxCurveSegments segments a curve, and 65,536
// vertices outline any shape a document draws; past the cap the joins are
// left out and the segments still cover.
const maxStrokeVertices = 1 << 16

// maxCurveSegments caps the segments a cubic flattens into for a stroke.
// Anti-DoS guard, not spec-mandated: 64 segments keep a quarter circle of
// 1000 pixels' radius within a quarter pixel of the curve.
const maxCurveSegments = 64

// Draw covers m with paths mapped by tr from path units to pixels,
// each path over the coverage before it (source-over, a union). A filled
// path covers where its winding number is not zero, its subpaths summed
// by direction: subpaths of opposite directions cancel where they
// overlap, those of one direction do not, so an even-odd hole drawn as a
// second subpath of the same direction is covered. A stroke's width
// scales by the square root of tr's determinant. A path with a
// non-finite coordinate is skipped.
func Draw(m *image.Alpha, paths []Path, tr compose.Matrix) {
	b := m.Bounds()
	if b.Empty() {
		return
	}
	z := vector.NewRasterizer(b.Dx(), b.Dy())
	src := image.Opaque
	for _, p := range paths {
		if !finiteOps(p.Ops, tr) {
			continue
		}
		z.Reset(b.Dx(), b.Dy())
		z.DrawOp = draw.Over
		if p.Width > 0 {
			w := p.Width * math.Sqrt(math.Abs(tr.A*tr.D-tr.B*tr.C))
			if math.IsNaN(w) || w <= 0 || math.IsInf(w, 0) {
				continue
			}
			stroke(z, polylines(p.Ops, tr), w/2)
		} else {
			fill(z, p.Ops, tr)
		}
		z.Draw(m, b, src, image.Point{})
	}
}

// finiteOps reports whether every point of ops maps through tr to a
// finite pixel position within a range float32 arithmetic keeps exact
// enough.
func finiteOps(ops []compose.PathOp, tr compose.Matrix) bool {
	for _, op := range ops {
		n := 1
		if op.Kind == compose.CurveTo {
			n = 3
		}
		for _, p := range op.P[:n] {
			x, y := apply(tr, p)
			if math.IsNaN(x) || math.IsNaN(y) || math.Abs(x) > maxPixelCoordinate || math.Abs(y) > maxPixelCoordinate {
				return false
			}
		}
	}
	return true
}

// maxPixelCoordinate bounds a mapped coordinate, in pixels. Anti-DoS
// guard, not spec-mandated: a thousand times the largest mask side keeps
// float32 rasterisation exact to a hundredth of a pixel and covers every
// path that reaches a mask.
const maxPixelCoordinate = 1 << 22

func apply(m compose.Matrix, p compose.Point) (float64, float64) {
	return m.A*p.X + m.C*p.Y + m.E, m.B*p.X + m.D*p.Y + m.F
}

// fill adds the subpaths of ops to z, each closed.
func fill(z *vector.Rasterizer, ops []compose.PathOp, tr compose.Matrix) {
	open := false
	for _, op := range ops {
		switch op.Kind {
		case compose.MoveTo:
			if open {
				z.ClosePath()
			}
			x, y := apply(tr, op.P[0])
			z.MoveTo(float32(x), float32(y))
			open = true
		case compose.LineTo:
			if !open {
				continue
			}
			x, y := apply(tr, op.P[0])
			z.LineTo(float32(x), float32(y))
		case compose.CurveTo:
			if !open {
				continue
			}
			x1, y1 := apply(tr, op.P[0])
			x2, y2 := apply(tr, op.P[1])
			x3, y3 := apply(tr, op.P[2])
			z.CubeTo(float32(x1), float32(y1), float32(x2), float32(y2), float32(x3), float32(y3))
		case compose.ClosePath:
			if open {
				z.ClosePath()
				open = false
			}
		}
	}
	if open {
		z.ClosePath()
	}
}

type pt struct{ x, y float64 }

// polyline is a flattened subpath; closed joins its last point to its
// first.
type polyline struct {
	pts    []pt
	closed bool
}

// polylines flattens ops mapped by tr into pixel polylines.
func polylines(ops []compose.PathOp, tr compose.Matrix) []polyline {
	var out []polyline
	var cur *polyline
	total := 0
	add := func(p pt) {
		if cur != nil && total < maxStrokeVertices {
			cur.pts = append(cur.pts, p)
			total++
		}
	}
	for _, op := range ops {
		switch op.Kind {
		case compose.MoveTo:
			out = append(out, polyline{})
			cur = &out[len(out)-1]
			x, y := apply(tr, op.P[0])
			add(pt{x, y})
		case compose.LineTo:
			x, y := apply(tr, op.P[0])
			add(pt{x, y})
		case compose.CurveTo:
			if cur == nil || len(cur.pts) == 0 {
				continue
			}
			p0 := cur.pts[len(cur.pts)-1]
			x1, y1 := apply(tr, op.P[0])
			x2, y2 := apply(tr, op.P[1])
			x3, y3 := apply(tr, op.P[2])
			l := math.Hypot(x1-p0.x, y1-p0.y) + math.Hypot(x2-x1, y2-y1) + math.Hypot(x3-x2, y3-y2)
			n := int(min(math.Ceil(math.Sqrt(l)), maxCurveSegments))
			n = max(n, 1)
			for i := 1; i <= n; i++ {
				t := float64(i) / float64(n)
				u := 1 - t
				a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
				add(pt{a*p0.x + b*x1 + c*x2 + d*x3, a*p0.y + b*y1 + c*y2 + d*y3})
			}
		case compose.ClosePath:
			if cur != nil {
				cur.closed = true
				cur = nil
			}
		}
	}
	return out
}

// stroke adds to z a quadrilateral per segment and a disc per vertex of
// lines, every polygon wound one way so overlaps add up rather than
// cancel.
func stroke(z *vector.Rasterizer, lines []polyline, hw float64) {
	disc := discPoints(hw)
	for _, l := range lines {
		pts := l.pts
		n := len(pts)
		if n == 0 {
			continue
		}
		segs := n - 1
		if l.closed && n > 2 {
			segs = n
		}
		for i := 0; i < segs; i++ {
			a, b := pts[i], pts[(i+1)%n]
			dx, dy := b.x-a.x, b.y-a.y
			d := math.Hypot(dx, dy)
			if d == 0 {
				continue
			}
			nx, ny := -dy/d*hw, dx/d*hw
			polygon(z, []pt{{a.x + nx, a.y + ny}, {b.x + nx, b.y + ny}, {b.x - nx, b.y - ny}, {a.x - nx, a.y - ny}})
		}
		for _, p := range pts {
			q := make([]pt, len(disc))
			for i, d := range disc {
				q[i] = pt{p.x + d.x, p.y + d.y}
			}
			polygon(z, q)
		}
	}
}

// discPoints returns a polygon approximating a disc of radius r about
// the origin, its sides at most a pixel and a half long.
func discPoints(r float64) []pt {
	n := int(min(max(math.Ceil(2*math.Pi*r/1.5), 8), 256))
	out := make([]pt, n)
	for i := range out {
		s, c := math.Sincos(2 * math.Pi * float64(i) / float64(n))
		out[i] = pt{r * c, r * s}
	}
	return out
}

// polygon adds the closed polygon q to z wound with a positive area.
func polygon(z *vector.Rasterizer, q []pt) {
	area := 0.0
	for i := range q {
		j := (i + 1) % len(q)
		area += q[i].x*q[j].y - q[j].x*q[i].y
	}
	if area < 0 {
		for i, j := 0, len(q)-1; i < j; i, j = i+1, j-1 {
			q[i], q[j] = q[j], q[i]
		}
	}
	z.MoveTo(float32(q[0].x), float32(q[0].y))
	for _, p := range q[1:] {
		z.LineTo(float32(p.x), float32(p.y))
	}
	z.ClosePath()
}
