package chart

import (
	"testing"
)

// TestLive_ReadsCellsOverTheCache locks that a chart with [Chart.Cells]
// draws the cells' values in place of its cache, keeps the cache of a
// range the cells do not give and of a pivot chart, and leaves the chart
// it is given unchanged.
func TestLive_ReadsCellsOverTheCache(t *testing.T) {
	cells := func(ref string) []Value {
		if ref == "S!$A$1:$A$2" {
			return []Value{Num(2), Num(1)}
		}
		return nil
	}
	cached := Data{Ref: "S!$A$1:$A$2", Cached: true, Values: []Value{Num(9)}}
	other := Data{Ref: "X!$A$1", Cached: true, Values: []Value{Num(7)}}
	s := &Series{Values: cached, Categories: other}
	c := &Chart{Cells: cells, Groups: []*Group{{Kind: Bar, Series: []*Series{s}}}}
	got := live(c).Groups[0].Series[0]
	if v := got.Values.Values; len(v) != 2 || v[0].Number != 2 || v[1].Number != 1 {
		t.Errorf("values %v, want the cells' 2 and 1", v)
	}
	if v := got.Categories.Values; len(v) != 1 || v[0].Number != 7 {
		t.Errorf("categories %v, want the cache kept", v)
	}
	if len(s.Values.Values) != 1 {
		t.Errorf("the chart's own series was changed: %v", s.Values.Values)
	}
	c.Pivot = true
	if v := live(c).Groups[0].Series[0].Values.Values; len(v) != 1 || v[0].Number != 9 {
		t.Errorf("pivot chart values %v, want the cache", v)
	}
}

// TestLabelsAt_SeriesDeletesGroupLabels locks that a series that deletes
// its labels shows none of its group's, and one that sets none shows
// them.
func TestLabelsAt_SeriesDeletesGroupLabels(t *testing.T) {
	off, plain := &Series{Labels: Labels{Deleted: true}}, &Series{}
	g := &Group{Kind: Bar, Labels: Labels{ShowValue: true}, Series: []*Series{off, plain}}
	rd := &renderer{c: &Chart{Groups: []*Group{g}}}
	if rd.labelsAt(g, off, 0).Any() {
		t.Error("a series that deletes its labels shows its group's")
	}
	if !rd.labelsAt(g, plain, 0).Any() {
		t.Error("a series without labels of its own does not show its group's")
	}
}
