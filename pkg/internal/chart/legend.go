package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// maxLegendEntries caps the entries a legend draws. Anti-DoS guard, not
// spec-mandated: a legend of more entries than this does not fit a page.
const maxLegendEntries = 512

// legendEntry is one entry of the legend: its key and its text.
type legendEntry struct {
	label  *block
	fill   *Paint
	border *Stroke
	// lineKey draws the key as a line with the marker, as line series do;
	// markerKey draws the marker alone, as a scatter series without a line
	// does.
	lineKey, markerKey bool
	// circleKey draws the key as a filled circle, as the legend of a kind
	// the renderer does not draw has (chart2's UnsupportedChart).
	circleKey bool
	marker    Marker
}

// legendEntries returns the legend's entries in order: a series each, or
// a category each for a group whose points vary in colour.
func (rd *renderer) legendEntries(st TextStyle, maxW float64) []legendEntry {
	var out []legendEntry
	hidden := map[int]bool{}
	for _, h := range rd.c.Legend.Hidden {
		hidden[h] = true
	}
	n := 0
	swapped := len(rd.c.Groups) > 0 && rd.c.Groups[0].Kind == Bar && rd.c.Groups[0].Horizontal
	for _, g := range rd.c.Groups {
		// A pie lists its categories; another group whose points vary in
		// colour does too.
		vary := (g.Kind == Pie || g.Kind == Doughnut || g.VaryColors && len(g.Series) == 1) && len(g.Series) > 0
		if vary {
			s := g.Series[0]
			cnt := max(s.Values.Len(), s.Categories.Len())
			for j := 0; j < cnt && len(out) < maxLegendEntries; j++ {
				name := s.Categories.label(j, rd.c.Locale)
				if name == "" && s.Categories.Len() == 0 {
					name = formatGeneral(float64(j + 1))
				}
				if !hidden[n] {
					out = append(out, rd.entry(g, s, j, name, st, maxW))
				}
				n++
			}
			continue
		}
		// Series enter in reverse on a horizontal bar chart that does not
		// stack, and on a stacked chart with the legend at its side, so
		// the legend reads as the chart does (chart2's
		// VSeriesPlotter::createLegendEntries).
		stacked := g.Grouping != Clustered
		reverse := stacked && (rd.c.Legend.Position == LegendLeft || rd.c.Legend.Position == LegendRight)
		if swapped {
			reverse = !stacked
		}
		for _, s := range g.Series {
			if len(out) >= maxLegendEntries {
				break
			}
			if !s.NoLegend && !hidden[n] {
				e := rd.entry(g, s, -1, s.Name, st, maxW)
				if reverse {
					out = append([]legendEntry{e}, out...)
				} else {
					out = append(out, e)
				}
			}
			n++
			// A series' trendlines follow it, each a line key (chart2's
			// VSeriesPlotter::createLegendEntriesForSeries).
			for _, t := range s.Trendlines {
				if len(out) >= maxLegendEntries {
					break
				}
				out = append(out, legendEntry{label: rd.measure(trendName(t, s, rd.office()), st, maxW), border: rd.trendStroke(g, s, t), lineKey: true, marker: Marker{Symbol: MarkerNone}})
			}
		}
	}
	return out
}

// entry builds the legend entry of series s, or of its point j.
func (rd *renderer) entry(g *Group, s *Series, j int, name string, st TextStyle, maxW float64) legendEntry {
	e := legendEntry{label: rd.measure(name, st, maxW), fill: rd.fillOf(g, s, j), border: rd.borderOf(g, s, j)}
	if g.Kind == Unsupported {
		e.circleKey, e.border = true, nil
		return e
	}
	if lined(g.Kind) && (g.Kind != Radar || !g.Filled) {
		e.marker = rd.markerOf(g, s, j)
		if e.border != nil && !e.border.None {
			e.lineKey = true
		} else {
			e.markerKey = true
		}
	}
	return e
}

// legend lays the legend out and places it at its side of the chart, as
// LibreOffice's VLegend does, taking its room from rem unless it overlays.
func (rd *renderer) legend(page typeset.Rect, rem *typeset.Rect) {
	lg := rd.c.Legend
	fs := fontSize(lg.Style.Size)
	xPad := max(minLegendSpacing, fs*0.33)
	xOff := max(minLegendSpacing, fs*0.66)
	yPad := max(minLegendSpacing, fs*0.2)
	yOff := max(minLegendSpacing, fs*0.2)
	office, officeSide := rd.officeLegend(), rd.officeSideLegend()
	if office || officeSide {
		yPad, yOff = officeLegendPad, 2*officeLegendPad
	}
	// leftPad and rightPad pad the entries from the legend's edges.
	leftPad, rightPad := xPad, xPad
	if officeSide {
		leftPad, rightPad = officeSideKeyPad, officeSideTextPad
	}
	symDist := max(minLegendSpacing, fs*0.22)
	symH := float64(fs * 0.6)
	if office || officeSide {
		symDist, symH = float64(fs*officeKeyGap), float64(fs*officeKeySize)
	}
	symW := symH
	side := lg.Position == LegendLeft || lg.Position == LegendRight || lg.Position == LegendTopRight
	maxText := rem.Width * 3 / 10
	if !side {
		maxText = rem.Width
	}
	entries := rd.legendEntries(lg.Style, maxText)
	for _, e := range entries {
		if e.lineKey {
			symW = max(symW, lineKeyWidth)
		}
	}
	var live []legendEntry
	for _, e := range entries {
		if e.label != nil {
			live = append(live, e)
		}
	}
	if len(live) == 0 {
		return
	}
	entries = live
	entryW, entryH := 0.0, 0.0
	for _, e := range entries {
		entryW = max(entryW, e.label.w)
		entryH = max(entryH, e.label.h)
	}
	symPlus := symW + symDist
	maxEntryW := xOff + symPlus + entryW
	maxEntryH := yOff + entryH
	n := len(entries)
	var cols, rows int
	switch {
	case officeSide:
		// Office keeps a legend beside the plot area in one column and
		// drops the entries its height does not hold (officeSideRows).
		n = officeSideRows(page, *rem, n, maxEntryH, yPad, yOff)
		entries = entries[:n]
		cols, rows = 1, n
	case side:
		maxRows := max(int((rem.Height-2*yPad)/maxEntryH), 1)
		cols = (n + maxRows - 1) / maxRows
		rows = (n + cols - 1) / cols
	default:
		maxCols := max(int((rem.Width-2*xPad)/maxEntryW), 1)
		if office && oneRow(entries, symPlus, xOff, rem.Width-2*xPad) {
			maxCols = n
		}
		rows = (n + maxCols - 1) / maxCols
		cols = (n + rows - 1) / rows
	}
	colW := make([]float64, cols)
	rowH := make([]float64, rows)
	for i, e := range entries {
		c, r := i%cols, i/cols
		colW[c] = max(colW[c], e.label.w+symPlus)
		rowH[r] = max(rowH[r], e.label.h)
	}
	w, h := leftPad+rightPad+float64(float64(cols-1)*xOff), float64(2*yPad)+float64(float64(rows-1)*yOff)
	for _, v := range colW {
		w += v
	}
	for _, v := range rowH {
		h += v
	}
	var x, y float64
	corner := false
	switch lg.Position {
	case LegendLeft:
		x, y = page.X+legendSideMargin, page.Y+float64(page.Height/2)-float64(h/2)
		if officeSide {
			x, y = page.X+officePad, officeSideLegendTop(page, *rem, h)
		}
	case LegendTop:
		x, y = page.X+float64(page.Width/2)-float64(w/2), rem.Y+legendTopMargin
		if office {
			y = rem.Y
		}
	case LegendBottom:
		x, y = page.X+float64(page.Width/2)-float64(w/2), rem.Y+rem.Height-legendTopMargin-h
		if office {
			y = page.Y + page.Height - officePad - h
		}
	case LegendTopRight:
		x, y = page.X+page.Width-legendSideMargin-w, rem.Y+legendTopMargin
		if officeSide {
			// The first key stands officeCornerKeyTop under rem's top,
			// officeLegendGap lower when the plot area stays beside the
			// legend.
			l := entries[0].label
			ln := l.lines[0]
			keyTop := yPad + float64((l.lineH[0]-ln.Ascent-ln.Descent)/2) + ln.Ascent - float64(fs*officeKeyRaise) - float64(symH/2)
			corner = officeCornerUnder(page, h)
			x, y = page.X+page.Width-officePad-w, rem.Y+officeCornerKeyTop-keyTop
			if !corner {
				y += officeLegendGap
			}
		}
	case LegendRight:
		x, y = page.X+page.Width-legendSideMargin-w, page.Y+float64(page.Height/2)-float64(h/2)
		if officeSide {
			x, y = page.X+page.Width-officePad-w, officeSideLegendTop(page, *rem, h)
		}
	}
	if l := lg.Layout; l != nil {
		// A placed legend keeps its corner, and its size when the file
		// gives one; one that would leave the chart moves back in by 30
		// hundredths of a millimetre when that keeps it past a quarter of
		// the chart (chart2's lcl_calculatePositionAndRemainingSpace).
		lr := l.rect(page)
		x, y = lr.X, lr.Y
		if lr.Width > 0 && lr.Height > 0 {
			w, h = lr.Width, lr.Height
		}
		const edge = 30 * mm100
		if x+w > page.X+page.Width {
			if nx := page.X + page.Width - w - edge; nx > page.X+float64(page.Width/4) {
				x = nx
			}
		}
		if y+h > page.Y+page.Height {
			if ny := page.Y + page.Height - h - edge; ny > page.Y+float64(page.Height/4) {
				y = ny
			}
		}
	}
	x = max(page.X, min(x, page.X+page.Width-w))
	y = max(page.Y, min(y, page.Y+page.Height-h))
	rd.frame(typeset.Rect{X: x, Y: y, Width: w, Height: h}, lg.Frame)
	cx := x + leftPad
	for c := 0; c < cols; c++ {
		cy := y + yPad
		for r := 0; r < rows; r++ {
			i := c + r*cols
			if i >= n {
				break
			}
			e := entries[i]
			lineH := e.label.lineH[0]
			sy := cy + float64((lineH-symH)/2)
			if office || officeSide {
				l := e.label.lines[0]
				base := cy + float64((lineH-l.Ascent-l.Descent)/2) + l.Ascent
				sy = base - float64(fs*officeKeyRaise) - float64(symH/2)
			}
			rd.key(e, typeset.Rect{X: cx, Y: sy, Width: symW, Height: symH})
			rd.out = append(rd.out, e.label.draw(cx+symPlus+float64(e.label.w/2), cy+float64(e.label.h/2), alignLeft)...)
			cy += rowH[r] + yOff
		}
		cx += colW[c] + xOff
	}
	if lg.Overlay {
		return
	}
	if officeSide {
		// An Office legend beside the plot area keeps officeSideLegendGap
		// from the room of the plot area and its axis labels.
		if lg.Position == LegendLeft {
			d := x + w + officeSideLegendGap - rem.X
			rem.X += d
			rem.Width -= d
		} else {
			rem.Width = x - officeSideLegendGap - rem.X
		}
		if lg.Position == LegendTopRight {
			// A legend in the top right corner moves the plot area down
			// by officeLegendGap beside it; one the plot area goes under
			// moves it down by the legend's height and keeps
			// officeLegendGap more over the chart's bottom.
			d := officeLegendGap
			if corner {
				d = h
				rem.Height -= officeLegendGap
			}
			rem.Y += d
			rem.Height -= d
		}
		return
	}
	if office {
		// An Office legend above or below the plot area keeps
		// officeLegendGap from the room of the axis labels.
		if lg.Position == LegendTop {
			d := y + h + officeLegendGap - rem.Y
			rem.Y += d
			rem.Height -= d
		} else {
			rem.Height = y - officeLegendGap - rem.Y
		}
		return
	}
	switch lg.Position {
	case LegendLeft:
		d := w + legendSideMargin
		rem.X += d
		rem.Width -= d
	case LegendTop, LegendTopRight:
		d := h + legendTopMargin
		rem.Y += d
		rem.Height -= d
	case LegendBottom:
		rem.Height -= h + legendTopMargin
	case LegendRight:
		rem.Width -= w + legendSideMargin
	}
}

// officeSideLegendTop returns the top of a legend h points high that
// Office puts beside the plot area: centred between rem's top, under the
// chart's title or officeTopPad under an untitled chart's top, and
// officePad over the bottom of the chart's rectangle page.
//
// # Corpus evidence
//
//   - probe, xlsx: a legend of two 10 pt entries left or right of the
//     plot area of a chart 145.2 pt high under a 10 pt title has its
//     first key 71.42 pt under the chart's top, both sides alike, its
//     keys 18.08 pt apart; its first key starts 11.0 pt from the left
//     edge, its longest text ends 10.06 to 10.18 pt from the right edge,
//     and the plot area ends 16.0 pt left of the keys at the right, with
//     entries 6.5 to 84.2 pt wide.
//   - libreoffice/xlsx: in untitled charts 203.28 and 209.16 pt high,
//     chart-area-style-border's and fdo54361-1's one-entry right legends
//     and auto_marker_excel10's ten entries have their middle key centred
//     half the chart's height under its top, to 0.05 pt: centred between
//     officeTopPad under the top and officePad over the bottom, 0.2 pt
//     lower than this rule puts them, as under a title.
func officeSideLegendTop(page, rem typeset.Rect, h float64) float64 {
	bottom := page.Y + page.Height - officePad
	return rem.Y + float64((bottom-rem.Y)/2) - float64(h/2)
}

// officeCornerKeyTop is the distance from the top of the room under a
// chart's title to the first key of a legend in its top right corner
// that the plot area goes under ([officeCornerUnder]).
//
// # Corpus evidence
//
//   - probe, xlsx: legends of 10 pt entries at the top right of charts
//     361.2 pt wide under a 10 pt title have their first key 33.50 pt
//     under the chart's top when the plot area goes under them, 39.50 pt
//     when it stays beside them, the plot area's top 32.31 plus the
//     legend's height or 38.31 pt under it, and 31.05 or 25.05 pt over
//     its bottom; the keys stand 319.39 pt from the left and the plot
//     area ends 57.80 pt from the right edge, as beside a right legend.
const officeCornerKeyTop = 7.29

// officeCornerUnder reports a legend h points high at the top right of
// the chart's rectangle page that Office puts the plot area under rather
// than beside: one shorter than a third of the chart.
//
// A third is a value inside the bounds the charts give; the rule that
// the plot area under the legend keep at least the legend's height fits
// the same charts.
//
// # Corpus evidence
//
//   - probe, xlsx: one entry in a chart 145.2 pt high and two in charts
//     145.2 and 203.28 pt high go over the plot area, legends of 0.12 to
//     0.25 of the chart's height; two entries in a chart 101.64 pt high,
//     three, four, five and six at 145.2 pt and four at 203.28 pt stand
//     beside it, legends of 0.36 to 0.75 of the chart's height, each
//     entry 18.08 pt high.
func officeCornerUnder(page typeset.Rect, h float64) bool {
	return h < float64(page.Height/3)
}

// officeSideRows returns how many of n entries, each pitch points apart
// with yOff between them and yPad above and below them all, a legend
// beside the plot area holds: as many as fit between rem's top and
// officePad over the bottom of the chart's rectangle page, at least one.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: auto_marker_excel10's right legend of twelve
//     10 pt entries 18.08 pt apart, in an untitled chart 209.16 pt high,
//     prints its first ten in one column and drops the last two, where
//     eleven would take 199.0 pt of the 197.66 pt between officeTopPad
//     under the top and officePad over the bottom.
func officeSideRows(page, rem typeset.Rect, n int, pitch, yPad, yOff float64) int {
	avail := page.Y + page.Height - officePad - rem.Y
	fit := int(math.Floor((avail - float64(2*yPad) + yOff) / pitch))
	return max(1, min(n, fit))
}

// oneRow reports entries that fit in one row of width avail, each as wide
// as its own key and label, xOff apart: Office lays a legend above or
// below the plot area out in one row when they do, where LibreOffice
// gives every column the widest entry's width.
//
// # Corpus evidence
//
//   - litho/xlsx: charts_labels_trend_errors chart 1 prints its four
//     entries, "y", "Linear fit", "Poly. (y)" and "3 per. Mov. Avg. (y)",
//     in one row below the plot area in Excel, each as wide as its text.
func oneRow(entries []legendEntry, symPlus, xOff, avail float64) bool {
	w := float64(len(entries)-1) * xOff
	for _, e := range entries {
		w += symPlus + e.label.w
	}
	return w <= avail
}

// key draws an entry's legend key into r.
func (rd *renderer) key(e legendEntry, r typeset.Rect) {
	if e.circleKey {
		d := math.Min(r.Width, r.Height) / 2
		rd.paint(ellipse(r.X+float64(r.Width/2), r.Y+float64(r.Height/2), d, d), e.fill, nil)
		return
	}
	if e.markerKey {
		m := e.marker
		m.Size = math.Min(m.Size, r.Height)
		rd.marker(m, r.X+float64(r.Width/2), r.Y+float64(r.Height/2))
		return
	}
	if !e.lineKey {
		rd.paint(rectPath(r).Ops, e.fill, keyBorder(e.border))
		return
	}
	cy := r.Y + float64(r.Height/2)
	if e.border != nil {
		b := *e.border
		b.Width = math.Min(b.Width, r.Height)
		rd.add(line(r.X, cy, r.X+r.Width, cy, &b))
	}
	m := e.marker
	m.Size = math.Min(m.Size, r.Height)
	rd.marker(m, r.X+float64(r.Width/2), cy)
}

// keyBorder returns the border a legend key draws: the item's border, no
// wider than a hairline-thin 1 point.
func keyBorder(s *Stroke) *Stroke {
	if s == nil {
		return nil
	}
	b := *s
	b.Width = math.Min(b.Width, 1)
	return &b
}
