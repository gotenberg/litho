package sheet

import (
	"slices"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// colX returns the x of column c's left edge from the sheet's origin, in
// printed points ([grid.colWidth]), hidden columns taking no room.
func (g *grid) colX(c int) float64 {
	return colSum(&g.colPrefix, g.colWidth, c)
}

// rowY returns the y of row r's top edge from the sheet's origin, in
// printed points ([grid.rowHeight]), hidden rows taking no room.
func (g *grid) rowY(r int) float64 {
	return rowSum(&g.rowPrefix, g.rowHeight, r)
}

// mirrored reports a sheet whose columns run from the right edge of its
// pages, Calc's print of a right-to-left sheet (ODF 1.3 Part 3, 20.404
// style:writing-mode rl-tb on the table): column A at the right edge of
// the page's area, each cell's text, borders and fill moved with its
// column, the drawings moved with the columns and not turned, the row
// headings on the right, each cell's text set right to left (the
// trailing period of a Hebrew cell at its left end, a percentage sign
// at its number's left). Where Calc puts a drawing anchored in a cell of
// such a sheet is not decided: the one reference that holds some places
// them elsewhere than their cells and offsets say.
//
// # Corpus evidence
//
//   - litho/ods: rtl-sheet prints its four columns from the right margin,
//     column A rightmost, numbers right-aligned in their cells, and
//     "סעיף א." with its period at the left.
//   - libreoffice/ods: sc/tdf137081_RTL_page_anchored.ods prints the label
//     of its dimension line at the area's width less the line's offset,
//     1.3 points from where litho puts it; its rotated polyline lies 17
//     points further left, and its third shape is not printed.
//   - libreoffice/ods: sc/tdf137082_RTL_cell_anchored.ods stores its two
//     cell-anchored shapes 12.22 and 2.2 mm from the sheet's origin
//     though they are anchored in columns C and G, and prints one of
//     them 16 points left of where litho mirrors it.
func (g *grid) mirrored() bool {
	return g.sh.RightToLeft && !g.doc.ExcelRules
}

// layoutColX returns the x of column c's left edge from the sheet's
// origin on the sum of the layout widths ([grid.layoutColWidth]), the
// coordinates drawings take: Calc keeps a drawing in 1/100 mm on the
// sheet, and places the drawing layer of a page at the twips of the
// columns and rows before the page's first, converted whole
// (ScPrintFunc::LocateArea, sc/source/ui/view/printfun.cxx), where the
// cells add up truncated widths and heights ([calcPrinted]).
func (g *grid) layoutColX(c int) float64 {
	if g.doc.ExcelRules {
		return g.colX(c)
	}
	return colSum(&g.layoutColPrefix, g.layoutColWidth, c)
}

// layoutRowY is [grid.layoutColX] for row r's top edge.
func (g *grid) layoutRowY(r int) float64 {
	if g.doc.ExcelRules {
		return g.rowY(r)
	}
	return rowSum(&g.layoutRowPrefix, g.layoutRowHeight, r)
}

// colSum returns the running sum of width over the columns before c,
// filling *prefix for every column on first use.
func colSum(prefix *[]float64, width func(int) float64, c int) float64 {
	c = min(max(c, 0), maxColumns)
	if *prefix == nil {
		p := make([]float64, maxColumns+1)
		for i := range maxColumns {
			p[i+1] = p[i] + width(i)
		}
		*prefix = p
	}
	return (*prefix)[c]
}

// rowSum returns the running sum of height over the rows before r,
// extending *prefix up to r.
func rowSum(prefix *[]float64, height func(int) float64, r int) float64 {
	r = min(max(r, 0), maxRows)
	p := *prefix
	if len(p) <= r {
		// One allocation reaches a far row: append's growth would copy the
		// sums once per doubling on the way.
		p = slices.Grow(p, r+1-len(p))
	}
	for len(p) <= r {
		n := len(p)
		if n == 0 {
			p = append(p, 0)
			continue
		}
		p = append(p, p[n-1]+height(n-1))
	}
	*prefix = p
	return p[r]
}

// colAt returns the column whose span holds x, from the sheet's origin in
// layout coordinates ([grid.layoutColX]).
func (g *grid) colAt(x float64) int {
	g.layoutColX(0)
	p := g.layoutColPrefix
	if g.doc.ExcelRules {
		p = g.colPrefix
	}
	i := sort.SearchFloat64s(p, x)
	if i > 0 && (i >= len(p) || p[i] > x) {
		i--
	}
	return min(i, maxColumns-1)
}

// rowAt returns the row whose span holds y, from the sheet's origin in
// layout coordinates ([grid.layoutRowY]), looking no further than limit.
func (g *grid) rowAt(y float64, limit int) int {
	g.layoutRowY(limit + 1)
	p := g.layoutRowPrefix
	if g.doc.ExcelRules {
		p = g.rowPrefix
	}
	p = p[:limit+2]
	i := sort.SearchFloat64s(p, y)
	if i > 0 && (i >= len(p) || p[i] > y) {
		i--
	}
	return min(i, limit)
}

// anchorPoint returns the sheet position of an anchor: its cell's corner
// plus the offset, the offset bounded by the cell unless unbounded, as
// Calc places a drawing anchored in a cell.
func (g *grid) anchorPoint(a Anchor, unbounded bool) (float64, float64) {
	r := min(max(a.Row, 0), maxRows-1)
	c := min(max(a.Column, 0), maxColumns-1)
	dx, dy := min(max(a.DX, -maxSide), maxSide), min(max(a.DY, -maxSide), maxSide)
	if !unbounded {
		dx, dy = min(max(dx, 0), g.layoutColWidth(c)), min(max(dy, 0), g.layoutRowHeight(r))
	}
	return max(g.layoutColX(c)+dx, 0), max(g.layoutRowY(r)+dy, 0)
}

// drawingRect returns a drawing's box on the sheet, in points from the
// sheet's origin in layout coordinates ([grid.layoutColX]).
func (g *grid) drawingRect(d *Drawing) typeset.Rect {
	if d.Absolute {
		return typeset.Rect{X: max(d.X, 0), Y: max(d.Y, 0), Width: clampSide(d.Width), Height: clampSide(d.Height)}
	}
	x, y := g.anchorPoint(d.From, d.Unbounded)
	if d.To != nil {
		x2, y2 := g.anchorPoint(*d.To, d.Unbounded)
		return typeset.Rect{X: x, Y: y, Width: max(x2-x, 0), Height: max(y2-y, 0)}
	}
	return typeset.Rect{X: x, Y: y, Width: clampSide(d.Width), Height: clampSide(d.Height)}
}

// clampSide returns v within [0, maxSide], 0 for a value that is not a
// number.
func clampSide(v float64) float64 {
	if !numeric.Positive(v) {
		return 0
	}
	return min(v, maxSide)
}

// drawingCells returns the cells a drawing's box covers, its painted box
// when it has one (Drawing.Painted), as Calc extends the print range to a
// drawing's bounding box (ScDrawLayer::GetPrintArea); false for an empty
// box. Under Excel's rules the box covers the cell its bottom-right
// corner touches too, at that cell's top or left edge: Excel extends a
// sheet's print range to a drawing's end cell even when the drawing stops
// at its edge. A drawing that fills its cell (Drawing.InCell) covers
// that cell alone.
//
// # Corpus evidence
//
//   - litho/xlsx: charts_bar_column, fitted to one landscape page, with
//     charts ending at the left edge of column T and the top of row 45,
//     prints at 66%, the zoom that fits columns A to T: A to S fit at 70%.
//   - litho/xlsx: sparklines, fitted to one landscape Letter page with
//     sparklines in its last column P, prints at 95%, the zoom that fits
//     columns A to P (690.24 of 721.92 points): A to Q fit at 89%.
//   - libreoffice/ods: sc/tdf119191_FitToCellSize.ods, a chevron anchored
//     from D5 to F14 and turned so it paints within columns B to D,
//     prints columns A to E on one A4 page: column F, which its anchors
//     reach, would take a second.
func (g *grid) drawingCells(d *Drawing) (Range, bool) {
	rc := g.drawingRect(d)
	if rc.Width <= 0 && rc.Height <= 0 {
		return Range{}, false
	}
	if d.InCell && !d.Absolute {
		r, c := min(max(d.From.Row, 0), maxRows-1), min(max(d.From.Column, 0), maxColumns-1)
		return Range{FirstRow: r, LastRow: r, FirstColumn: c, LastColumn: c}, true
	}
	var limit int
	switch {
	case d.Painted != nil && !d.Absolute && numeric.Finite(d.Painted.X) && numeric.Finite(d.Painted.Y):
		x, y := g.anchorPoint(d.From, d.Unbounded)
		p := d.Painted
		dx, dy := min(max(p.X, -maxSide), maxSide), min(max(p.Y, -maxSide), maxSide)
		rc = typeset.Rect{X: max(x+dx, 0), Y: max(y+dy, 0), Width: clampSide(p.Width), Height: clampSide(p.Height)}
		limit = min(max(d.From.Row, 0)+maxDrawingRows, maxRows-1)
	case d.Absolute:
		// A bottom edge past the sheet, infinite or not a number looks
		// down to the last row: converting it to int would overflow.
		limit = maxRows - 1
		if bottom := rc.Y + rc.Height; bottom < maxRows-2 {
			limit = int(bottom) + 1 // fits int: below maxRows, checked above
		}
	case d.To != nil:
		limit = min(max(d.To.Row, 0)+1, maxRows-1)
	default:
		limit = min(max(d.From.Row, 0)+maxDrawingRows, maxRows-1)
	}
	r := Range{FirstRow: g.rowAt(rc.Y, limit), FirstColumn: g.colAt(rc.X)}
	end := -1e-6
	if g.doc.ExcelRules {
		end = 1e-6
	}
	r.LastRow = g.rowAt(rc.Y+rc.Height+end, limit)
	r.LastColumn = g.colAt(rc.X + rc.Width + end)
	r.LastRow = max(r.LastRow, r.FirstRow)
	r.LastColumn = max(r.LastColumn, r.FirstColumn)
	return r, true
}

// maxDrawingRows bounds how many rows below its anchor a one-cell
// anchored drawing's extent is looked for. Anti-DoS guard, not
// spec-mandated: a drawing of maxSide points covers at most that many rows
// of one point.
const maxDrawingRows = maxSide
