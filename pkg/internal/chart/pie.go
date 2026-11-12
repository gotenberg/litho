package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// arc appends to ops the circular arc of centre (cx, cy) and radius r from
// angle a0 to a1, in radians counter-clockwise from the x axis with y up,
// as cubic Béziers of at most a quarter turn each.
func arc(ops []compose.PathOp, cx, cy, r, a0, a1 float64) []compose.PathOp {
	n := int(math.Ceil(math.Abs(a1-a0) / (math.Pi / 2)))
	n = max(n, 1)
	step := (a1 - a0) / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	at := func(a float64) compose.Point { return compose.Point{X: cx + r*math.Cos(a), Y: cy - r*math.Sin(a)} }
	for i := range n {
		s, e := a0+float64(i)*step, a0+float64(i+1)*step
		p0, p3 := at(s), at(e)
		c1 := compose.Point{X: p0.X - k*r*math.Sin(s), Y: p0.Y - k*r*math.Cos(s)}
		c2 := compose.Point{X: p3.X + k*r*math.Sin(e), Y: p3.Y + k*r*math.Cos(e)}
		ops = append(ops, compose.PathOp{Kind: compose.CurveTo, P: [3]compose.Point{c1, c2, p3}})
	}
	return ops
}

// sector returns the outline of the ring sector between radii r0 and r1
// and angles a0 and a1, a pie slice when r0 is 0.
func sector(cx, cy, r0, r1, a0, a1 float64) []compose.PathOp {
	start := compose.Point{X: cx + r1*math.Cos(a0), Y: cy - r1*math.Sin(a0)}
	ops := []compose.PathOp{{Kind: compose.MoveTo, P: [3]compose.Point{start}}}
	ops = arc(ops, cx, cy, r1, a0, a1)
	if r0 <= 0 {
		ops = append(ops, compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{{X: cx, Y: cy}}})
	} else {
		ops = append(ops, compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{{X: cx + r0*math.Cos(a1), Y: cy - r0*math.Sin(a1)}}})
		ops = arc(ops, cx, cy, r0, a1, a0)
	}
	return append(ops, compose.PathOp{Kind: compose.ClosePath})
}

// pieLabel is a data label of a sector, laid out before the pie is sized.
type pieLabel struct {
	b      *block
	lb     Labels
	mid    float64
	r0, r1 float64
	cx, cy float64
}

// pies draws a pie or doughnut group into plot: the sectors of the first
// series, or a ring per series, clockwise from the first slice angle.
func (rd *renderer) pies(plot compose.Rect) {
	g := rd.c.Groups[0]
	if len(g.Series) == 0 {
		return
	}
	series := g.Series
	if g.Kind == Pie {
		series = series[:1]
	}
	cx, cy := plot.X+plot.Width/2, plot.Y+plot.Height/2
	r := math.Min(plot.Width, plot.Height) / 2
	maxExpl := 0.0
	for _, s := range series {
		maxExpl = math.Max(maxExpl, s.Explosion)
		for _, p := range s.Points {
			if p.HasExplosion {
				maxExpl = math.Max(maxExpl, p.Explosion)
			}
		}
	}
	maxExpl = math.Max(0, math.Min(maxExpl, 400)) / 100
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
	r /= 1 + maxExpl
	if !(r > 1) {
		return
	}
	hole := 0.0
	if g.Kind == Doughnut {
		hole = r * math.Max(10, math.Min(holeSize(g), 90)) / 100
	}
	start := (90 - g.FirstSliceAngle) * math.Pi / 180
	dir := -1.0
	if g.CounterClockwise {
		dir = 1
	}
	if rd.fixed != nil {
		// A placed pie fills its rectangle, less the room its most
		// exploded sector takes (chart2's PieChart::getMaxOffset).
		r = math.Min(plot.Width, plot.Height) / 2 / (1 + maxExpl)
		if g.Kind == Doughnut {
			hole = r * math.Max(10, math.Min(holeSize(g), 90)) / 100
		}
	}
	ringW := (r - hole) / float64(len(series))
	var labels []pieLabel
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
		r0 := hole + float64(si)*ringW
		r1 := r0 + ringW
		a := start
		for j := range n {
			v, ok := vals.number(j)
			if !ok || v == 0 {
				continue
			}
			sweep := dir * math.Abs(v) / total * 2 * math.Pi
			a0, a1 := a, a+sweep
			a = a1
			mid := (a0 + a1) / 2
			ex := s.Explosion
			if p := rd.point(s, j); p != nil && p.HasExplosion {
				ex = p.Explosion
			}
			ex = math.Max(0, math.Min(ex, 400)) / 100 * r
			if g.Kind == Doughnut && si != len(series)-1 {
				ex = 0
			}
			pcx, pcy := cx+ex*math.Cos(mid), cy-ex*math.Sin(mid)
			lo, hi := math.Min(a0, a1), math.Max(a0, a1)
			ops := sector(pcx, pcy, r0, r1, hi, lo)
			if math.Abs(sweep) >= 2*math.Pi-1e-9 {
				ops = ellipse(pcx, pcy, r1, r1)
				if r0 > 0 {
					ops = append(ops, ellipse(pcx, pcy, r0, r0)...)
				}
			}
			fill := rd.fillOf(g, s, j)
			if fill != nil && !fill.None && fill.Shape != nil {
				rd.out = append(rd.out, shape.Fill(ops, opsBounds(ops), *fill.Shape)...)
			}
			p := paintPath(ops, fill, rd.borderOf(g, s, j))
			if p != nil {
				if p.Fill != nil {
					p.Fill.Rule = compose.EvenOdd
				}
				rd.out = append(rd.out, p)
			}
			lb := rd.labelsAt(g, s, j)
			if !lb.Any() {
				continue
			}
			b := rd.measure(rd.labelText(g, s, j, v, lb, math.Abs(v)/total), lb.Style, 0)
			if b == nil {
				continue
			}
			labels = append(labels, pieLabel{b: b, lb: lb, mid: mid, r0: r0, r1: r1, cx: pcx, cy: pcy})
		}
	}
	for _, l := range labels {
		rd.placePieLabel(l)
	}
}

// placePieLabel draws a sector's label: inside the sector, at the middle
// of its radius, when it fits or is asked there; else outside the edge
// with a leader line.
func (rd *renderer) placePieLabel(l pieLabel) {
	w, h := l.b.rotated()
	c, s := math.Cos(l.mid), math.Sin(l.mid)
	inside := l.lb.Placement != LabelOutsideEnd
	rm := (l.r0 + l.r1) / 2
	if l.r0 == 0 {
		rm = l.r1 * 0.6
	}
	if l.lb.Placement == LabelInsideEnd {
		rm = l.r1 - math.Max(w, h)/2 - labelSpacing
	}
	if inside && l.lb.Placement != LabelCenter && l.lb.Placement != LabelInsideEnd {
		// Best fit: inside when the label's half diagonal fits the ring.
		if math.Hypot(w, h)/2 > (l.r1-l.r0)/2+l.r0*0.4 {
			inside = false
		}
	}
	if inside {
		rd.labelFrame(l.b, l.lb, l.cx+rm*c, l.cy-rm*s)
		return
	}
	d := l.r1 + labelSpacing
	ex, ey := l.cx+d*c, l.cy-d*s
	lx, ly := ex+c*w/2, ey-s*h/2
	rd.labelFrame(l.b, l.lb, lx, ly)
}

// radar draws a radar group into avail: a spoke per category clockwise
// from twelve o'clock, the value gridlines as polygons, each series a
// closed line or filled polygon.
func (rd *renderer) radar(avail compose.Rect) {
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
	cx, cy := avail.X+avail.Width/2, avail.Y+avail.Height/2
	r := math.Min(avail.Width/2-maxW-labelSpacing, avail.Height/2-maxH-labelSpacing)
	if !(r > 1) {
		return
	}
	angle := func(i int) float64 { return math.Pi/2 - 2*math.Pi*float64(i)/float64(x.n) }
	at := func(i int, v float64) compose.Point {
		f := math.Max(0, math.Min(1, y.sc.frac(v)))
		a := angle(i)
		return compose.Point{X: cx + r*f*math.Cos(a), Y: cy - r*f*math.Sin(a)}
	}
	if y.ax != nil && y.ax.MajorGrid != nil {
		for _, v := range y.sc.ticks() {
			var pts []compose.Point
			for i := range x.n {
				pts = append(pts, at(i, v))
			}
			if p := paintPath(polygon(pts...), nil, y.ax.MajorGrid); p != nil {
				rd.out = append(rd.out, p)
			}
		}
	}
	spoke := (*Stroke)(nil)
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
		var pts []compose.Point
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
			rd.out = append(rd.out, &compose.Path{Ops: ops, Stroke: st})
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
			lx := cx + d*math.Cos(a) + math.Cos(a)*w/2
			ly := cy - d*math.Sin(a) - math.Sin(a)*h/2
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
			rd.out = append(rd.out, b.draw(p.X-labelSpacing-w/2, p.Y, alignRight)...)
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
