package odf

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
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

// columnOf returns the column whose cells name the range addr, def when
// none does.
func (t *localTable) columnOf(addr string, def int) int {
	if addr == "" {
		return def
	}
	for _, row := range t.rows[min(t.headerRows, len(t.rows)):] {
		for j, c := range row {
			if c.desc == addr {
				return j
			}
		}
	}
	return def
}

// headerOf returns the header cell that names the range addr.
func (t *localTable) headerOf(addr string) (string, bool) {
	for _, row := range t.rows[:min(t.headerRows, len(t.rows))] {
		for _, c := range row {
			if c.desc == addr {
				return c.value.Text, true
			}
		}
	}
	return "", false
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

// maxStyleParts caps the parts of a number style translated. Anti-DoS
// guard, not spec-mandated: a date style has a dozen parts.
const maxStyleParts = 64

// numberFormat translates the data style name (ODF 1.3 Part 3, 16.29) into
// the SpreadsheetML number format code the chart model carries: digits
// with decimals and grouping, scientific, percentage, currency, literal
// text, dates and times, and a negative section from a style:map. ""
// stands for General.
func (r *chartReader) numberFormat(name string) string {
	return r.formatCode(name, 0)
}

// formatCode translates one data style, following its maps depth deep.
func (r *chartReader) formatCode(name string, depth int) string {
	st := r.numbers[name]
	if st == nil || depth > 2 {
		return ""
	}
	if parts := elementKids(st); len(parts) == 1 && parts[0].Is(NSNumber, "number") && st.Name.Local == "number-style" {
		if _, ok := parts[0].Lookup(NSNumber, "decimal-places"); !ok {
			// A lone number without decimal places is LibreOffice's
			// standard format, General.
			return ""
		}
	}
	var b strings.Builder
	for i, part := range st.Children {
		if i >= maxStyleParts {
			break
		}
		if part.IsText() {
			continue
		}
		switch part.Name.Local {
		case "number":
			b.WriteString(digits(part))
		case "scientific-number":
			b.WriteString(digits(part))
			exp := 2
			if v, err := strconv.Atoi(part.A(NSNumber, "min-exponent-digits")); err == nil && v > 0 && v < 10 {
				exp = v
			}
			b.WriteString("E+" + strings.Repeat("0", exp))
		case "fraction":
			b.WriteString("# ?/?")
		case "text":
			if t := part.TextContent(); t != "" {
				b.WriteString(`"` + strings.ReplaceAll(t, `"`, ``) + `"`)
			}
		case "currency-symbol":
			if t := part.TextContent(); t != "" {
				b.WriteString(`"` + strings.ReplaceAll(t, `"`, ``) + `"`)
			}
		case "year":
			b.WriteString(pick(part, "yyyy", "yy"))
		case "month":
			switch {
			case part.A(NSNumber, "textual") == "true":
				b.WriteString(pick(part, "mmmm", "mmm"))
			default:
				b.WriteString(pick(part, "mm", "m"))
			}
		case "day":
			b.WriteString(pick(part, "dd", "d"))
		case "day-of-week":
			b.WriteString(pick(part, "dddd", "ddd"))
		case "hours":
			b.WriteString(pick(part, "hh", "h"))
		case "minutes":
			b.WriteString(pick(part, "mm", "m"))
		case "seconds":
			b.WriteString(pick(part, "ss", "s"))
		case "am-pm":
			b.WriteString("AM/PM")
		}
	}
	code := b.String()
	if st.Name.Local == "percentage-style" && !strings.Contains(code, "%") {
		code += "%"
	}
	for _, m := range st.All(NSStyle, "map") {
		cond := strings.ReplaceAll(m.A(NSStyle, "condition"), " ", "")
		other := r.formatCode(m.A(NSStyle, "apply-style-name"), depth+1)
		if other == "" {
			continue
		}
		if strings.HasPrefix(cond, "value()>=0") || strings.HasPrefix(cond, "value()>0") {
			return other + ";" + code
		}
	}
	return code
}

// pick returns long for number:style="long", else short.
func pick(e *Element, long, short string) string {
	if e.A(NSNumber, "style") == "long" {
		return long
	}
	return short
}

// digits translates a number:number element's digit pattern.
func digits(e *Element) string {
	minInt := 1
	if v, err := strconv.Atoi(e.A(NSNumber, "min-integer-digits")); err == nil && v >= 0 && v < 20 {
		minInt = v
	}
	dec := 0
	if v, err := strconv.Atoi(e.A(NSNumber, "decimal-places")); err == nil && v >= 0 && v < 20 {
		dec = v
	}
	if _, ok := e.Lookup(NSNumber, "decimal-places"); !ok {
		// No decimal places given: as many as the value needs.
		return intPartOf(e, minInt) + ".##########"
	}
	minDec := dec
	if v, err := strconv.Atoi(e.A(NSNumber, "min-decimal-places")); err == nil && v >= 0 && v <= dec {
		minDec = v
	}
	intPart := intPartOf(e, minInt)
	if dec == 0 {
		return intPart
	}
	return intPart + "." + strings.Repeat("0", minDec) + strings.Repeat("#", dec-minDec)
}

// intPartOf returns the integer digit pattern of a number:number element.
func intPartOf(e *Element, minInt int) string {
	intPart := strings.Repeat("0", minInt)
	if intPart == "" {
		intPart = "#"
	}
	if e.A(NSNumber, "grouping") == "true" {
		intPart = "#,##" + intPart
		if minInt == 0 {
			intPart = "#,###"
		}
	}
	return intPart
}

// elementKids returns e's child elements, its text nodes left out.
func elementKids(e *Element) []*Element {
	var out []*Element
	for _, c := range kids(e) {
		if !c.IsText() {
			out = append(out, c)
		}
	}
	return out
}
