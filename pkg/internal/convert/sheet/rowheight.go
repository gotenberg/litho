package sheet

import (
	"math"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// screenPixel is the length of a pixel of the screen Calc computes a
// row's optimal height on, in points (sc/source/core/data/column2.cxx,
// ScColumn::GetNeededSize with the document's nPPTY): the text's height
// and each cell padding are truncated to whole pixels, and their sum
// converted back.
//
// # Corpus evidence
//
//   - probe, ods: a flat ODS of Liberation Sans 10 point text with 1 point
//     of padding, converted by Gotenberg 9.0.0: a row of three wrapped lines
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
// height of its own, and to fit its lines alone ([excelHeight]). ctx's
// error, checked every [rowsPerCheck] rows, stops it.
func (b *builder) optimalHeights(g *grid) error {
	pb := &pageBuilder{builder: b, g: g}
	excel := b.doc.ExcelRules
	fit := excel && b.doc.FitRows
	if fit {
		b.excelDrops(g)
	}
	// The fit of a cell's own font, by its format, where no conditional
	// format changes it: a sheet holds few formats and many cells.
	fitOf := map[*Style]float64{}
	cellFit := func(ri int, c *Cell) float64 {
		cache := g.cond == nil && len(c.Runs) == 0
		if h, ok := fitOf[c.Style]; ok && cache {
			return h
		}
		h := b.cellScreenFit(g, ri, c)
		if cache {
			fitOf[c.Style] = h
		}
		return h
	}
	budget := maxMeasuredCells
	for k, ri := range g.rowOrder {
		if k%rowsPerCheck == 0 {
			if err := b.ctx.Err(); err != nil {
				return err
			}
		}
		row := g.rows[ri]
		if row.CustomHeight || row.Hidden || (excel && !fit && row.Height > 0) {
			continue
		}
		base := g.defH
		if row.Height > 0 {
			base = min(row.Height, maxSide)
		}
		need := 0.0
		if !excel {
			need = b.calcStyledHeight(g, row)
		}
		// shrink says every cell of the row was measured: Calc sets a row
		// of optimal height to what it measures, lower than the height
		// the file stores too.
		shrink := true
		for i := range row.Cells {
			c := &row.Cells[i]
			if c.Column < 0 || c.Column >= maxColumns || budget <= 0 {
				shrink = false
				continue
			}
			if !hasContent(c) {
				if fit && c.Style != nil {
					// An empty cell's font counts when Excel measures a row.
					need = max(need, cellFit(ri, c))
				}
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
			// A row that stores a height is measured by its fonts alone,
			// not by its wrapped lines.
			if excel && ((fit && row.Height > 0) || (!g.style(ri, c.Column, c).Align.Wrap && !strings.Contains(c.Value.Text, "\n"))) {
				if fit {
					need = max(need, cellFit(ri, c))
				}
				continue
			}
			u := &unit{r: ri, c: c.Column, lastR: ri, lastC: lastC, cell: c, st: g.style(ri, c.Column, c)}
			u.w = g.spanW(c.Column, lastC)
			d, ok := pb.display(u)
			if !ok {
				continue
			}
			if !excel && g.calcStandard(ri, c, u.st, &d) {
				size := d.font.size
				if len(c.Runs) == 1 && c.Runs[0].Font != nil && numeric.Positive(c.Runs[0].Font.Size) {
					size = c.Runs[0].Font.Size
				}
				need = max(need, calcStandardHeight(size, d.padding))
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
					p := pb.paragraph(&d, d.font, lineH, css.AlignLeft, paraTurn{})
					if mh := b.measureBlock(p, w); mh > 0 {
						lines = math.Round(mh / editLine(lineH))
						h = lines * editLine(lineH)
					}
				}
			}
			if excel {
				pitch := b.excelLine(d.font)
				for _, r := range d.runs {
					if r.Font != nil {
						pitch = max(pitch, b.excelLine(overlay(d.font, r.Font)))
					}
				}
				need = max(need, excelHeight(pitch, lines))
				continue
			}
			if rot := u.st.Align.Rotation; rot != 0 && !u.st.Align.Stacked {
				a := math.Abs(rot) * math.Pi / 180
				h = float64(d.textW*detmath.Sin(a)) + float64(h*detmath.Cos(a))
			}
			px := pixels(h) + pixels(d.padding[css.Top]) + pixels(d.padding[css.Bottom])
			// Calc keeps a row height in whole twips.
			need = max(need, math.Floor(float64(px*screenPixel*20)+1e-6)/20)
		}
		if fit && need > base+epsilon {
			// Excel measures the row, which keeps at least the height it
			// stores, else the sheet's default.
			g.heights[ri] = min(need, maxSide)
			continue
		}
		if excel && need > base+epsilon || !excel && (need > base+calcRowSlack || shrink && need < base-calcRowSlack) {
			g.heights[ri] = min(need, maxSide)
		}
	}
	return nil
}

// calcStyledHeight returns the least optimal height Calc gives row: its
// standard row height (calcStandardRow), and the height of the font of
// every style that shows in the row as a plain line ([calcStandardHeight]):
// the document default's, which every column without a style of its own
// holds, the row's, each column's where no cell of the row stands, and
// each cell's, empty ones included, since Calc measures a row by the
// attributes of its cells before their text (ScColumn::GetOptimalHeight,
// sc/source/core/data/column2.cxx).
func (b *builder) calcStyledHeight(g *grid, row *Row) float64 {
	h := max(calcStandardRow, b.calcStyleHeight(&b.doc.Default))
	if row.Style != nil {
		h = max(h, b.calcStyleHeight(row.Style))
	}
	for i := range g.sh.Columns {
		col := &g.sh.Columns[i]
		if col.Style == nil || col.Hidden || row.Style != nil {
			continue
		}
		lo := sort.Search(len(row.Cells), func(k int) bool { return row.Cells[k].Column >= col.First })
		hi := sort.Search(len(row.Cells), func(k int) bool { return row.Cells[k].Column > col.Last })
		if hi-lo < col.Last-col.First+1 {
			h = max(h, b.calcStyleHeight(col.Style))
		}
	}
	for i := range row.Cells {
		if st := row.Cells[i].Style; st != nil && !st.Align.Wrap && st.Align.Rotation == 0 && !st.Align.Stacked {
			h = max(h, b.calcStyleHeight(st))
		}
	}
	return h
}

// calcStyleHeight returns [calcStandardHeight] for the font and padding of
// the style st.
func (b *builder) calcStyleHeight(st *Style) float64 {
	f := resolveFont(st.Font, b.doc.Default.Font)
	p := st.Padding
	if p == ([4]float64{}) {
		p = b.doc.Default.Padding
	}
	return calcStandardHeight(f.size, p)
}

// calcRowSlack is how far from the height a row stores Calc's measure
// must come before the row takes it: 0.05 points, the rounding of
// a height written in inches or centimetres, so that a row stored at
// 0.178in (12.816 points) keeps the 12.78 points of 256 twips as Calc
// prints them.
const calcRowSlack = 0.05

// calcStandardRow is Calc's standard row height, in points: 256 twips,
// ScGlobal::nStdRowHeight for its default font, Liberation Sans 10, and
// the least height Calc gives a row of optimal height.
const calcStandardRow = 12.8

// cellScreenFit returns the height Excel fits a row to for the unwrapped
// cell c of row ri: the tallest of its font's and its runs' fonts'
// ([builder.screenFit]).
func (b *builder) cellScreenFit(g *grid, ri int, c *Cell) float64 {
	f := b.measuredFont(g, ri, c)
	h := b.screenFit(f)
	for _, r := range c.Runs {
		if r.Font != nil {
			h = max(h, b.screenFit(overlay(f, r.Font)))
		}
	}
	return h
}

// measuredFont returns the font Excel measures the cell c of row ri in:
// its own format's, the Default font's for a cell without one, which
// Excel reads as the first cell format whatever its row's or column's
// (ECMA-376 Part 1, 18.3.1.4 c s, default 0).
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/orderOfCNumFmtElements.xlsx, in an Arial Cyr
//     10 book, prints its rows of cells without a format under columns
//     formatted in Calibri 11 103 printer pixels tall, Arial 10's standard
//     row.
func (b *builder) measuredFont(g *grid, ri int, c *Cell) font {
	if c.Style == nil {
		return resolveFont(Font{}, b.doc.Default.Font)
	}
	return resolveFont(g.style(ri, c.Column, c).Font, b.doc.Default.Font)
}

// screenFit returns the height, in points, Excel gives a row it measures
// as it opens a book whose fonts it does not know (FitRows) for a line of
// the font f: the line GDI gives f on the screen, 2 pixels for the cell's
// insets, its ascent and descent each rounded to a pixel at f's size in
// whole pixels and its line gap truncated, printed at the standard row's
// printer pixels a screen pixel; the standard row for the Default font.
//
// # Corpus evidence
//
//   - probe, xlsx: Excel for the web prints rows without a height of their
//     own holding a Calibri 14 cell 151 printer pixels tall (25 screen
//     pixels), a Cambria 16 bold cell 163 (27), a Calibri 10 cell beside
//     a Calibri 11 one 121, in a Calibri 11 book without knownFonts; rows
//     stored 14.45 points tall print 121.
//   - libreoffice/xlsx: sc/invalid_ext_data_validation.xlsx prints its rows
//     of GE Inspira Sans 11, a face no font serves, at the standard row.
//   - openxmlsdk/xlsx: VendorRatingCriteriaWorkbook_TP10192758.xltx, without
//     knownFonts, prints its rows stored 12.75 points tall, of wrapped Arial
//     10 text, as stored.
//   - libreoffice/xlsx: sc/forum-mso-en4-145327.xlsx prints its rows of
//     Calibri 10 text 17 screen pixels tall, its default; sc/tdf124525.xlsx
//     its rows of Tahoma 8 text at the standard row, 121 printer pixels.
func (b *builder) screenFit(f font) float64 {
	std, screen := b.doc.StandardRowHeight, b.doc.StandardScreenRow
	def := resolveFont(Font{}, b.doc.Default.Font)
	// A face no font serves is measured as the Default font: its
	// substitute's line is not the one Excel measures.
	if !numeric.Positive(std) || !numeric.Positive(screen) || b.m.key(f) == b.m.key(def) || !b.m.served(f) {
		return std
	}
	px := b.m.screenLine(f)
	return math.Floor(float64(px*std/excelPixel)/screen+1e-6) * excelPixel
}

// excelDrops sets, for every row of a book Excel measures as it opens it
// (FitRows), how much higher than the Default font's bottom baseline
// ([builder.bottomBaseline]) Excel sets the row's bottom-aligned text: as
// far as the deepest descent and line gap of the fonts of the row's
// cells, empty ones included, pass the Default font's.
//
// # Corpus evidence
//
//   - probe, xlsx: in a Calibri 11 book without knownFonts, Excel for the
//     web sets bottom-aligned Calibri 11 text 38 printer pixels above the
//     bottom of a row holding a Calibri 14 cell, an empty one included,
//     against 31 elsewhere, and bottom-aligned Calibri 14, 24 and 36 and
//     Arial 20 text alone in rows of 48 points 7, 33, 60 and 14 pixels
//     higher than Calibri 11 (this gives 7, 29, 56 and 15); Calibri 8,
//     Arial 11, Times New Roman 11, Courier New 11 and Cambria 11 on the
//     Calibri 11 baseline.
//   - litho/xlsx: fonts, whose fonts Excel knows, sets every cell on the
//     baseline of the Calibri 11 label beside it ([pageBuilder.excelShift]).
func (b *builder) excelDrops(g *grid) {
	def := resolveFont(Font{}, b.doc.Default.Font)
	def.excel = true
	base := b.m.descent(def) + b.m.lineGap(def)
	budget := maxMeasuredCells
	// The depth of a cell's own font, by its format, where no conditional
	// format changes it; -1 for a face no font serves.
	depthOf := map[*Style]float64{}
	depth := func(ri int, c *Cell) float64 {
		if d, ok := depthOf[c.Style]; ok && g.cond == nil {
			return d
		}
		f := b.measuredFont(g, ri, c)
		f.excel, f.script = true, Baseline
		d := -1.0
		if b.m.served(f) {
			d = b.m.descent(f) + b.m.lineGap(f)
		}
		if g.cond == nil {
			depthOf[c.Style] = d
		}
		return d
	}
	for _, ri := range g.rowOrder {
		row := g.rows[ri]
		deep := base
		for i := range row.Cells {
			c := &row.Cells[i]
			if budget <= 0 || (!hasContent(c) && c.Style == nil) {
				continue
			}
			budget--
			// A script counts at its full size, the size its baseline is
			// set from ([pageBuilder.excelScriptShift]).
			d := depth(ri, c)
			if d < 0 {
				continue
			}
			deep = max(deep, d)
			if len(c.Runs) == 0 {
				continue
			}
			f := b.measuredFont(g, ri, c)
			f.excel, f.script = true, Baseline
			for _, r := range c.Runs {
				if r.Font != nil {
					rf := overlay(f, r.Font)
					rf.script = Baseline
					if b.m.served(rf) {
						deep = max(deep, b.m.descent(rf)+b.m.lineGap(rf))
					}
				}
			}
		}
		if deep > base+epsilon {
			g.drops[ri] = deep - base
		}
	}
}

// rowsPerCheck is how many rows the row heights and the page plan go
// through between two checks of the context.
const rowsPerCheck = 4096

// excelHeight returns the height Excel gives a row without a height of
// its own whose wrapped text takes a count of lines set pitch points
// apart ([builder.excelLine]), in whole printer pixels. Excel fits such a
// row as it opens a workbook, and leaves a row whose text is merely taller
// than the default unfitted.
//
// # Corpus evidence
//
//   - litho/xlsx: alignment prints its rows of three wrapped lines of
//     Calibri 11 43.56 points tall, three rows of 14.52; fonts prints its
//     row of 14 point text without a height 14.52 points tall.
//   - libreoffice/xlsx: sc/tdf139934.xlsx prints its rows of two wrapped
//     lines of Arial 10 24.72 points tall, two rows of 12.36.
func excelHeight(pitch, lines float64) float64 {
	return math.Round(pitch*max(lines, 1)/excelPixel) * excelPixel
}

// excelLine returns how far apart Excel sets the wrapped lines of a cell
// in the font f, in points: the line GDI gives the font in printer pixels
// ([measurer.printedLine]) under the cell's top inset, the standard row
// at least, so that lines of the Normal font are a standard row apart; the
// line of f's own face when no standard row is known.
//
// # Corpus evidence
//
//   - litho/xlsx: alignment sets the three wrapped Calibri 11 lines of
//     each of its wrapped cells 14.52 points apart, a standard row, where
//     their font's line is 13.43 points.
//   - probe, xlsx: Excel for the web sets wrapped Calibri 14 lines 18.00
//     points apart (150 printer pixels: 8, an ascent of 111 and a descent
//     of 31) and Calibri 20 lines 25.44 (212: 8, 159 and 45), top, centre
//     and bottom aligned, in a Calibri 11 book.
func (b *builder) excelLine(f font) float64 {
	std := b.doc.StandardRowHeight
	if !numeric.Positive(std) {
		return b.m.lineHeight(f)
	}
	return max(std, b.doc.Default.Padding[css.Top]+b.m.printedLine(f))
}

// excelPixel is the printer pixel Excel lays a printed sheet out in, in
// points.
const excelPixel = 72.0 / 600

// editLine returns the height of a wrapped line of text whose natural
// line is h points: Calc's edit engine rounds it up to whole twips.
//
// # Corpus evidence
//
//   - probe, ods: a flat ODS of wrapped Liberation Sans 10 point text
//     (11.17 points a line), converted by Gotenberg 9.0.0, sets its lines 11.2
//     points apart.
func editLine(h float64) float64 {
	return math.Ceil(float64(h*20)-1e-6) / 20
}

// calcStandard reports a cell of row ri whose optimal height Calc takes
// from its font size alone ([calcStandardHeight]) rather than from its
// text: one line of text in one font, the font of its script, unwrapped,
// not justified, upright and unrotated, under no conditional format
// (ScColumn::GetOptimalHeight, sc/source/core/data/column2.cxx, its
// bStdOnly case). Text cut into several runs, by its formatting or by its
// scripts, is laid out by Calc's edit engine and measured as it prints.
func (g *grid) calcStandard(ri int, c *Cell, st *Style, d *display) bool {
	if d.wrap || st.Align.Horizontal == HJustify || st.Align.Rotation != 0 || st.Align.Stacked {
		return false
	}
	if len(c.Runs) > 1 || strings.Contains(d.text, "\n") {
		return false
	}
	return g.cond == nil || !g.cond.covers(ri, c.Column)
}

// calcStandardHeight returns, in points, the optimal height Calc gives a
// row for a cell of one plain line in a font of size points with the
// padding pad (lcl_GetAttribHeight, sc/source/core/data/column2.cxx): the
// size in twips times 1.18, truncated to a whole twip as Calc's 16-bit
// count truncates it, plus the top and bottom margins in twips, less
// STD_ROWHEIGHT_DIFF, 23 twips. The face plays no part.
//
// # Corpus evidence
//
//   - probe, ods: through gotenberg/gotenberg:9.4.1, rows of optimal
//     height holding one cell of Liberation Sans, Carlito, DejaVu Sans,
//     Liberation Serif, Caladea or Liberation Mono at 11, 12, 14, 18 and
//     24 points print 13.78, 14.99, 17.34, 22.02 and 29.14 points tall
//     whatever the face, and Liberation Sans and DejaVu Sans from 10.5 to
//     40 points in half points 13.18 to 48.04: 276, 300, 347, 441, 583
//     twips for 11 to 24 points, each within a hundredth of a point of
//     this rule; with fo:padding 1pt and without it alike, Calc's default
//     margin being 20 twips.
//   - probe, ods: chart2/PivotTableExample.ods of libreoffice/ods with its
//     rows' stored optimal height changed to 3 mm or to 10 mm prints them
//     13.78 points tall, as with the 4.87 mm it stores: Calc measures the
//     row again as it loads it.
func calcStandardHeight(size float64, pad [4]float64) float64 {
	h := math.Trunc(float64(math.Round(size*20) * 1.18))
	h += math.Round(pad[css.Top]*20) + math.Round(pad[css.Bottom]*20)
	if h > calcRowHeightDiff {
		h -= calcRowHeightDiff
	}
	return h / 20
}

// calcRowHeightDiff is Calc's STD_ROWHEIGHT_DIFF, in twips
// (sc/inc/global.hxx).
const calcRowHeightDiff = 23
