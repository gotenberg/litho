package sheet

import (
	"sort"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// colX returns the x of column c's left edge from the sheet's origin, in
// points, hidden columns taking no room.
func (g *grid) colX(c int) float64 {
	c = min(max(c, 0), maxColumns)
	if g.colPrefix == nil {
		g.colPrefix = make([]float64, maxColumns+1)
		for i := range maxColumns {
			g.colPrefix[i+1] = g.colPrefix[i] + g.colWidth(i)
		}
	}
	return g.colPrefix[c]
}

// rowY returns the y of row r's top edge from the sheet's origin, in
// points, hidden rows taking no room.
func (g *grid) rowY(r int) float64 {
	r = min(max(r, 0), maxRows)
	for len(g.rowPrefix) <= r {
		n := len(g.rowPrefix)
		if n == 0 {
			g.rowPrefix = append(g.rowPrefix, 0)
			continue
		}
		g.rowPrefix = append(g.rowPrefix, g.rowPrefix[n-1]+g.rowHeight(n-1))
	}
	return g.rowPrefix[r]
}

// colAt returns the column whose span holds x, from the sheet's origin.
func (g *grid) colAt(x float64) int {
	g.colX(0)
	i := sort.SearchFloat64s(g.colPrefix, x)
	if i > 0 && (i >= len(g.colPrefix) || g.colPrefix[i] > x) {
		i--
	}
	return min(i, maxColumns-1)
}

// rowAt returns the row whose span holds y, from the sheet's origin,
// looking no further than limit.
func (g *grid) rowAt(y float64, limit int) int {
	g.rowY(limit + 1)
	p := g.rowPrefix[:limit+2]
	i := sort.SearchFloat64s(p, y)
	if i > 0 && (i >= len(p) || p[i] > y) {
		i--
	}
	return min(i, limit)
}

// anchorPoint returns the sheet position of an anchor: its cell's corner
// plus the offset, the offset bounded by the cell, as Calc places a
// drawing anchored in a cell.
func (g *grid) anchorPoint(a Anchor) (float64, float64) {
	r := min(max(a.Row, 0), maxRows-1)
	c := min(max(a.Column, 0), maxColumns-1)
	dx := min(max(a.DX, 0), g.colWidth(c))
	dy := min(max(a.DY, 0), g.rowHeight(r))
	return g.colX(c) + dx, g.rowY(r) + dy
}

// drawingRect returns a drawing's box on the sheet, in points from the
// sheet's origin.
func (g *grid) drawingRect(d *Drawing) compose.Rect {
	if d.Absolute {
		return compose.Rect{X: max(d.X, 0), Y: max(d.Y, 0), Width: clampSide(d.Width), Height: clampSide(d.Height)}
	}
	x, y := g.anchorPoint(d.From)
	if d.To != nil {
		x2, y2 := g.anchorPoint(*d.To)
		return compose.Rect{X: x, Y: y, Width: max(x2-x, 0), Height: max(y2-y, 0)}
	}
	return compose.Rect{X: x, Y: y, Width: clampSide(d.Width), Height: clampSide(d.Height)}
}

// clampSide returns v within [0, maxSide], 0 for a value that is not a
// number.
func clampSide(v float64) float64 {
	if !(v > 0) {
		return 0
	}
	return min(v, maxSide)
}

// drawingCells returns the cells a drawing's box covers, false for an
// empty box.
func (g *grid) drawingCells(d *Drawing) (Range, bool) {
	rc := g.drawingRect(d)
	if rc.Width <= 0 && rc.Height <= 0 {
		return Range{}, false
	}
	var limit int
	switch {
	case d.Absolute:
		limit = min(int(rc.Y+rc.Height)+1, maxRows-1)
	case d.To != nil:
		limit = min(max(d.To.Row, 0)+1, maxRows-1)
	default:
		limit = min(max(d.From.Row, 0)+maxDrawingRows, maxRows-1)
	}
	r := Range{FirstRow: g.rowAt(rc.Y, limit), FirstColumn: g.colAt(rc.X)}
	r.LastRow = g.rowAt(rc.Y+rc.Height-1e-6, limit)
	r.LastColumn = g.colAt(rc.X + rc.Width - 1e-6)
	r.LastRow = max(r.LastRow, r.FirstRow)
	r.LastColumn = max(r.LastColumn, r.FirstColumn)
	return r, true
}

// maxDrawingRows bounds how many rows below its anchor a one-cell
// anchored drawing's extent is looked for. Anti-DoS guard, not
// spec-mandated: a drawing of maxSide points covers at most that many rows
// of one point.
const maxDrawingRows = maxSide
