package slides

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// maxTableCells caps the cells of a table drawn. Anti-DoS guard, not
// spec-mandated: a slide's table shows a few hundred cells.
const maxTableCells = 1 << 14

// verticalCellHeight returns the height the vertical text body of a
// cell w wide inside its insets needs, its insets left out: Impress lays
// the lines out as long as the cell is wide, and the cell grows to that
// length when a line wraps, to its longest line otherwise.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/tdf100926.pptx's 213-point columns of vert and vert270 text
//     wrap "Top to Bottom vertical text" before "text" and grow the 119-point
//     row to 204; sd/tcPr-vert-roundtrip.pptx's 148-point line in a
//     320-point column leaves its 173-point row as it is.
func (b *builder) verticalCellHeight(body *TextBody, w float64) float64 {
	nb := *body
	nb.Wrap = false
	return min(b.naturalWidth(&nb, noFit), w)
}

// table returns the fragments of the table shape s in its box: the
// columns from the box's left edge at their widths, the rows from its top
// at their heights, each row grown to the tallest text of its cells, each
// cell's fill, text and borders (ECMA-376 Part 1, 21.1.3.13 tbl). It hangs
// a Table element from parent, with a TR per row and a TD per cell, a
// merged cell one TD with its spans and nothing for the cells it covers,
// a row the source lists short padded with empty TDs, so every row counts
// the table's columns (ISO 14289-1, 7.2; ISO 32000-1, 14.8.5.7). The
// model carries no header row, so every cell is a TD.
func (b *builder) table(s *Shape, parent *compose.StructElem) []compose.Fragment {
	t := s.Table
	if t == nil || len(t.ColumnWidths) == 0 || len(t.Rows) == 0 {
		return nil
	}
	cols := t.ColumnWidths
	rows := t.Rows
	if len(cols)*len(rows) > maxTableCells {
		b.warn(CodeTable, fmt.Sprintf("a table has more than %d cells; the rest are left out", maxTableCells))
		if len(cols) > maxTableCells {
			cols = cols[:maxTableCells]
		}
		rows = rows[:max(maxTableCells/len(cols), 1)]
	}
	side := func(v float64) float64 {
		if !positiveFinite(v) {
			return 0
		}
		return min(v, maxCoordinate)
	}
	xs := make([]float64, len(cols)+1)
	xs[0] = s.Box.X
	for i, w := range cols {
		xs[i+1] = xs[i] + side(w)
	}
	hs := make([]float64, len(rows))
	for r, row := range rows {
		hs[r] = side(row.Height)
		// A row grows to the text of its cells that span it alone.
		for c := range min(len(row.Cells), len(cols)) {
			cell := &row.Cells[c]
			if cell.Covered || cell.Text == nil || max(cell.RowSpan, 1) > 1 {
				continue
			}
			w := xs[min(c+max(cell.ColSpan, 1), len(cols))] - xs[c]
			inner := insetRect(compose.Rect{Width: w, Height: max(hs[r], 1)}, cell.Text.Insets)
			tb := side(cell.Text.Insets[css.Top]) + side(cell.Text.Insets[css.Bottom])
			if cell.Text.Vertical.Vertical() {
				hs[r] = max(hs[r], b.verticalCellHeight(cell.Text, inner.Width)+tb)
				continue
			}
			if lay := b.layoutBody(cell.Text, inner.Width, inner.Height, noFit); lay != nil {
				hs[r] = max(hs[r], lay.height+tb)
			}
		}
	}
	ys := make([]float64, len(rows)+1)
	ys[0] = s.Box.Y
	for r := range rows {
		ys[r+1] = ys[r] + hs[r]
	}
	tbl := &compose.StructElem{Role: "Table"}
	parent.Kids = append(parent.Kids, tbl)
	// spanned marks the cells a span covers, so that a row the source
	// lists short still counts every column (ISO 14289-1, 7.2).
	spanned := make([]bool, len(rows)*len(cols))
	var fills, texts, borders []compose.Fragment
	for r, row := range rows {
		tr := &compose.StructElem{Role: "TR"}
		tbl.Kids = append(tbl.Kids, tr)
		for c := range min(len(row.Cells), len(cols)) {
			cell := &row.Cells[c]
			if cell.Covered || spanned[r*len(cols)+c] {
				continue
			}
			c1 := min(c+max(cell.ColSpan, 1), len(cols))
			r1 := min(r+max(cell.RowSpan, 1), len(rows))
			for y := r; y < r1; y++ {
				for x := c; x < c1; x++ {
					spanned[y*len(cols)+x] = true
				}
			}
			td := &compose.StructElem{Role: "TD"}
			if c1-c > 1 {
				td.ColSpan = c1 - c
			}
			if r1-r > 1 {
				td.RowSpan = r1 - r
			}
			tr.Kids = append(tr.Kids, td)
			rc := compose.Rect{X: xs[c], Y: ys[r], Width: xs[c1] - xs[c], Height: ys[r1] - ys[r]}
			fills = append(fills, b.fill(&cell.Fill, rc, rectOps(rc), shape.FillNorm)...)
			if cell.Text != nil && len(cell.Text.Paragraphs) > 0 {
				body := cell.Text
				if body.Vertical == css.SidewaysRL {
					// A cell's text turned a quarter turn clockwise (vert)
					// sits at the cell's left edge when anchored at its
					// top, at its right edge at its bottom, as Impress
					// prints sd/tdf100926.pptx and
					// sd/tcPr-vert-roundtrip.pptx.
					vb := *body
					switch body.Anchor {
					case AnchorTop:
						vb.Anchor = AnchorBottom
					case AnchorBottom:
						vb.Anchor = AnchorTop
					case AnchorMiddle:
					}
					body = &vb
				}
				txt := b.shapeText(body, nil, rc)
				texts = append(texts, txt.frags...)
				b.links = append(b.links, txt.links...)
				td.Kids = append(td.Kids, txt.structure...)
			}
			x0, y0, x1, y1 := rc.X, rc.Y, rc.X+rc.Width, rc.Y+rc.Height
			for side, ops := range [4][]compose.PathOp{
				css.Top: lineOps(x0, y0, x1, y0), css.Right: lineOps(x1, y0, x1, y1),
				css.Bottom: lineOps(x0, y1, x1, y1), css.Left: lineOps(x0, y0, x0, y1),
			} {
				borders = append(borders, b.stroke(&cell.Borders[side], ops)...)
			}
		}
		for c := len(row.Cells); c < len(cols); c++ {
			if !spanned[r*len(cols)+c] {
				tr.Kids = append(tr.Kids, &compose.StructElem{Role: "TD"})
			}
		}
	}
	out := make([]compose.Fragment, 0, len(fills)+len(texts)+len(borders))
	out = append(out, fills...)
	out = append(out, texts...)
	return append(out, borders...)
}
