package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// maxExplosion caps how far a slice is pushed out of its pie, in percent
// of the radius. Anti-DoS guard, not spec-mandated: ECMA-376's explosion
// has no upper bound, and a pie that makes room for a slice pushed out
// four radii already draws at a fifth of its radius.
const maxExplosion = 400

// minHoleSize and maxHoleSize bound a doughnut's hole, in percent of its
// radius: ECMA-376's ST_HoleSize allows at most 90, and the floor keeps a
// hole visible.
const (
	minHoleSize = 10
	maxHoleSize = 90
)

// arc appends to ops the circular arc of centre (cx, cy) and radius r from
// angle a0 to a1, in radians counter-clockwise from the x axis with y up,
// as cubic Béziers of at most a quarter turn each.
func arc(ops []typeset.PathOp, cx, cy, r, a0, a1 float64) []typeset.PathOp {
	n := int(math.Ceil(math.Abs(a1-a0) / (math.Pi / 2)))
	n = max(n, 1)
	step := (a1 - a0) / float64(n)
	k := 4.0 / 3 * detmath.Tan(step/4)
	at := func(a float64) typeset.Point {
		return typeset.Point{X: cx + float64(r*detmath.Cos(a)), Y: cy - float64(r*detmath.Sin(a))}
	}
	for i := range n {
		s, e := a0+float64(float64(i)*step), a0+float64(float64(i+1)*step)
		p0, p3 := at(s), at(e)
		c1 := typeset.Point{X: p0.X - float64(k*r*detmath.Sin(s)), Y: p0.Y - float64(k*r*detmath.Cos(s))}
		c2 := typeset.Point{X: p3.X + float64(k*r*detmath.Sin(e)), Y: p3.Y + float64(k*r*detmath.Cos(e))}
		ops = append(ops, typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{c1, c2, p3}})
	}
	return ops
}

// sector returns the outline of the ring sector between radii r0 and r1
// and angles a0 and a1, a pie slice when r0 is 0.
func sector(cx, cy, r0, r1, a0, a1 float64) []typeset.PathOp {
	start := typeset.Point{X: cx + float64(r1*detmath.Cos(a0)), Y: cy - float64(r1*detmath.Sin(a0))}
	ops := []typeset.PathOp{{Kind: typeset.MoveTo, P: [3]typeset.Point{start}}}
	ops = arc(ops, cx, cy, r1, a0, a1)
	if r0 <= 0 {
		ops = append(ops, typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{{X: cx, Y: cy}}})
	} else {
		ops = append(ops, typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{{X: cx + float64(r0*detmath.Cos(a1)), Y: cy - float64(r0*detmath.Sin(a1))}}})
		ops = arc(ops, cx, cy, r0, a1, a0)
	}
	return append(ops, typeset.PathOp{Kind: typeset.ClosePath})
}

// pieLabel is a data label of a sector, laid out before the pie is sized.
type pieLabel struct {
	b      *block
	lb     Labels
	mid    float64
	r0, r1 float64
	cx, cy float64
	// tilt squeezes the label's offset from the centre vertically on a
	// three-dimensional pie, 0 on a flat one.
	tilt float64
}

// pies draws a pie or doughnut group into plot: the sectors of the first
// series, or a ring per series, clockwise from the first slice angle.
func (rd *renderer) pies(plot typeset.Rect) {
	g := rd.c.Groups[0]
	if len(g.Series) == 0 {
		return
	}
	series := g.Series
	if g.Kind == Pie {
		series = series[:1]
	}
	cx, cy := plot.X+float64(plot.Width/2), plot.Y+float64(plot.Height/2)
	r := math.Min(plot.Width, plot.Height) / 2
	maxExpl := 0.0
	for _, s := range series {
		maxExpl = math.Max(maxExpl, s.Explosion)
		if rd.officeLayout() {
			// Office shrinks the pie for the series' explosion alone
			// (officePie).
			continue
		}
		for _, p := range s.Points {
			if p.HasExplosion {
				maxExpl = math.Max(maxExpl, p.Explosion)
			}
		}
	}
	maxExpl = math.Max(0, math.Min(maxExpl, maxExplosion)) / 100
	// Labels placed outside take room from the pie.
	outside := false
	maxW, maxH := 0.0, 0.0
	for _, s := range series {
		for j := range rd.data(s.Values).Len() {
			lb := rd.labelsAt(g, s, j)
			if lb.Any() && lb.Placement == LabelOutsideEnd {
				outside = true
				if b := rd.measure(rd.labelText(g, s, j, 0, lb, 0), lb.Style, 0); b != nil {
					w, h := b.rotated()
					maxW, maxH = math.Max(maxW, w), math.Max(maxH, h)
				}
			}
		}
	}
	if outside && rd.fixed == nil {
		r = math.Min((plot.Width-2*maxW)/2, (plot.Height-2*maxH)/2) - labelSpacing
	}
	t := rd.pieView(g)
	if t != nil && rd.fixed == nil {
		// A tilted pie is an ellipse on its rim: its height is its
		// width times the tilt, plus the rim, so the plot's width bounds
		// it before its height does.
		w := plot.Width / 2
		if outside {
			w = float64((plot.Width-2*maxW)/2) - labelSpacing
		}
		r = math.Min(w, (float64(plot.Height/2)-maxH)/(t.tilt+float64(t.rim/2)))
	}
	r /= 1 + maxExpl
	if !above(r, 1) {
		return
	}
	hole := 0.0
	if g.Kind == Doughnut {
		hole = float64(r * math.Max(minHoleSize, math.Min(holeSize(g), maxHoleSize)) / 100)
	}
	first := g.FirstSliceAngle
	dir := -1.0
	if g.CounterClockwise {
		dir = 1
	}
	if rd.fixed != nil {
		// A placed pie fills its rectangle, less the room its most
		// exploded sector takes (chart2's PieChart::getMaxOffset).
		r = math.Min(plot.Width, plot.Height) / 2 / (1 + maxExpl)
		if t != nil {
			r = math.Min(plot.Width/2, plot.Height/2/(t.tilt+float64(t.rim/2))) / (1 + maxExpl)
		}
		if g.Kind == Doughnut {
			hole = float64(r * math.Max(minHoleSize, math.Min(holeSize(g), maxHoleSize)) / 100)
		}
	}
	if t != nil {
		// A three-dimensional pie turns by its view's rotY: its group has
		// no first slice angle (ECMA-376 Part 1, 21.2.2.140 pie3DChart).
		first += rd.c.View3D.RotY
		cy -= float64(t.rim * r / 2)
	}
	start := float64((90 - first) * math.Pi / 180)
	ringW := (r - hole) / float64(len(series))
	var labels []pieLabel
	var tops []typeset.Fragment
	for si, s := range series {
		vals := rd.data(s.Values)
		n := min(vals.Len(), MaxPoints)
		total := 0.0
		for j := range n {
			if v, ok := vals.number(j); ok {
				total += math.Abs(v)
			}
		}
		if total <= 0 {
			continue
		}
		r0 := hole + float64(float64(si)*ringW)
		r1 := r0 + ringW
		a := start
		for j := range n {
			v, ok := vals.number(j)
			if !ok || v == 0 {
				continue
			}
			sweep := float64(dir * math.Abs(v) / total * 2 * math.Pi)
			a0, a1 := a, a+sweep
			a = a1
			mid := (a0 + a1) / 2
			ex := s.Explosion
			if p := rd.point(s, j); p != nil && p.HasExplosion {
				ex = p.Explosion
			}
			ex = math.Max(0, math.Min(ex, maxExplosion)) / 100 * r
			if g.Kind == Doughnut && si != len(series)-1 {
				ex = 0
			}
			pcx, pcy := cx+float64(ex*detmath.Cos(mid)), cy-float64(ex*detmath.Sin(mid))
			lo, hi := math.Min(a0, a1), math.Max(a0, a1)
			ops := sector(pcx, pcy, r0, r1, hi, lo)
			if math.Abs(sweep) >= 2*math.Pi-1e-9 {
				ops = ellipse(pcx, pcy, r1, r1)
				if r0 > 0 {
					ops = append(ops, ellipse(pcx, pcy, r0, r0)...)
				}
			}
			fill := rd.fillOf(g, s, j)
			if t != nil {
				// The rims in front are drawn first, the tops over them.
				for _, rim := range t.rimOps(pcx, pcy, r1, lo, hi) {
					rd.add(paintPath(rim, shaded(fill, pieRim), rd.borderOf(g, s, j)))
				}
				ops = tiltOps(ops, pcy, t.tilt)
				mark := len(rd.out)
				rd.pieSector(ops, fill, rd.borderOf(g, s, j))
				tops = append(tops, rd.out[mark:]...)
				rd.out = rd.out[:mark]
			} else {
				rd.pieSector(ops, fill, rd.borderOf(g, s, j))
			}
			lb := rd.labelsAt(g, s, j)
			if !lb.Any() {
				continue
			}
			b := rd.measure(rd.labelText(g, s, j, v, lb, math.Abs(v)/total), lb.Style, 0)
			if b == nil {
				continue
			}
			l := pieLabel{b: b, lb: lb, mid: mid, r0: r0, r1: r1, cx: pcx, cy: pcy}
			if t != nil {
				l.tilt = t.tilt
			}
			labels = append(labels, l)
		}
	}
	rd.out = append(rd.out, tops...)
	for _, l := range labels {
		rd.placePieLabel(l)
	}
}

// pieSector paints a sector's outline ops with fill and border, its
// holes left empty.
func (rd *renderer) pieSector(ops []typeset.PathOp, fill *Paint, border *Stroke) {
	if fill != nil && !fill.None && fill.Shape != nil {
		rd.out = append(rd.out, shape.Fill(ops, opsBounds(ops), *fill.Shape)...)
	}
	if p := paintPath(ops, fill, border); p != nil {
		if p.Fill != nil {
			p.Fill.Rule = typeset.EvenOdd
		}
		rd.out = append(rd.out, p)
	}
}

// placePieLabel draws a sector's label: inside the sector, at the middle
// of its radius, when it fits or is asked there; else outside the edge
// with a leader line.
func (rd *renderer) placePieLabel(l pieLabel) {
	w, h := l.b.rotated()
	c, s := detmath.Cos(l.mid), detmath.Sin(l.mid)
	if l.tilt > 0 {
		s *= l.tilt
	}
	inside := l.lb.Placement != LabelOutsideEnd
	rm := (l.r0 + l.r1) / 2
	if l.r0 == 0 {
		rm = l.r1 * 0.6
	}
	if l.lb.Placement == LabelInsideEnd {
		rm = l.r1 - float64(math.Max(w, h)/2) - labelSpacing
	}
	if inside && l.lb.Placement != LabelCenter && l.lb.Placement != LabelInsideEnd {
		// Best fit: inside when the label's half diagonal fits the ring.
		if detmath.Hypot(w, h)/2 > float64((l.r1-l.r0)/2)+float64(l.r0*0.4) {
			inside = false
		}
	}
	if inside {
		rd.labelFrame(l.b, l.lb, l.cx+float64(rm*c), l.cy-float64(rm*s))
		return
	}
	d := l.r1 + labelSpacing
	ex, ey := l.cx+float64(d*c), l.cy-float64(d*s)
	lx, ly := ex+float64(c*w/2), ey-float64(s*h/2)
	rd.labelFrame(l.b, l.lb, lx, ly)
}

// radar draws a radar group into avail: a spoke per category clockwise
// from twelve o'clock, the value gridlines as polygons, each series a
// closed line or filled polygon.
func (rd *renderer) radar(avail typeset.Rect) {
	g := rd.c.Groups[0]
	x := &dim{ax: rd.c.axisAt(g.XAxis), category: true}
	y := &dim{ax: rd.c.axisAt(g.YAxis), zero: true, border: true}
	for _, s := range g.Series {
		cats := rd.data(s.Categories)
		x.n = max(x.n, rd.data(s.Values).Len(), cats.Len())
		if x.names == nil {
			for i := range cats.Len() {
				x.names = append(x.names, cats.label(i, rd.c.Locale))
			}
		}
	}
	x.n = min(x.n, MaxPoints)
	for len(x.names) < x.n {
		x.names = append(x.names, formatGeneral(float64(len(x.names)+1)))
	}
	if x.n < 1 {
		return
	}
	lo, hi, _ := rd.valueRange(g)
	if math.IsInf(lo, 0) {
		lo, hi = 0, 0
	}
	y.sc = autoScale(y.ax, lo, hi, scaleOptions{zero: true, border: true, maxCount: maxAutoTicks})
	st := x.style()
	maxW, maxH := 0.0, 0.0
	var names []*block
	for _, n := range x.names {
		b := rd.measure(n, st, avail.Width*0.3)
		names = append(names, b)
		if b != nil && x.labelled() {
			w, h := b.rotated()
			maxW, maxH = math.Max(maxW, w), math.Max(maxH, h)
		}
	}
	cx, cy := avail.X+float64(avail.Width/2), avail.Y+float64(avail.Height/2)
	r := math.Min(float64(avail.Width/2)-maxW-labelSpacing, float64(avail.Height/2)-maxH-labelSpacing)
	if !above(r, 1) {
		return
	}
	angle := func(i int) float64 { return math.Pi/2 - 2*math.Pi*float64(i)/float64(x.n) }
	at := func(i int, v float64) typeset.Point {
		f := math.Max(0, math.Min(1, y.sc.frac(v)))
		a := angle(i)
		return typeset.Point{X: cx + float64(r*f*detmath.Cos(a)), Y: cy - float64(r*f*detmath.Sin(a))}
	}
	if y.ax != nil && y.ax.MajorGrid != nil {
		for _, v := range y.sc.ticks() {
			var pts []typeset.Point
			for i := range x.n {
				pts = append(pts, at(i, v))
			}
			if p := paintPath(polygon(pts...), nil, y.ax.MajorGrid); p != nil {
				rd.out = append(rd.out, p)
			}
		}
	}
	var spoke *Stroke
	if x.ax != nil {
		spoke = x.ax.MajorGrid
		if spoke == nil {
			spoke = x.ax.Line
		}
	}
	for i := range x.n {
		p := at(i, y.sc.max)
		rd.add(line(cx, cy, p.X, p.Y, spoke))
	}
	sv := rd.stack(g)
	for i, s := range g.Series {
		var pts []typeset.Point
		var idx []int
		for j := 0; j < sv.n && j < x.n; j++ {
			v := sv.hi[i][j]
			if math.IsNaN(v) {
				continue
			}
			pts = append(pts, at(j, v))
			idx = append(idx, j)
		}
		if len(pts) == 0 {
			continue
		}
		if g.Filled {
			rd.paint(polygon(pts...), rd.fillOf(g, s, -1), rd.borderOf(g, s, -1))
		} else if st := rd.borderOf(g, s, -1).compose(); st != nil {
			ops := polygon(pts...)
			rd.out = append(rd.out, &typeset.Path{Ops: ops, Stroke: st})
			for k, p := range pts {
				rd.marker(rd.markerOf(g, s, idx[k]), p.X, p.Y)
			}
		}
	}
	if x.labelled() {
		for i, b := range names {
			if b == nil {
				continue
			}
			w, h := b.rotated()
			a := angle(i)
			d := r + labelSpacing
			lx := cx + float64(d*detmath.Cos(a)) + float64(detmath.Cos(a)*w/2)
			ly := cy - float64(d*detmath.Sin(a)) - float64(detmath.Sin(a)*h/2)
			rd.out = append(rd.out, b.draw(lx, ly, alignCenter)...)
		}
	}
	if y.labelled() {
		for _, v := range y.sc.ticks() {
			b := rd.measure(formatLabel(y.format, v, rd.c.Locale), y.style(), 0)
			if b == nil {
				continue
			}
			w, _ := b.rotated()
			p := at(0, v)
			rd.out = append(rd.out, b.draw(p.X-labelSpacing-float64(w/2), p.Y, alignRight)...)
		}
	}
}

// holeSize returns a doughnut's hole as a percentage of its diameter, 50
// by default.
func holeSize(g *Group) float64 {
	if g.HoleSize > 0 {
		return g.HoleSize
	}
	return 50
}
