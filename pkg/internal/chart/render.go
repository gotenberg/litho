package chart

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// The distances of LibreOffice's automatic chart layout, in points
// (chart2's ChartView and VLegend, which count in hundredths of a
// millimetre).
const (
	// mm100 is a hundredth of a millimetre.
	mm100 = 72.0 / 2540
	// layoutDistance is the fraction of the chart's width and height kept
	// clear around the plot area and above a title.
	layoutDistance = 0.02
	// mainTitleExtra is added above the main title: 135.
	mainTitleExtra = 135 * mm100
	// pieDistance replaces layoutDistance around a pie: 350.
	pieDistance = 350 * mm100
	// legendSideMargin and legendTopMargin keep the legend from the
	// chart's edge: 210 and 185.
	legendSideMargin = 210 * mm100
	legendTopMargin  = 185 * mm100
	// minLegendSpacing is the least padding in a legend: 100.
	minLegendSpacing = 100 * mm100
	// tickLength is the length of a tick mark outside the axis: 150.
	tickLength = 150 * mm100
	// minorTickLength is the length of a minor tick mark outside the
	// axis, three quarters of a major one's: 112.5.
	minorTickLength = tickLength * 3 / 4
	// labelSpacing is the gap between an axis and its labels: 100.
	labelSpacing = 100 * mm100
	// axisTitleDistance is the gap between the axes and an axis title: 420
	// below the x axis, 450 left of the y axis.
	xTitleDistance = 420 * mm100
	yTitleDistance = 450 * mm100
	// titleHPad and titleVPad pad a title's text: 0.18 and 0.30 of its font
	// size on each side.
	titleHPad = 0.18
	titleVPad = 0.30
	// lineKeyWidth is the width of a line series' legend key: 800.
	lineKeyWidth = 800 * mm100
)

// Render draws c into r, as LibreOffice draws a chart: the chart area,
// the title and subtitle at the top, the legend at its side, and the plot
// area in the space left, with its axes, gridlines, series and data labels.
// Every fragment lies inside r: the content is clipped to it. Text is shaped
// with fonts and emitted as [compose.Text] runs, so extraction reads it.
//
// It returns nil for a nil chart, a nil font index, or a rectangle without
// area, and ctx's error stops it early with what it drew so far. The group
// nesting of the result is at most 3. The output depends on c, fonts and r
// alone.
//
// Three-dimensional charts are drawn as their two-dimensional kind;
// trendlines and error bars are not drawn.
func Render(ctx context.Context, fonts *text.Fonts, c *Chart, r compose.Rect) []compose.Fragment {
	if c == nil || fonts == nil || !(r.Width > 0) || !(r.Height > 0) || math.IsInf(r.Width, 0) || math.IsInf(r.Height, 0) {
		return nil
	}
	rd := &renderer{ctx: ctx, fonts: fonts, c: binned(c), page: r}
	rd.render(r)
	if len(rd.out) == 0 {
		return nil
	}
	return []compose.Fragment{&compose.Group{Clip: rectPath(r), Content: rd.out}}
}

// render lays the chart out into r and paints it.
func (rd *renderer) render(r compose.Rect) {
	c := rd.c
	rd.frame(r, c.Area)
	if rd.ctx.Err() != nil {
		return
	}
	rem := r
	pie := len(c.Groups) > 0 && (c.Groups[0].Kind == Pie || c.Groups[0].Kind == Doughnut)
	if c.Title != nil {
		rd.title(c.Title, r, &rem, layoutDistance*r.Height+mainTitleExtra)
	}
	if c.Subtitle != nil {
		rd.title(c.Subtitle, r, &rem, layoutDistance*r.Height)
	}
	if c.Legend != nil {
		rd.legend(r, &rem)
	}
	if rd.ctx.Err() != nil {
		return
	}
	dx, dy := layoutDistance*r.Width, layoutDistance*r.Height
	if pie {
		dx, dy = pieDistance, pieDistance
	}
	plot := compose.Rect{X: rem.X + dx, Y: rem.Y + dy, Width: rem.Width - 2*dx, Height: rem.Height - 2*dy}
	if l := c.PlotLayout; l != nil && l.Width > 0 && l.Height > 0 {
		// A plot area the file places: its rectangle with the axis labels,
		// or without them, as a pie's always is (oox's PlotAreaConverter,
		// chart2's DiagramPositioning).
		plot = l.rect(r)
		if l.Inner || pie {
			rd.fixed = &plot
		}
	}
	if !(plot.Width > 1) || !(plot.Height > 1) {
		return
	}
	switch {
	case len(c.Groups) == 0:
	case pie:
		rd.pies(plot)
	default:
		rd.cartesian(plot)
	}
	if c.Notice != nil {
		rd.notice(c.Notice, r)
	}
}

// notice draws t centred on the chart's rectangle r, its lines at most two
// thirds of r's width, in a frame that fits them (chart2's
// UnsupportedChart: a text shape growing from the page's centre, its
// width capped at two thirds of the page's). LibreOffice sets the text
// narrower than the face's advances, by noticeNarrowing, so the lines
// break where they would at that width.
func (rd *renderer) notice(t *Title, r compose.Rect) {
	b := rd.measure(t.Text, t.Style, r.Width*2/3/noticeNarrowing)
	if b == nil {
		return
	}
	w, h := b.rotated()
	cx, cy := r.X+r.Width/2, r.Y+r.Height/2
	rd.frame(compose.Rect{X: cx - w/2, Y: cy - h/2, Width: w, Height: h}, t.Frame)
	rd.out = append(rd.out, b.draw(cx, cy, alignCenter)...)
}

// title places t centred at the top of rem, top distance below rem's top,
// and takes its height from rem unless it overlays.
func (rd *renderer) title(t *Title, page compose.Rect, rem *compose.Rect, distance float64) {
	b := rd.measure(t.Text, t.Style, page.Width*0.8-2*titleHPad*t.Style.Size)
	if b == nil {
		return
	}
	w, h := b.rotated()
	w += 2 * titleHPad * b.style.Size
	h += 2 * titleVPad * b.style.Size
	cx := rem.X + rem.Width/2
	top := rem.Y + distance
	if t.Layout != nil {
		// A placed title keeps its top-left corner where the file says
		// (oox's LayoutConverter positions the title shape).
		lr := t.Layout.rect(page)
		cx, top = lr.X+w/2, lr.Y
	}
	rd.frame(compose.Rect{X: cx - w/2, Y: top, Width: w, Height: h}, t.Frame)
	rd.out = append(rd.out, b.draw(cx, top+h/2, alignCenter)...)
	if t.Overlay {
		return
	}
	rem.Y += h + distance
	rem.Height -= h + distance
}

// frame paints the fill and border of rect.
func (rd *renderer) frame(rect compose.Rect, f Frame) {
	rd.paint(rectPath(rect).Ops, f.Fill, f.Border)
}

// paint fills the outline ops with fill, a gradient or pattern through
// the shape package, and strokes it with border.
func (rd *renderer) paint(ops []compose.PathOp, fill *Paint, border *Stroke) {
	if fill != nil && !fill.None && fill.Shape != nil {
		rd.out = append(rd.out, shape.Fill(ops, opsBounds(ops), *fill.Shape)...)
	}
	rd.add(paintPath(ops, fill, border))
}

// opsBounds returns the bounding box of the points of ops.
func opsBounds(ops []compose.PathOp) compose.Rect {
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, op := range ops {
		n := 1
		switch op.Kind {
		case compose.CurveTo:
			n = 3
		case compose.ClosePath:
			n = 0
		case compose.MoveTo, compose.LineTo:
		}
		for _, pt := range op.P[:n] {
			x0, y0, x1, y1 = math.Min(x0, pt.X), math.Min(y0, pt.Y), math.Max(x1, pt.X), math.Max(y1, pt.Y)
		}
	}
	if x0 > x1 || y0 > y1 {
		return compose.Rect{}
	}
	return compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// paintPath returns a path of ops filled and stroked, nil when it paints
// nothing.
func paintPath(ops []compose.PathOp, fill *Paint, border *Stroke) *compose.Path {
	p := &compose.Path{Ops: ops}
	if fill != nil && !fill.None && fill.Shape == nil && fill.Color.A > 0 {
		p.Fill = &compose.Fill{Color: fill.Color}
	}
	p.Stroke = border.compose()
	if p.Fill == nil && p.Stroke == nil {
		return nil
	}
	return p
}

// compose returns the stroke s draws, nil for none.
func (s *Stroke) compose() *compose.Stroke {
	if s == nil || s.None || s.Color.A <= 0 || s.Width < 0 || math.IsNaN(s.Width) {
		return nil
	}
	return &compose.Stroke{Color: s.Color, Width: min(s.Width, maxStrokeWidth), Dash: s.Dash, Join: compose.RoundJoin}
}

// noticeNarrowing is the width LibreOffice gives a chart's notice text,
// as a fraction of the width of the face's advances.
//
// # Corpus evidence
//
//   - pptx: chart2/funnel-pp1.pptx prints the 10-point Liberation Serif
//     notice 275.93 points wide, where its advances sum to 282.78;
//     xlsx: chart2/funnel1.xlsx its first line 185.51 for 190.50.
const noticeNarrowing = 0.9757

// maxStrokeWidth caps a line's width, in points: Office allows 1584.
const maxStrokeWidth = 1584

// rectPath returns the outline of r.
func rectPath(r compose.Rect) *compose.Path {
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.Width, r.Y+r.Height
	return &compose.Path{Ops: []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: x0, Y: y0}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x1, Y: y0}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x1, Y: y1}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x0, Y: y1}}},
		{Kind: compose.ClosePath},
	}}
}

// line returns a straight segment stroked with s, nil for no stroke.
func line(x0, y0, x1, y1 float64, s *Stroke) *compose.Path {
	st := s.compose()
	if st == nil {
		return nil
	}
	return &compose.Path{Ops: []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: x0, Y: y0}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x1, Y: y1}}},
	}, Stroke: st}
}

// add appends the fragments that are not nil paths.
func (rd *renderer) add(ps ...*compose.Path) {
	for _, p := range ps {
		if p != nil {
			rd.out = append(rd.out, p)
		}
	}
}
