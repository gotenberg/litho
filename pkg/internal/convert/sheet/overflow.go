package sheet

import (
	"sort"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// maxOverflowCells caps the text cells of a sheet whose width the print
// range is extended by. Anti-DoS guard, not spec-mandated: a sheet's
// last columns hold a few hundred text cells at most.
const maxOverflowCells = 1 << 16

// textNeed returns the function that measures the room a text cell
// needs on one line: its text's width, its padding and its indent; 0 for
// a cell that does not overflow (a number, a wrapped, shrunk, filled,
// justified or rotated text), as Calc's ScColumn::GetNeededSize measures
// it for the print range.
func (b *builder) textNeed(g *grid) func(r, c int, cell *Cell) float64 {
	pb := &pageBuilder{builder: b, g: g}
	return func(r, c int, cell *Cell) float64 {
		if cell == nil || cell.Value.Kind != Text {
			return 0
		}
		st := g.style(r, c, cell)
		switch {
		case st.Align.Wrap, st.Align.Shrink, st.Align.Rotation != 0, st.Align.Stacked:
			return 0
		case st.Align.Horizontal == HJustify, st.Align.Horizontal == HDistributed, st.Align.Horizontal == HFill:
			return 0
		}
		u := &unit{r: r, c: c, lastR: r, lastC: c, cell: cell, st: st, w: g.colWidth(c), h: g.rowHeight(r)}
		d, ok := pb.display(u)
		if !ok || d.wrap {
			return 0
		}
		return d.textW + d.padding[css.Left] + d.padding[css.Right] + min(max(st.Align.Indent, 0), maxSide)
	}
}

// textWidthDPI is the resolution Calc measures a cell's text at for its
// print range, whose width in pixels it keeps in 16 bits
// (ScColumn::SetTextWidth): a wider text's width wraps around.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/cell-value.xlsx, a 10,987-point text in D3 of 64.8-point
//     columns on A4 at 7 columns a page, prints 8 pages, its print range
//     ending 3,122 points past the text's start: 91,555 pixels at 600 dpi
//     wrapped to 26,019.
const textWidthDPI = 600

// textWidthCache returns the width w in points as Calc's cache of text
// widths keeps it: in whole pixels at textWidthDPI, modulo 2^16.
func textWidthCache(w float64) float64 {
	px := int64(w * textWidthDPI / 72)
	return float64(px&0xffff) * 72 / textWidthDPI
}

// overflowEnd returns the last column the text of the cell at (r, c)
// reaches into the empty cells right of it, c itself when it reaches none
// (ScTable::MaybeAddExtraColumn): the room the text misses in its column,
// half of it when centred, none when aligned right, taken from the next
// columns while they hold no value.
func (g *grid) overflowEnd(r, c int, cell *Cell) int {
	if g.need == nil || hasContent(g.cell(r, c+1)) {
		return c
	}
	if end, ok := g.overflows[[2]int{r, c}]; ok {
		return end
	}
	if g.overflowBudget <= 0 {
		return c
	}
	g.overflowBudget--
	end := g.reach(r, c, cell)
	g.overflows[[2]int{r, c}] = end
	return end
}

// reach measures how far the text of the cell at (r, c) overflows, as
// overflowEnd says.
func (g *grid) reach(r, c int, cell *Cell) int {
	missing := textWidthCache(g.need(r, c, cell)) - g.colWidth(c)
	if missing <= epsilon {
		return c
	}
	switch g.style(r, c, cell).Align.Horizontal {
	case HCenter, HCenterContinuous:
		missing /= 2
	case HRight:
		if !g.sh.RightToLeft {
			return c
		}
	case HGeneral, HLeft, HFill, HJustify, HDistributed:
		if g.sh.RightToLeft {
			return c
		}
	}
	end := c
	for i := 0; missing > epsilon && end+1 < maxColumns && i < maxOverflowScan; i++ {
		if hasContent(g.cell(r, end+1)) {
			break
		}
		end++
		missing -= g.colWidth(end)
	}
	return end
}

// extendPrint returns the last column the text of the visible cells in
// columns [0, last] and rows [first, lastRow] reaches, at least last
// (ScTable::ExtendPrintArea).
func (g *grid) extendPrint(last, firstRow, lastRow int) int {
	end := last
	i := sort.SearchInts(g.rowOrder, firstRow)
	for ; i < len(g.rowOrder) && g.rowOrder[i] <= lastRow; i++ {
		ri := g.rowOrder[i]
		row := g.rows[ri]
		if row.Hidden {
			continue
		}
		for k := range row.Cells {
			cell := &row.Cells[k]
			if cell.Column > last {
				break
			}
			if cell.Column < 0 || g.colHidden(cell.Column) || cell.Value.Kind != Text {
				continue
			}
			end = max(end, g.overflowEnd(ri, cell.Column, cell))
		}
	}
	return end
}
