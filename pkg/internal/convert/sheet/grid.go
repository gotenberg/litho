package sheet

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
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
	// merges maps the top-left cell of each merge to it; covered marks
	// every other cell of a merge.
	merges  map[[2]int]Range
	covered map[[2]int][2]int
	// rowOrder lists the indexes of rows with cells, rising.
	rowOrder []int
	// colPrefix and rowPrefix are the running sums of the column widths
	// and row heights, filled on first use.
	colPrefix, rowPrefix []float64
	// cond is the sheet's conditional formatting, nil for none.
	cond *conditions
	// need measures the room a text cell needs on one line, nil for no
	// measuring; overflowBudget counts down the cells it may measure.
	need           func(r, c int, cell *Cell) float64
	overflowBudget int
	// overflows caches the last column each text cell reaches.
	overflows map[[2]int]int
}

// newGrid indexes the sheet sh of doc, with its conditional formats.
func newGrid(doc *Document, sh *Sheet) *grid {
	g := newPlainGrid(doc, sh)
	g.cond = newConditions(doc, sh)
	return g
}

// newPlainGrid indexes the sheet sh of doc without its conditional
// formats, as the formula evaluator reads it.
func newPlainGrid(doc *Document, sh *Sheet) *grid {
	g := &grid{
		sh: sh, doc: doc, defW: sh.DefaultColumnWidth, defH: sh.DefaultRowHeight,
		rows: map[int]*Row{}, heights: map[int]float64{}, overflows: map[[2]int]int{}, merges: map[[2]int]Range{}, covered: map[[2]int][2]int{},
	}
	if !positive(g.defW) || g.defW > maxSide {
		g.defW = defaultColumnWidth
	}
	if !positive(g.defH) || g.defH > maxSide {
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
		// cell and draws as a single cell.
		if (m.LastRow-m.FirstRow+1)*(m.LastColumn-m.FirstColumn+1) > maxMergeCells {
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

// colWidth returns the width of column c, 0 when hidden.
func (g *grid) colWidth(c int) float64 {
	col := g.column(c)
	if col == nil {
		return g.defW
	}
	if col.Hidden {
		return 0
	}
	if col.Width > 0 {
		return min(col.Width, maxSide)
	}
	return g.defW
}

// rowHidden reports a row that does not print.
func (g *grid) rowHidden(r int) bool {
	row := g.rows[r]
	return row != nil && row.Hidden
}

// rowHeight returns the height of row r, 0 when hidden: the row's own,
// grown to fit its text when it has no custom height.
func (g *grid) rowHeight(r int) float64 {
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
	if st.Fill.Color.A > 0 {
		return true
	}
	for _, b := range st.Border {
		if b.Line != LineNone {
			return true
		}
	}
	return (st.DiagonalUp || st.DiagonalDown) && st.Diagonal.Line != LineNone
}

// usedRange returns the cells a sheet without print areas prints: from A1
// to the last row and column of the cells with a value or a visible style,
// the merges and the cells under its drawings, and on to the last column
// a text overflows into unless the sheet prints on one page of its
// content; false when there are none.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/cell-value.xlsx, whose D3 text overflows far
//     past its last column, prints 8 pages.
//   - gotenberg/xlsx: budget-single, whose Notes sheet's A2 text
//     overflows, prints a page of column A alone as one page of content.
func (g *grid) usedRange(single bool) (Range, bool) {
	u := Range{FirstRow: math.MaxInt32, FirstColumn: math.MaxInt32, LastRow: -1, LastColumn: -1}
	add := func(r, c int) {
		u.FirstRow, u.LastRow = min(u.FirstRow, r), max(u.LastRow, r)
		u.FirstColumn, u.LastColumn = min(u.FirstColumn, c), max(u.LastColumn, c)
	}
	dataLast, attrLast := -1, -1
	for _, ri := range g.rowOrder {
		row := g.rows[ri]
		for i := range row.Cells {
			c := &row.Cells[i]
			if c.Column < 0 || c.Column >= maxColumns {
				continue
			}
			if hasContent(c) {
				dataLast = max(dataLast, c.Column)
				add(ri, c.Column)
			} else if visible(c.Style) {
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
