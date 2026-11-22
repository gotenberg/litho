package xlsx

import (
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// pivotStyleNamed returns the pivot table style name: the workbook's own
// (its styles part's tableStyles), else the preset of that name, nil for
// none.
func (r *reader) pivotStyleNamed(name string) *tableStyle {
	if ts, ok := r.styles.tables[name]; ok {
		return ts
	}
	xml, ok := presetPivotStyles[name]
	if !ok {
		return nil
	}
	root, err := ooxml.Parse(r.ctx, []byte(`<s xmlns="`+nsMain+`">`+xml+`</s>`), nil)
	if err != nil {
		return nil
	}
	var ts *tableStyle
	if el := root.Child(nsMain, "tableStyles").Child(nsMain, "tableStyle"); el != nil {
		ts = r.parseTableStyle(el, root.Child(nsMain, "dxfs").Kids(), 0)
	}
	r.styles.tables[name] = ts
	return ts
}

// The kinds of a pivot table's row or column line (ECMA-376 Part 1,
// 18.18.43 ST_ItemType on a rowItems or colItems i element).
const (
	lineItem = iota
	lineSubtotal
	lineGrand
	lineBlank
)

// pivotLine is one row or column of a pivot table's body: its kind and
// the depth of the field it shows, 0 for the outermost.
type pivotLine struct {
	kind, depth int
	// parent marks an item whose next line goes deeper: a subheading.
	parent bool
}

// pivotLayout is what styling a pivot table needs from its definition
// (18.10.1.73): the location (18.10.1.49), the lines of its body, its
// page fields and the parts of its style it shows (18.10.7.74
// pivotTableStyleInfo).
type pivotLayout struct {
	area                sheet.Range
	headerRows, rowCols int
	rows, cols          []pivotLine
	// pages is the area of the page fields: their labels in its first
	// column, their values in its last.
	pages                  sheet.Range
	hasPages               bool
	rowHeaders, colHeaders bool
	rowStripes, colStripes bool
	lastColumn             bool
}

// readPivotLines reads a rowItems or colItems element (18.10.1.84,
// 18.10.1.18): each i's kind and depth, its r repeating the fields
// before the x elements it holds.
func readPivotLines(e *ooxml.Element) []pivotLine {
	var out []pivotLine
	for _, i := range e.Kids() {
		if !i.Is(nsMain, "i") || len(out) >= maxPivotItems {
			continue
		}
		n := 0
		for _, x := range i.Kids() {
			if x.Is(nsMain, "x") {
				n++
			}
		}
		l := pivotLine{depth: max(intAttr(i, "r", 0), 0)}
		switch attr(i, "t") {
		case "", "data":
			l.depth += max(n-1, 0)
		case "grand":
			l.kind = lineGrand
		case "blank":
			l.kind = lineBlank
		default:
			l.kind = lineSubtotal
		}
		l.depth = min(l.depth, maxPivotItems)
		out = append(out, l)
	}
	for k := range out {
		out[k].parent = out[k].kind == lineItem && k+1 < len(out) && out[k+1].kind != lineGrand && out[k+1].depth > out[k].depth
	}
	return out
}

// pivotLayoutOf reads the layout of the pivot table definition root,
// false when its location does not parse.
func pivotLayoutOf(root *ooxml.Element) (pivotLayout, bool) {
	loc := root.Child(nsMain, "location")
	area, ok := rangeRef(attr(loc, "ref"))
	if !ok {
		return pivotLayout{}, false
	}
	l := pivotLayout{
		area:       area,
		headerRows: min(max(intAttr(loc, "firstDataRow", 0), 0), area.LastRow-area.FirstRow+1),
		rowCols:    min(max(intAttr(loc, "firstDataCol", 0), 0), area.LastColumn-area.FirstColumn+1),
		rows:       readPivotLines(root.Child(nsMain, "rowItems")),
		cols:       readPivotLines(root.Child(nsMain, "colItems")),
	}
	pages := 0
	for _, f := range root.Child(nsMain, "pageFields").Kids() {
		if f.Is(nsMain, "pageField") {
			pages++
		}
	}
	// Page fields stand in one column of label and value above the
	// location, an empty row between, as Excel prints a table whose
	// fields stand down then over in one column (colPageCount 1).
	if down := intAttr(loc, "rowPageCount", 0); pages > 0 && down >= pages && intAttr(loc, "colPageCount", 1) <= 1 && area.FirstRow-down-1 >= 0 {
		l.pages = sheet.Range{FirstRow: area.FirstRow - down - 1, FirstColumn: area.FirstColumn, LastRow: area.FirstRow - down - 2 + pages, LastColumn: area.FirstColumn + 1}
		l.hasPages = true
	}
	if si := root.Child(nsMain, "pivotTableStyleInfo"); si != nil {
		l.rowHeaders, l.colHeaders = boolAttr(si, "showRowHeaders"), boolAttr(si, "showColHeaders")
		l.rowStripes, l.colStripes = boolAttr(si, "showRowStripes"), boolAttr(si, "showColStripes")
		l.lastColumn = boolAttr(si, "showLastColumn")
	}
	return l, true
}

// stylePivot paints the pivot table style its definition root names over
// the cells the sheet saved for the table, as styleTable paints a table:
// its fill where the cell has none, its font weight, slant and colour
// where the cell keeps the Normal style's, its borders where the cell
// draws none.
func (w *sheetReader) stylePivot(root *ooxml.Element) {
	si := root.Child(nsMain, "pivotTableStyleInfo")
	if si == nil {
		return
	}
	ts := w.r.pivotStyleNamed(attr(si, "name"))
	l, ok := pivotLayoutOf(root)
	if ts == nil || !ok {
		return
	}
	cache := map[styleKey]*sheet.Style{}
	normal := w.r.normal()
	apply := func(cell *sheet.Cell, row, col int) {
		key := styleKey{base: w.styleAt(cell, row, col), overlay: ts.pivotOverlay(&l, row, col)}
		if key.overlay == (overlay{}) {
			return
		}
		st, ok := cache[key]
		if !ok {
			st = key.apply(normal)
			cache[key] = st
		}
		cell.Style = st
	}
	if l.hasPages {
		w.eachAreaCell(l.pages, apply)
	}
	w.eachAreaCell(l.area, apply)
}

// pivotPart is one element of a pivot table style that covers a cell, and
// the block of cells it covers there, whose edges take the element's
// outer border sides and whose inside its inner lines.
type pivotPart struct {
	name  string
	block sheet.Range
}

// subheadings and subtotals name the elements of the first three levels
// of row subheadings and subtotal rows and columns (18.18.84
// ST_TableStyleType).
var (
	rowSubheadings  = [3]string{"firstRowSubheading", "secondRowSubheading", "thirdRowSubheading"}
	subtotalRows    = [3]string{"firstSubtotalRow", "secondSubtotalRow", "thirdSubtotalRow"}
	subtotalColumns = [3]string{"firstSubtotalColumn", "secondSubtotalColumn", "thirdSubtotalColumn"}
)

// pivotParts returns the elements that cover the cell at row and col of
// the pivot table l, highest precedence first: the grand total row and
// column, the row subheadings, the blank and subtotal rows, the subtotal
// columns, the first header cell, the header rows, the first column, the
// row and column stripes, the page fields, the whole table. The header
// rows run from the location's top to its first data row; a row of the
// body is a subheading of its depth when it holds an item whose next row
// goes deeper (ECMA-376 Part 1, 18.18.84 ST_TableStyleType names the
// elements and leaves their reach to the application).
//
// # Corpus evidence
//
//   - openxmlsdk/xlsx v2FxTestFiles/spreadsheet/Pivot4.xlsx: under
//     PivotStyleLight16 Excel fills the three header rows and sets them
//     bold with one line under the last; the depth-0 row "USA" is bold
//     with a line under it across the table, the depth-1 row bold, deeper
//     rows plain; the page field's label and value are filled with a line
//     under them.
//   - libreoffice/xlsx sc/PivotTable_CachedDefinitionAndDataInSync.xlsx:
//     under PivotStyleLight15 the row stripe starts on the first data
//     row, across the row labels too; the grand total row takes its own
//     fill, bold and line over it.
//   - libreoffice/xlsx sc/pivottable_outline_mode.xlsx: in outline form
//     each outer item's row, holding its subtotal at the top, is bold
//     with a line under it across the table.
func (ts *tableStyle) pivotParts(l *pivotLayout, row, col int) []pivotPart {
	a := l.area
	var out []pivotPart
	add := func(on bool, name string, block sheet.Range) {
		if on && ts.elements[name] != nil {
			out = append(out, pivotPart{name, block})
		}
	}
	if l.hasPages && row >= l.pages.FirstRow && row <= l.pages.LastRow {
		p := l.pages
		add(col == p.LastColumn, "pageFieldValues", sheet.Range{FirstRow: p.FirstRow, LastRow: p.LastRow, FirstColumn: col, LastColumn: col})
		add(col == p.FirstColumn, "pageFieldLabels", sheet.Range{FirstRow: p.FirstRow, LastRow: p.LastRow, FirstColumn: col, LastColumn: col})
		return out
	}
	if row < a.FirstRow || row > a.LastRow || col < a.FirstColumn || col > a.LastColumn {
		return nil
	}
	bodyTop := a.FirstRow + l.headerRows
	bodyLeft := a.FirstColumn + l.rowCols
	wholeRow := sheet.Range{FirstRow: row, LastRow: row, FirstColumn: a.FirstColumn, LastColumn: a.LastColumn}
	var line, column pivotLine
	inBody := row >= bodyTop
	if k := row - bodyTop; inBody && k < len(l.rows) {
		line = l.rows[k]
	}
	inData := col >= bodyLeft
	if k := col - bodyLeft; inData && k < len(l.cols) {
		column = l.cols[k]
	}
	dataCol := sheet.Range{FirstRow: bodyTop, LastRow: a.LastRow, FirstColumn: col, LastColumn: col}
	add(inBody && line.kind == lineGrand, "totalRow", wholeRow)
	add(inData && inBody && l.lastColumn && column.kind == lineGrand, "lastColumn", dataCol)
	if inBody && l.rowHeaders && line.parent && line.depth < len(rowSubheadings) {
		add(true, rowSubheadings[line.depth], wholeRow)
	}
	add(inBody && line.kind == lineBlank, "blankRow", wholeRow)
	if inBody && line.kind == lineSubtotal && line.depth < len(subtotalRows) {
		add(true, subtotalRows[line.depth], wholeRow)
	}
	if inBody && inData && column.kind == lineSubtotal && column.depth < len(subtotalColumns) {
		add(true, subtotalColumns[column.depth], dataCol)
	}
	header := sheet.Range{FirstRow: a.FirstRow, LastRow: bodyTop - 1, FirstColumn: a.FirstColumn, LastColumn: a.LastColumn}
	add(l.colHeaders && !inBody && row == a.FirstRow && col == a.FirstColumn, "firstHeaderCell", sheet.Range{FirstRow: row, LastRow: row, FirstColumn: col, LastColumn: col})
	add(l.colHeaders && !inBody, "headerRow", header)
	add(l.rowHeaders && inBody && !inData, "firstColumn", sheet.Range{FirstRow: bodyTop, LastRow: a.LastRow, FirstColumn: a.FirstColumn, LastColumn: bodyLeft - 1})
	if inBody && l.rowStripes {
		k := row - bodyTop
		n := ts.rowStripe[0] + ts.rowStripe[1]
		start := bodyTop + k - k%n
		first := k%n < ts.rowStripe[0]
		band := sheet.Range{FirstRow: start, LastRow: start + ts.rowStripe[0] - 1, FirstColumn: a.FirstColumn, LastColumn: a.LastColumn}
		if !first {
			band.FirstRow, band.LastRow = start+ts.rowStripe[0], start+n-1
		}
		band.LastRow = min(band.LastRow, a.LastRow)
		add(!first, "secondRowStripe", band)
		add(first, "firstRowStripe", band)
	}
	if inBody && inData && l.colStripes {
		k := col - bodyLeft
		n := ts.colStripe[0] + ts.colStripe[1]
		start := bodyLeft + k - k%n
		first := k%n < ts.colStripe[0]
		band := sheet.Range{FirstRow: bodyTop, LastRow: a.LastRow, FirstColumn: start, LastColumn: start + ts.colStripe[0] - 1}
		if !first {
			band.FirstColumn, band.LastColumn = start+ts.colStripe[0], start+n-1
		}
		band.LastColumn = min(band.LastColumn, a.LastColumn)
		add(!first, "secondColumnStripe", band)
		add(first, "firstColumnStripe", band)
	}
	add(true, "wholeTable", a)
	return out
}

// pivotOverlay returns what the pivot table style gives the cell at row
// and col of the pivot table l: each font and fill attribute from the
// first element that sets it, each border side from the first element
// that draws it there, its outer side on the edge of the block the
// element covers and its inner line inside it.
func (ts *tableStyle) pivotOverlay(l *pivotLayout, row, col int) overlay {
	parts := ts.pivotParts(l, row, col)
	var o overlay
	for _, p := range parts {
		d := ts.elements[p.name]
		if !o.hasBold && d.bold != nil {
			o.bold, o.hasBold = *d.bold, true
		}
		if !o.hasItalic && d.italic != nil {
			o.italic, o.hasItalic = *d.italic, true
		}
		if !o.hasColor && d.color != nil {
			o.color, o.hasColor = *d.color, true
		}
		if !o.hasFill && d.fill != nil {
			o.fill, o.hasFill = *d.fill, true
		}
	}
	for side, s := range [4]struct {
		outer, inner int
	}{
		css.Top:    {dxfTop, dxfHorizontal},
		css.Bottom: {dxfBottom, dxfHorizontal},
		css.Left:   {dxfLeft, dxfVertical},
		css.Right:  {dxfRight, dxfVertical},
	} {
		for _, p := range parts {
			edge := false
			switch css.Side(side) {
			case css.Top:
				edge = row == p.block.FirstRow
			case css.Bottom:
				edge = row == p.block.LastRow
			case css.Left:
				edge = col == p.block.FirstColumn
			case css.Right:
				edge = col == p.block.LastColumn
			}
			b := ts.elements[p.name].border[s.inner]
			if edge {
				b = ts.elements[p.name].border[s.outer]
			}
			if b != nil {
				o.border[side] = *b
				break
			}
		}
	}
	return o
}
