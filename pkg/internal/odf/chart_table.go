package odf

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
)

// tableCell is one cell of a chart's embedded table.
type tableCell struct {
	value chart.Value
	// desc is the source range the cell's draw:g names: LibreOffice marks
	// the first cell of each series' column, and the header cells, with
	// the range they cache.
	desc string
}

// localTable is the table a chart embeds as its data (ODF 1.3 Part 3,
// 11.3): the header row of series names, the header column of
// categories, a column per series.
type localTable struct {
	rows [][]tableCell
	// headerRows and firstData count the header rows and the header
	// columns, the categories.
	headerRows, firstData int
}

// readLocalTable reads a table:table, nil for none.
func readLocalTable(t *Element) *localTable {
	if t == nil {
		return nil
	}
	lt := &localTable{}
	if t.Child(NSTable, "table-header-columns") != nil {
		lt.firstData = 1
	}
	cells := 0
	var addRows func(parent *Element, header bool)
	addRows = func(parent *Element, header bool) {
		for _, row := range parent.Children {
			switch {
			case row.Is(NSTable, "table-header-rows"):
				addRows(row, true)
			case row.Is(NSTable, "table-rows"):
				addRows(row, false)
			case row.Is(NSTable, "table-row"):
				rep := repeat(row.A(NSTable, "number-rows-repeated"))
				line := readRow(row, &cells)
				for range rep {
					if cells+len(line) > maxTableCells || len(lt.rows) >= chart.MaxPoints+1 {
						return
					}
					cells += len(line)
					lt.rows = append(lt.rows, line)
					if header {
						lt.headerRows++
					}
				}
			}
		}
	}
	addRows(t, false)
	return lt
}

// repeat reads a repetition count, 1 when absent or invalid, capped by
// chart.MaxPoints.
func repeat(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 1
	}
	return min(n, chart.MaxPoints)
}

// readRow reads the cells of a table:table-row.
func readRow(row *Element, budget *int) []tableCell {
	var out []tableCell
	for _, c := range row.Children {
		if !c.Is(NSTable, "table-cell") && !c.Is(NSTable, "covered-table-cell") {
			continue
		}
		cell := tableCell{}
		switch c.A(NSOffice, "value-type") {
		case "float", "percentage", "currency":
			if v, err := strconv.ParseFloat(c.A(NSOffice, "value"), 64); err == nil {
				cell.value = chart.Value{Kind: chart.Number, Number: v, Text: paraText(c)}
			}
		case "":
			if t := paraText(c); t != "" {
				cell.value = chart.Str(t)
			}
		default:
			cell.value = chart.Str(paraText(c))
		}
		if g := c.Child(NSDraw, "g"); g != nil {
			cell.desc = strings.TrimSpace(g.Child(NSSVG, "desc").TextContent())
		}
		rep := repeat(c.A(NSTable, "number-columns-repeated"))
		for range rep {
			if len(out)+*budget >= maxTableCells {
				return out
			}
			out = append(out, cell)
		}
	}
	return out
}

// find returns the row and column of the cell that names the range
// addr.
func (t *localTable) find(addr string) (int, int, bool) {
	if addr == "" {
		return 0, 0, false
	}
	for i, row := range t.rows {
		for j, c := range row {
			if c.desc == addr {
				return i, j, true
			}
		}
	}
	return 0, 0, false
}

// run returns the cells of the range addr from its first cell (row, col):
// along the row for a range one row high and several columns wide, as a
// series in rows is, else down the column to the end of the data.
func (t *localTable) run(row, col int, addr string) chart.Data {
	d := chart.Data{Cached: true}
	if cols, rows, ok := rangeShape(addr); ok && rows == 1 && cols > 1 {
		for j := col; j < col+cols && j < len(t.rows[row]); j++ {
			d.Values = append(d.Values, t.rows[row][j].value)
		}
		return d
	}
	for _, r := range t.rows[row:] {
		if col < len(r) {
			d.Values = append(d.Values, r[col].value)
		} else {
			d.Values = append(d.Values, chart.Value{})
		}
	}
	return d
}

// headers returns the name and categories of the series whose values
// start at (row, col): for a series in rows, the header column's cell and
// the header row's cells over its columns; for one in columns, the header
// row's cell, and no categories of its own.
func (t *localTable) headers(row, col int, addr string) (string, chart.Data) {
	if cols, rows, ok := rangeShape(addr); ok && rows == 1 && cols > 1 {
		name := ""
		if t.firstData > 0 && len(t.rows[row]) > 0 {
			name = t.rows[row][0].value.Text
		}
		cats := chart.Data{Cached: true}
		if t.headerRows > 0 {
			for j := col; j < col+cols && j < len(t.rows[0]); j++ {
				cats.Values = append(cats.Values, chart.Str(t.rows[0][j].value.Text))
			}
		}
		return name, cats
	}
	return t.header(col), chart.Data{}
}

// rangeShape returns the columns and rows a cell range address spans,
// "Sheet1.A1:Sheet1.E1" or "$'My sheet'.$A$1:.$A$5" (ODF 1.3 Part 3,
// 18.3.6 cellRangeAddress), and whether it parsed.
func rangeShape(addr string) (int, int, bool) {
	from, to, ok := strings.Cut(addr, ":")
	if !ok {
		return 1, 1, cellRef(from) != nil
	}
	a, b := cellRef(from), cellRef(to)
	if a == nil || b == nil {
		return 0, 0, false
	}
	return abs(b[0]-a[0]) + 1, abs(b[1]-a[1]) + 1, true
}

// cellRef returns the column and row of a cell address's cell part, nil
// when it does not parse.
func cellRef(s string) []int {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.ReplaceAll(s, "$", "")
	col, i := 0, 0
	for ; i < len(s) && s[i] >= 'A' && s[i] <= 'Z'; i++ {
		col = col*26 + int(s[i]-'A'+1)
		if col > 1<<20 {
			return nil
		}
	}
	row, err := strconv.Atoi(s[i:])
	if i == 0 || err != nil || row < 1 {
		return nil
	}
	return []int{col - 1, row - 1}
}

// abs returns the magnitude of n.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// header returns the series name over column j.
func (t *localTable) header(j int) string {
	if t.headerRows == 0 || j < 0 || len(t.rows) == 0 || j >= len(t.rows[0]) {
		return ""
	}
	return t.rows[0][j].value.Text
}

// column returns the data rows of column j.
func (t *localTable) column(j int) chart.Data {
	d := chart.Data{Cached: true}
	if j < 0 {
		return d
	}
	for _, row := range t.rows[min(t.headerRows, len(t.rows)):] {
		if j < len(row) {
			d.Values = append(d.Values, row[j].value)
		} else {
			d.Values = append(d.Values, chart.Value{})
		}
	}
	return d
}

// categories returns the header column's labels, nil without one.
func (t *localTable) categories() chart.Data {
	if t.firstData == 0 {
		return chart.Data{}
	}
	d := t.column(0)
	for i, v := range d.Values {
		if v.Kind == chart.Number && v.Text != "" {
			d.Values[i] = chart.Str(v.Text)
		}
	}
	return d
}

// numberFormat translates the data style name (ODF 1.3 Part 3, 16.29)
// into the SpreadsheetML number format code the chart model carries,
// through the document's data styles; "" stands for General, as the
// chart model reads it, and for a name that resolves to no style. A style
// of another language than English (16.29.2 number:language and
// number:country) writes its numbers with that locale's separators, as
// Calc formats a chart's labels: each section of the code takes the
// locale's tag, [$-40E] for hu-HU.
func (r *chartReader) numberFormat(name string) string {
	code := r.formats.Code(ScopeContent, name)
	if code == "General" {
		return ""
	}
	el := r.formats.Element(ScopeContent, name)
	if el == nil || code == "" || strings.Contains(code, "[$") {
		return code
	}
	lang := el.A(NSNumber, "language")
	if c := el.A(NSNumber, "country"); c != "" {
		lang += "-" + c
	}
	id, ok := numfmt.LCIDOf(lang)
	if !ok {
		return code
	}
	tag := "[$-" + strings.ToUpper(strconv.FormatUint(id, 16)) + "]"
	return tag + strings.Join(formatSections(code), ";"+tag)
}

// formatSections splits a format code at the semicolons between its
// sections, those outside quotes, brackets and escapes (ECMA-376 Part 1,
// 18.8.31).
func formatSections(code string) []string {
	var out []string
	quoted, bracket, start := false, false, 0
	for i := 0; i < len(code); i++ {
		switch c := code[i]; {
		case c == '\\' && !quoted:
			i++
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '[':
			bracket = true
		case c == ']':
			bracket = false
		case c == ';' && !bracket:
			out = append(out, code[start:i])
			start = i + 1
		}
	}
	return append(out, code[start:])
}

// rangeName returns the name LibreOffice gives a series of the values
// range addr without a label: "Column B" or "Row 3", "" when addr does not
// parse.
func rangeName(addr string) string {
	from, _, _ := strings.Cut(addr, ":")
	cell := cellRef(from)
	cols, rows, ok := rangeShape(addr)
	if cell == nil || !ok {
		return ""
	}
	if rows == 1 && cols > 1 {
		return "Row " + strconv.Itoa(cell[1]+1)
	}
	name := ""
	for c := cell[0] + 1; c > 0; c = (c - 1) / 26 {
		name = string(rune('A'+(c-1)%26)) + name
	}
	return "Column " + name
}
