package xlsx

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
)

func TestRangeValues_ReadsTheNamedSheetsCells(t *testing.T) {
	r := &reader{doc: &sheet.Document{Sheets: []*sheet.Sheet{{Name: "My sheet", Rows: []sheet.Row{
		{Index: 1, Cells: []sheet.Cell{{Column: 1, Value: sheet.Value{Kind: sheet.Number, Number: 2.5}}}},
		{Index: 2, Cells: []sheet.Cell{{Column: 1, Value: sheet.Value{Kind: sheet.Text, Text: "x"}}}},
	}}}}}
	got := r.rangeValues("'My sheet'!$B$2:$B$3")
	want := []chart.Value{chart.Num(2.5), {Kind: chart.Text, Text: "x"}}
	if len(got) != len(want) {
		t.Fatalf("got %d values, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("value %d = %v, want %v", i, got[i], want[i])
		}
	}
	if r.rangeValues("Other!$A$1:$A$2") != nil || r.rangeValues("$A$1") != nil {
		t.Error("an unknown or unqualified sheet must resolve to nil")
	}
}

// TestCellValues_ReadsCellsAsExcelDraws locks that a chart's range reads
// the workbook's cells, hidden rows and columns left out only for a chart
// that plots visible cells, and that a range of several rows and columns,
// a category range of several levels, reads none.
func TestCellValues_ReadsCellsAsExcelDraws(t *testing.T) {
	num := func(col int, v float64) sheet.Cell {
		return sheet.Cell{Column: col, Value: sheet.Value{Kind: sheet.Number, Number: v}}
	}
	r := &reader{doc: &sheet.Document{Sheets: []*sheet.Sheet{{
		Name:    "S",
		Columns: []sheet.Column{{First: 1, Last: 1, Hidden: true}},
		Rows: []sheet.Row{
			{Index: 0, Cells: []sheet.Cell{num(0, 1), num(1, 10), num(2, 100)}},
			{Index: 1, Hidden: true, Cells: []sheet.Cell{num(0, 2)}},
			{Index: 2, Cells: []sheet.Cell{num(0, 3)}},
		},
	}}}}
	numbers := func(vs []chart.Value) []float64 {
		var out []float64
		for _, v := range vs {
			out = append(out, v.Number)
		}
		return out
	}
	for _, tc := range []struct {
		ref     string
		visible bool
		want    []float64
	}{
		{"S!$A$1:$A$3", false, []float64{1, 2, 3}},
		{"S!$A$1:$A$3", true, []float64{1, 3}},
		{"S!$A$1:$C$1", false, []float64{1, 10, 100}},
		{"S!$A$1:$C$1", true, []float64{1, 100}},
	} {
		if got := numbers(r.cellValues(tc.ref, tc.visible)); !slices.Equal(got, tc.want) {
			t.Errorf("%s, visible only %v: %v, want %v", tc.ref, tc.visible, got, tc.want)
		}
	}
	if got := r.cellValues("S!$A$2:$A$2", true); got == nil || len(got) != 0 {
		t.Errorf("a hidden row alone gives %v, want no point", got)
	}
	for _, ref := range []string{"S!$A$1:$B$2", "Other!$A$1:$A$2", "$A$1"} {
		if got := r.cellValues(ref, false); got != nil {
			t.Errorf("%s gives %v, want nil", ref, got)
		}
	}
}
