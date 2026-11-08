package xlsx

import (
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
