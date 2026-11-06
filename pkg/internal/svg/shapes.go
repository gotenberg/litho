package svg

import (
	"fmt"
	"math"

	"golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// kappa is the control point distance, in radii, of the cubic Bézier that
// approximates a quarter circle: 4/3 (√2 − 1).
const kappa = 0.5522847498307936

// geometry returns the outline of the shape n styled st, in its user
// units (SVG 1.1, 9.2 to 9.7, and 8 for path), nil when the shape draws
// nothing: a rect, circle or ellipse of zero size, a polyline of fewer
// than two points. An error in the attributes renders the shape up to the
// error, or not at all, as SVG 1.1, F.2 asks.
func (r *renderer) geometry(n *html.Node, st *style) []compose.PathOp {
	lc := r.lc(st)
	num := func(key string, a axis) float64 {
		l, ok := parseLength(attrOf(n, key))
		if !ok {
			return 0
		}
		return l.resolve(lc, a)
	}
	switch n.Data {
	case "rect":
		x, y := num("x", axisX), num("y", axisY)
		w, h := num("width", axisX), num("height", axisY)
		if !(w > 0) || !(h > 0) {
			return nil
		}
		// SVG 1.1, 9.2: an auto radius takes the other's value, and each
		// is at most half its side.
		rxl, rxok := parseLength(attrOf(n, "rx"))
		ryl, ryok := parseLength(attrOf(n, "ry"))
		rx, ry := 0.0, 0.0
		if rxok && rxl.v >= 0 {
			rx = rxl.resolve(lc, axisX)
		} else {
			rxok = false
		}
		if ryok && ryl.v >= 0 {
			ry = ryl.resolve(lc, axisY)
		} else {
			ryok = false
		}
		switch {
		case rxok && !ryok:
			ry = rx
		case ryok && !rxok:
			rx = ry
		}
		rx, ry = min(rx, w/2), min(ry, h/2)
		if rx <= 0 || ry <= 0 {
			return rectOps(x, y, w, h)
		}
		return roundRect(x, y, w, h, rx, ry)
	case "circle":
		rad := num("r", axisDiag)
		if !(rad > 0) {
			return nil
		}
		return ellipseOps(num("cx", axisX), num("cy", axisY), rad, rad)
	case "ellipse":
		rx, ry := num("rx", axisX), num("ry", axisY)
		// SVG 2, 10.4: an auto radius takes the other's value, and a
		// negative one is invalid, which leaves it auto.
		rxl, rxok := parseLength(attrOf(n, "rx"))
		ryl, ryok := parseLength(attrOf(n, "ry"))
		rxok = rxok && rxl.v >= 0
		ryok = ryok && ryl.v >= 0
		switch {
		case rxok && !ryok:
			ry = rx
		case ryok && !rxok:
			rx = ry
		}
		if !(rx > 0) || !(ry > 0) {
			return nil
		}
		return ellipseOps(num("cx", axisX), num("cy", axisY), rx, ry)
	case "line":
		return []compose.PathOp{
			{Kind: compose.MoveTo, P: [3]compose.Point{{X: num("x1", axisX), Y: num("y1", axisY)}}},
			{Kind: compose.LineTo, P: [3]compose.Point{{X: num("x2", axisX), Y: num("y2", axisY)}}},
		}
	case "polyline", "polygon":
		v, _ := parseNumberList(attrOf(n, "points"), 2*maxPathSegments)
		if len(v) < 4 {
			return nil
		}
		ops := make([]compose.PathOp, 0, len(v)/2+1)
		for i := 0; i+1 < len(v); i += 2 {
			k := compose.LineTo
			if i == 0 {
				k = compose.MoveTo
			}
			ops = append(ops, compose.PathOp{Kind: k, P: [3]compose.Point{{X: v[i], Y: v[i+1]}}})
		}
		if n.Data == "polygon" {
			ops = append(ops, compose.PathOp{Kind: compose.ClosePath})
		}
		return ops
	case "path":
		ops, ok := parsePath(attrOf(n, "d"))
		if !ok {
			if len(ops) >= maxPathSegments {
				r.d.warn("svg-limit", fmt.Sprintf("a path of more than %d segments was cut short", maxPathSegments))
			}
		}
		return trimTrailingMove(ops)
	}
	return nil
}

// trimTrailingMove drops moveto operators that end the path: they draw
// nothing and would leave an empty subpath.
func trimTrailingMove(ops []compose.PathOp) []compose.PathOp {
	for len(ops) > 0 && ops[len(ops)-1].Kind == compose.MoveTo {
		ops = ops[:len(ops)-1]
	}
	return ops
}

// ellipseOps returns the ellipse of centre (cx, cy) and radii rx, ry as
// four cubic Béziers, starting at the rightmost point and turning
// clockwise on screen, the direction SVG 2, 10.4 draws it in.
func ellipseOps(cx, cy, rx, ry float64) []compose.PathOp {
	kx, ky := rx*kappa, ry*kappa
	pt := func(x, y float64) compose.Point { return compose.Point{X: x, Y: y} }
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{pt(cx+rx, cy)}},
		{Kind: compose.CurveTo, P: [3]compose.Point{pt(cx+rx, cy+ky), pt(cx+kx, cy+ry), pt(cx, cy+ry)}},
		{Kind: compose.CurveTo, P: [3]compose.Point{pt(cx-kx, cy+ry), pt(cx-rx, cy+ky), pt(cx-rx, cy)}},
		{Kind: compose.CurveTo, P: [3]compose.Point{pt(cx-rx, cy-ky), pt(cx-kx, cy-ry), pt(cx, cy-ry)}},
		{Kind: compose.CurveTo, P: [3]compose.Point{pt(cx+kx, cy-ry), pt(cx+rx, cy-ky), pt(cx+rx, cy)}},
		{Kind: compose.ClosePath},
	}
}

// roundRect returns the rectangle at (x, y) of w by h with corners of
// radii rx and ry, drawn as SVG 1.1, 9.2 lays it out.
func roundRect(x, y, w, h, rx, ry float64) []compose.PathOp {
	kx, ky := rx*kappa, ry*kappa
	pt := func(px, py float64) compose.Point { return compose.Point{X: px, Y: py} }
	line := func(px, py float64) compose.PathOp {
		return compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{pt(px, py)}}
	}
	curve := func(a, b, c compose.Point) compose.PathOp {
		return compose.PathOp{Kind: compose.CurveTo, P: [3]compose.Point{a, b, c}}
	}
	r, b := x+w, y+h
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{pt(x+rx, y)}},
		line(r-rx, y),
		curve(pt(r-rx+kx, y), pt(r, y+ry-ky), pt(r, y+ry)),
		line(r, b-ry),
		curve(pt(r, b-ry+ky), pt(r-rx+kx, b), pt(r-rx, b)),
		line(x+rx, b),
		curve(pt(x+rx-kx, b), pt(x, b-ry+ky), pt(x, b-ry)),
		line(x, y+ry),
		curve(pt(x, y+ry-ky), pt(x+rx-kx, y), pt(x+rx, y)),
		{Kind: compose.ClosePath},
	}
}

// shape draws a basic shape or a path, styled st: its fill, then its
// stroke (SVG 1.1, 11.1). Opacity on a shape that paints only one of the
// two folds into its colour, which is exact; with both it opens a group.
func (r *renderer) shape(n *html.Node, st *style, s sink) {
	if !st.visible {
		return
	}
	ops := r.geometry(n, st)
	if len(ops) == 0 {
		return
	}
	box := opsBBox(ops)
	var fill *compose.Fill
	if n.Data != "line" {
		fill = r.fillOf(st, box)
	}
	stroke := r.strokeOf(st, box)
	if fill == nil && stroke == nil {
		return
	}
	if fill != nil && stroke != nil || st.clipPath != "" {
		inner, done := r.open(s, n, st, &box)
		defer done()
		s = inner
	} else {
		s.alpha *= st.opacity
	}
	if s.alpha <= 0 {
		return
	}
	fill = withAlpha(fill, s.alpha)
	stroke = strokeAlpha(stroke, s.alpha)
	if fill == nil && stroke == nil {
		return
	}
	var caps []compose.PathOp
	if stroke != nil && stroke.Cap != compose.ButtCap {
		ops, caps = zeroLengthCaps(ops, stroke)
	}
	if len(ops) > 0 {
		r.emit(s, &compose.Path{Ops: ops, Fill: fill, Stroke: stroke})
	}
	if len(caps) > 0 {
		r.emit(s, &compose.Path{Ops: caps, Fill: &compose.Fill{Color: stroke.Color}})
	}
}

// zeroLengthCaps splits the zero-length subpaths out of ops: SVG 1.1,
// 11.4 draws their round or square cap, a disc or an axis-aligned square
// of the stroke width centred on the point, which a PDF viewer may leave
// out. It returns ops without them and the caps' outlines.
func zeroLengthCaps(ops []compose.PathOp, s *compose.Stroke) ([]compose.PathOp, []compose.PathOp) {
	var keep, caps []compose.PathOp
	half := s.Width / 2
	for i := 0; i < len(ops); {
		j := i + 1
		for j < len(ops) && ops[j].Kind != compose.MoveTo {
			j++
		}
		sub := ops[i:j]
		p := sub[0].P[0]
		zero := sub[0].Kind == compose.MoveTo && len(sub) > 1
		for _, op := range sub[1:] {
			for _, q := range op.P[:pointsOf(op.Kind)] {
				zero = zero && q == p
			}
		}
		switch {
		case !zero:
			keep = append(keep, sub...)
		case s.Cap == compose.RoundCap:
			caps = append(caps, ellipseOps(p.X, p.Y, half, half)...)
		default:
			caps = append(caps, rectOps(p.X-half, p.Y-half, s.Width, s.Width)...)
		}
		i = j
	}
	return keep, caps
}

// withAlpha returns f with its colours' alpha multiplied by a, nil when
// nothing is left to paint.
func withAlpha(f *compose.Fill, a float64) *compose.Fill {
	if f == nil || a >= 1 {
		return f
	}
	c := *f
	if c.Shading == nil {
		c.Color.A *= a
		if c.Color.A <= 0 {
			return nil
		}
		return &c
	}
	sh := *c.Shading
	sh.Stops = append([]compose.ColorStop(nil), sh.Stops...)
	for i := range sh.Stops {
		sh.Stops[i].Color.A *= a
	}
	if !anyOpaque(sh.Stops) {
		return nil
	}
	c.Shading = &sh
	return &c
}

// strokeAlpha returns s with its colour's alpha multiplied by a, nil when
// nothing is left to paint.
func strokeAlpha(s *compose.Stroke, a float64) *compose.Stroke {
	if s == nil || a >= 1 {
		return s
	}
	c := *s
	c.Color.A *= a
	if c.Color.A <= 0 {
		return nil
	}
	return &c
}

// strokeOf returns the stroke of a shape styled st (SVG 1.1, 11.4), nil
// when it has none: no stroke paint, or a zero width.
func (r *renderer) strokeOf(st *style, box bbox) *compose.Stroke {
	if st.stroke.kind == paintNone || st.strokeOpacity <= 0 {
		return nil
	}
	w := st.strokeWidth.resolve(r.lc(st), axisDiag)
	if !(w > 0) {
		return nil
	}
	c, ok := r.flatPaint(st.stroke, st, box, "stroke")
	if !ok {
		return nil
	}
	c.A *= st.strokeOpacity
	if c.A <= 0 {
		return nil
	}
	s := &compose.Stroke{Color: c, Width: w, Cap: st.lineCap, Join: st.lineJoin, Miter: st.miterLimit}
	if len(st.dashArray) > 0 {
		dash := make([]float64, 0, 2*len(st.dashArray))
		sum := 0.0
		for _, l := range st.dashArray {
			v := l.resolve(r.lc(st), axisDiag)
			dash = append(dash, v)
			sum += v
		}
		// An odd count repeats to an even one, and a pattern of zero
		// length draws a solid line (SVG 1.1, 11.4).
		if len(dash)%2 == 1 {
			dash = append(dash, dash...)
		}
		if sum > 0 && finite(sum) {
			s.Dash = dash
			off := st.dashOffset.resolve(r.lc(st), axisDiag)
			period := 2 * sum
			if len(st.dashArray)%2 == 0 {
				period = sum
			}
			// The composition model takes a phase into the pattern, so a
			// negative offset wraps into one period.
			off = math.Mod(off, period)
			if off < 0 {
				off += period
			}
			s.DashPhase = off
		}
	}
	return s
}
