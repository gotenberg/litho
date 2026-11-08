package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
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
func (rd *renderer) legend(page compose.Rect, rem *compose.Rect) {
	lg := rd.c.Legend
	fs := lg.Style.Size
	if !(fs >= minFontSize) {
		fs = 10
	}
	xPad := max(minLegendSpacing, fs*0.33)
	xOff := max(minLegendSpacing, fs*0.66)
	yPad := max(minLegendSpacing, fs*0.2)
	yOff := max(minLegendSpacing, fs*0.2)
	symDist := max(minLegendSpacing, fs*0.22)
	symH := fs * 0.6
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
	if side {
		maxRows := int((rem.Height - 2*yPad) / maxEntryH)
		if maxRows < 1 {
			maxRows = 1
		}
		cols = (n + maxRows - 1) / maxRows
		rows = (n + cols - 1) / cols
	} else {
		maxCols := int((rem.Width - 2*xPad) / maxEntryW)
		if maxCols < 1 {
			maxCols = 1
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
	w, h := 2*xPad+float64(cols-1)*xOff, 2*yPad+float64(rows-1)*yOff
	for _, v := range colW {
		w += v
	}
	for _, v := range rowH {
		h += v
	}
	var x, y float64
	switch lg.Position {
	case LegendLeft:
		x, y = page.X+legendSideMargin, page.Y+page.Height/2-h/2
	case LegendTop:
		x, y = page.X+page.Width/2-w/2, rem.Y+legendTopMargin
	case LegendBottom:
		x, y = page.X+page.Width/2-w/2, rem.Y+rem.Height-legendTopMargin-h
	case LegendTopRight:
		x, y = page.X+page.Width-legendSideMargin-w, rem.Y+legendTopMargin
	case LegendRight:
		x, y = page.X+page.Width-legendSideMargin-w, page.Y+page.Height/2-h/2
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
			if nx := page.X + page.Width - w - edge; nx > page.X+page.Width/4 {
				x = nx
			}
		}
		if y+h > page.Y+page.Height {
			if ny := page.Y + page.Height - h - edge; ny > page.Y+page.Height/4 {
				y = ny
			}
		}
	}
	x = max(page.X, min(x, page.X+page.Width-w))
	y = max(page.Y, min(y, page.Y+page.Height-h))
	rd.frame(compose.Rect{X: x, Y: y, Width: w, Height: h}, lg.Frame)
	cx := x + xPad
	for c := 0; c < cols; c++ {
		cy := y + yPad
		for r := 0; r < rows; r++ {
			i := c + r*cols
			if i >= n {
				break
			}
			e := entries[i]
			lineH := e.label.lineH[0]
			sy := cy + (lineH-symH)/2
			rd.key(e, compose.Rect{X: cx, Y: sy, Width: symW, Height: symH})
			rd.out = append(rd.out, e.label.draw(cx+symPlus+e.label.w/2, cy+e.label.h/2, alignLeft)...)
			cy += rowH[r] + yOff
		}
		cx += colW[c] + xOff
	}
	if lg.Overlay {
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

// key draws an entry's legend key into r.
func (rd *renderer) key(e legendEntry, r compose.Rect) {
	if e.circleKey {
		d := math.Min(r.Width, r.Height) / 2
		rd.paint(ellipse(r.X+r.Width/2, r.Y+r.Height/2, d, d), e.fill, nil)
		return
	}
	if e.markerKey {
		m := e.marker
		m.Size = math.Min(m.Size, r.Height)
		rd.marker(m, r.X+r.Width/2, r.Y+r.Height/2)
		return
	}
	if !e.lineKey {
		rd.paint(rectPath(r).Ops, e.fill, keyBorder(e.border))
		return
	}
	cy := r.Y + r.Height/2
	if e.border != nil {
		b := *e.border
		b.Width = math.Min(b.Width, r.Height)
		rd.add(line(r.X, cy, r.X+r.Width, cy, &b))
	}
	m := e.marker
	m.Size = math.Min(m.Size, r.Height)
	rd.marker(m, r.X+r.Width/2, cy)
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
