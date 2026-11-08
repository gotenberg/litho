package chart

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// stackedValues are a group's plotted values: for series i and category
// j, the value's start and end on the value axis. A NaN start stands on
// the category axis; a NaN end is a gap.
type stackedValues struct {
	lo, hi [][]float64
	// raw are the series' values as the file gives them.
	raw [][]float64
	n   int
}

// stack resolves the plotted values of a category group.
func (rd *renderer) stack(g *Group) stackedValues {
	var sv stackedValues
	for _, s := range g.Series {
		sv.n = max(sv.n, rd.data(s.Values).Len())
	}
	sv.n = min(sv.n, MaxPoints)
	for _, s := range g.Series {
		vals := rd.data(s.Values)
		raw := make([]float64, sv.n)
		for j := range raw {
			if v, ok := vals.number(j); ok {
				raw[j] = v
			} else {
				raw[j] = math.NaN()
			}
		}
		sv.raw = append(sv.raw, raw)
	}
	sums := make([]float64, sv.n)
	if g.Grouping == PercentStacked {
		for _, raw := range sv.raw {
			for j, v := range raw {
				if !math.IsNaN(v) {
					sums[j] += math.Abs(v)
				}
			}
		}
	}
	pos, neg := make([]float64, sv.n), make([]float64, sv.n)
	separate := g.Kind == Bar
	for _, raw := range sv.raw {
		lo, hi := make([]float64, sv.n), make([]float64, sv.n)
		for j, v := range raw {
			if g.Grouping == Clustered || math.IsNaN(v) {
				lo[j], hi[j] = math.NaN(), v
				if g.Grouping != Clustered && math.IsNaN(v) {
					// A gap in a stack keeps the running total.
					lo[j], hi[j] = math.NaN(), math.NaN()
				}
				continue
			}
			if g.Grouping == PercentStacked {
				if sums[j] == 0 {
					v = 0
				} else {
					v /= sums[j]
				}
			}
			acc := &pos[j]
			if separate && v < 0 {
				acc = &neg[j]
			}
			lo[j], hi[j] = *acc, *acc+v
			*acc += v
		}
		sv.lo = append(sv.lo, lo)
		sv.hi = append(sv.hi, hi)
	}
	return sv
}

// valueRange returns the range of the values g plots and whether it is
// percent-stacked.
func (rd *renderer) valueRange(g *Group) (float64, float64, bool) {
	lo, hi := math.Inf(1), math.Inf(-1)
	if g.Kind == Scatter || g.Kind == Bubble {
		for _, s := range g.Series {
			vals := rd.data(s.Values)
			for j := range min(vals.Len(), MaxPoints) {
				if v, ok := vals.number(j); ok {
					lo, hi = math.Min(lo, v), math.Max(hi, v)
				}
			}
		}
		return lo, hi, false
	}
	sv := rd.stack(g)
	for i := range sv.hi {
		for j, v := range sv.hi[i] {
			if math.IsNaN(v) {
				continue
			}
			lo, hi = math.Min(lo, v), math.Max(hi, v)
			if b := sv.lo[i][j]; !math.IsNaN(b) {
				lo, hi = math.Min(lo, b), math.Max(hi, b)
			}
		}
	}
	return lo, hi, g.Grouping == PercentStacked
}

// series draws g's series against x and y, and returns the drawing of
// their data labels, which go over every series.
func (rd *renderer) series(g *Group, x, y *dim) []func() {
	switch g.Kind {
	case Scatter, Bubble:
		return rd.scatter(g, x, y)
	case Bar:
		return rd.bars(g, x, y)
	case Area:
		return rd.areas(g, x, y)
	case Line, Pie, Doughnut, Radar:
	}
	return rd.lines(g, x, y)
}

// point maps a category position and a value to the page.
func point(x, y *dim, cat, v float64) compose.Point {
	cp, vp := x.valuePixel(cat), y.valuePixel(v)
	if x.vertical {
		return compose.Point{X: vp, Y: cp}
	}
	return compose.Point{X: cp, Y: vp}
}

// bars draws a bar group: per category, the series side by side with the
// group's gap and overlap, or stacked in one bar (ECMA-376 Part 1,
// 21.2.2.75 gapWidth and 21.2.2.131 overlap, as Office and LibreOffice
// size them).
func (rd *renderer) bars(g *Group, x, y *dim) []func() {
	sv := rd.stack(g)
	k := len(g.Series)
	if g.Grouping != Clustered {
		k = 1
	}
	if k == 0 {
		return nil
	}
	gap := math.Max(0, math.Min(g.GapWidth, 500)) / 100
	ov := math.Max(-1, math.Min(g.Overlap, 100)/100)
	if g.Grouping != Clustered {
		ov = 1
	}
	catW := x.length() / float64(max(x.n, 1))
	if !x.between && x.n > 1 {
		catW = x.length() / float64(x.n-1)
	}
	barW := catW / (float64(k) - float64(k-1)*ov + gap)
	step := barW * (1 - ov)
	total := barW + float64(k-1)*step
	base := y.sc.clamp(x.crossValue())
	if y.sc.log > 0 {
		base = y.sc.min
	}
	var labels []func()
	for i, s := range g.Series {
		slot := i
		if g.Grouping != Clustered {
			slot = 0
		}
		for j := 0; j < sv.n && j < x.n; j++ {
			v := sv.hi[i][j]
			if math.IsNaN(v) {
				continue
			}
			b := sv.lo[i][j]
			if math.IsNaN(b) {
				b = base
			}
			c := x.valuePixel(float64(j))
			sign := 1.0
			if x.ax != nil && x.ax.Reverse {
				sign = -1
			}
			if x.vertical {
				sign = -sign
			}
			p0 := c + sign*(-total/2+float64(slot)*step)
			p1 := p0 + sign*barW
			v0, v1 := y.valuePixel(y.sc.clamp(b)), y.valuePixel(y.sc.clamp(v))
			var r compose.Rect
			if x.vertical {
				r = compose.Rect{X: math.Min(v0, v1), Y: math.Min(p0, p1), Width: math.Abs(v1 - v0), Height: math.Abs(p1 - p0)}
			} else {
				r = compose.Rect{X: math.Min(p0, p1), Y: math.Min(v0, v1), Width: math.Abs(p1 - p0), Height: math.Abs(v1 - v0)}
			}
			rd.paint(rectPath(r).Ops, rd.fillOf(g, s, j), rd.borderOf(g, s, j))
			lb := mergeLabels(g.Labels, s.labelsOf(j))
			if lb.Any() {
				raw, neg := sv.raw[i][j], v < b
				stacked := g.Grouping != Clustered
				labels = append(labels, func() { rd.barLabel(g, s, j, raw, lb, r, x.vertical, neg, stacked) })
			}
		}
	}
	return labels
}

// mergeLabels returns s, or the group's labels when the series sets none.
func mergeLabels(group, s Labels) Labels {
	if s.Any() {
		return s
	}
	if group.Any() {
		return group
	}
	return s
}

// barLabel draws the data label of a bar r.
func (rd *renderer) barLabel(g *Group, s *Series, j int, v float64, lb Labels, r compose.Rect, horizontal, neg, stacked bool) {
	b := rd.measure(rd.labelText(g, s, j, v, lb, 0), lb.Style, 0)
	if b == nil {
		return
	}
	w, h := b.rotated()
	pl := lb.Placement
	switch pl {
	case LabelAuto, LabelBestFit:
		pl = LabelOutsideEnd
		if stacked {
			pl = LabelCenter
		}
	case LabelAbove, LabelRight:
		// Above a bar is past its end, as LibreOffice places a bar's "top"
		// label.
		pl = LabelOutsideEnd
	case LabelBelow, LabelLeft:
		pl = LabelInsideBase
	case LabelCenter, LabelInsideEnd, LabelInsideBase, LabelOutsideEnd:
	}
	cx, cy := r.X+r.Width/2, r.Y+r.Height/2
	end := func(inside bool) {
		d := labelSpacing
		if inside {
			d = -d
		}
		if horizontal {
			if neg {
				cx = r.X - d - w/2
				if inside {
					cx = r.X - d + w/2
				}
			} else {
				cx = r.X + r.Width + d + w/2
				if inside {
					cx = r.X + r.Width + d - w/2
				}
			}
			return
		}
		if neg {
			cy = r.Y + r.Height + d + h/2
			if inside {
				cy = r.Y + r.Height + d - h/2
			}
		} else {
			cy = r.Y - d - h/2
			if inside {
				cy = r.Y - d + h/2
			}
		}
	}
	switch pl {
	case LabelOutsideEnd:
		end(false)
	case LabelInsideEnd:
		end(true)
	case LabelInsideBase:
		if horizontal {
			cx = r.X + labelSpacing + w/2
		} else {
			cy = r.Y + r.Height - labelSpacing - h/2
		}
	case LabelAuto, LabelCenter, LabelAbove, LabelBelow, LabelLeft, LabelRight, LabelBestFit:
	}
	rd.labelFrame(b, lb, cx, cy)
}

// labelFrame draws a data label b centred on (cx, cy) with its frame.
func (rd *renderer) labelFrame(b *block, lb Labels, cx, cy float64) {
	w, h := b.rotated()
	if lb.Frame.Fill != nil || lb.Frame.Border != nil {
		pad := 0.18 * b.style.Size
		rd.frame(compose.Rect{X: cx - w/2 - pad, Y: cy - h/2, Width: w + 2*pad, Height: h}, lb.Frame)
	}
	rd.out = append(rd.out, b.draw(cx, cy, alignCenter)...)
}

// labelText composes a data label's text from its parts: the series
// name, the category, the value and the percentage, joined by the
// separator.
func (rd *renderer) labelText(g *Group, s *Series, j int, v float64, lb Labels, pct float64) string {
	var parts []string
	if lb.ShowSeries && s.Name != "" {
		parts = append(parts, s.Name)
	}
	if lb.ShowCategory {
		cats := rd.data(s.Categories)
		name := cats.label(j)
		if name == "" {
			name = formatGeneral(float64(j + 1))
		}
		parts = append(parts, name)
	}
	if lb.ShowValue && !math.IsNaN(v) {
		f := lb.Format
		if f == "" {
			f = rd.data(s.Values).Format
		}
		parts = append(parts, formatLabel(f, v))
	}
	if lb.ShowPercent && !math.IsNaN(pct) && (g.Kind == Pie || g.Kind == Doughnut || g.Grouping == PercentStacked) {
		f := lb.PercentFormat
		if f == "" {
			f = "0%"
			if !lb.ShowValue && strings.Contains(lb.Format, "%") {
				f = lb.Format
			}
		}
		parts = append(parts, formatLabel(f, pct))
	}
	sep := lb.Separator
	if sep == "" {
		// A percentage without the value goes on a line of its own, as
		// Office and LibreOffice write it; other parts take a semicolon.
		sep = "; "
		if lb.ShowPercent && !lb.ShowValue {
			sep = "\n"
		}
	}
	return strings.Join(parts, sep)
}

// polyline returns the open path through pts, smoothed as a natural
// cubic spline through them when smooth is set, each coordinate a spline of
// the point's index, as LibreOffice smooths lines.
func polyline(pts []compose.Point, smooth bool) []compose.PathOp {
	if len(pts) == 0 {
		return nil
	}
	ops := []compose.PathOp{{Kind: compose.MoveTo, P: [3]compose.Point{pts[0]}}}
	if !smooth || len(pts) < 3 {
		for _, p := range pts[1:] {
			ops = append(ops, compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{p}})
		}
		return ops
	}
	xs, ys := make([]float64, len(pts)), make([]float64, len(pts))
	for i, p := range pts {
		xs[i], ys[i] = p.X, p.Y
	}
	dx, dy := splineSlopes(xs), splineSlopes(ys)
	for i := 1; i < len(pts); i++ {
		p0, p1 := pts[i-1], pts[i]
		c1 := compose.Point{X: p0.X + dx[i-1]/3, Y: p0.Y + dy[i-1]/3}
		c2 := compose.Point{X: p1.X - dx[i]/3, Y: p1.Y - dy[i]/3}
		ops = append(ops, compose.PathOp{Kind: compose.CurveTo, P: [3]compose.Point{c1, c2, p1}})
	}
	return ops
}

// splineSlopes returns the first derivatives, per unit step, of the
// natural cubic spline through v at the knots 0, 1, ... (second
// derivatives zero at both ends), by the tridiagonal algorithm.
func splineSlopes(v []float64) []float64 {
	n := len(v)
	// Solve for the second derivatives m of the natural spline:
	// m[i-1] + 4 m[i] + m[i+1] = 6 (v[i+1] - 2 v[i] + v[i-1]).
	m := make([]float64, n)
	c := make([]float64, n)
	d := make([]float64, n)
	for i := 1; i < n-1; i++ {
		r := 6 * (v[i+1] - 2*v[i] + v[i-1])
		den := 4.0
		if i > 1 {
			den -= c[i-1]
			r -= d[i-1]
		}
		c[i] = 1 / den
		d[i] = r / den
	}
	for i := n - 2; i >= 1; i-- {
		m[i] = d[i] - c[i]*m[i+1]
	}
	out := make([]float64, n)
	for i := 0; i < n-1; i++ {
		out[i] = v[i+1] - v[i] - (2*m[i]+m[i+1])/6
	}
	out[n-1] = v[n-1] - v[n-2] + (m[n-2]+2*m[n-1])/6
	return out
}

// lines draws a line group: each series a line through its points, split
// at gaps, with a marker at each point.
func (rd *renderer) lines(g *Group, x, y *dim) []func() {
	sv := rd.stack(g)
	var labels []func()
	for i, s := range g.Series {
		var runs [][]compose.Point
		var cur []compose.Point
		var pts []compose.Point
		var idx []int
		for j := 0; j < sv.n && j < x.n; j++ {
			v := sv.hi[i][j]
			if math.IsNaN(v) {
				if len(cur) > 0 {
					runs = append(runs, cur)
					cur = nil
				}
				continue
			}
			p := point(x, y, float64(j), y.sc.clamp(v))
			cur = append(cur, p)
			pts = append(pts, p)
			idx = append(idx, j)
		}
		if len(cur) > 0 {
			runs = append(runs, cur)
		}
		if st := rd.borderOf(g, s, -1).compose(); st != nil {
			st.Join = compose.RoundJoin
			st.Cap = compose.RoundCap
			for _, run := range runs {
				if len(run) > 1 {
					rd.out = append(rd.out, &compose.Path{Ops: polyline(run, s.Smooth), Stroke: st})
				}
			}
		}
		for k, p := range pts {
			rd.marker(rd.markerOf(g, s, idx[k]), p.X, p.Y)
		}
		for k, p := range pts {
			j := idx[k]
			lb := mergeLabels(g.Labels, s.labelsOf(j))
			if !lb.Any() {
				continue
			}
			raw := sv.raw[i][j]
			m := rd.markerOf(g, s, j)
			labels = append(labels, func() { rd.pointLabel(g, s, j, raw, lb, p, m.Size) })
		}
	}
	return labels
}

// pointLabel draws the data label of a line or scatter point p, beside a
// marker of size ms.
func (rd *renderer) pointLabel(g *Group, s *Series, j int, v float64, lb Labels, p compose.Point, ms float64) {
	b := rd.measure(rd.labelText(g, s, j, v, lb, math.NaN()), lb.Style, 0)
	if b == nil {
		return
	}
	w, h := b.rotated()
	d := labelSpacing + ms/2
	cx, cy := p.X, p.Y
	switch lb.Placement {
	case LabelCenter:
	case LabelBelow:
		cy += d + h/2
	case LabelLeft:
		cx -= d + w/2
	case LabelRight:
		cx += d + w/2
	case LabelAuto, LabelInsideEnd, LabelInsideBase, LabelOutsideEnd, LabelAbove, LabelBestFit:
		cy -= d + h/2
	}
	rd.labelFrame(b, lb, cx, cy)
}

// areas draws an area group: each series filled between its line and the
// series under it, or the category axis.
func (rd *renderer) areas(g *Group, x, y *dim) []func() {
	sv := rd.stack(g)
	base := y.sc.clamp(x.crossValue())
	var labels []func()
	// The last series is drawn first, at the back, so the first stays in
	// front (chart2's AreaChart reverses a two-dimensional area chart's
	// series, ReverseSeriesOrderAreaAndNetChart).
	for i := len(g.Series) - 1; i >= 0; i-- {
		s := g.Series[i]
		var top, bottom []compose.Point
		for j := 0; j < sv.n && j < x.n; j++ {
			v := sv.hi[i][j]
			b := sv.lo[i][j]
			if math.IsNaN(v) {
				v = 0
				if g.Grouping != Clustered && i > 0 {
					v = sv.hi[i-1][j]
					if math.IsNaN(v) {
						v = 0
					}
				}
			}
			if math.IsNaN(b) {
				b = base
			}
			top = append(top, point(x, y, float64(j), y.sc.clamp(v)))
			bottom = append(bottom, point(x, y, float64(j), y.sc.clamp(b)))
		}
		if len(top) == 0 {
			continue
		}
		if len(top) == 1 {
			// One point spreads over its category.
			continue
		}
		var pts []compose.Point
		pts = append(pts, top...)
		for k := len(bottom) - 1; k >= 0; k-- {
			pts = append(pts, bottom[k])
		}
		rd.paint(polygon(pts...), rd.fillOf(g, s, -1), rd.borderOf(g, s, -1))
		for j := 0; j < len(top); j++ {
			lb := mergeLabels(g.Labels, s.labelsOf(j))
			if !lb.Any() || math.IsNaN(sv.raw[i][j]) {
				continue
			}
			raw := sv.raw[i][j]
			p := compose.Point{X: (top[j].X + bottom[j].X) / 2, Y: (top[j].Y + bottom[j].Y) / 2}
			labels = append(labels, func() {
				lb2 := lb
				lb2.Placement = LabelCenter
				rd.pointLabel(g, s, j, raw, lb2, p, 0)
			})
		}
	}
	return labels
}

// scatter draws a scatter or bubble group: each point at its x and y
// values, joined by the series' line in data order, or as a bubble.
func (rd *renderer) scatter(g *Group, x, y *dim) []func() {
	var labels []func()
	maxSize := 0.0
	if g.Kind == Bubble {
		for _, s := range g.Series {
			sz := rd.data(s.Sizes)
			for j := range min(sz.Len(), MaxPoints) {
				if v, ok := sz.number(j); ok {
					maxSize = math.Max(maxSize, math.Abs(v))
				}
			}
		}
	}
	scaleB := g.BubbleScale
	if !(scaleB > 0) {
		scaleB = 100
	}
	// The largest bubble's radius is a quarter of the plot area's smaller
	// side (LibreOffice's BubbleChart).
	rmax := math.Min(x.inner.Width, y.inner.Height) / 4 * math.Min(scaleB, 300) / 100
	for _, s := range g.Series {
		cats, vals, sizes := rd.data(s.Categories), rd.data(s.Values), rd.data(s.Sizes)
		var pts []compose.Point
		var idx []int
		for j := range min(vals.Len(), MaxPoints) {
			yv, ok := vals.number(j)
			if !ok {
				continue
			}
			xv, ok := xValue(cats, j)
			if !ok {
				continue
			}
			pts = append(pts, compose.Point{X: x.valuePixel(x.sc.clamp(xv)), Y: y.valuePixel(y.sc.clamp(yv))})
			idx = append(idx, j)
		}
		if g.Kind == Bubble {
			for k, p := range pts {
				v, ok := sizes.number(idx[k])
				if !ok || v <= 0 || maxSize <= 0 {
					continue
				}
				r := rmax * math.Sqrt(v/maxSize)
				rd.paint(ellipse(p.X, p.Y, r, r), rd.fillOf(g, s, idx[k]), rd.borderOf(g, s, idx[k]))
			}
		} else {
			if st := rd.borderOf(g, s, -1).compose(); st != nil && len(pts) > 1 {
				st.Join = compose.RoundJoin
				st.Cap = compose.RoundCap
				rd.out = append(rd.out, &compose.Path{Ops: polyline(pts, s.Smooth), Stroke: st})
			}
			for k, p := range pts {
				rd.marker(rd.markerOf(g, s, idx[k]), p.X, p.Y)
			}
		}
		for k, p := range pts {
			j := idx[k]
			lb := mergeLabels(g.Labels, s.labelsOf(j))
			if !lb.Any() {
				continue
			}
			v, _ := vals.number(j)
			labels = append(labels, func() { rd.pointLabel(g, s, j, v, lb, p, rd.markerOf(g, s, j).Size) })
		}
	}
	return labels
}
