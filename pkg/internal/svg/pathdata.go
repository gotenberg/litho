package svg

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// parsePath parses path data (SVG 1.1, 8.3) into path operators in user
// units: every command, absolute and relative, with quadratic Béziers
// raised to cubics (8.3.7) and elliptical arcs converted to cubics (F.6).
// On an error it returns the path up to the last complete command, as SVG
// 1.1, F.2 asks, and ok false. It stops at [maxPathSegments] operators.
func parsePath(d string) (ops []compose.PathOp, ok bool) {
	sc := scanner{s: d}
	var cur, start, ctrl compose.Point
	var prev byte // the previous command, uppercase, for S and T
	var cmd byte
	open := false // a subpath is started
	closed := false
	add := func(op compose.PathOp) bool {
		if len(ops) >= maxPathSegments {
			return false
		}
		ops = append(ops, op)
		return true
	}
	// ensure starts a subpath at the current point after a closepath,
	// which PDF does not do implicitly.
	ensure := func() bool {
		if closed {
			closed = false
			return add(compose.PathOp{Kind: compose.MoveTo, P: [3]compose.Point{cur}})
		}
		return true
	}
	sc.skipWSP()
	for !sc.done() {
		c := sc.peek()
		if isCommand(c) {
			cmd = c
			sc.i++
			sc.skipWSP()
		} else if cmd == 0 {
			return ops, false
		}
		if !open && cmd != 'M' && cmd != 'm' {
			// SVG 1.1, 8.3.2: path data must begin with a moveto.
			return ops, false
		}
		upper := upperASCII(cmd)
		rel := cmd >= 'a'
		base := compose.Point{}
		if rel {
			base = cur
		}
		switch upper {
		case 'Z':
			if !add(compose.PathOp{Kind: compose.ClosePath}) {
				return ops, false
			}
			cur, closed, prev = start, true, 'Z'
			sc.skipSep()
			// Z takes no arguments: a number right after it is an error
			// unless a command intervenes.
			if !sc.done() && !isCommand(sc.peek()) {
				return ops, false
			}
			continue
		case 'M':
			p, good := sc.point()
			if !good {
				return ops, false
			}
			p = compose.Point{X: p.X + base.X, Y: p.Y + base.Y}
			if !add(compose.PathOp{Kind: compose.MoveTo, P: [3]compose.Point{p}}) {
				return ops, false
			}
			cur, start, open, closed, prev = p, p, true, false, 'M'
			// Coordinate pairs after a moveto are implicit linetos
			// (SVG 1.1, 8.3.2).
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
		case 'L':
			p, good := sc.point()
			if !good || !ensure() {
				return ops, false
			}
			p = compose.Point{X: p.X + base.X, Y: p.Y + base.Y}
			if !add(compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{p}}) {
				return ops, false
			}
			cur, prev = p, 'L'
		case 'H', 'V':
			v, good := sc.number()
			if !good || !ensure() {
				return ops, false
			}
			p := cur
			if upper == 'H' {
				p.X = v + base.X
			} else {
				p.Y = v + base.Y
			}
			if !add(compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{p}}) {
				return ops, false
			}
			cur, prev = p, upper
		case 'C', 'S':
			var c1 compose.Point
			if upper == 'C' {
				p, good := sc.point()
				if !good {
					return ops, false
				}
				c1 = compose.Point{X: p.X + base.X, Y: p.Y + base.Y}
				sc.skipSep()
			} else {
				// S reflects the previous curve's second control point
				// (SVG 1.1, 8.3.6).
				c1 = cur
				if prev == 'C' || prev == 'S' {
					c1 = compose.Point{X: 2*cur.X - ctrl.X, Y: 2*cur.Y - ctrl.Y}
				}
			}
			p2, good := sc.point()
			if !good {
				return ops, false
			}
			sc.skipSep()
			p3, good := sc.point()
			if !good || !ensure() {
				return ops, false
			}
			c2 := compose.Point{X: p2.X + base.X, Y: p2.Y + base.Y}
			end := compose.Point{X: p3.X + base.X, Y: p3.Y + base.Y}
			if !add(compose.PathOp{Kind: compose.CurveTo, P: [3]compose.Point{c1, c2, end}}) {
				return ops, false
			}
			cur, ctrl, prev = end, c2, upper
		case 'Q', 'T':
			var q compose.Point
			if upper == 'Q' {
				p, good := sc.point()
				if !good {
					return ops, false
				}
				q = compose.Point{X: p.X + base.X, Y: p.Y + base.Y}
				sc.skipSep()
			} else {
				q = cur
				if prev == 'Q' || prev == 'T' {
					q = compose.Point{X: 2*cur.X - ctrl.X, Y: 2*cur.Y - ctrl.Y}
				}
			}
			p, good := sc.point()
			if !good || !ensure() {
				return ops, false
			}
			end := compose.Point{X: p.X + base.X, Y: p.Y + base.Y}
			if !add(quadToCubic(cur, q, end)) {
				return ops, false
			}
			cur, ctrl, prev = end, q, upper
		case 'A':
			rx, good := sc.number()
			sc.skipSep()
			ry, good2 := sc.number()
			sc.skipSep()
			rot, good3 := sc.number()
			sc.skipSep()
			large, good4 := sc.flag()
			sc.skipSep()
			sweep, good5 := sc.flag()
			sc.skipSep()
			p, good6 := sc.point()
			if !good || !good2 || !good3 || !good4 || !good5 || !good6 || !ensure() {
				return ops, false
			}
			end := compose.Point{X: p.X + base.X, Y: p.Y + base.Y}
			for _, op := range arcToCubics(cur, rx, ry, rot, large, sweep, end) {
				if !add(op) {
					return ops, false
				}
			}
			cur, prev = end, 'A'
		default:
			return ops, false
		}
		sc.skipSep()
	}
	return ops, true
}

// isCommand reports a path command letter (SVG 1.1, 8.3.9).
func isCommand(c byte) bool {
	switch upperASCII(c) {
	case 'M', 'Z', 'L', 'H', 'V', 'C', 'S', 'Q', 'T', 'A':
		return true
	}
	return false
}

// upperASCII returns the ASCII letter c in upper case, any other byte as
// it is.
func upperASCII(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 'a' + 'A'
	}
	return c
}

// point reads a coordinate pair.
func (sc *scanner) point() (compose.Point, bool) {
	x, ok := sc.number()
	if !ok {
		return compose.Point{}, false
	}
	sc.skipSep()
	y, ok := sc.number()
	if !ok {
		return compose.Point{}, false
	}
	return compose.Point{X: x, Y: y}, true
}

// quadToCubic raises the quadratic Bézier p0 q p1 to the cubic that draws
// the same curve.
func quadToCubic(p0, q, p1 compose.Point) compose.PathOp {
	c1 := compose.Point{X: p0.X + 2.0/3*(q.X-p0.X), Y: p0.Y + 2.0/3*(q.Y-p0.Y)}
	c2 := compose.Point{X: p1.X + 2.0/3*(q.X-p1.X), Y: p1.Y + 2.0/3*(q.Y-p1.Y)}
	return compose.PathOp{Kind: compose.CurveTo, P: [3]compose.Point{c1, c2, p1}}
}

// arcToCubics converts the elliptical arc from p0 to p1 (SVG 1.1, 8.3.8)
// into cubic Béziers of at most a quarter turn each: endpoint to centre
// parameterization (F.6.5), radii out of range corrected (F.6.6), and each
// piece approximated with the control distance 4/3 tan(θ/4) of the unit
// circle. A zero radius draws a line, and coincident ends draw nothing
// (F.6.2).
func arcToCubics(p0 compose.Point, rx, ry, rotDeg float64, large, sweep bool, p1 compose.Point) []compose.PathOp {
	if p0 == p1 {
		return nil
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		return []compose.PathOp{{Kind: compose.LineTo, P: [3]compose.Point{p1}}}
	}
	phi := rotDeg * math.Pi / 180
	cos, sin := math.Cos(phi), math.Sin(phi)
	// F.6.5.1: the midpoint in the ellipse's axes.
	dx, dy := (p0.X-p1.X)/2, (p0.Y-p1.Y)/2
	x1 := cos*dx + sin*dy
	y1 := -sin*dx + cos*dy
	// F.6.6.2: scale the radii up when no ellipse reaches both ends.
	if lam := x1*x1/(rx*rx) + y1*y1/(ry*ry); lam > 1 {
		s := math.Sqrt(lam)
		rx, ry = rx*s, ry*s
	}
	// F.6.5.2: the centre in the ellipse's axes.
	num := rx*rx*ry*ry - rx*rx*y1*y1 - ry*ry*x1*x1
	den := rx*rx*y1*y1 + ry*ry*x1*x1
	k := 0.0
	if den > 0 && num > 0 {
		k = math.Sqrt(num / den)
	}
	if large == sweep {
		k = -k
	}
	cx1, cy1 := k*rx*y1/ry, -k*ry*x1/rx
	// F.6.5.3: the centre in user space.
	cx := cos*cx1 - sin*cy1 + (p0.X+p1.X)/2
	cy := sin*cx1 + cos*cy1 + (p0.Y+p1.Y)/2
	// F.6.5.5 and F.6.5.6: the start angle and the sweep.
	ux, uy := (x1-cx1)/rx, (y1-cy1)/ry
	vx, vy := (-x1-cx1)/rx, (-y1-cy1)/ry
	theta := math.Atan2(uy, ux)
	delta := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
	if !sweep && delta > 0 {
		delta -= 2 * math.Pi
	} else if sweep && delta < 0 {
		delta += 2 * math.Pi
	}
	if !finite(delta) || !finite(theta) || !finite(cx) || !finite(cy) {
		return []compose.PathOp{{Kind: compose.LineTo, P: [3]compose.Point{p1}}}
	}
	n := int(math.Ceil(math.Abs(delta)/(math.Pi/2) - 1e-9)) // at most 4: |delta| <= 2π
	n = max(n, 1)
	step := delta / float64(n)
	t := 4.0 / 3 * math.Tan(step/4)
	onEllipse := func(a float64) (compose.Point, compose.Point) {
		ca, sa := math.Cos(a), math.Sin(a)
		// The point and the derivative, both rotated by phi.
		px, py := rx*ca, ry*sa
		tx, ty := -rx*sa, ry*ca
		return compose.Point{X: cos*px - sin*py + cx, Y: sin*px + cos*py + cy},
			compose.Point{X: cos*tx - sin*ty, Y: sin*tx + cos*ty}
	}
	ops := make([]compose.PathOp, 0, n)
	a := theta
	from, d0 := onEllipse(a)
	for i := range n {
		b := a + step
		to, d1 := onEllipse(b)
		if i == n-1 {
			to = p1
		}
		ops = append(ops, compose.PathOp{Kind: compose.CurveTo, P: [3]compose.Point{
			{X: from.X + t*d0.X, Y: from.Y + t*d0.Y},
			{X: to.X - t*d1.X, Y: to.Y - t*d1.Y},
			to,
		}})
		a, from, d0 = b, to, d1
	}
	return ops
}
