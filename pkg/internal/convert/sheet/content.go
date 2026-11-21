package sheet

import (
	"cmp"
	"context"
	"slices"
	"sort"
)

// contentPages returns the pages of c that hold something, in the sheet's
// page order: a page whose own cells hold one [grid.shows] counts in a
// row and a column that are not hidden, a page a drawing overlaps, and a
// page text overflows into from the left. The repeated rows and columns a
// page shows make it hold nothing, as Calc judges the page by its own
// cells (ScDocument::IsPrintEmpty, called on each page's own range by
// lcl_SetHidden in sc/source/ui/view/printfun.cxx). It visits the cells
// and drawings of the sheet, never the pages between them, and stops past
// [maxSheetPages] pages. ctx's error, checked every [rowsPerCheck] rows,
// stops it.
func (sp *sheetPlan) contentPages(ctx context.Context, c *cut) ([]pageKey, error) {
	g := sp.g
	seen := map[pageKey]bool{}
	var keys []pageKey
	// mark records the page of bands ri and ci, false once past the cap.
	mark := func(ri, ci int) bool {
		k := pageKey{ri, ci}
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
		return len(keys) <= maxSheetPages
	}
	full := false
	i := sort.SearchInts(g.rowOrder, c.a.FirstRow)
	for n := 0; !full && i < len(g.rowOrder) && g.rowOrder[i] <= c.a.LastRow; i, n = i+1, n+1 {
		if n%rowsPerCheck == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		r := g.rowOrder[i]
		row := g.rows[r]
		ri := bandAt(c.rows, r)
		if row.Hidden || ri < 0 {
			continue
		}
		for k := range row.Cells {
			cell := &row.Cells[k]
			if cell.Column > c.a.LastColumn || full {
				break
			}
			if cell.Column < 0 || g.colHidden(cell.Column) {
				continue
			}
			if cell.Column >= c.a.FirstColumn && g.shows(cell) {
				if ci := bandAt(c.cols, cell.Column); ci >= 0 && !mark(ri, ci) {
					full = true
					break
				}
			}
			if cell.Value.Kind != Text {
				continue
			}
			// The bands whose first column the text reaches from the left
			// ([sheetPlan.lastPrinted] asks the same of
			// [grid.extendPrint]).
			end := g.overflowEnd(r, cell.Column, cell)
			for ci := sort.Search(len(c.cols), func(j int) bool { return c.cols[j].first > cell.Column }); ci < len(c.cols) && c.cols[ci].first <= end; ci++ {
				if !mark(ri, ci) {
					full = true
					break
				}
			}
		}
	}
	for k := 0; !full && k < len(g.sh.Drawings); k++ {
		d, ok := g.drawingCells(&g.sh.Drawings[k])
		if !ok {
			continue
		}
		r0 := sort.Search(len(c.rows), func(j int) bool { return c.rows[j].last >= d.FirstRow })
		c0 := sort.Search(len(c.cols), func(j int) bool { return c.cols[j].last >= d.FirstColumn })
		for ri := r0; !full && ri < len(c.rows) && c.rows[ri].first <= d.LastRow; ri++ {
			for ci := c0; ci < len(c.cols) && c.cols[ci].first <= d.LastColumn; ci++ {
				if !mark(ri, ci) {
					full = true
					break
				}
			}
		}
	}
	if g.sh.Page.Order == OverThenDown {
		slices.SortFunc(keys, func(x, y pageKey) int { return cmp.Or(cmp.Compare(x.ri, y.ri), cmp.Compare(x.ci, y.ci)) })
	} else {
		slices.SortFunc(keys, func(x, y pageKey) int { return cmp.Or(cmp.Compare(x.ci, y.ci), cmp.Compare(x.ri, y.ri)) })
	}
	return keys, nil
}

// bandAt returns the index of the band of bands, in order and apart, that
// holds the line i, -1 for none.
func bandAt(bands []band, i int) int {
	j := sort.Search(len(bands), func(j int) bool { return bands[j].last >= i })
	if j < len(bands) && bands[j].first <= i {
		return j
	}
	return -1
}
