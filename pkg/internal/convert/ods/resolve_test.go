package ods

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
)

func TestRangeValues_ReadsTheSheetsCells(t *testing.T) {
	r := &reader{doc: &sheet.Document{Sheets: []*sheet.Sheet{{Name: "My sheet", Rows: []sheet.Row{
		{Index: 1, Cells: []sheet.Cell{{Column: 1, Value: sheet.Value{Kind: sheet.Number, Number: 2.5}}}},
		{Index: 3, Cells: []sheet.Cell{{Column: 1, Value: sheet.Value{Kind: sheet.Text, Text: "x"}}, {Column: 2, Value: sheet.Value{Kind: sheet.Bool, Number: 1}}}},
	}}}}}
	got := r.rangeValues("$'My sheet'.$B$2:.$B$4")
	want := []chart.Value{chart.Num(2.5), {}, {Kind: chart.Text, Text: "x"}}
	if len(got) != len(want) {
		t.Fatalf("got %d values, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("value %d = %v, want %v", i, got[i], want[i])
		}
	}
	if r.rangeValues("Other.A1:Other.A2") != nil || r.rangeValues("nonsense") != nil {
		t.Error("an unknown sheet or a malformed address must resolve to nil")
	}
	if n := tableName("'It''s'.A1"); n != "It's" {
		t.Errorf("tableName = %q, want It's", n)
	}
}
