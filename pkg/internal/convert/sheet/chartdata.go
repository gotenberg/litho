package sheet

import "github.com/gotenberg/litho/pkg/internal/chart"

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
