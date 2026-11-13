package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// The distances of Office's automatic layout of a two-dimensional
// cartesian chart, in points or ems of the text they follow, as Excel,
// PowerPoint and Word print a chart whose title, legend and plot area the
// file leaves to the application.
const (
	// officePad keeps the axis labels and a legend under the plot area
	// from the chart's left, right and bottom edges.
	officePad = 6.5
	// officeRightRoom is kept right of the plot area and of the labels
	// under or over it that overhang its right edge, inside officePad.
	officeRightRoom = 4.5
	// officeTitleTop is the distance from the chart's top edge to the top
	// of the title's first line.
	officeTitleTop = 7.5
	// officeTitleGap separates the title's last line from what follows.
	officeTitleGap = 6.5
	// officeLegendPad pads each legend entry above and below its line.
	officeLegendPad = 2.94
	// officeLegendGap separates a legend above or below the plot area
	// from the room of its axis labels.
	officeLegendGap = 6.0
	// officeSideLabelGap separates the plot area from the labels of an
	// axis beside it, in ems of their font size.
	officeSideLabelGap = 0.928
	// officeUnderLabelGap separates the plot area from the labels of an
	// axis under or over it, in ems of their font size.
	officeUnderLabelGap = 0.634
	// officeMinLabelRoom is the height of the room an axis beside the
	// plot area keeps above it, half of it over the plot area's top, when
	// its labels overhang less.
	officeMinLabelRoom = 12.0
	// officeAxisTitlePad separates the title of an axis under or over the
	// plot area from the axis labels; twice as much separates it from the
	// chart's edge or the legend.
	//
	// # Corpus evidence
	//
	//   - litho/xlsx: Excel's 10 pt bold title of a category axis under
	//     the plot area adds its line and 8.97 pt to the room under it,
	//     with or without a legend below, its line 2.99 pt under the
	//     labels' line.
	officeAxisTitlePad = 2.99
)

// office reports a chart drawn by Office's rules.
func (rd *renderer) office() bool { return rd.c.Rules == OfficeRules }

// officeLayout reports a chart whose title, legend and plot area Office's
// rules place: a two-dimensional cartesian chart under [OfficeRules]
// whose file does not place its plot area. Pies, radar charts and
// three-dimensional plots keep LibreOffice's layout.
func (rd *renderer) officeLayout() bool {
	c := rd.c
	if !rd.office() || len(c.Groups) == 0 || c.PlotLayout != nil {
		return false
	}
	g0 := c.Groups[0]
	if g0.Kind == Pie || g0.Kind == Doughnut || g0.Kind == Radar {
		return false
	}
	return rd.threeD(g0) == nil
}

// officeLegend reports a legend Office's rules place: one above or below
// the plot area of a chart [renderer.officeLayout] places, at no position
// the file fixes.
func (rd *renderer) officeLegend() bool {
	lg := rd.c.Legend
	return lg != nil && lg.Layout == nil && (lg.Position == LegendTop || lg.Position == LegendBottom) && rd.officeLayout()
}

// officePlot draws the title and legend of r's chart where Office puts
// them and returns the rectangle left for the plot area and its axis
// labels. A legend at the side keeps LibreOffice's placement and the
// layout distance on its side; so does the top of a chart with neither a
// title nor a legend above the plot area. The title of an axis beside the
// plot area keeps LibreOffice's room.
//
// # Corpus evidence
//
//   - probe, xlsx: across 46 column charts 361.2 pt wide and 72.6 to
//     174.2 pt high, with a 10 pt bold title and no legend, Excel puts the
//     plot area 32.31 pt under the chart's top, 25.05 pt over its bottom
//     and 11.0 pt from its right edge, whatever the height; its value
//     labels start 6.5 pt from the left edge.
//   - litho/xlsx: the same distances hold for 14 pt titles (37.20 pt from
//     the top), and a legend under the plot area adds 24.09 pt at the
//     bottom.
//   - litho/docx: Word puts the plot area 37.09 pt under the top with a
//     14 pt title and 9 pt labels, 23.20 pt over the bottom, 46.08 pt with
//     a legend under it, and 11.0 pt from the right edge.
//   - litho/xlsx, litho/docx: the line of a legend under the plot area
//     ends 9.45 pt over the chart's bottom at 10 pt and 9.41 pt at 9 pt;
//     the entries of a side legend lie 18.1 pt apart at 10 pt and 16.9 pt
//     at 9 pt, their line and 5.9 pt.
//   - probe, pptx: PowerPoint's value axis is 57.2 pt shorter than the
//     chart, where Excel's is 57.36 pt shorter.
func (rd *renderer) officePlot(r typeset.Rect) typeset.Rect {
	c := rd.c
	rem := r
	top := false
	if t := c.Title; t != nil {
		if bottom, ok := rd.officeTitle(t, r); ok && !t.Overlay {
			rem.Height -= bottom + officeTitleGap - rem.Y
			rem.Y = bottom + officeTitleGap
			top = true
		}
	}
	left, right, bottom := officePad, officePad, officePad
	dx, dy := layoutDistance*r.Width, layoutDistance*r.Height
	if lg := c.Legend; lg != nil {
		if !top && rd.officeLegend() && lg.Position == LegendTop {
			rem.Y += dy
			rem.Height -= dy
		}
		rd.legend(r, &rem)
		if !lg.Overlay {
			switch lg.Position {
			case LegendLeft:
				left = dx
			case LegendRight:
				right = dx
			case LegendTopRight:
				right = dx
				top = true
			case LegendTop:
				top = true
			case LegendBottom:
				if rd.officeLegend() {
					bottom = 0
				}
			}
		}
	}
	if !top {
		rem.Y += dy
		rem.Height -= dy
	}
	return typeset.Rect{X: rem.X + left, Y: rem.Y, Width: rem.Width - left - right, Height: rem.Height - bottom}
}

// officeTitle draws t at the top of the chart's rectangle r, its first
// line officeTitleTop under r's top unless the file places it, and returns
// the bottom of its last line; ok is false when t draws nothing.
func (rd *renderer) officeTitle(t *Title, r typeset.Rect) (float64, bool) {
	b := rd.measure(t.Text, t.Style, r.Width*0.8-2*titleHPad*t.Style.Size)
	if b == nil {
		return 0, false
	}
	w, h := b.rotated()
	cx, top := r.X+r.Width/2, r.Y+officeTitleTop
	if t.Layout != nil {
		lr := t.Layout.rect(r)
		cx, top = lr.X+w/2, lr.Y
	}
	px, py := titleHPad*b.style.Size, titleVPad*b.style.Size
	rd.frame(typeset.Rect{X: cx - w/2 - px, Y: top - py, Width: w + 2*px, Height: h + 2*py}, t.Frame)
	rd.out = append(rd.out, b.draw(cx, top+h/2, alignCenter)...)
	return r.Y + officeTitleTop + h, true
}

// officeLabelGap returns the distance Office keeps between the plot area
// and the labels of d, whose font is size points.
//
// # Corpus evidence
//
//   - probe, xlsx: the right edge of Excel's value labels lies 7.43, 9.28,
//     12.99 and 16.70 pt left of the plot area at 8, 10, 14 and 18 pt,
//     with tick marks outside the axis; the top of 10 pt category labels
//     6.34 pt under it.
//   - litho/xlsx: 9.28 pt for 10 pt value labels without tick marks.
//   - litho/docx: Word keeps 8.35 pt beside 9 pt value labels and 5.71 pt
//     over 9 pt category labels.
func officeLabelGap(d *dim, size float64) float64 {
	if d.vertical {
		return officeSideLabelGap * size
	}
	return officeUnderLabelGap * size
}

// officeRoom returns bb, the plot area inner with its axis labels, grown
// as Office's layout grows it: to hold half of officeMinLabelRoom above
// inner when an axis beside the plot area draws labels, and
// officeRightRoom right of inner and of the labels under or over it unless
// a legend stands at the right.
//
// # Corpus evidence
//
//   - probe, xlsx: with 8 pt value labels, Excel puts the plot area
//     12.5 pt under a 10 pt title, where 10, 14 and 18 pt labels put it
//     6.5 pt plus half their line under it.
//   - litho/xlsx: a bar chart, whose category labels lie between its
//     ticks, has its plot area 12.5 pt under a 14 pt title.
//   - litho/docx: Word puts the plot area 12.5 pt under a 14 pt title
//     with 9 pt value labels, and with 9 pt category labels on a bar
//     chart.
//   - probe, xlsx: Excel's plot area ends 11.0 pt from the chart's right
//     edge, whatever its height.
//   - litho/xlsx: 11.0 pt as well under 14 pt titles; 18.61 pt when the
//     last label of a horizontal value axis, 15.2 pt wide, is centred on
//     the plot area's right edge, and 16.07 pt for a scatter chart's
//     10.1 pt one; 30.98 pt with value labels 15.2 pt wide right of the
//     plot area, 6.5 pt from the edge.
func (rd *renderer) officeRoom(bb, inner typeset.Rect, dims []*dim) typeset.Rect {
	if !rd.officeLayout() {
		return bb
	}
	right := inner.X + inner.Width
	for _, d := range dims {
		if d.vertical && d.labelled() {
			bb = union(bb, typeset.Rect{X: inner.X, Y: inner.Y - officeMinLabelRoom/2, Width: inner.Width})
			break
		}
	}
	for _, d := range dims {
		if d.vertical {
			continue
		}
		for _, l := range d.labels {
			w, _ := l.b.rotated()
			right = math.Max(right, l.cx+w/2)
		}
	}
	if lg := rd.c.Legend; lg != nil && !lg.Overlay && (lg.Position == LegendRight || lg.Position == LegendTopRight) {
		// A legend at the side keeps LibreOffice's distance to the plot
		// area (officePlot).
		return bb
	}
	return union(bb, typeset.Rect{X: right, Y: inner.Y, Width: officeRightRoom})
}

// officeOrigin returns the value Office counts the ticks of the linear
// scale sc from: its minimum, so that a minimum the file fixes off the
// major unit's multiples still bears the first label.
//
// # Corpus evidence
//
//   - litho/xlsx: an axis fixed from 50 to 200 with an automatic unit of
//     20 is labelled 50, 70, ..., 190 by Excel.
func officeOrigin(sc scale) float64 {
	if sc.log > 0 {
		return 0
	}
	return sc.min
}

// officeIntervals returns the most major intervals Office's automatic
// scale gives a value axis length points long whose labels are lineH
// points high: as many labels as fit end to end along it, one interval
// fewer, at least one and at most maxAutoTicks. Each label takes its
// line height rounded to a whole pixel at 96 pixels an inch.
//
// The rounding is the one rule found that fits Excel's and PowerPoint's
// choices together: a height in proportion to the size fits either
// application alone, not both, and floor or ceiling rounding fit
// neither.
//
// # Corpus evidence
//
//   - probe, xlsx: on 80 column charts of one series, frames 5 to 30 rows
//     high, labels of 8, 10, 14 and 18 pt Calibri, Excel's choice is the
//     smallest step of 1, 2 and 5 times a power of ten whose labels fit
//     by this count, on every chart. The tightest charts bound the height
//     of a 10 pt label to (11.77, 12.22] pt and of a 14 pt one to more
//     than 17.08 pt (five labels refused on 85.42 pt). Charts too short
//     for two labels keep one interval.
//   - probe, pptx: on 12 column charts with 10 pt labels, PowerPoint's
//     choice follows the same count with a label of at most 12.13 pt (six
//     labels on 72.8 pt), so a height of 1.2207 ems, Calibri's line,
//     misses one chart there, and 1.20 ems misses Excel's 14 pt chart.
//     The rounded height (12.0 pt at 10 pt, 17.25 pt at 14 pt) fits all
//     92.
func officeIntervals(length, lineH float64) int {
	px := math.Round(lineH * 96 / 72)
	if !numeric.Positive(px) {
		return maxAutoTicks
	}
	n := math.Floor(length/(px*72/96)) - 1
	return int(math.Max(1, math.Min(n, maxAutoTicks)))
}
