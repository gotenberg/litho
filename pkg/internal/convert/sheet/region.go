package sheet

// region is a block of a page's cells laid out together: the repeated
// rows or columns, or the page's bands, with its box on the page in sheet
// points from the top-left corner of the page's cells.
type region struct {
	rows, cols band
	x0, y0     float64
	w, h       float64
}

// x returns the left edge of sheet column c in the region's page
// coordinates; c may lie outside the region.
func (rg *region) x(g *grid, c int) float64 {
	return rg.x0 + g.colX(c) - g.colX(rg.cols.first)
}

// y returns the top edge of sheet row r in the region's page coordinates.
func (rg *region) y(g *grid, r int) float64 {
	return rg.y0 + g.rowY(r) - g.rowY(rg.rows.first)
}

// spanW returns the width of the columns [a, b].
func (g *grid) spanW(a, b int) float64 {
	return g.colX(b+1) - g.colX(a)
}

// spanH returns the height of the rows [a, b].
func (g *grid) spanH(a, b int) float64 {
	return g.rowY(b+1) - g.rowY(a)
}

// unit is a cell, or a merge, of a region: the cell that holds its value
// and style and its box.
type unit struct {
	r, c       int
	lastR      int
	lastC      int
	cell       *Cell
	st         *Style
	x, y, w, h float64
	merged     bool
}

// units calls fn for every cell or merge whose top-left cell lies in the
// region, and every merge that starts before it and reaches into it,
// rows then columns, hidden rows and columns left out.
func (b *pageBuilder) units(rg *region, fn func(u *unit)) {
	g := b.g
	seen := map[[2]int]bool{}
	for r := rg.rows.first; r <= rg.rows.last; r++ {
		if g.rowHidden(r) {
			continue
		}
		for c := rg.cols.first; c <= rg.cols.last; c++ {
			if g.colHidden(c) {
				continue
			}
			if b.cellBudget <= 0 {
				return
			}
			b.cellBudget--
			r0, c0 := r, c
			if tl, ok := g.covered[[2]int{r, c}]; ok {
				r0, c0 = tl[0], tl[1]
				if seen[tl] || (r0 >= rg.rows.first && c0 >= rg.cols.first) {
					continue
				}
				seen[tl] = true
			}
			u := unit{r: r0, c: c0, lastR: r0, lastC: c0}
			if m, ok := g.merges[[2]int{r0, c0}]; ok {
				u.lastR, u.lastC, u.merged = m.LastRow, m.LastColumn, true
			}
			u.cell = g.cell(r0, c0)
			u.st = g.style(r0, c0, u.cell)
			u.x, u.y = rg.x(g, c0), rg.y(g, r0)
			u.w, u.h = g.spanW(c0, u.lastC), g.spanH(r0, u.lastR)
			fn(&u)
		}
	}
}
