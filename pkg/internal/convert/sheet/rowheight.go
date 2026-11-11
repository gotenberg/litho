package sheet

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// screenPixel is the length of a pixel of the screen Calc computes a
// row's optimal height on, in points (sc/source/core/data/column2.cxx,
// ScColumn::GetNeededSize with the document's nPPTY): the text's height
// and each cell padding are truncated to whole pixels, and their sum
// converted back.
//
// # Corpus evidence
//
//   - litho/pdf: a flat ODS of Liberation Sans 10 point text with 1 point of
//     padding, converted by Gotenberg 9.0.0: a row of three wrapped lines
//     of 11.2 points is 35.04 points tall, 47 pixels of 0.7455 points: 45
//     for the text and one a padding.
//   - gotenberg/xlsx: scrolled-pages, rows of Calibri 12 point text (Carlito, 14.65
//     points a line) without a height of their own print 15.65 points
//     apart, 21 such pixels.
const screenPixel = 0.7455

// pixels returns v truncated to whole screen pixels, as a count.
func pixels(v float64) float64 {
	return math.Floor(v/screenPixel + 1e-9)
}

// maxMeasuredCells caps the cells whose text is measured for the optimal
// row heights. Anti-DoS guard, not spec-mandated: past it the rows keep
// their heights.
const maxMeasuredCells = 1 << 16

// optimalHeights grows every row without a custom height to the height
// of its text, as Calc's optimal row height: the tallest cell's lines plus
// its top and bottom padding. A cell merged down several rows does not
// count. A book laid out by Excel's rules grows a row alone when it has no
// height of its own, and to fit its lines alone ([excelHeight]).
func (b *builder) optimalHeights(g *grid) {
	pb := &pageBuilder{builder: b, g: g}
	excel := b.doc.ExcelRules
	defLine := b.m.lineHeight(resolveFont(Font{}, b.doc.Default.Font))
	budget := maxMeasuredCells
	for _, ri := range g.rowOrder {
		row := g.rows[ri]
		if row.CustomHeight || row.Hidden || (excel && row.Height > 0) {
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
			if excel && !g.style(ri, c.Column, c).Align.Wrap && !strings.Contains(c.Value.Text, "\n") {
				continue
			}
			u := &unit{r: ri, c: c.Column, lastR: ri, lastC: lastC, cell: c, st: g.style(ri, c.Column, c)}
			u.w = g.spanW(c.Column, lastC)
			d, ok := pb.display(u)
			if !ok {
				continue
			}
			lineH := 0.0
			for line := range strings.SplitSeq(d.text, "\n") {
				lineH = max(lineH, b.m.textLineHeight(d.font, line))
			}
			for _, r := range d.runs {
				if r.Font != nil {
					lineH = max(lineH, b.m.lineHeight(overlay(d.font, r.Font)))
				}
			}
			lines := float64(strings.Count(d.text, "\n") + 1)
			h := lineH * lines
			if d.wrap {
				w := u.w - d.padding[css.Left] - d.padding[css.Right] - max(u.st.Align.Indent, 0)
				if w > 0 {
					p := pb.paragraph(&d, d.font, lineH, css.AlignLeft)
					if mh := b.measureBlock(p, w); mh > 0 {
						lines = math.Round(mh / editLine(lineH))
						h = lines * editLine(lineH)
					}
				}
			}
			if excel {
				need = max(need, excelHeight(g.defH, lineH, defLine, lines))
				continue
			}
			if rot := u.st.Align.Rotation; rot != 0 && !u.st.Align.Stacked {
				a := math.Abs(rot) * math.Pi / 180
				h = d.textW*math.Sin(a) + h*math.Cos(a)
			}
			px := pixels(h) + pixels(d.padding[css.Top]) + pixels(d.padding[css.Bottom])
			// Calc keeps a row height in whole twips.
			need = max(need, math.Floor(px*screenPixel*20+1e-6)/20)
		}
		if need > base+epsilon {
			g.heights[ri] = min(need, maxSide)
		}
	}
}

// excelHeight returns the height Excel gives a row without a height of
// its own whose wrapped text takes a count of lines of lineH points: a line of
// the sheet's default row height defH for each, scaled by the line of the
// text's font against the default font's defLine, in whole printer pixels.
// Excel fits such a row as it opens a workbook, and leaves a row whose
// text is merely taller than the default unfitted.
//
// # Corpus evidence
//
//   - litho/xlsx: alignment prints its rows of three wrapped lines of
//     Calibri 11 43.56 points tall, three rows of 14.52; fonts prints its
//     row of 14 point text without a height 14.52 points tall.
//   - libreoffice/xlsx: sc/tdf139934.xlsx prints its rows of two wrapped
//     lines of Arial 10 24.72 points tall, two rows of 12.36.
func excelHeight(defH, lineH, defLine, lines float64) float64 {
	per := defH
	if defLine > 0 && lineH > defLine {
		per = defH * lineH / defLine
	}
	return math.Round(per*max(lines, 1)/excelPixel) * excelPixel
}

// excelPixel is the printer pixel Excel lays a printed sheet out in, in
// points.
const excelPixel = 72.0 / 600

// editLine returns the height of a wrapped line of text whose natural
// line is h points: Calc's edit engine rounds it up to whole twips.
//
// # Corpus evidence
//
//   - litho/pdf: a flat ODS of wrapped Liberation Sans 10 point text (11.17
//     points a line), converted by Gotenberg 9.0.0, sets its lines 11.2
//     points apart.
func editLine(h float64) float64 {
	return math.Ceil(h*20-1e-6) / 20
}
