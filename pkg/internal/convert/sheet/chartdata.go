package sheet

import (
	"sort"

	"github.com/gotenberg/litho/pkg/internal/chart"
)

// ChartValues returns the values of the cells of sh in the range rg, row
// by row, as the points of a chart series whose file holds no cache: a
// number or a boolean as a number, a string or an error as its text, and
// an absent cell as an empty point. At most chart.MaxPoints values are
// returned.
func ChartValues(sh *Sheet, rg Range) []chart.Value {
	var out []chart.Value
	next := 0
	for ri := rg.FirstRow; ri <= rg.LastRow; ri++ {
		for next < len(sh.Rows) && sh.Rows[next].Index < ri {
			next++
		}
		var row *Row
		if next < len(sh.Rows) && sh.Rows[next].Index == ri {
			row = &sh.Rows[next]
		}
		for col := rg.FirstColumn; col <= rg.LastColumn; col++ {
			if len(out) >= chart.MaxPoints {
				return out
			}
			out = append(out, chartValue(rowCell(row, col)))
		}
	}
	return out
}

// ChartFormat returns the number format of the first cell of rg in sh,
// the format of the source data a chart's axis linked to its source
// shows (ECMA-376 Part 1, 21.2.2.121 numFmt, sourceLinked): the cell's
// own style's, else its row's, else its column's, else def's; "" for
// General.
func ChartFormat(sh *Sheet, def *Style, rg Range) string {
	st := def
	i := sort.Search(len(sh.Rows), func(i int) bool { return sh.Rows[i].Index >= rg.FirstRow })
	var row *Row
	if i < len(sh.Rows) && sh.Rows[i].Index == rg.FirstRow {
		row = &sh.Rows[i]
	}
	cols := sh.Columns
	j := sort.Search(len(cols), func(j int) bool { return cols[j].Last >= rg.FirstColumn })
	switch c := rowCell(row, rg.FirstColumn); {
	case c != nil && c.Style != nil:
		st = c.Style
	case row != nil && row.Style != nil:
		st = row.Style
	case j < len(cols) && cols[j].First <= rg.FirstColumn && cols[j].Style != nil:
		st = cols[j].Style
	}
	if st == nil {
		return ""
	}
	return st.Format
}

// rowCell returns the cell of row at column col, nil for none or a nil
// row.
func rowCell(row *Row, col int) *Cell {
	if row == nil {
		return nil
	}
	for i := range row.Cells {
		if row.Cells[i].Column == col {
			return &row.Cells[i]
		}
		if row.Cells[i].Column > col {
			break
		}
	}
	return nil
}

// chartValue turns a cell's value into a chart point.
func chartValue(c *Cell) chart.Value {
	if c == nil {
		return chart.Value{}
	}
	switch c.Value.Kind {
	case Number, Bool:
		return chart.Num(c.Value.Number)
	case Text, Error:
		return chart.Value{Kind: chart.Text, Text: c.Value.Text}
	case Empty:
	}
	return chart.Value{}
}
