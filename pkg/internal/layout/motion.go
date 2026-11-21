package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// motionChords is how many chords a cubic Bézier curve of an offset path is
// measured and walked along by: a quarter circle of a 1000-point radius
// cut in 32 chords is 0.12 point shorter than the arc, the chord's
// position along it off by less.
const motionChords = 32

// motionOf returns the offset transform of b (CSS Motion Path 1, 2.7):
// the translation that puts its anchor point on its offset path at its
// offset distance, then the rotation offset-rotate asks, about that
// point; and whether b has one. The matrix maps coordinates relative to
// o, the transform origin [transformOf] composes about, in the frame of
// r, b's border box as it paints.
//
// The path is laid in the box [motionFrame] says. The anchor point is
// offset-anchor in the border box, the transform origin when it is auto
// (2.5).
func motionOf(b *box, r typeset.Rect, o typeset.Point) (typeset.Matrix, bool) {
	m := b.st.Motion()
	if m == nil || m.Path.Kind == css.OffsetNone {
		return typeset.Identity, false
	}
	frame := motionFrame(b, r)
	var p typeset.Point
	var dir float64
	if m.Path.Kind == css.OffsetRay {
		p, dir = rayPoint(m, r, frame)
	} else {
		ops, closed := offsetOps(m, r, frame)
		p, dir = pathPoint(ops, closed, m.Distance)
	}
	angle := m.Rotate.Angle
	if !m.Rotate.Fixed {
		angle += dir
	}
	a := o
	if m.AnchorSet {
		a = typeset.Point{X: r.X + resolve(m.Anchor[0], r.Width), Y: r.Y + resolve(m.Anchor[1], r.Height)}
	}
	ax, ay := a.X-o.X, a.Y-o.Y
	px, py := p.X-o.X, p.Y-o.Y
	sin, cos := detmath.Sincos(clamp(angle))
	// T(p) R T(-a): a lands on p, the box turned about it.
	return typeset.Matrix{
		A: cos, B: sin, C: -sin, D: cos,
		E: px - (float64(cos*ax) - float64(sin*ay)),
		F: py - (float64(sin*ax) + float64(cos*ay)),
	}, true
}

// motionFrame returns the rectangle an offset path other than path() is
// laid in, in the frame of r: the <coord-box> of the box whose containing
// block holds b (CSS Motion Path 1, 2.1), or the top-most box's border
// box for the initial containing block.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 lays ray(),
//     circle(), inset() and a <coord-box> alone in the border box of the
//     element whose content box holds the moving box, by default, on
//     screen and in print.
func motionFrame(b *box, r typeset.Rect) typeset.Rect {
	a := cbGenerator(b)
	if a == nil {
		a = b
		for a.parent != nil {
			a = a.parent
		}
	}
	f := typeset.Rect{X: r.X + a.x - b.x, Y: r.Y + a.y - b.y, Width: a.borderW(), Height: a.borderH()}
	var in [4]float64
	switch b.st.Motion().Path.Box {
	case css.GeoPadding:
		in = a.bd
	case css.GeoContent:
		for i := range in {
			in[i] = a.bd[i] + a.pd[i]
		}
	case css.GeoBorder, css.GeoMargin:
	}
	f.X += in[css.Left]
	f.Y += in[css.Top]
	f.Width = max(f.Width-in[css.Left]-in[css.Right], 0)
	f.Height = max(f.Height-in[css.Top]-in[css.Bottom], 0)
	return f
}

// offsetOps returns the path of an offset-path that is a shape or a box,
// and whether it is a closed loop (CSS Motion Path 1, 2.2): a path() in
// b's own border box r, its first point where the data starts, closed
// when its last command closes it; another basic shape in the frame f as
// a clip-path lays it, closed, a circle or an ellipse from its rightmost
// point round as SVG 2, 10.4 and 10.5 draw one; a box alone as its
// rectangle from the top-left corner.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 places a box on
//     path('M 20 20 ...') with its anchor 20px right of and 20px below its
//     own border box's top left, at 0%; at 25% of circle() at the circle's
//     bottom, turned a half turn; at 30% of inset() along its top edge;
//     at 40% of border-box down its right edge, turned a quarter turn.
func offsetOps(m *css.MotionPath, r, f typeset.Rect) ([]typeset.PathOp, bool) {
	s := m.Path.Shape
	switch {
	case m.Path.Kind == css.OffsetBox:
		return rectOps(f), true
	case m.Path.Kind == css.OffsetURL || s == nil:
		// A reference litho does not resolve is path("m 0 0") (2.1).
		return []typeset.PathOp{{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: r.X, Y: r.Y}}}}, false
	case s.Kind == css.ShapePath:
		data, _ := svg.PathData(s.Path) // a path cut short by an error keeps what it drew, as SVG 1.1, F.2 asks
		ops := make([]typeset.PathOp, 0, len(data))
		for _, op := range data {
			for i := range op.P {
				op.P[i] = typeset.Point{X: r.X + float64(op.P[i].X*css.Pt), Y: r.Y + float64(op.P[i].Y*css.Pt)}
			}
			ops = append(ops, op)
		}
		return ops, len(ops) > 0 && ops[len(ops)-1].Kind == typeset.ClosePath
	case s.Kind == css.ShapeCircle || s.Kind == css.ShapeEllipse:
		p, ok := shapePath(s, f)
		if !ok {
			return nil, true
		}
		x0, y0, x1, y1 := opsBounds(p.Ops)
		return ellipseOps((x0+x1)/2, (y0+y1)/2, (x1-x0)/2, (y1-y0)/2), true
	}
	p, ok := shapePath(s, f)
	if !ok {
		return nil, true
	}
	return p.Ops, true
}

// ellipseOps returns the ellipse of centre (cx, cy) and radii rx and ry
// from its rightmost point, through its bottom, as SVG 2, 10.5 lays out
// an ellipse's path, in four cubic curves.
func ellipseOps(cx, cy, rx, ry float64) []typeset.PathOp {
	kx, ky := float64(kappa*rx), float64(kappa*ry)
	pt := func(x, y float64) typeset.Point { return typeset.Point{X: x, Y: y} }
	curve := func(c1, c2, to typeset.Point) typeset.PathOp {
		return typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{c1, c2, to}}
	}
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{pt(cx+rx, cy)}},
		curve(pt(cx+rx, cy+ky), pt(cx+kx, cy+ry), pt(cx, cy+ry)),
		curve(pt(cx-kx, cy+ry), pt(cx-rx, cy+ky), pt(cx-rx, cy)),
		curve(pt(cx-rx, cy-ky), pt(cx-kx, cy-ry), pt(cx, cy-ry)),
		curve(pt(cx+kx, cy-ry), pt(cx+rx, cy-ky), pt(cx+rx, cy)),
		{Kind: typeset.ClosePath},
	}
}

// walkPath calls seg for each straight piece of ops in order, a cubic
// curve cut in [motionChords] chords, a close path back to its subpath's
// start, until seg returns false; tan gives the path's direction at a
// fraction of the piece, a curve's own tangent there. A move draws no
// piece (SVG 2, 9.5.1: a path's length leaves its moves out).
func walkPath(ops []typeset.PathOp, seg func(a, b typeset.Point, tan func(f float64) float64) bool) {
	chord := func(a, b typeset.Point) func(float64) float64 {
		return func(float64) float64 { return detmath.Atan2(b.Y-a.Y, b.X-a.X) }
	}
	var cur, start typeset.Point
	for _, op := range ops {
		switch op.Kind {
		case typeset.MoveTo:
			cur, start = op.P[0], op.P[0]
		case typeset.LineTo:
			if !seg(cur, op.P[0], chord(cur, op.P[0])) {
				return
			}
			cur = op.P[0]
		case typeset.CurveTo:
			prev := cur
			for i := 1; i <= motionChords; i++ {
				q := bezier(cur, op.P[0], op.P[1], op.P[2], float64(i)/motionChords)
				p0, i0, fallback := cur, float64(i-1), chord(prev, q)
				tan := func(f float64) float64 {
					t := (i0 + f) / motionChords
					d := bezierTangent(p0, op.P[0], op.P[1], op.P[2], t)
					if d.X == 0 && d.Y == 0 {
						return fallback(f)
					}
					return detmath.Atan2(d.Y, d.X)
				}
				if !seg(prev, q, tan) {
					return
				}
				prev = q
			}
			cur = op.P[2]
		case typeset.ClosePath:
			if !seg(cur, start, chord(cur, start)) {
				return
			}
			cur = start
		}
	}
}

// bezier returns the point at t of the cubic Bézier curve p0 to p3.
func bezier(p0, p1, p2, p3 typeset.Point, t float64) typeset.Point {
	u := 1 - t
	a, b, c, d := float64(u*u)*u, float64(3*u*u)*t, float64(3*u*t)*t, float64(t*t)*t
	return typeset.Point{
		X: float64(a*p0.X) + float64(b*p1.X) + float64(c*p2.X) + float64(d*p3.X),
		Y: float64(a*p0.Y) + float64(b*p1.Y) + float64(c*p2.Y) + float64(d*p3.Y),
	}
}

// bezierTangent returns the derivative at t of the cubic Bézier curve p0
// to p3.
func bezierTangent(p0, p1, p2, p3 typeset.Point, t float64) typeset.Point {
	u := 1 - t
	a, b, c := float64(3*u)*u, float64(6*u)*t, float64(3*t)*t
	return typeset.Point{
		X: float64(a*(p1.X-p0.X)) + float64(b*(p2.X-p1.X)) + float64(c*(p3.X-p2.X)),
		Y: float64(a*(p1.Y-p0.Y)) + float64(b*(p2.Y-p1.Y)) + float64(c*(p3.Y-p2.Y)),
	}
}

// pathPoint returns the point at distance d along ops and the path's
// direction there, in radians clockwise from the x axis (CSS Motion Path
// 1, 2.2 and 2.4): a percentage of the path's length; on a closed loop d
// taken modulo the length, on an open path clamped to it; a path of no
// length puts the point at its start.
func pathPoint(ops []typeset.PathOp, closed bool, d css.Dimension) (typeset.Point, float64) {
	total := 0.0
	var first typeset.Point
	have := false
	walkPath(ops, func(a, b typeset.Point, _ func(float64) float64) bool {
		if !have {
			first, have = a, true
		}
		total += detmath.Hypot(b.X-a.X, b.Y-a.Y)
		return true
	})
	if !have {
		if len(ops) > 0 {
			return ops[0].P[0], 0
		}
		return typeset.Point{}, 0
	}
	dist := resolve(d, total)
	switch {
	case !(total > 0):
		return first, 0
	case closed:
		dist = math.Mod(dist, total)
		if dist < 0 {
			dist += total
		}
	default:
		dist = min(max(dist, 0), total)
	}
	var p typeset.Point
	dir := 0.0
	walked := 0.0
	walkPath(ops, func(a, b typeset.Point, tan func(float64) float64) bool {
		l := detmath.Hypot(b.X-a.X, b.Y-a.Y)
		if l <= 0 {
			return true
		}
		dir = tan(1)
		p = b
		if walked+l >= dist {
			t := (dist - walked) / l
			dir = tan(t)
			p = typeset.Point{X: a.X + float64(t*(b.X-a.X)), Y: a.Y + float64(t*(b.Y-a.Y))}
			return false
		}
		walked += l
		return true
	})
	return p, dir
}

// rayPoint returns the point at the offset distance along the ray() of m
// and the ray's direction (CSS Motion Path 1, 3.1): from its at
// <position> in the frame f, else from the offset starting position, the
// top-left corner of the border box r under offset-position: auto, the
// frame's centre when there is none; its length the distance its
// <ray-size> names to the frame's sides or corners, less half the larger
// side of r when it contains the box, a percentage being of that length.
func rayPoint(m *css.MotionPath, r, f typeset.Rect) (typeset.Point, float64) {
	ray := m.Path.Ray
	start := typeset.Point{X: f.X + f.Width/2, Y: f.Y + f.Height/2}
	switch {
	case ray.AtSet:
		start = typeset.Point{X: f.X + resolve(ray.At[0], f.Width), Y: f.Y + resolve(ray.At[1], f.Height)}
	case m.Position == css.OffsetAt:
		start = typeset.Point{X: f.X + resolve(m.Start[0], f.Width), Y: f.Y + resolve(m.Start[1], f.Height)}
	case m.Position == css.OffsetAuto:
		start = typeset.Point{X: r.X, Y: r.Y}
	}
	sin, cos := detmath.Sincos(clamp(ray.Angle))
	length := raySize(ray, start, sin, -cos, f)
	if ray.Contain {
		length = max(length-float64(max(r.Width, r.Height)/2), 0)
	}
	d := resolve(m.Distance, length)
	if ray.Contain {
		d = min(max(d, 0), length)
	}
	return typeset.Point{X: start.X + float64(d*sin), Y: start.Y - float64(d*cos)}, clamp(ray.Angle) - math.Pi/2
}

// raySize returns the length of a ray from s along (dx, dy) that its
// <ray-size> names in the frame f (CSS Motion Path 1, 3.1).
func raySize(ray *css.Ray, s typeset.Point, dx, dy float64, f typeset.Rect) float64 {
	left, right := math.Abs(s.X-f.X), math.Abs(f.X+f.Width-s.X)
	top, bottom := math.Abs(s.Y-f.Y), math.Abs(f.Y+f.Height-s.Y)
	switch ray.Size {
	case css.RayFarthestSide:
		return max(left, right, top, bottom)
	case css.RayClosestCorner:
		return detmath.Hypot(min(left, right), min(top, bottom))
	case css.RayFarthestCorner:
		return detmath.Hypot(max(left, right), max(top, bottom))
	case css.RaySides:
		if s.X < f.X || s.X > f.X+f.Width || s.Y < f.Y || s.Y > f.Y+f.Height {
			return 0
		}
		t := math.Inf(1)
		if dx > 0 {
			t = min(t, right/dx)
		} else if dx < 0 {
			t = min(t, left/-dx)
		}
		if dy > 0 {
			t = min(t, bottom/dy)
		} else if dy < 0 {
			t = min(t, top/-dy)
		}
		return clamp(t)
	case css.RayClosestSide:
	}
	return min(left, right, top, bottom)
}
