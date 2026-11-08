package chart

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
)

// layoutPasses is how often the plot area is fitted to its axis labels:
// LibreOffice measures the labels at a first size, shrinks the plot area by
// what they take and lays them out again.
const layoutPasses = 3

// dim is one dimension of a cartesian plot area: an axis with its scale or
// categories, resolved for a plot rectangle.
type dim struct {
	ax *Axis
	// category spaces n categories evenly; between puts them between the
	// ticks.
	category bool
	n        int
	between  bool
	names    []string
	sc       scale
	// vertical draws the dimension up the page.
	vertical bool
	format   string
	// zero and border are the automatic scale's rules; lo and hi the
	// data's range.
	zero, border bool
	lo, hi       float64
	// percent formats the ticks as percentages of a percent-stacked group.
	percent bool
	// loc is the separators of the chart's locale.
	loc numfmt.Locale
	// other is the crossing dimension.
	other *dim

	// The geometry of the last layout pass.
	inner  compose.Rect
	lineAt float64
	labels []tickLabel
	ticks  []float64
	// outside is where the labels go: -1 to the left or below, +1 to the
	// right or above.
	outside float64
	// table is the data table drawn in place of the labels, nil for none.
	table *dataTable
}

// item is one axis label's text at its fraction of the axis.
type item struct {
	text string
	f    float64
}

// tickLabel is one axis label at its position along the axis.
type tickLabel struct {
	b   *block
	pos float64
	// cx and cy centre the label.
	cx, cy float64
}

// visible reports a drawn axis.
func (d *dim) visible() bool { return d.ax == nil || !d.ax.Deleted }

// labelled reports an axis that draws tick labels.
func (d *dim) labelled() bool {
	return d.visible() && (d.ax == nil || d.ax.Labels != LabelNone)
}

// frac returns where category i, or value v, lies along d.
func (d *dim) catFrac(i float64) float64 {
	var f float64
	switch {
	case d.between:
		f = (i + 0.5) / float64(max(d.n, 1))
	case d.n <= 1:
		f = 0.5
	default:
		f = i / float64(d.n-1)
	}
	if d.ax != nil && d.ax.Reverse {
		f = 1 - f
	}
	return f
}

// pixel maps a fraction along d to a page coordinate.
func (d *dim) pixel(f float64) float64 {
	if d.vertical {
		return d.inner.Y + d.inner.Height - f*d.inner.Height
	}
	return d.inner.X + f*d.inner.Width
}

// valuePixel maps a value to a page coordinate.
func (d *dim) valuePixel(v float64) float64 {
	if d.category {
		return d.pixel(d.catFrac(v))
	}
	return d.pixel(d.sc.frac(v))
}

// length returns the axis length on the page.
func (d *dim) length() float64 {
	if d.vertical {
		return d.inner.Height
	}
	return d.inner.Width
}

// crossValue returns the value of the other dimension where this one's
// axis line runs, from this axis's crossing (ECMA-376 Part 1, 21.2.2.33
// crosses).
func (d *dim) crossValue() float64 {
	o := d.other
	cross, at := CrossAuto, 0.0
	if d.ax != nil {
		cross, at = d.ax.Crosses, d.ax.CrossAt
	}
	if o.category {
		switch cross {
		case CrossMax:
			return float64(o.n) - 0.5
		case CrossValue:
			return at - 1
		case CrossAuto, CrossMin:
		}
		if o.between {
			return -0.5
		}
		return 0
	}
	switch cross {
	case CrossMin:
		return o.sc.min
	case CrossMax:
		return o.sc.max
	case CrossValue:
		return o.sc.clamp(at)
	case CrossAuto:
	}
	if o.sc.log > 0 {
		return o.sc.min
	}
	return o.sc.clamp(0)
}

// crossPixel returns where the axis line of d runs on the page.
func (d *dim) crossPixel() float64 {
	o := d.other
	v := d.crossValue()
	if o.category {
		f := (v + 0.5) / float64(max(o.n, 1))
		if !o.between && o.n > 1 {
			f = v / float64(o.n-1)
		}
		if o.ax != nil && o.ax.Reverse {
			f = 1 - f
		}
		return o.pixel(math.Max(0, math.Min(1, f)))
	}
	return o.pixel(o.sc.frac(v))
}

// atHighEnd reports an axis line at the high end of the other dimension,
// whose labels go outside on that side.
func (d *dim) atHighEnd() bool {
	if d.ax != nil {
		switch d.ax.Labels {
		case LabelHigh:
			return true
		case LabelLow:
			return false
		case LabelNextTo, LabelNone:
		}
		if d.ax.Crosses == CrossMax {
			return d.other.ax == nil || !d.other.ax.Reverse
		}
	}
	return d.other.ax != nil && d.other.ax.Reverse && d.other.category
}

// tickPositions returns the fractions along d of its major ticks.
func (d *dim) tickPositions() []float64 {
	var out []float64
	if d.category {
		if d.between {
			for i := 0; i <= d.n; i++ {
				out = append(out, float64(i)/float64(max(d.n, 1)))
			}
		} else {
			for i := 0; i < d.n; i++ {
				out = append(out, d.catFrac(float64(i)))
			}
		}
		return out
	}
	for _, v := range d.sc.ticks() {
		out = append(out, d.sc.frac(v))
	}
	return out
}

// cartesian draws the groups of a plot area with axes into avail.
func (rd *renderer) cartesian(avail compose.Rect) {
	c := rd.c
	g0 := c.Groups[0]
	if g0.Kind == Radar {
		rd.radar(avail)
		return
	}
	x, ys, yOf := rd.dims(g0)
	if x == nil {
		return
	}
	all := append([]*dim{x}, ys...)
	avail = rd.reserveTitles(avail, all...)
	inner := avail
	fit := func() {
		for _, d := range all {
			d.inner = inner
		}
		for _, d := range all {
			rd.fitScale(d)
		}
		for _, d := range all {
			rd.placeLabels(d)
		}
	}
	passes := layoutPasses
	if rd.fixed != nil {
		inner, passes = *rd.fixed, 0
	}
	for pass := 0; pass < passes; pass++ {
		fit()
		bb := labelBounds(inner, all)
		next := compose.Rect{X: avail.X + (inner.X - bb.X), Y: avail.Y + (inner.Y - bb.Y)}
		next.Width = avail.X + avail.Width - (bb.X + bb.Width - (inner.X + inner.Width)) - next.X
		next.Height = avail.Y + avail.Height - (bb.Y + bb.Height - (inner.Y + inner.Height)) - next.Y
		if !(next.Width > 4) || !(next.Height > 4) {
			break
		}
		if math.Abs(next.X-inner.X)+math.Abs(next.Y-inner.Y)+math.Abs(next.Width-inner.Width)+math.Abs(next.Height-inner.Height) < 0.01 {
			break
		}
		inner = next
	}
	fit()
	if rd.ctx.Err() != nil {
		return
	}
	rd.frame(inner, c.PlotArea)
	for _, d := range all {
		rd.grids(d, inner)
	}
	for _, d := range all {
		rd.axisLine(d)
	}
	var labels []func()
	for _, g := range c.Groups {
		if rd.ctx.Err() != nil {
			return
		}
		y := yOf[g]
		if y == nil {
			continue
		}
		labels = append(labels, rd.series(g, x, y)...)
	}
	for _, f := range labels {
		f()
	}
	for _, d := range all {
		for _, l := range d.labels {
			rd.out = append(rd.out, l.b.draw(l.cx, l.cy, alignCenter)...)
		}
		if d.table != nil {
			rd.drawDataTable(d, d.table)
		}
	}
	ext := labelBounds(inner, all)
	for _, d := range all {
		rd.axisTitle(d, ext)
	}
}

// labelBounds returns the smallest rectangle holding inner and the axis
// labels of dims.
func labelBounds(inner compose.Rect, dims []*dim) compose.Rect {
	bb := inner
	for _, d := range dims {
		for _, l := range d.labels {
			w, h := l.b.rotated()
			bb = union(bb, compose.Rect{X: l.cx - w/2, Y: l.cy - h/2, Width: w, Height: h})
		}
	}
	return bb
}

// compatible reports a group that can share the plot area of g0.
func compatible(g0, g *Group) bool {
	xy0, xy := g0.Kind == Scatter || g0.Kind == Bubble, g.Kind == Scatter || g.Kind == Bubble
	if xy0 != xy || g.Kind == Pie || g.Kind == Doughnut || g.Kind == Radar {
		return false
	}
	return g0.Horizontal == g.Horizontal || g.Kind != Bar
}

// union returns the smallest rectangle holding a and b.
func union(a, b compose.Rect) compose.Rect {
	x0, y0 := math.Min(a.X, b.X), math.Min(a.Y, b.Y)
	x1, y1 := math.Max(a.X+a.Width, b.X+b.Width), math.Max(a.Y+a.Height, b.Y+b.Height)
	return compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// axisAt returns axis i of the chart, nil when out of range.
func (c *Chart) axisAt(i int) *Axis {
	if i < 0 || i >= len(c.Axes) {
		return nil
	}
	return c.Axes[i]
}

// dims builds the dimensions of g0's plot area: the category (or x)
// dimension, and a value dimension per value axis the groups drawn in it
// name, the first g0's, with the value ranges of their groups.
func (rd *renderer) dims(g0 *Group) (*dim, []*dim, map[*Group]*dim) {
	c := rd.c
	xy := g0.Kind == Scatter || g0.Kind == Bubble
	xAxis, xGroup := g0.XAxis, g0
	if a := c.axisAt(xAxis); a != nil && a.Deleted {
		// A combined chart draws the category axis of a group that shows
		// one, against that group's value axis.
		for _, g := range c.Groups {
			if b := c.axisAt(g.XAxis); b != nil && !b.Deleted && compatible(g0, g) {
				xAxis, xGroup = g.XAxis, g
				break
			}
		}
	}
	x := &dim{ax: c.axisAt(xAxis), loc: c.Locale}
	horizontal := g0.Kind == Bar && g0.Horizontal
	x.vertical = horizontal
	if xy {
		x.border = true
		x.lo, x.hi = math.Inf(1), math.Inf(-1)
	} else {
		x.category = true
		x.between = x.ax == nil || x.ax.BetweenCategories
		if g0.Kind == Area && x.ax == nil {
			x.between = false
		}
	}
	if x.ax != nil {
		x.format = x.ax.Format
	}
	var ys []*dim
	byAxis := map[int]*dim{}
	yOf := map[*Group]*dim{}
	for _, g := range c.Groups {
		if g != g0 && !compatible(g0, g) {
			continue
		}
		key := g.YAxis
		if a := c.axisAt(key); a == nil || a.Kind != ValueAxis {
			key = g0.YAxis
		}
		y := byAxis[key]
		if y == nil {
			if len(ys) >= maxValueAxes {
				y = ys[len(ys)-1]
			} else {
				y = &dim{ax: c.axisAt(key), zero: true, border: true, other: x, vertical: !horizontal, loc: c.Locale}
				y.lo, y.hi = math.Inf(1), math.Inf(-1)
				if y.ax != nil {
					y.format = y.ax.Format
				}
				ys = append(ys, y)
			}
			byAxis[key] = y
		}
		yOf[g] = y
		for _, s := range g.Series {
			vals := rd.data(s.Values)
			if y.format == "" {
				y.format = vals.Format
			}
			cats := rd.data(s.Categories)
			if xy {
				if x.format == "" && cats.numeric() {
					x.format = cats.Format
				}
				for i := range min(vals.Len(), MaxPoints) {
					xv, ok := xValue(cats, i)
					if _, vok := vals.number(i); ok && vok {
						x.lo, x.hi = math.Min(x.lo, xv), math.Max(x.hi, xv)
					}
				}
			} else {
				x.n = min(max(x.n, vals.Len(), cats.Len()), MaxPoints)
				if x.names == nil && cats.Len() > 0 {
					for i := range min(cats.Len(), MaxPoints) {
						x.names = append(x.names, cats.labelAs(i, x.format, c.Locale))
					}
				}
			}
		}
		lo, hi, pct := rd.valueRange(g)
		y.lo, y.hi = math.Min(y.lo, lo), math.Max(y.hi, hi)
		y.percent = y.percent || pct
	}
	if len(ys) == 0 {
		return nil, nil, nil
	}
	x.other = ys[0]
	if y := yOf[xGroup]; y != nil {
		x.other = y
	}
	if a := x.other.ax; a != nil && (a.Position == AxisRight || a.Position == AxisTop) {
		// The category axis crosses the primary value axis, not a
		// secondary one on the far side.
		for _, y := range ys {
			if y.ax != nil && (y.ax.Position == AxisLeft || y.ax.Position == AxisBottom) {
				x.other = y
				break
			}
		}
	}
	for _, y := range ys {
		if math.IsInf(y.lo, 0) || math.IsInf(y.hi, 0) {
			y.lo, y.hi = 0, 0
		}
		if y.percent {
			y.border = false
			if y.ax == nil || y.ax.Format == "" {
				y.format = "0%"
			}
		}
	}
	if xy && (math.IsInf(x.lo, 0) || math.IsInf(x.hi, 0)) {
		x.lo, x.hi = 0, 0
	}
	if !xy {
		for len(x.names) < x.n {
			x.names = append(x.names, formatGeneral(float64(len(x.names)+1)))
		}
		if x.n == 0 {
			x.n = 1
		}
	}
	return x, ys, yOf
}

// maxValueAxes caps the value axes of a plot area: a primary and a
// secondary, as Office and LibreOffice offer.
const maxValueAxes = 2

// xValue returns the x value of point i of a scatter series: the number,
// or the 1-based index when the x data is not numeric.
func xValue(cats Data, i int) (float64, bool) {
	if cats.numeric() {
		return cats.number(i)
	}
	return float64(i + 1), true
}

// data returns d with its values filled by the chart's resolver when the
// file held no cache.
func (rd *renderer) data(d Data) Data {
	if d.Values == nil && !d.Cached && d.Ref != "" && rd.c.Resolver != nil {
		vals := rd.c.Resolver(d.Ref)
		if len(vals) > MaxPoints {
			vals = vals[:MaxPoints]
		}
		d.Values = vals
	}
	return d
}

// fitScale resolves d's scale for its current length, with as many major
// intervals as its labels leave room for.
func (rd *renderer) fitScale(d *dim) {
	if d.category {
		return
	}
	opt := scaleOptions{zero: d.zero, border: d.border, maxCount: maxAutoTicks}
	d.sc = autoScale(d.ax, d.lo, d.hi, opt)
	if !d.labelled() {
		return
	}
	need := 0.0
	ticks := d.sc.ticks()
	// Ticks whose labels format alike are fewer intervals than they look:
	// LibreOffice caps the count so no label repeats (tdf#48041).
	same, run, prev := 0, 0, ""
	for i, v := range ticks {
		t := d.tickText(v)
		if i > 0 && t == prev {
			run++
			same = max(same, run)
		} else {
			run = 0
		}
		prev = t
	}
	for _, v := range ticks {
		b := rd.measure(d.tickText(v), d.style(), 0)
		if b == nil {
			continue
		}
		w, h := b.rotated()
		if d.vertical {
			need = math.Max(need, h)
		} else {
			need = math.Max(need, w)
		}
	}
	if need > 0 || same > 0 {
		if need > 0 {
			opt.maxCount = int(d.length() / need)
		}
		if same > 0 {
			opt.maxCount = min(opt.maxCount, len(ticks)/(same+1))
		}
		d.sc = autoScale(d.ax, d.lo, d.hi, opt)
	}
}

// style returns the tick label style of d.
func (d *dim) style() TextStyle {
	if d.ax != nil {
		return d.ax.Style
	}
	return TextStyle{Size: 10, Color: compose.Black}
}

// tickText formats a tick value of d.
func (d *dim) tickText(v float64) string {
	return formatLabel(d.format, v, d.loc)
}

// placeLabels lays out d's tick labels along its axis.
func (rd *renderer) placeLabels(d *dim) {
	d.labels = d.labels[:0]
	d.ticks = d.tickPositions()
	d.lineAt = d.crossPixel()
	high := d.atHighEnd()
	d.outside = -1
	if high {
		d.outside = 1
	}
	if d.ax != nil && (d.ax.Labels == LabelLow || d.ax.Labels == LabelHigh) {
		o := d.other
		if d.ax.Labels == LabelLow {
			d.lineAt = o.pixel(0)
		} else {
			d.lineAt = o.pixel(1)
		}
	}
	d.table = nil
	if t := rd.layoutDataTable(d); t != nil {
		// The data table takes the labels' place under the axis.
		d.table = t
		right := d.inner.X + d.inner.Width
		h := t.height()
		d.labels = append(d.labels, tickLabel{b: &block{w: right - t.x, h: h}, cx: (t.x + right) / 2, cy: t.top + h/2})
		return
	}
	if !d.labelled() {
		return
	}
	gap := labelSpacing
	if d.ax != nil && (d.ax.MajorTicks == TickOut || d.ax.MajorTicks == TickCross) {
		gap += tickLength
	}
	st := d.style()
	var items []item
	maxW := 0.0
	if d.category {
		for i := 0; i < d.n && i < len(d.names); i++ {
			items = append(items, item{d.names[i], d.catFrac(float64(i))})
		}
		if !d.vertical {
			maxW = d.length() / float64(max(d.n, 1))
			if !d.between && d.n > 1 {
				maxW = d.length() / float64(d.n-1)
			}
			if s := math.Abs(math.Sin(st.Rotation * math.Pi / 180)); s > 0.5 {
				// A turned label breaks at a share of the chart's height.
				maxW = rd.page.Height * 0.4 / s
			}
		} else {
			maxW = d.inner.Width * 0.3
			if rd.fixed != nil {
				// Beside a placed plot area the labels take the room
				// left of the axis (chart2's VCartesianAxis for a
				// vertical category axis).
				maxW = math.Max(d.inner.X-rd.page.X-labelSpacing, maxW)
			}
		}
	} else {
		for _, v := range d.sc.ticks() {
			items = append(items, item{d.tickText(v), d.sc.frac(v)})
		}
	}
	breakable := d.category && (d.ax == nil || !d.ax.NoBreak)
	overlap := d.ax != nil && d.ax.Overlap
	if !breakable {
		maxW = 0
	}
	measure := func(st TextStyle, maxW float64) []*block {
		bs := make([]*block, len(items))
		for i, it := range items {
			bs[i] = rd.measure(it.text, st, maxW)
		}
		return bs
	}
	blocks := measure(st, maxW)
	if rd.fixed != nil && d.category && !d.vertical && st.Rotation != 0 {
		// Beside a placed plot area, a turned label longer than the room
		// under the axis loses its end for an ellipsis (chart2's
		// lcl_ResizeTextShapeToFitAvailableSpace).
		room := rd.page.Y + rd.page.Height - d.lineAt
		for i, b := range blocks {
			if b == nil {
				continue
			}
			if _, h := b.rotated(); h > room {
				if t := truncateLabel(items[i].text, b.w, h-room); t != items[i].text {
					items[i].text = t
					blocks[i] = rd.measure(t, st, maxW)
				}
			}
		}
	}
	// A label whose words break inside a word keeps its words whole on one
	// line instead (chart2's VCartesianAxis::createTextShapes, after
	// lcl_hasWordBreak).
	if breakable && !d.vertical && maxW > 0 && !overlap && st.Rotation == 0 && rd.breaksWord(items[min(1, len(items)):], blocks[min(1, len(blocks)):], st, maxW) {
		breakable = false
		blocks = measure(st, 0)
	}
	place := func(blocks []*block) {
		d.labels = d.labels[:0]
		for i, it := range items {
			b := blocks[i]
			if b == nil {
				continue
			}
			w, h := b.rotated()
			p := d.pixel(it.f)
			l := tickLabel{b: b, pos: p}
			// A vertical axis's labels sit beside the line, a horizontal
			// one's under or over it (LibreOffice's
			// AXIS2D_TICKLABELSPACING).
			if d.vertical {
				l.cy = p
				l.cx = d.lineAt + d.outside*(gap+w/2)
			} else {
				l.cx = p
				l.cy = d.lineAt - d.outside*(gap+h/2)
				if r := b.style.Rotation; d.outside < 0 && r != 0 && math.Abs(r) < 90 {
					// A turned label under the axis ends at its tick, its
					// box beside it (chart2's lcl_correctRotation_Bottom).
					a := math.Abs(r) * math.Pi / 180
					l.cx = p - math.Copysign(w/2, r)
					l.cy = d.lineAt + gap + b.h/2 + b.w*math.Sin(a)/2
				}
			}
			d.labels = append(d.labels, l)
		}
	}
	place(blocks)
	// Labels kept whole that still overlap turn by 45 degrees, on a
	// horizontal axis whose labels are not turned already (chart2's
	// canAutoAdjustLabelPlacement and autoRotate45).
	if d.category && !d.vertical && !breakable && !overlap && st.Rotation == 0 && overlapping(d.labels) {
		st.Rotation = 45
		place(measure(st, 0))
	}
	d.labels = thin(d.labels, d.vertical)
}

// truncateLabel shortens s, w points wide, by excess points as chart2
// does: as many characters of the average width as cover the excess, and
// one more, then three for an ellipsis, which follows what is left when
// that is more than three characters.
func truncateLabel(s string, w, excess float64) string {
	r := []rune(s)
	if len(r) == 0 || !(w > 0) {
		return s
	}
	avg := w / float64(len(r))
	remove := int(excess/avg) + 1
	n := len(r) - remove - 3
	if n < 0 {
		n = min(3, len(r))
	}
	if n > 3 {
		return string(r[:n]) + "..."
	}
	return string(r[:n])
}

// overlapping reports neighbouring labels of a horizontal axis whose boxes
// overlap.
func overlapping(ls []tickLabel) bool {
	for i := 1; i < len(ls); i++ {
		if labelsOverlap(ls[i-1], ls[i]) {
			return true
		}
	}
	return false
}

// labelsOverlap reports two labels of a horizontal axis, turned alike,
// whose turned boxes overlap: their distance along the text and across it
// is less than their half sizes (chart2's doesOverlap tests the turned
// shapes, not their bounding boxes).
func labelsOverlap(a, b tickLabel) bool {
	r := a.b.style.Rotation * math.Pi / 180
	dx, dy := b.cx-a.cx, b.cy-a.cy
	along := math.Abs(dx*math.Cos(r) - dy*math.Sin(r))
	across := math.Abs(dx*math.Sin(r) + dy*math.Cos(r))
	return along < (a.b.w+b.b.w)/2-0.01 && across < (a.b.h+b.b.h)/2-0.01
}

// breaksWord reports a label whose layout in blocks, at most maxW wide,
// breaks a word across lines: a word wider than maxW alone.
func (rd *renderer) breaksWord(items []item, blocks []*block, st TextStyle, maxW float64) bool {
	for i, b := range blocks {
		if b == nil || len(b.lines) < 2 {
			continue
		}
		for _, w := range strings.Fields(items[i].text) {
			if wb := rd.measure(w, st, 0); wb != nil && wb.w > maxW+0.01 {
				return true
			}
		}
	}
	return false
}

// thin drops labels that overlap the previous one kept, keeping every
// k-th label as LibreOffice does when they do not fit.
func thin(ls []tickLabel, vertical bool) []tickLabel {
	if len(ls) < 2 {
		return ls
	}
	overlaps := func(a, b tickLabel) bool {
		if vertical {
			_, ah := a.b.rotated()
			_, bh := b.b.rotated()
			return math.Abs(a.cy-b.cy) < (ah+bh)/2-0.01
		}
		return labelsOverlap(a, b)
	}
	for step := 1; step <= len(ls); step++ {
		ok := true
		for i := step; i < len(ls); i += step {
			if overlaps(ls[i-step], ls[i]) {
				ok = false
				break
			}
		}
		if ok {
			if step == 1 {
				return ls
			}
			var out []tickLabel
			for i := 0; i < len(ls); i += step {
				out = append(out, ls[i])
			}
			return out
		}
	}
	return ls[:1]
}

// grids draws d's gridlines across inner.
func (rd *renderer) grids(d *dim, inner compose.Rect) {
	if d.ax == nil {
		return
	}
	draw := func(fs []float64, s *Stroke) {
		for _, f := range fs {
			p := d.pixel(f)
			if d.vertical {
				rd.add(line(inner.X, p, inner.X+inner.Width, p, s))
			} else {
				rd.add(line(p, inner.Y, p, inner.Y+inner.Height, s))
			}
		}
	}
	if d.ax.MinorGrid != nil && !d.category {
		var fs []float64
		for _, v := range d.sc.minorTicks() {
			fs = append(fs, d.sc.frac(v))
		}
		draw(fs, d.ax.MinorGrid)
	}
	if d.ax.MajorGrid != nil {
		draw(d.ticks, d.ax.MajorGrid)
	}
}

// axisLine draws d's axis line and tick marks.
func (rd *renderer) axisLine(d *dim) {
	if !d.visible() || d.ax == nil {
		return
	}
	at := d.lineAt
	if d.vertical {
		rd.add(line(at, d.inner.Y, at, d.inner.Y+d.inner.Height, d.ax.Line))
	} else {
		rd.add(line(d.inner.X, at, d.inner.X+d.inner.Width, at, d.ax.Line))
	}
	if d.ax.Line == nil {
		return
	}
	tick := func(fs []float64, kind TickMark, length float64) {
		if kind == TickNone {
			return
		}
		out, in := 0.0, 0.0
		switch kind {
		case TickOut:
			out = length
		case TickIn:
			in = length
		case TickCross:
			out, in = length, length
		case TickNone:
		}
		for _, f := range fs {
			p := d.pixel(f)
			a, b := at+d.outside*-in, at+d.outside*out
			if d.vertical {
				rd.add(line(a, p, b, p, d.ax.Line))
			} else {
				rd.add(line(p, at-d.outside*-in, p, at-d.outside*out, d.ax.Line))
			}
		}
	}
	tick(d.ticks, d.ax.MajorTicks, tickLength)
	var fs []float64
	if d.category {
		// A category axis's minor ticks halve its major intervals
		// (chart2's category axis subdivides each interval once).
		for i := 1; i < len(d.ticks); i++ {
			fs = append(fs, (d.ticks[i-1]+d.ticks[i])/2)
		}
	} else {
		for _, v := range d.sc.minorTicks() {
			fs = append(fs, d.sc.frac(v))
		}
	}
	tick(fs, d.ax.MinorTicks, minorTickLength)
}

// titleBlock lays out d's axis title, nil for none.
func (rd *renderer) titleBlock(d *dim) *block {
	if d.ax == nil || d.ax.Title == nil || d.ax.Deleted {
		return nil
	}
	st := d.ax.Title.Style
	if d.vertical && st.Rotation == 0 {
		st.Rotation = 90
	}
	maxW := rd.page.Width * 0.8
	if d.vertical {
		maxW = rd.page.Height * 0.8
	}
	return rd.measure(d.ax.Title.Text, st, maxW)
}

// reserveTitles takes the room of the axis titles from avail, as
// LibreOffice's createAxisTitleShapes2D does.
func (rd *renderer) reserveTitles(avail compose.Rect, dims ...*dim) compose.Rect {
	for _, d := range dims {
		b := rd.titleBlock(d)
		if b == nil {
			continue
		}
		w, h := b.rotated()
		w += 2 * titleHPad * b.style.Size
		h += 2 * titleVPad * b.style.Size
		high := d.atHighEnd()
		if d.vertical {
			avail.Width -= w + yTitleDistance
			if !high {
				avail.X += w + yTitleDistance
			}
		} else {
			avail.Height -= h + xTitleDistance
			if high {
				avail.Y += h + xTitleDistance
			}
		}
	}
	return avail
}

// axisTitle draws d's title beside ext, the plot area with its axis
// labels, as LibreOffice's changePositionOfAxisTitle places it.
func (rd *renderer) axisTitle(d *dim, ext compose.Rect) {
	b := rd.titleBlock(d)
	if b == nil {
		return
	}
	w, h := b.rotated()
	w += 2 * titleHPad * b.style.Size
	h += 2 * titleVPad * b.style.Size
	dx, dy := layoutDistance*rd.page.Width, layoutDistance*rd.page.Height
	var cx, cy float64
	if d.vertical {
		cy = ext.Y + ext.Height/2
		if d.outside > 0 {
			cx = ext.X + ext.Width + dx + w/2
		} else {
			cx = ext.X - dx - w/2
		}
	} else {
		cx = ext.X + ext.Width/2
		if d.outside > 0 {
			cy = ext.Y - dy - h/2
		} else {
			cy = ext.Y + ext.Height + dy + h/2
		}
	}
	cx = math.Max(rd.page.X+w/2, math.Min(cx, rd.page.X+rd.page.Width-w/2))
	cy = math.Max(rd.page.Y+h/2, math.Min(cy, rd.page.Y+rd.page.Height-h/2))
	rd.frame(compose.Rect{X: cx - w/2, Y: cy - h/2, Width: w, Height: h}, d.ax.Title.Frame)
	rd.out = append(rd.out, b.draw(cx, cy, alignCenter)...)
}
