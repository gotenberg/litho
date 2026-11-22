package sheet

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// The sheet limits, Excel's and Calc's: 1,048,576 rows by 16,384 columns
// (ECMA-376 Part 1, 18.3.1.73 and 18.3.1.13). An index past them is
// ignored. Anti-DoS guard as much as a format limit: every loop over rows
// or columns is bounded by them.
const (
	maxRows    = 1 << 20
	maxColumns = 1 << 14
)

// maxSide is the largest column width or row height honoured, in points.
// Anti-DoS guard, not spec-mandated: Excel stops at 409 points a row and
// 255 characters a column, about 1,800 points.
const maxSide = 14400

// defaultColumnWidth and defaultRowHeight stand in for a sheet whose
// defaults are not positive: Calc's 2.258 cm column (64 points) and 0.452 cm
// row (12.8 points) with its default Liberation Sans 10 point font.
const (
	defaultColumnWidth = 64
	defaultRowHeight   = 12.8
)

// grid is a sheet's geometry and cell lookup: column widths, row heights
// and hidden flags, the cells by position, the merges.
type grid struct {
	sh   *Sheet
	doc  *Document
	defW float64
	defH float64
	// rows maps a row index to its Row.
	rows map[int]*Row
	// heights holds the height of every row the builder grew to fit its
	// text, by index.
	heights map[int]float64
	// drops holds how much higher Excel sets the bottom-aligned text of a
	// row it measures ([builder.excelDrops]).
	drops map[int]float64
	// merges maps the top-left cell of each merge to it; covered marks
	// every other cell of a merge.
	merges  map[[2]int]Range
	covered map[[2]int][2]int
	// largeMerges counts the merges past maxMergeCells, each drawn as its
	// top-left cell.
	largeMerges int
	// rowOrder lists the indexes of rows with cells, rising.
	rowOrder []int
	// colPrefix and rowPrefix are the running sums of the printed column
	// widths and row heights, filled on first use; layoutColPrefix and
	// layoutRowPrefix those of the layout ones, under Calc's rules.
	colPrefix, rowPrefix             []float64
	layoutColPrefix, layoutRowPrefix []float64
	// zoom is, under Excel's rules, the print scale the rows are printed
	// at, 0 for 100% ([excelZoomed]).
	zoom float64
	// colCap is the widest a column prints, 0 for no bound
	// ([sheetPlan.columnCap]).
	colCap float64
	// cond is the sheet's conditional formatting, nil for none.
	cond *conditions
	// need measures the room a text cell needs on one line, nil for no
	// measuring; overflowBudget counts down the cells it may measure.
	need           func(r, c int, cell *Cell) float64
	overflowBudget int
	// overflows caches the last column each text cell reaches.
	overflows map[[2]int]int
}

// newGrid indexes the sheet sh of doc, with its conditional formats,
// whose formulas evaluate under ctx.
func newGrid(ctx context.Context, doc *Document, sh *Sheet) *grid {
	g := newPlainGrid(doc, sh)
	g.cond = newConditions(ctx, doc, sh)
	return g
}

// newPlainGrid indexes the sheet sh of doc without its conditional
// formats, as the formula evaluator reads it.
func newPlainGrid(doc *Document, sh *Sheet) *grid {
	g := &grid{
		sh: sh, doc: doc, defW: sh.DefaultColumnWidth, defH: sh.DefaultRowHeight,
		rows: map[int]*Row{}, heights: map[int]float64{}, drops: map[int]float64{}, overflows: map[[2]int]int{}, merges: map[[2]int]Range{}, covered: map[[2]int][2]int{},
	}
	if !numeric.Positive(g.defW) || g.defW > maxSide {
		g.defW = defaultColumnWidth
	}
	if !numeric.Positive(g.defH) || g.defH > maxSide {
		g.defH = defaultRowHeight
	}
	for i := range sh.Rows {
		r := &sh.Rows[i]
		if r.Index < 0 || r.Index >= maxRows {
			continue
		}
		if _, ok := g.rows[r.Index]; ok {
			continue
		}
		g.rows[r.Index] = r
		if len(r.Cells) > 0 {
			g.rowOrder = append(g.rowOrder, r.Index)
		}
	}
	sort.Ints(g.rowOrder)
	for _, m := range sh.Merges {
		m = clampRange(m)
		if m.LastRow < m.FirstRow || m.LastColumn < m.FirstColumn || (m.LastRow == m.FirstRow && m.LastColumn == m.FirstColumn) {
			continue
		}
		if _, ok := g.covered[[2]int{m.FirstRow, m.FirstColumn}]; ok {
			continue
		}
		// Anti-DoS guard, not spec-mandated: a merge marks at most
		// maxMergeCells cells covered; a larger one keeps its top-left
		// cell and draws as a single cell, which the sheet's plan
		// reports.
		if mergeTooLarge(m) {
			g.largeMerges++
			continue
		}
		g.merges[[2]int{m.FirstRow, m.FirstColumn}] = m
		for r := m.FirstRow; r <= m.LastRow; r++ {
			for c := m.FirstColumn; c <= m.LastColumn; c++ {
				if r != m.FirstRow || c != m.FirstColumn {
					g.covered[[2]int{r, c}] = [2]int{m.FirstRow, m.FirstColumn}
				}
			}
		}
	}
	return g
}

// maxMergeCells caps the cells one merge covers. Anti-DoS guard, not
// spec-mandated: a merged title spans a few hundred cells at most.
const maxMergeCells = 1 << 16

// mergeTooLarge reports a merge of more than maxMergeCells cells, m
// holding at least one column. It compares the merge's rows with the rows
// a merge of its columns may have and forms no product of the two counts:
// the whole grid is 2^34 cells, which a 32-bit int does not hold.
func mergeTooLarge(m Range) bool {
	rows, cols := m.LastRow-m.FirstRow+1, m.LastColumn-m.FirstColumn+1
	return rows > maxMergeCells/cols
}

// clampRange returns r with its indexes inside the sheet limits.
func clampRange(r Range) Range {
	c := func(v, hi int) int { return min(max(v, 0), hi-1) }
	return Range{FirstRow: c(r.FirstRow, maxRows), LastRow: c(r.LastRow, maxRows), FirstColumn: c(r.FirstColumn, maxColumns), LastColumn: c(r.LastColumn, maxColumns)}
}

// column returns the Column covering c, nil for none.
func (g *grid) column(c int) *Column {
	cols := g.sh.Columns
	i := sort.Search(len(cols), func(i int) bool { return cols[i].Last >= c })
	if i < len(cols) && cols[i].First <= c && c <= cols[i].Last {
		return &cols[i]
	}
	return nil
}

// colHidden reports a column that does not print.
func (g *grid) colHidden(c int) bool {
	col := g.column(c)
	return col != nil && col.Hidden
}

// colWidth returns the width column c prints at, 0 when hidden: its
// layout width ([grid.layoutColWidth]), under Calc's rules as Calc draws
// it ([calcPrinted]).
func (g *grid) colWidth(c int) float64 {
	return g.printed(g.layoutColWidth(c))
}

// rowHeight returns the height row r prints at, 0 when hidden: its
// layout height ([grid.layoutRowHeight]), under Calc's rules as Calc
// draws it ([calcPrinted]).
func (g *grid) rowHeight(r int) float64 {
	if g.zoom > 0 {
		return excelZoomed(g.layoutRowHeight(r), g.zoom)
	}
	return g.printed(g.layoutRowHeight(r))
}

// setZoom prints the rows at the scale z under Excel's rules, which round
// each row to a printer pixel at it ([excelZoomed]), and forgets the
// running sums taken at another.
func (g *grid) setZoom(z float64) {
	if !g.doc.ExcelRules || z == g.zoom || (z == 1 && g.zoom == 0) {
		return
	}
	g.zoom = 0
	if z != 1 {
		g.zoom = z
	}
	g.rowPrefix, g.layoutRowPrefix = nil, nil
}

// printed returns a column width or row height of v points as the sheet
// prints it: v under Excel's rules, [calcPrinted] under Calc's.
func (g *grid) printed(v float64) float64 {
	if g.doc.ExcelRules {
		return v
	}
	return calcPrinted(v)
}

// calcPrinted returns the length Calc prints a row height or column width
// of v points at. Calc holds both in whole twips (sc/inc/document.hxx,
// GetRowHeight and GetColWidth) and cuts its pages on their sums
// (ScTable::UpdatePageBreaks), but its print output works in 1/100 mm
// (ScPrintFunc sets nScaleX and nScaleY to HMM_PER_TWIPS,
// sc/source/ui/view/printfun.cxx) and truncates each row and column to
// whole 1/100 mm on its own (initRowInfo and initColWidths in
// sc/source/core/data/fillinfo.cxx, ScTable::GetScaledRowHeight), at
// least one. A zoom scales the truncated lengths: it is the scale of the
// output's map mode. A zero length stays zero.
//
// # Corpus evidence
//
//   - probe, ods: rows of 288, 283, 299, 300, 306, 307, 340 and 480 twips,
//     converted by Gotenberg 9.4.1, print 14.4, 14.145, 14.939, 14.995,
//     15.279, 15.335, 16.98 and 23.981 points apart: 508, 499, 527, 529,
//     539, 541, 599 and 846 hundredths of a millimetre. Columns of 960,
//     1,280, 1,344, 1,406, 1,417 and 1,600 twips print 47.99, 63.978,
//     67.181, 70.3, 70.838 and 79.994 points wide. Twenty rows of 299
//     twips span 298.772 points, twenty times 527. At 50% and 73% the same
//     rows print 0.5 and 0.73 times as far apart.
//   - probe, ods: 40 sheets of rows of 288 to 361 twips on an A4 body of
//     14,570 twips print floor(14,570 / height) rows a page, the cut on
//     whole twips: rows of 324 twips print 44 a page, where their printed
//     571 hundredths would fit 45 in the body's 25,700.
//   - probe, csv: rows of the default 256 twips print 12.784 points
//     apart, 451 hundredths, and its columns 33.392 and 71.235 points
//     wide, 1,178 and 2,513 hundredths: 668 and 1,425 twips truncated.
func calcPrinted(v float64) float64 {
	if !numeric.Positive(v) {
		return 0
	}
	twips := math.Round(v * 20)
	hmm := max(math.Floor(twips*127/72+1e-9), 1)
	return hmm * 72 / 2540
}

// layoutColWidth returns the width of column c Calc and Excel cut pages
// on, 0 when hidden.
func (g *grid) layoutColWidth(c int) float64 {
	col := g.column(c)
	if col != nil && col.Hidden {
		return 0
	}
	w := g.defW
	if col != nil && col.Width > 0 {
		w = min(col.Width, maxSide)
	}
	if g.colCap > 0 {
		w = min(w, g.colCap)
	}
	return w
}

// rowHidden reports a row that does not print.
func (g *grid) rowHidden(r int) bool {
	row := g.rows[r]
	return row != nil && row.Hidden
}

// layoutRowHeight returns the height of row r Calc and Excel cut pages
// on, 0 when hidden: the row's own, grown to fit its text when it has no
// custom height.
func (g *grid) layoutRowHeight(r int) float64 {
	row := g.rows[r]
	if row != nil && row.Hidden {
		return 0
	}
	if h, ok := g.heights[r]; ok {
		return h
	}
	if row != nil && row.Height > 0 {
		return min(row.Height, maxSide)
	}
	return g.defH
}

// cell returns the cell at (r, c), nil for none.
func (g *grid) cell(r, c int) *Cell {
	row := g.rows[r]
	if row == nil {
		return nil
	}
	cells := row.Cells
	i := sort.Search(len(cells), func(i int) bool { return cells[i].Column >= c })
	if i < len(cells) && cells[i].Column == c {
		return &cells[i]
	}
	return nil
}

// hasContent reports a cell that shows something: a value, or its text.
func hasContent(c *Cell) bool {
	return c != nil && (c.Value.Kind != Empty || len(c.Runs) > 0)
}

// style returns the style of the cell at (r, c), cell being it or nil: the
// cell's own, else its row's, else its column's, else the document
// default, with the sheet's conditional formats applied over it.
func (g *grid) style(r, c int, cell *Cell) *Style {
	return g.condStyle(r, c, cell, g.baseStyle(r, c, cell))
}

// baseStyle returns the style of the cell at (r, c) before conditional
// formatting.
func (g *grid) baseStyle(r, c int, cell *Cell) *Style {
	if cell != nil && cell.Style != nil {
		return cell.Style
	}
	if row := g.rows[r]; row != nil && row.Style != nil {
		return row.Style
	}
	if col := g.column(c); col != nil && col.Style != nil {
		return col.Style
	}
	return &g.doc.Default
}

// visible reports a style that paints on an empty cell: a fill, a border
// or a diagonal, which makes the cell part of the used range as Calc
// counts it.
func visible(st *Style) bool {
	if st == nil {
		return false
	}
	if st.Fill.Color.A > 0 || lined(st) {
		return true
	}
	return (st.DiagonalUp || st.DiagonalDown) && st.Diagonal.Line != LineNone
}

// lined reports a style that draws a border on a side of its cell.
func lined(st *Style) bool {
	if st == nil {
		return false
	}
	for _, b := range st.Border {
		if b.Line != LineNone {
			return true
		}
	}
	return false
}

// usedRange returns the cells a sheet without print areas prints: from A1
// to the last row and column of the cells with a value or a visible style,
// the merges and the cells under its drawings, and on to the last column
// a text overflows into unless the sheet prints on one page of its
// content; false when there are none.
//
// Calc counts a visible style only below the last value of its column, a
// column without one counting its first row as that value
// (ScTable::GetPrintArea, ScAttrArray::GetLastVisibleAttr), and stops at
// a run of columns formatted alike ([grid.attrColumns]). Excel counts
// every cell with a visible style, and a cell whose file stores an empty
// string as a cell with a value ([Cell.EmptyText]).
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/cell-value.xlsx, whose D3 text overflows far
//     past its last column, prints 8 pages.
//   - gotenberg/xlsx: budget-single, whose Notes sheet's A2 text
//     overflows, prints a page of column A alone as one page of content.
//   - probe, ods: A1 holding a value and J1 a border, on A4 at seven
//     columns a page, prints one page; with the border in J2, or J3 below
//     a value in A5, two; A1 and J1 with a border and A3 a value, one.
//   - poi/xlsx: 61652.xlsx prints its Model sheet to the filled cells of
//     column AW, past 30 columns formatted alike, on 15 pages;
//     55406_Conditional_formatting_sample.xlsx prints its filled cells to
//     column AL on a second page; 56278.xlsx prints a page for the column
//     of empty strings right of each table.
func (g *grid) usedRange(single bool) (Range, bool) {
	u := Range{FirstRow: math.MaxInt32, FirstColumn: math.MaxInt32, LastRow: -1, LastColumn: -1}
	add := func(r, c int) {
		u.FirstRow, u.LastRow = min(u.FirstRow, r), max(u.LastRow, r)
		u.FirstColumn, u.LastColumn = min(u.FirstColumn, c), max(u.LastColumn, c)
	}
	excel := g.doc.ExcelRules
	// lastValue holds, under Calc's rules, the last row of each column
	// with a value.
	var lastValue map[int]int
	if !excel {
		lastValue = map[int]int{}
		for _, ri := range g.rowOrder {
			for i := range g.rows[ri].Cells {
				if c := &g.rows[ri].Cells[i]; hasContent(c) {
					lastValue[c.Column] = ri
				}
			}
		}
	}
	dataLast, attrLast := -1, -1
	for _, ri := range g.rowOrder {
		row := g.rows[ri]
		for i := range row.Cells {
			c := &row.Cells[i]
			if c.Column < 0 || c.Column >= maxColumns {
				continue
			}
			switch {
			case hasContent(c) || (excel && c.EmptyText):
				dataLast = max(dataLast, c.Column)
				add(ri, c.Column)
			case !visible(c.Style):
			case excel:
				add(ri, c.Column)
			case ri > lastValue[c.Column]:
				attrLast = max(attrLast, c.Column)
				add(ri, 0)
			}
		}
	}
	if attrLast > dataLast {
		u.LastColumn = max(u.LastColumn, g.attrColumns(dataLast, attrLast))
	}
	for _, m := range g.merges {
		if hasContent(g.cell(m.FirstRow, m.FirstColumn)) {
			add(m.LastRow, m.LastColumn)
		}
	}
	for i := range g.sh.Drawings {
		if r, ok := g.drawingCells(&g.sh.Drawings[i]); ok {
			add(r.FirstRow, r.FirstColumn)
			add(r.LastRow, r.LastColumn)
		}
	}
	if u.LastRow < 0 {
		return Range{}, false
	}
	// Calc prints from A1 to the last used cell (ScTable::GetPrintArea,
	// ScPrintFunc::AdjustPrintArea), then on to the last column the text
	// of a cell reaches into the empty cells right of it
	// (ScDocument::ExtendPrintArea).
	u.FirstRow, u.FirstColumn = 0, 0
	if !single {
		u.LastColumn = g.extendPrint(u.LastColumn, 0, u.LastRow)
	}
	return u, true
}

// columnsStop is the run of columns formatted alike right of the last
// column with a value at which Calc stops extending the print range over
// formatted cells: 30 columns (SC_COLUMNS_STOP, sc/source/core/data
// table1.cxx).
const columnsStop = 30

// attrColumns returns the last column a sheet prints for its formatted
// cells right of dataLast, the last column with a value, attrLast being
// the last formatted one: attrLast, unless a run of at least columnsStop
// columns formatted alike starts before it, where the range stops, the
// unformatted columns before the run left out too, as Calc's
// ScTable::GetPrintArea compares the columns' visible attributes (fill,
// borders, diagonals).
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf133327.ods, rows filled across 261 columns without a
//     value, prints column A alone.
func (g *grid) attrColumns(dataLast, attrLast int) int {
	keys := map[*Style]string{}
	key := func(st *Style) string {
		k, ok := keys[st]
		if !ok {
			k = fmt.Sprint(st.Fill.Color, st.Border, st.Diagonal, st.DiagonalUp, st.DiagonalDown)
			keys[st] = k
		}
		return k
	}
	sig := make([]strings.Builder, attrLast-dataLast)
	for _, ri := range g.rowOrder {
		row := g.rows[ri]
		for i := range row.Cells {
			c := &row.Cells[i]
			if c.Column <= dataLast || c.Column > attrLast || hasContent(c) || !visible(c.Style) {
				continue
			}
			b := &sig[c.Column-dataLast-1]
			b.WriteString(strconv.Itoa(ri))
			b.WriteByte(':')
			b.WriteString(key(c.Style))
			b.WriteByte(';')
		}
	}
	at := func(c int) string { return sig[c-dataLast-1].String() }
	for start := dataLast + 1; start < attrLast; {
		end := start
		for end < attrLast && at(start) == at(end+1) {
			end++
		}
		if end+1-start >= columnsStop {
			last := start - 1
			for last > dataLast && at(last) == "" {
				last--
			}
			return max(last, 0)
		}
		start = end + 1
	}
	return attrLast
}
