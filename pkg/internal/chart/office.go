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
	// officeTopPad is the distance from the chart's top edge to what
	// comes first under it when no title stands there: a legend above the
	// plot area, or the room of the axis labels.
	officeTopPad = 5.0
	// officeSideKeyPad is the distance from the edge of a legend beside
	// the plot area to its keys, and officeSideTextPad from its longest
	// text to its other edge; the legend stands officePad from the
	// chart's edge.
	officeSideKeyPad  = 4.5
	officeSideTextPad = 3.6
	// officeSideLegendGap separates a legend beside the plot area from the
	// room of the plot area and its axis labels.
	officeSideLegendGap = 7.0
)

// The key of an entry of a legend Office places, in ems of the legend's
// font size: a square officeKeySize wide whose centre stands
// officeKeyRaise over the text's baseline, officeKeyGap before the text.
//
// # Corpus evidence
//
//   - probe, xlsx: Excel's keys are 5.49 pt squares at 10 pt, centred
//     2.86 pt over the baseline, 2.38 to 2.44 pt before the text, in
//     legends above, below and beside the plot area.
//   - litho/docx: Word's are 4.94 pt squares at 9 pt, centred 2.61 pt
//     over the baseline, 2.21 to 2.31 pt before the text.
const (
	officeKeySize  = 0.549
	officeKeyRaise = 0.2875
	officeKeyGap   = 0.245
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

// officeSideLegend reports a legend Office's rules place left or right of
// the plot area of a chart [renderer.officeLayout] places, at no position
// the file fixes: at the left, at the right or at the top right.
func (rd *renderer) officeSideLegend() bool {
	lg := rd.c.Legend
	return lg != nil && lg.Layout == nil && (lg.Position == LegendLeft || lg.Position == LegendRight || lg.Position == LegendTopRight) && rd.officeLayout()
}

// officePlot draws the title and legend of r's chart where Office puts
// them and returns the rectangle left for the plot area and its axis
// labels.
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
//   - probe, xlsx: without a title, Excel puts the plot area 11.10 pt
//     under the chart's top in frames 87, 145 and 203 pt high, half a
//     10 pt label line under officeTopPad, and a legend above it 5.0 pt
//     under the top.
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
	dx := layoutDistance * r.Width
	if !top && (rd.officeSideLegend() || rd.officeLegend() && c.Legend.Position == LegendTop) {
		// What comes first under an untitled chart's top, a legend above
		// the plot area or the room a legend beside it is centred in,
		// starts officeTopPad under it.
		rem.Y += officeTopPad
		rem.Height -= officeTopPad
		top = true
	}
	if lg := c.Legend; lg != nil {
		side := rd.officeSideLegend()
		rd.legend(r, &rem)
		if !lg.Overlay {
			switch lg.Position {
			case LegendLeft:
				left = dx
				if side {
					left = 0
				}
			case LegendRight:
				right = dx
				if side {
					right = 0
				}
			case LegendTopRight:
				right = dx
				if side {
					right = 0
				}
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
		rem.Y += officeTopPad
		rem.Height -= officeTopPad
	}
	return typeset.Rect{X: rem.X + left, Y: rem.Y, Width: rem.Width - left - right, Height: rem.Height - bottom}
}

// officeTitle draws t at the top of the chart's rectangle r, its first
// line officeTitleTop under r's top unless the file places it, and returns
// the bottom of its last line; ok is false when t draws nothing.
func (rd *renderer) officeTitle(t *Title, r typeset.Rect) (float64, bool) {
	b := rd.measure(t.Text, t.Style, float64(r.Width*0.8)-float64(2*titleHPad*t.Style.Size))
	if b == nil {
		return 0, false
	}
	w, h := b.rotated()
	cx, top := r.X+float64(r.Width/2), r.Y+officeTitleTop
	if t.Layout != nil {
		lr := t.Layout.rect(r)
		cx, top = lr.X+float64(w/2), lr.Y
	}
	px, py := float64(titleHPad*b.style.Size), float64(titleVPad*b.style.Size)
	rd.frame(typeset.Rect{X: cx - float64(w/2) - px, Y: top - py, Width: w + 2*px, Height: h + 2*py}, t.Frame)
	rd.out = append(rd.out, b.draw(cx, top+float64(h/2), alignCenter)...)
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
		return float64(officeSideLabelGap * size)
	}
	return float64(officeUnderLabelGap * size)
}

// officeRoom returns bb, the plot area inner with its axis labels, grown
// as Office's layout grows it: to hold half of officeMinLabelRoom above
// inner when an axis beside the plot area draws labels, and
// officeRightRoom right of inner and of the labels under or over it unless
// a legend stands at the top right.
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
			right = math.Max(right, l.cx+float64(w/2))
		}
	}
	if lg := rd.c.Legend; lg != nil && !lg.Overlay && (lg.Position == LegendTopRight || lg.Position == LegendRight) && !rd.officeSideLegend() {
		// A legend the file places keeps LibreOffice's distance to the
		// plot area (officePlot).
		return bb
	}
	return union(bb, typeset.Rect{X: right, Y: inner.Y, Width: officeRightRoom})
}

// officeTitleRoom returns the room the title b of d's axis takes across
// the axis: its height for an axis under or over the plot area, its width
// for one beside it.
//
// # Corpus evidence
//
//   - probe, xlsx: a 10 pt bold title of a value axis beside the plot
//     area moves the plot area 21.20 pt right, a 14 pt one 26.09 pt,
//     their line and 8.99 pt, however long the title; the title's line
//     ends 3.04 pt left of the value labels and starts 5.97 pt right of
//     the chart's edge pad, centred on the plot area's height. A 14 pt
//     title of the category axis is centred on the plot area's width.
func officeTitleRoom(d *dim, b *block) float64 {
	w, h := b.rotated()
	if !d.vertical {
		return h
	}
	if b.style.Rotation == 0 {
		return w + 2*officeUprightTitleInset
	}
	return w
}

// officeSideTitleWrap is the length a line of the title of an axis
// beside the plot area wraps at, in heights of the chart, and
// officeSideTitleInset the least distance from the plot area's top to the
// title's top: a title centred on the plot area that would rise higher
// starts there and runs down past the centre, out of the chart if it must.
//
// # Corpus evidence
//
//   - probe, xlsx: Excel's 10 pt bold value-axis titles of 24 and 38
//     characters, 97.4 and 153.8 pt long, are centred on the plot area
//     when they fit its height and otherwise start 3.07 to 3.22 pt under
//     its top, in charts 101.64, 145.2 and 203.28 pt high, one, two and
//     three lines long; the lines wrap within 0.757 to 0.769 of the
//     chart's height (153.8 pt kept on one line at 203.28 pt, 78.2 pt
//     broken at 101.64 pt).
const (
	officeSideTitleWrap  = 0.76
	officeSideTitleInset = 3.2
)

// officeUprightTitleInset pads each side of the unturned title of an axis
// beside the plot area, in points.
//
// # Corpus evidence
//
//   - litho/xlsx: charts_scatter_bubble's 10 pt bold "y", unturned by
//     its file, takes 16.73 pt beside the plot area for 4.72 pt of text,
//     7.50 pt from the chart's edge pad and 4.55 pt from the value
//     labels: 1.5 pt more on each side than a turned title's line.
const officeUprightTitleInset = 1.5

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

// categoryWrap returns the share of the distance between ticks at which a
// category label under or over the plot area breaks: 5% short of it under
// LibreOffice's rules, to keep a gap between labels (chart2's
// createTextShapes), and the whole distance in a chart Office lays out.
//
// # Corpus evidence
//
//   - probe, xlsx: Excel breaks "Category number three" into its three
//     words under ticks 36.85 pt apart, "Category" 36.52 pt wide on a line
//     of its own, and keeps "number one", 49.9 pt, on one line under
//     ticks 54.05 pt apart while it breaks "number three", 56.4 pt.
func (rd *renderer) categoryWrap() float64 {
	if rd.officeLayout() {
		return 1
	}
	return 0.95
}

// officeLabelExcess returns how much more room the labels of x, a
// category axis under the plot area, took at the last layout pass than
// one line of them: the length Office adds back to a value axis beside the
// plot area before it counts its labels (officeIntervals), as if the
// category labels had neither wrapped nor turned.
//
// # Corpus evidence
//
//   - probe, xlsx: column charts 145.2 pt high whose six long category
//     labels wrap to two or three lines, or turn by 45 degrees, have plot
//     areas 63.43 and 65.65 pt high, too short for 0 to 50 by 10 by
//     officeIntervals, and Excel labels them 0 to 50 by 10, the count at
//     87.84 pt, the height with labels of one line; line charts over dates
//     whose labels turn have 60.86 pt and 0 to 10 by 2, the count at
//     87.84 pt as well.
//   - probe, xlsx: the same labels turned in charts 116.16, 203.28 and
//     290.4 pt high, plot areas 39.81, 82.17 and 169.29 pt high, are
//     labelled 0 to 60 by 20 and 0 to 50 by 5, the counts at 58.80,
//     145.92 and 233.04 pt.
func (rd *renderer) officeLabelExcess(x *dim) float64 {
	if x == nil || !x.category || x.vertical || x.outside > 0 || !rd.officeLayout() {
		return 0
	}
	ext, line := 0.0, 0.0
	for _, l := range x.labels {
		_, h := l.b.rotated()
		ext = math.Max(ext, l.cy+float64(h/2)-x.lineAt)
		if len(l.b.lines) > 0 {
			line = math.Max(line, faceLineHeight(l.b.lines[0], l.b.style.Size))
		}
	}
	if line == 0 {
		return 0
	}
	return math.Max(0, ext-officeLabelGap(x, fontSize(x.style().Size))-line)
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
	h := officeLabelLine(lineH)
	if !numeric.Positive(h) {
		return maxAutoTicks
	}
	return officeClampIntervals(math.Floor(length/h) - 1)
}

// officeHorizontalIntervals returns the most major intervals Office's
// automatic scale gives a value axis drawn across the chart, length points
// long, whose labels are lineH points high: one interval for every
// officeLabelPitch label lines along it, at least one and at most
// maxAutoTicks. The labels' widths do not count.
//
// # Corpus evidence
//
//   - probe, xlsx: on 34 bar charts of one series, axes 110 to 688 pt
//     long, labels of 8, 10 and 14 pt Calibri from 5 to 25 pt wide,
//     Excel's choice is the smallest step of 1, 2 and 5 times a power of
//     ten whose intervals are each at least three label lines long, on
//     every chart. At 10 pt the intervals bound the length of three lines
//     to (35.73, 36.70] pt whatever the labels' width: 9 intervals of
//     35.73 pt refused under labels 15 pt wide, 3 of 36.70 pt kept under
//     labels 25 pt wide. At 8 pt intervals of 32.5 pt are kept, at 14 pt
//     intervals of 35.4 pt refused, which a fixed length misses and three
//     rounded lines (29.25 pt and 51.75 pt) fit.
//   - probe, xlsx: on 15 scatter charts, frames 4 to 10 columns wide, the
//     horizontal axis follows the same count, and the vertical one the
//     count of officeIntervals.
func officeHorizontalIntervals(length, lineH float64) int {
	h := officeLabelLine(lineH)
	if !numeric.Positive(h) {
		return maxAutoTicks
	}
	return officeClampIntervals(math.Floor(length / float64(officeLabelPitch*h)))
}

// officeLabelPitch is the length of a major interval of a value axis drawn
// across the chart, in label lines (officeHorizontalIntervals).
const officeLabelPitch = 3

// officeLabelLine returns the line height lineH rounded to a whole pixel
// at 96 pixels an inch, the height Office's automatic scale counts a label
// as.
func officeLabelLine(lineH float64) float64 {
	return math.Round(lineH*96/72) * 72 / 96
}

// officeClampIntervals returns n intervals, at least one and at most
// maxAutoTicks.
func officeClampIntervals(n float64) int {
	return int(math.Max(1, math.Min(n, maxAutoTicks)))
}
