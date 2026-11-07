package sheet

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// pixel is the length of a screen pixel at 96 per inch, in points: Calc
// computes a row's optimal height in such pixels (sc/source/core/data/
// column2.cxx, ScColumn::GetNeededSize with the document's nPPTY), each
// cell padding truncated to whole pixels.
//
// # Corpus evidence
//
//   - litho: a flat ODS of Liberation Sans 10 point text with 1 point of
//     padding, converted by Gotenberg 9.0.0: a row of one line is 12.78
//     points tall and a row of three wrapped lines 35.04, three lines of
//     11.2 points and 1.5 points of padding.
const pixel = 0.75

// pixels returns v truncated to whole screen pixels.
func pixels(v float64) float64 {
	return math.Floor(v/pixel+1e-9) * pixel
}

// maxMeasuredCells caps the cells whose text is measured for the optimal
// row heights. Anti-DoS guard, not spec-mandated: past it the rows keep
// their heights.
const maxMeasuredCells = 1 << 16

// optimalHeights grows every row without a custom height to the height
// of its text, as Calc's optimal row height: the tallest cell's lines plus
// its top and bottom padding. A cell merged down several rows does not
// count.
func (b *builder) optimalHeights(g *grid) {
	pb := &pageBuilder{builder: b, g: g}
	budget := maxMeasuredCells
	for _, ri := range g.rowOrder {
		row := g.rows[ri]
		if row.CustomHeight || row.Hidden {
			continue
		}
		base := g.defH
		if row.Height > 0 {
			base = min(row.Height, maxSide)
		}
		need := 0.0
		for i := range row.Cells {
			c := &row.Cells[i]
			if !hasContent(c) || c.Column < 0 || c.Column >= maxColumns || budget <= 0 {
				continue
			}
			if _, ok := g.covered[[2]int{ri, c.Column}]; ok {
				continue
			}
			lastC := c.Column
			if m, ok := g.merges[[2]int{ri, c.Column}]; ok {
				if m.LastRow > m.FirstRow {
					continue
				}
				lastC = m.LastColumn
			}
			budget--
			u := &unit{r: ri, c: c.Column, lastR: ri, lastC: lastC, cell: c, st: g.style(ri, c.Column, c)}
			u.w = g.spanW(c.Column, lastC)
			d, ok := pb.display(u)
			if !ok {
				continue
			}
			lineH := b.m.lineHeight(d.font)
			for _, r := range d.runs {
				if r.Font != nil {
					lineH = max(lineH, b.m.lineHeight(overlay(d.font, r.Font)))
				}
			}
			h := lineH * float64(strings.Count(d.text, "\n")+1)
			if d.wrap {
				w := u.w - d.padding[css.Left] - d.padding[css.Right] - max(u.st.Align.Indent, 0)
				if w > 0 {
					p := pb.paragraph(&d, d.font, lineH, css.AlignLeft)
					if mh := b.measureBlock(p, w); mh > 0 {
						h = mh
					}
				}
			}
			if rot := u.st.Align.Rotation; rot != 0 && !u.st.Align.Stacked {
				a := math.Abs(rot) * math.Pi / 180
				h = d.textW*math.Sin(a) + h*math.Cos(a)
			}
			need = max(need, h+pixels(d.padding[css.Top])+pixels(d.padding[css.Bottom]))
		}
		if need > base+epsilon {
			g.heights[ri] = min(need, maxSide)
		}
	}
}
