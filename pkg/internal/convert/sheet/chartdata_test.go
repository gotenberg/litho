package sheet

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/chart"
)

func TestChartValues_ReadsTheCellsRowByRow(t *testing.T) {
	sh := &Sheet{Rows: []Row{
		{Index: 1, Cells: []Cell{{Column: 1, Value: Value{Kind: Number, Number: 2.5}}}},
		{Index: 3, Cells: []Cell{{Column: 1, Value: Value{Kind: Text, Text: "x"}}, {Column: 2, Value: Value{Kind: Bool, Number: 1}}}},
	}}
	got := ChartValues(sh, Range{FirstRow: 1, FirstColumn: 1, LastRow: 3, LastColumn: 2})
	want := []chart.Value{chart.Num(2.5), {}, {}, {}, {Kind: chart.Text, Text: "x"}, chart.Num(1)}
	if len(got) != len(want) {
		t.Fatalf("got %d values, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("value %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestChartFormat checks that the source format of a chart range is its
// first cell's: the cell's style, else its row's, else its column's, else
// the default.
func TestChartFormat(t *testing.T) {
	def := &Style{Format: "0.0"}
	sh := &Sheet{
		Columns: []Column{{First: 3, Last: 3, Style: &Style{Format: "0%"}}},
		Rows: []Row{
			{Index: 0, Cells: []Cell{{Column: 1, Style: &Style{Format: "#,##0"}}}},
			{Index: 1, Style: &Style{Format: "0.00"}, Cells: []Cell{{Column: 1}}},
		},
	}
	for _, tc := range []struct {
		rg   Range
		want string
	}{
		{Range{FirstRow: 0, LastRow: 1, FirstColumn: 1, LastColumn: 1}, "#,##0"},
		{Range{FirstRow: 1, LastRow: 1, FirstColumn: 1, LastColumn: 1}, "0.00"},
		{Range{FirstRow: 5, LastRow: 6, FirstColumn: 3, LastColumn: 3}, "0%"},
		{Range{FirstRow: 5, LastRow: 6, FirstColumn: 2, LastColumn: 2}, "0.0"},
	} {
		if got := ChartFormat(sh, def, tc.rg); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.rg, got, tc.want)
		}
	}
}
