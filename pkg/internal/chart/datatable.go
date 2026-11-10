package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// dataTable is the laid-out data table of a category dimension.
type dataTable struct {
	// x is the table's left edge, the first column's; top is the axis
	// line.
	x, top float64
	// nameW is the first column's width, rowH a row's height.
	nameW, rowH float64
	// symW and symH size a legend key.
	symW, symH float64
	series     []tableSeries
}

// tableSeries is one row of the data table.
type tableSeries struct {
	g    *Group
	s    *Series
	name *block
}

// tableKeyMargin is the space each side of a key in a data table's first
// column: 100 hundredths of a millimetre (chart2's DataTableView
// constSymbolMargin).
const tableKeyMargin = 100 * mm100

// layoutDataTable lays the chart's data table out under the category
// dimension d, as chart2's DataTableView does: cells padded by 0.18 of the
// font size across and 0.30 down, the first column as wide as the longest
// series name and its key, the others the categories' width. It returns
// nil when the chart has no table or d is not a horizontal category axis.
func (rd *renderer) layoutDataTable(d *dim) *dataTable {
	dt := rd.c.DataTable
	if dt == nil || !d.category || d.vertical || d.n == 0 {
		return nil
	}
	st := dt.Style
	size := textStyle(st).Size
	t := &dataTable{top: d.lineAt}
	t.rowH = size*chartLineHeight + 2*titleVPad*size
	t.symH = size * 0.6
	t.symW = t.symH
	for _, g := range rd.c.Groups {
		if lined(g.Kind) && dt.Keys {
			t.symW = math.Max(t.symW, lineKeyWidth)
		}
		for _, s := range g.Series {
			if len(t.series) >= MaxSeries {
				break
			}
			ts := tableSeries{g: g, s: s, name: rd.measure(s.Name, st, 0)}
			t.series = append(t.series, ts)
			if ts.name != nil {
				t.nameW = math.Max(t.nameW, ts.name.w)
			}
		}
	}
	t.nameW += 2 * titleHPad * size
	if dt.Keys {
		t.nameW += t.symW + 2*tableKeyMargin
	}
	t.x = d.inner.X - t.nameW
	return t
}

// height returns the table's height: the header row and a row a series.
func (t *dataTable) height() float64 {
	return t.rowH * float64(len(t.series)+1)
}

// drawDataTable draws t under d: the category names, then the series'
// names and values, with the borders chart2's DataTableView draws.
func (rd *renderer) drawDataTable(d *dim, t *dataTable) {
	dt := rd.c.DataTable
	st := dt.Style
	n := d.n
	colW := d.length() / float64(n)
	rows := len(t.series)
	cell := func(s string, x0, x1, y float64, al align, indent float64) {
		b := rd.measure(s, st, 0)
		if b == nil {
			return
		}
		cy := y + t.rowH/2
		cx := (x0 + x1) / 2
		if al == alignLeft {
			cx = x0 + indent + titleHPad*b.style.Size + b.w/2
		}
		if f := dt.Fill; f != nil && !f.None && f.Color.A > 0 {
			rd.add(paintPath(rectPath(compose.Rect{X: cx - b.w/2, Y: cy - b.h/2, Width: b.w, Height: b.h}).Ops, f, nil))
		}
		rd.out = append(rd.out, b.draw(cx, cy, alignCenter)...)
	}
	hline := func(x0, x1, y float64) { rd.add(line(x0, y, x1, y, dt.Line)) }
	vline := func(x, y0, y1 float64) { rd.add(line(x, y0, x, y1, dt.Line)) }
	for j := 0; j < n && j < len(d.names); j++ {
		x0 := d.inner.X + float64(j)*colW
		cell(d.names[j], x0, x0+colW, t.top, alignCenter, 0)
		col := j + 1
		if (dt.Outline && col == 1) || (dt.VBorder && col > 1 && col < n) {
			vline(x0, t.top, t.top+t.rowH)
		}
		if (dt.Outline && col == n) || (dt.VBorder && col > 1 && col < n) {
			vline(x0+colW, t.top, t.top+t.rowH)
		}
		if dt.Outline {
			hline(x0, x0+colW, t.top)
		}
	}
	for i, ts := range t.series {
		y := t.top + t.rowH*float64(i+1)
		row := i + 1
		indent := 0.0
		if dt.Keys {
			indent = t.symW + 2*tableKeyMargin
			e := rd.entry(ts.g, ts.s, -1, "", st, 0)
			rd.key(e, compose.Rect{X: t.x + tableKeyMargin, Y: y + (t.rowH-t.symH)/2, Width: t.symW, Height: t.symH})
		}
		if ts.s.Name != "" {
			cell(ts.s.Name, t.x, d.inner.X, y, alignLeft, indent)
		}
		if dt.Outline {
			vline(t.x, y, y+t.rowH)
			vline(d.inner.X, y, y+t.rowH)
		}
		if (dt.Outline && row == 1) || (dt.HBorder && row > 1 && row < rows) {
			hline(t.x, d.inner.X, y)
		}
		if (dt.Outline && row == rows) || (dt.HBorder && row > 1 && row < rows) {
			hline(t.x, d.inner.X, y+t.rowH)
		}
		vals := rd.data(ts.s.Values)
		for j := range n {
			x0 := d.inner.X + float64(j)*colW
			if txt := vals.label(j, rd.c.Locale); txt != "" {
				cell(txt, x0, x0+colW, y, alignCenter, 0)
			}
			col := j + 1
			if col > 1 && dt.VBorder {
				vline(x0, y, y+t.rowH)
			}
			if row > 1 && dt.HBorder {
				hline(x0, x0+colW, y)
			}
			if row == rows && dt.Outline {
				hline(x0, x0+colW, y+t.rowH)
			}
			if col == n && dt.Outline {
				vline(x0+colW, y, y+t.rowH)
			}
		}
	}
}
