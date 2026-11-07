package layout

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// The expected heights and widths of this file are Chromium's for the same
// markup.

func pctH(v float64) func(*css.Style) { return func(s *css.Style) { s.Height = pct(v) } }

func collapseVis(s *css.Style) { s.Visibility = css.VisibilityCollapse }

// rowHeightsOf returns the heights of the rows of the first table in root,
// in px.
func rowHeightsOf(t *testing.T, root *Node) []float64 {
	t.Helper()
	tm := tableOf(t, root, page(400, 1000))
	var out []float64
	for _, r := range tm.rows {
		out = append(out, pxOf(r.box.h))
	}
	return out
}

// cellOf returns the cell box of node n in tm.
func cellOf(t *testing.T, tm *tableData, n *Node) *box {
	t.Helper()
	for _, c := range tm.cells {
		if c.node == n {
			return c
		}
	}
	t.Fatal("no cell for the node")
	return nil
}

func TestTable_PercentHeights(t *testing.T) {
	cell := func(m ...func(*css.Style)) *Node {
		return td(mods(append([]func(*css.Style){height(18)}, m...)...), "")
	}
	nop := spacing(0, 0)
	tests := []struct {
		name string
		root *Node
		want string
	}{
		{"row percentage", doc(table(mods(nop, height(200)), tr(mods(pctH(0.25)), cell()), tr(nil, cell()))), "[50 150]"},
		{"cell percentage", doc(table(mods(nop, height(200)), tr(nil, td(mods(pctH(0.5)), "")), tr(nil, cell()))), "[100 100]"},
		{"auto table", doc(table(mods(nop), tr(mods(pctH(0.5)), cell()), tr(nil, cell()))), "[18 18]"},
		{"spacing and border", doc(table(mods(spacing(0, 10), border(5, css.Solid, black), height(190)), tr(mods(pctH(0.5)), cell()), tr(nil, cell()))), "[85 75]"},
		{"past 100%", doc(table(mods(nop, height(100)), tr(mods(pctH(0.8)), cell()), tr(mods(pctH(0.8)), cell()))), "[80 20]"},
		{"fixed rows share the excess", doc(table(mods(nop, height(300)), tr(nil, td(mods(height(30)), "")), tr(mods(height(50)), cell()))), "[112.5 187.5]"},
	}
	for _, tc := range tests {
		if got := fmt.Sprint(rowHeightsOf(t, tc.root)); got != tc.want {
			t.Errorf("%s: rows %s, want %s", tc.name, got, tc.want)
		}
	}
	// A table's percentage height resolves against its wrapper's
	// containing block.
	tm := tableOf(t, doc(el(sty(height(300)), table(mods(nop, pctH(0.5)), tr(nil, cell())))), page(400, 1000))
	if got := pxOf(tm.table.borderH()); got != 150 {
		t.Errorf("table height %v, want 150", got)
	}
}

func TestTable_VisibilityCollapse(t *testing.T) {
	cell := func() *Node { return td(mods(height(18), width(8)), "") }
	sp := spacing(10, 10)
	// A collapsed row takes no height and no spacing; a cell spanning into
	// it ends with the row above.
	span := spans(cell(), 1, 2)
	tm := tableOf(t, doc(table(mods(sp), tr(nil, span, cell()), tr(mods(collapseVis), cell()), tr(nil, cell(), cell()))), page(400, 1000))
	if h, c := pxOf(tm.table.borderH()), pxOf(cellOf(t, tm, span).borderH()); h != 66 || c != 18 {
		t.Errorf("table %v tall, spanning cell %v, want 66 and 18", h, c)
	}
	// A collapsed row group does the same.
	tm = tableOf(t, doc(table(mods(sp), tbody(nil, tr(nil, cell(), cell())), tbody(mods(collapseVis), tr(nil, cell())), tbody(nil, tr(nil, cell(), cell())))), page(400, 1000))
	if h := pxOf(tm.table.borderH()); h != 66 {
		t.Errorf("table %v tall, want 66", h)
	}
	// A collapsed column takes no width and no spacing; a cell spanning
	// into it ends with the column before.
	wide := spans(cell(), 2, 1)
	tm = tableOf(t, doc(table(mods(sp), col(nil), col(mods(collapseVis)), tr(nil, wide, cell()), tr(nil, cell(), td(mods(width(80)), ""), cell()))), page(400, 1000))
	if w, c := pxOf(tm.table.borderW()), pxOf(cellOf(t, tm, wide).borderW()); w != 46 || c != 8 {
		t.Errorf("table %v wide, spanning cell %v, want 46 and 8", w, c)
	}
}

func TestTable_ColumnMinWidth(t *testing.T) {
	// min-width on a column group and on a column floors its columns (WPT
	// css/CSS2/normal-flow/min-width-applies-to-005).
	mw := func(v float64) func(*css.Style) { return func(s *css.Style) { s.MinWidth = css.Pts(px(v)) } }
	for _, cols := range []*Node{colgroup(mods(mw(96))), col(mods(mw(96)))} {
		tm := tableOf(t, doc(table(mods(spacing(0, 0)), cols, tr(nil, td(mods(height(10)), "")))), page(400, 1000))
		if got := colWidths(tm); fmt.Sprint(got) != "[96]" {
			t.Errorf("columns %v, want [96]", got)
		}
	}
}

func TestTable_HeightWithoutRowGroups(t *testing.T) {
	// A table honours its height with or without a row group, as
	// Chromium sizes an empty table.
	for _, tc := range []struct {
		kids []*Node
		want float64
	}{{nil, 50}, {[]*Node{col(nil)}, 50}, {[]*Node{tbody(nil)}, 50}} {
		tm := tableOf(t, doc(table(mods(height(50), width(40)), tc.kids...)), page(400, 1000))
		if got := pxOf(tm.table.h); got != tc.want {
			t.Errorf("%d children: table %v tall, want %v", len(tc.kids), got, tc.want)
		}
	}
}

func TestTable_FlexItems(t *testing.T) {
	full := func(s *css.Style) { s.Width = pct(1) }
	// Fixed layout tables of 100% width share a row container equally:
	// their max-content width is as wide as a table gets, so the items
	// shrink alike from it.
	tbl := func() *Node { return el(sty(), table(mods(fixedLayout, full, height(10)))) }
	got := flexRects(t, doc(el(flexBox(width(90)), tbl(), tbl(), tbl())), 0, 0)
	want := [][4]float64{{0, 0, 30, 10}, {30, 0, 30, 10}, {60, 0, 30, 10}}
	if !eqRects(got, want) {
		t.Errorf("row items %v, want %v", got, want)
	}
	// A column item takes its flex basis, its table's height aside.
	col := el(flexBox(width(50), dir(css.FlexColumn)), table(mods(width(50), height(80), flexF(0, 0, 20))))
	if got := flexRects(t, doc(col), 0, 0); len(got) != 1 || got[0][3] != 20 {
		t.Errorf("column item %v, want 20 tall", got)
	}
}
