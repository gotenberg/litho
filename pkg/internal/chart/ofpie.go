package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// The geometry of Office's pie of pie and bar of pie, in diameters of the
// first pie: the gap between the pie and a bar, and between the pie and a
// second pie. A bar is as high as a second pie would be across and half as
// wide.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: chart2/barOfPieChart.xlsx, gap width left to the
//     schema, second size 75%: Excel's pie is 193.6 pt across, its bar
//     48.35 pt right of it, 145.14 pt high and 72.57 pt wide, centred on
//     the pie.
//   - litho/xlsx: charts_pie_doughnut's bar of pie, gap width 100, second
//     size 75%: pie 99.26 pt across, bar 24.67 pt right of it, 74.35 by
//     37.17 pt.
//   - litho/pptx: chart-pie's bar of pie, second size 60%: pie 137.28 pt
//     across, bar 34.36 pt right of it, 41.04 pt wide; its pie of pie,
//     second size 75%: second pie 102.96 pt across, 68.58 pt right of the
//     first.
const (
	ofPieBarGap = 0.25
	ofPiePieGap = 0.5
)

// ofPieSecond reports which of the n points of s the group's [OfPie]
// sends to the second plot, nil when it sends none or all.
func (rd *renderer) ofPieSecond(o *OfPie, vals Data, n int) []bool {
	second := make([]bool, n)
	total := 0.0
	for j := range n {
		if v, ok := vals.number(j); ok {
			total += math.Abs(v)
		}
	}
	if o.Split == SplitCustom {
		for _, i := range o.Custom {
			if i >= 0 && i < n {
				second[i] = true
			}
		}
	}
	k := 0
	for j := range n {
		v, ok := vals.number(j)
		switch o.Split {
		case SplitPosition:
			at := math.Max(0, math.Min(math.Round(o.SplitAt), float64(n)))
			second[j] = float64(j) >= float64(n)-at
		case SplitValue:
			second[j] = ok && v < o.SplitAt
		case SplitPercent:
			second[j] = ok && total > 0 && float64(math.Abs(v)/total*100) < o.SplitAt
		case SplitCustom:
		}
		if second[j] {
			k++
		}
	}
	if k == 0 || k == n {
		return nil
	}
	return second
}

// ofPie draws a pie group g with an [OfPie] into plot, or reports false
// when its split leaves one plot empty and the group draws as a pie. The
// first pie shows the points that stay and one sector of the split points,
// centred at three o'clock, the others clockwise after it; the second plot
// stands right of it, centred on it, the split points from its top down
// in a bar or clockwise in a pie from where the first pie's first point
// starts. The whole is centred across plot, the pie as large as a pie of
// the chart would be, smaller when the whole would not fit across.
//
// # Corpus evidence
//
//   - litho/pptx: chart-pie's pie of pie, 15 of 107 split: the first
//     pie's sector of them runs from 64.8 to 115.2 degrees clockwise from
//     twelve o'clock, its first point from 115.2 degrees, and the second
//     pie's first point from 115.2 degrees as well; the series lines are
//     tangent to the second pie.
//   - litho/xlsx: charts_pie_doughnut's bar of pie, 750 of 2,700 split:
//     the sector runs from 40 to 140 degrees, its series lines from the
//     sector's ends to the bar's left corners, the bar's segments from the
//     top down in point order.
//   - libreoffice/xlsx: chart2/barOfPieChart.xlsx draws its sector of the
//     split points in the series' fill, the point having none of its own.
func (rd *renderer) ofPie(g *Group, plot, square typeset.Rect) bool {
	o := g.OfPie
	s := g.Series[0]
	vals := rd.data(s.Values)
	n := min(vals.Len(), MaxPoints)
	second := rd.ofPieSecond(o, vals, n)
	if second == nil {
		return false
	}
	total, other := 0.0, 0.0
	for j := range n {
		if v, ok := vals.number(j); ok {
			total += math.Abs(v)
			if second[j] {
				other += math.Abs(v)
			}
		}
	}
	if !(total > 0) || !(other > 0) {
		return false
	}
	size := math.Max(5, math.Min(o.SecondSize, 200)) / 100
	gap, width := ofPiePieGap, float64(1+ofPiePieGap)+size
	if o.Bar {
		gap, width = ofPieBarGap, float64(1+ofPieBarGap)+float64(size/2)
	}
	d := math.Min(square.Width, square.Height)
	d = math.Min(d, float64(plot.Width-2*officePieInset)/width)
	if !above(d, 2) {
		return true
	}
	r := float64(d / 2)
	left := plot.X + float64((plot.Width-float64(width*d))/2)
	cx, cy := left+r, square.Y+float64(square.Height/2)
	// Clockwise angles from twelve o'clock, in degrees.
	sweep := float64(other / total * 360)
	from, to := 90-float64(sweep/2), 90+float64(sweep/2)
	var labels []pieLabel
	sectorAt := func(cx, cy, r, a0, a1 float64, fill *Paint, border *Stroke) {
		lo, hi := float64((90-a1)*math.Pi/180), float64((90-a0)*math.Pi/180)
		ops := sector(cx, cy, 0, r, hi, lo)
		if a1-a0 >= 360-1e-9 {
			ops = ellipse(cx, cy, r, r)
		}
		rd.pieSector(ops, fill, border)
	}
	label := func(j int, v, cx, cy, r, a0, a1 float64) {
		lb := rd.labelsAt(g, s, j)
		if !lb.Any() {
			return
		}
		if b := rd.measure(rd.labelText(g, s, j, v, lb, math.Abs(v)/total), lb.Style, 0); b != nil {
			labels = append(labels, pieLabel{b: b, lb: lb, mid: float64((90 - float64((a0+a1)/2)) * math.Pi / 180), r1: r, cx: cx, cy: cy})
		}
	}
	// The sector of the split points.
	fill := s.Fill
	if p := rd.point(s, n); p != nil && p.Fill != nil {
		fill = p.Fill
	} else if fill == nil {
		fill = &Paint{Color: rd.c.AutoColor(n, n+1)}
	}
	sectorAt(cx, cy, r, from, to, fill, rd.borderOf(g, s, n))
	label(n, other, cx, cy, r, from, to)
	a := to
	for j := range n {
		v, ok := vals.number(j)
		if !ok || v == 0 || second[j] {
			continue
		}
		a1 := a + float64(math.Abs(v)/total*360)
		sectorAt(cx, cy, r, a, a1, rd.fillOf(g, s, j), rd.borderOf(g, s, j))
		label(j, v, cx, cy, r, a, a1)
		a = a1
	}
	ends := [2]typeset.Point{clockPoint(cx, cy, r, from), clockPoint(cx, cy, r, to)}
	x0 := cx + r + float64(gap*d)
	if o.Bar {
		h := float64(size * d)
		w := float64(h / 2)
		y := cy - float64(h/2)
		for j := range n {
			v, ok := vals.number(j)
			if !ok || v == 0 || !second[j] {
				continue
			}
			seg := typeset.Rect{X: x0, Y: y, Width: w, Height: float64(math.Abs(v) / other * h)}
			rd.paint(rectPath(seg).Ops, rd.fillOf(g, s, j), rd.borderOf(g, s, j))
			if lb := rd.labelsAt(g, s, j); lb.Any() {
				if b := rd.measure(rd.labelText(g, s, j, v, lb, math.Abs(v)/total), lb.Style, 0); b != nil {
					bw, _ := b.rotated()
					rd.labelFrame(b, lb, x0+w+labelSpacing+float64(bw/2), seg.Y+float64(seg.Height/2))
				}
			}
			y += seg.Height
		}
		if st := o.Lines.compose(); st != nil {
			rd.out = append(rd.out, &typeset.Path{Stroke: st, Ops: []typeset.PathOp{
				{Kind: typeset.MoveTo, P: [3]typeset.Point{ends[0]}},
				{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x0, Y: cy - float64(h/2)}}},
				{Kind: typeset.MoveTo, P: [3]typeset.Point{ends[1]}},
				{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x0, Y: cy + float64(h/2)}}},
			}})
		}
	} else {
		r2 := float64(size * r)
		cx2 := x0 + r2
		a := to
		for j := range n {
			v, ok := vals.number(j)
			if !ok || v == 0 || !second[j] {
				continue
			}
			a1 := a + float64(math.Abs(v)/other*360)
			sectorAt(cx2, cy, r2, a, a1, rd.fillOf(g, s, j), rd.borderOf(g, s, j))
			label(j, v, cx2, cy, r2, a, a1)
			a = a1
		}
		if st := o.Lines.compose(); st != nil {
			var ops []typeset.PathOp
			for i, e := range ends {
				t, ok := tangent(e, cx2, cy, r2, i == 0)
				if !ok {
					continue
				}
				ops = append(ops, typeset.PathOp{Kind: typeset.MoveTo, P: [3]typeset.Point{e}}, typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{t}})
			}
			if len(ops) > 0 {
				rd.out = append(rd.out, &typeset.Path{Stroke: st, Ops: ops})
			}
		}
	}
	for _, l := range labels {
		rd.placePieLabel(l)
	}
	return true
}

// clockPoint returns the point of the circle of centre (cx, cy) and radius
// r at a degrees clockwise from twelve o'clock.
func clockPoint(cx, cy, r, a float64) typeset.Point {
	s, c := detmath.SincosDegrees(a)
	return typeset.Point{X: cx + float64(r*s), Y: cy - float64(r*c)}
}

// tangent returns the point where a line from e touches the circle of
// centre (cx, cy) and radius r: the upper of the two touching points when
// upper is set, else the lower; false when e lies inside the circle.
func tangent(e typeset.Point, cx, cy, r float64, upper bool) (typeset.Point, bool) {
	dx, dy := e.X-cx, e.Y-cy
	dist := detmath.Hypot(dx, dy)
	if !above(dist, r) {
		return typeset.Point{}, false
	}
	base := detmath.Atan2(dy, dx)
	off := detmath.Acos(r / dist)
	p := func(a float64) typeset.Point {
		s, c := detmath.Sincos(a)
		return typeset.Point{X: cx + float64(r*c), Y: cy + float64(r*s)}
	}
	t1, t2 := p(base+off), p(base-off)
	if (t1.Y < t2.Y) == upper {
		return t1, true
	}
	return t2, true
}
