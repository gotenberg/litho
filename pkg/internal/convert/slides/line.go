package slides

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// maxDash caps the entries of a dash array drawn. Anti-DoS guard, not
// spec-mandated: the preset dashes have at most six entries.
const maxDash = 64

// minArrowBase is the line width an arrowhead is sized from when the line
// is thinner: Impress sizes arrowheads from at least 0.7 millimetre
// (oox's line properties import, the arrow width of a hairline).
const minArrowBase = 0.7 * 72 / 25.4

// arrowFactor is an arrowhead's width and length at the medium size, in
// base widths: DrawingML's medium is three times the line width
// (ECMA-376 Part 1, 20.1.10.32 ST_LineEndWidth, as PowerPoint draws it).
const arrowFactor = 3

// stroke returns what strokes the outline ops with the line l, its
// arrowheads at the ends of an open outline (ECMA-376 Part 1, 20.1.2.2.24
// ln, 20.1.8.37 headEnd, 20.1.8.57 tailEnd).
func (b *builder) stroke(l *Line, ops []compose.PathOp) []compose.Fragment {
	if l == nil || !(l.Width > 0) || l.Color.A <= 0 || len(ops) < 2 || math.IsInf(l.Width, 0) {
		return nil
	}
	w := min(l.Width, maxLineWidth)
	st := &compose.Stroke{Color: colorOf(l.Color), Width: w}
	switch l.Cap {
	case CapRound:
		st.Cap = compose.RoundCap
	case CapSquare:
		st.Cap = compose.SquareCap
	case CapFlat:
		st.Cap = compose.ButtCap
	}
	switch l.Join {
	case JoinRound:
		st.Join = compose.RoundJoin
	case JoinBevel:
		st.Join = compose.BevelJoin
	case JoinMiter:
		st.Join = compose.MiterJoin
	}
	if len(l.Dash) > 0 && len(l.Dash) <= maxDash {
		ok, sum := true, 0.0
		for _, d := range l.Dash {
			if !(d >= 0) || math.IsInf(d, 0) {
				ok = false
			}
			sum += d
		}
		if ok && sum > 0 {
			st.Dash = append([]float64(nil), l.Dash...)
		}
	}
	closed := false
	for _, op := range ops {
		closed = closed || op.Kind == compose.ClosePath
	}
	var heads []compose.Fragment
	if !closed {
		ops = append([]compose.PathOp(nil), ops...)
		if l.Head.Kind != ArrowNone {
			if p, d, ok := pathEnd(ops, false); ok {
				heads = append(heads, arrowhead(l.Head, p, d, w, st.Color)...)
				shorten(ops, false, l.Head, w)
			}
		}
		if l.Tail.Kind != ArrowNone {
			if p, d, ok := pathEnd(ops, true); ok {
				heads = append(heads, arrowhead(l.Tail, p, d, w, st.Color)...)
				shorten(ops, true, l.Tail, w)
			}
		}
	}
	return append([]compose.Fragment{&compose.Path{Ops: ops, Stroke: st}}, heads...)
}

// maxLineWidth caps a line's width, in points. Anti-DoS guard, not
// spec-mandated: DrawingML stops at 1584 points (20.1.10.35
// ST_LineWidth).
const maxLineWidth = 1584

// pathEnd returns an end of the open path ops, its first point or, with
// last, its last one, with the unit direction the path leaves it in,
// outward.
func pathEnd(ops []compose.PathOp, last bool) (compose.Point, compose.Point, bool) {
	var pts []compose.Point
	for _, op := range ops {
		switch op.Kind {
		case compose.MoveTo, compose.LineTo:
			pts = append(pts, op.P[0])
		case compose.CurveTo:
			pts = append(pts, op.P[0], op.P[1], op.P[2])
		case compose.ClosePath:
		}
	}
	if len(pts) < 2 {
		return compose.Point{}, compose.Point{}, false
	}
	end, i, step := 0, 1, 1
	if last {
		end, i, step = len(pts)-1, len(pts)-2, -1
	}
	p := pts[end]
	for ; i >= 0 && i < len(pts); i += step {
		dx, dy := p.X-pts[i].X, p.Y-pts[i].Y
		if n := math.Hypot(dx, dy); n > 1e-9 {
			return p, compose.Point{X: dx / n, Y: dy / n}, true
		}
	}
	return compose.Point{}, compose.Point{}, false
}

// arrowSize returns the width and length of the arrowhead a at a line of
// width w.
func arrowSize(a Arrow, w float64) (float64, float64) {
	base := max(w, minArrowBase) * arrowFactor
	aw, al := a.Width, a.Length
	if !(aw > 0) || aw > 8 {
		aw = 1
	}
	if !(al > 0) || al > 8 {
		al = 1
	}
	return base * aw, base * al
}

// shorten pulls the end of the open path ops back under a closed
// arrowhead, so that the line's own end does not show past its tip: the
// first point, or the last with last, when it ends a straight segment.
func shorten(ops []compose.PathOp, last bool, a Arrow, w float64) {
	if a.Kind == ArrowOpen || a.Kind == ArrowOval || len(ops) < 2 {
		return
	}
	_, l := arrowSize(a, w)
	cut := l / 2
	i, j := 0, 1
	if last {
		i, j = len(ops)-1, len(ops)-2
	}
	if ops[i].Kind == compose.CurveTo || (last && ops[i].Kind != compose.LineTo) || (!last && ops[j].Kind != compose.LineTo) {
		return
	}
	p, q := ops[i].P[0], ops[j].P[0]
	if ops[j].Kind == compose.CurveTo {
		q = ops[j].P[2]
	}
	n := math.Hypot(p.X-q.X, p.Y-q.Y)
	if n <= cut {
		return
	}
	ops[i].P[0] = compose.Point{X: p.X + (q.X-p.X)*cut/n, Y: p.Y + (q.Y-p.Y)*cut/n}
}

// arrowhead returns the arrowhead a with its tip at p, pointing along the
// unit direction d, at a line of width w in colour c (ECMA-376 Part 1,
// 20.1.10.33 ST_LineEndType).
func arrowhead(a Arrow, p, d compose.Point, w float64, c compose.Color) []compose.Fragment {
	aw, al := arrowSize(a, w)
	// at maps a point given along the line (u back from the tip) and
	// across it (v to the left) to the page.
	at := func(u, v float64) compose.Point {
		return compose.Point{X: p.X - d.X*u - d.Y*v, Y: p.Y - d.Y*u + d.X*v}
	}
	mv := func(q compose.Point) compose.PathOp { return compose.PathOp{Kind: compose.MoveTo, P: [3]compose.Point{q}} }
	ln := func(q compose.Point) compose.PathOp { return compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{q}} }
	fill := &compose.Fill{Color: c}
	switch a.Kind {
	case ArrowTriangle:
		return []compose.Fragment{&compose.Path{Fill: fill, Ops: []compose.PathOp{mv(at(0, 0)), ln(at(al, aw/2)), ln(at(al, -aw/2)), {Kind: compose.ClosePath}}}}
	case ArrowStealth:
		return []compose.Fragment{&compose.Path{Fill: fill, Ops: []compose.PathOp{mv(at(0, 0)), ln(at(al, aw/2)), ln(at(al*0.7, 0)), ln(at(al, -aw/2)), {Kind: compose.ClosePath}}}}
	case ArrowDiamond:
		return []compose.Fragment{&compose.Path{Fill: fill, Ops: []compose.PathOp{mv(at(-al/2, 0)), ln(at(0, aw/2)), ln(at(al/2, 0)), ln(at(0, -aw/2)), {Kind: compose.ClosePath}}}}
	case ArrowOval:
		rx, ry := al/2, aw/2
		const k = 0.5522847498
		pt3 := func(a1, b1, a2, b2, a3, b3 float64) [3]compose.Point { return [3]compose.Point{at(a1, b1), at(a2, b2), at(a3, b3)} }
		return []compose.Fragment{&compose.Path{Fill: fill, Ops: []compose.PathOp{
			mv(at(rx, 0)),
			{Kind: compose.CurveTo, P: pt3(rx, ry*k, rx*k, ry, 0, ry)},
			{Kind: compose.CurveTo, P: pt3(-rx*k, ry, -rx, ry*k, -rx, 0)},
			{Kind: compose.CurveTo, P: pt3(-rx, -ry*k, -rx*k, -ry, 0, -ry)},
			{Kind: compose.CurveTo, P: pt3(rx*k, -ry, rx, -ry*k, rx, 0)},
			{Kind: compose.ClosePath},
		}}}
	case ArrowOpen:
		st := &compose.Stroke{Color: c, Width: w, Join: compose.MiterJoin, Cap: compose.ButtCap}
		return []compose.Fragment{&compose.Path{Stroke: st, Ops: []compose.PathOp{mv(at(al, aw/2)), ln(at(0, 0)), ln(at(al, -aw/2))}}}
	case ArrowNone:
	}
	return nil
}
