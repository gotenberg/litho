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
