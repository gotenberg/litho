package layout

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// tableOf returns the table data of the first table in the body of root,
// laid out on page pb.
func tableOf(t *testing.T, root *Node, pb PageBox) *tableData {
	t.Helper()
	rb := boxes(t, root, pb)
	var found *tableData
	var walk func(*box)
	walk = func(b *box) {
		if found != nil {
			return
		}
		if b.tdata != nil {
			found = b.tdata
			return
		}
		for _, k := range b.kids {
			walk(k)
		}
	}
	walk(rb)
	if found == nil {
		t.Fatal("no table")
	}
	return found
}

// colWidths returns the column widths of tm in px, rounded to a
// thousandth.
func colWidths(tm *tableData) []float64 {
	hs, _ := tm.spacing()
	out := make([]float64, tm.nCols)
	for i := range out {
		out[i] = math.Round((tm.colX[i+1]-tm.colX[i]-hs)/css.Pt*1000) / 1000
	}
	return out
}

func pxOf(v float64) float64 { return math.Round(v/css.Pt*1000) / 1000 }

func TestTable_ColumnWidths(t *testing.T) {
	w := func(v float64) []func(*css.Style) { return mods(width(v)) }
	tests := []struct {
		name  string
		table *Node
		want  []float64
		total float64
	}{
		{"all auto at max-content", table(nil, tr(nil, td(nil, "aa"), td(nil, "aaaa"))), []float64{20, 40}, 60},
		{"all auto between min and max", table(w(50), tr(nil, td(nil, "aa aa"), td(nil, "a a"))), []float64{32, 18}, 50},
		{"fixed and auto", table(w(100), tr(nil, td(w(30), "a"), td(nil, "aaaa"))), []float64{30, 70}, 100},
		{"percentage", table(w(100), tr(nil, td(mods(func(s *css.Style) { s.Width = pct(0.25) }), "a"), td(nil, "a"))), []float64{25, 75}, 100},
		{"a cell wider than the table", table(w(30), tr(nil, td(nil, "aaaaa"))), []float64{50}, 50},
		{"spanning cell", table(nil,
			tr(nil, spans(td(nil, "aaaaaaaaaa"), 2, 1)),
			tr(nil, td(nil, "a"), td(nil, "aaa"))), []float64{25, 75}, 100},
		{"empty column", table(w(100), col(nil), col(nil), tr(nil, td(nil, "a"))), []float64{100, 0}, 100},
		{"fixed layout", table(mods(width(100), fixedLayout), col(w(20)), tr(nil, td(nil, "aaaaaaaa"), td(nil, ""), td(nil, ""))), []float64{20, 40, 40}, 100},
		{"fixed layout first row", table(mods(width(100), fixedLayout), tr(nil, td(nil, ""), td(w(10), ""))), []float64{90, 10}, 100},
		{"max-width", table(mods(func(s *css.Style) { s.MaxWidth = css.Pts(px(30)) }), tr(nil, td(nil, "a a a a"))), []float64{30}, 30},
		{"min-width", table(mods(func(s *css.Style) { s.MinWidth = css.Pts(px(80)) }), tr(nil, td(nil, "a"))), []float64{80}, 80},
		{"border-box", table(mods(width(50), padding(5), func(s *css.Style) { s.BoxSizing = css.BorderBox }), tr(nil, td(nil, "a"))), []float64{40}, 50},
		{"percentage table", table(mods(func(s *css.Style) { s.Width = pct(0.5) }), tr(nil, td(nil, "a"))), []float64{100}, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := tableOf(t, doc(tt.table), page(200, 100))
			got := colWidths(tm)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("columns %v, want %v", got, tt.want)
			}
			if w := pxOf(tm.table.borderW()); w != tt.total {
				t.Errorf("table width %v, want %v", w, tt.total)
			}
		})
	}
}

func TestTable_ShrinkToFitWidth(t *testing.T) {
	tb := table(nil, tr(nil, td(nil, "aa aa aa")))
	for name, root := range map[string]*Node{
		"float":        doc(el(sty(), table(mods(float(css.FloatLeft)), tr(nil, td(nil, "aa aa aa"))))),
		"inline-table": doc(el(sty(), table(mods(display(css.InlineTable)), tr(nil, td(nil, "aa aa aa"))))),
		"block":        doc(tb),
	} {
		tm := tableOf(t, root, page(50, 100))
		if w := pxOf(tm.table.borderW()); w != 50 {
			t.Errorf("%s: width %v, want the 50px available", name, w)
		}
	}
	tm := tableOf(t, doc(el(sty(float(css.FloatLeft)), table(nil, tr(nil, td(nil, "aa aa"))))), page(200, 100))
	if w := pxOf(tm.table.borderW()); w != 50 {
		t.Errorf("in a float: width %v, want max-content 50", w)
	}
}

func TestTable_AutoMarginsCentre(t *testing.T) {
	tm := tableOf(t, doc(table(mods(width(40), margin(0, auto)), tr(nil, td(nil, "a")))), page(100, 100))
	if x := pxOf(tm.wrapper.x); x != 30 {
		t.Errorf("x %v, want 30", x)
	}
}

func TestTable_Spacing(t *testing.T) {
	cell := mods(width(10), height(10))
	tm := tableOf(t, doc(table(mods(spacing(2, 3), border(1, css.Solid, black)),
		tr(nil, td(cell, ""), td(cell, "")), tr(nil, td(cell, ""), td(cell, "")))), page(100, 100))
	tb := tm.table
	if got := rect(tb); got != [4]float64{0, 0, 28, 31} {
		t.Errorf("table %v, want 28 by 31: spacing around every cell inside the border", got)
	}
	if got := rect(tm.cells[3]); got != [4]float64{15, 17, 10, 10} {
		t.Errorf("last cell %v, want at 15, 17", got)
	}
	if tm.collapse {
		t.Fatal("collapsing")
	}
	tm = tableOf(t, doc(table(mods(spacing(2, 3), collapse), tr(nil, td(cell, "")))), page(100, 100))
	if got := rect(tm.table); got != [4]float64{0, 0, 10, 10} {
		t.Errorf("collapsed %v, want no spacing", got)
	}
}

func TestTable_RowHeightsAndBaseline(t *testing.T) {
	big := mods(font(20))
	root := doc(table(nil,
		tr(nil, td(nil, "a"), td(big, "a"), td(mods(valign(css.Middle)), "a"), td(mods(valign(css.AlignBottom)), "a"), td(mods(valign(css.AlignTop)), "a"))))
	deep(root.Children[0].Children[0].Children[0].Children[1], font(20))
	tm := tableOf(t, root, page(200, 100))
	row := tm.rows[0].box
	if h := pxOf(row.h); h != 20 {
		t.Errorf("row height %v, want 20: the baselines at 16 and the large cell's descent below", h)
	}
	tops := []float64{}
	for _, c := range tm.cells {
		tops = append(tops, pxOf(c.lines[0].y))
	}
	if fmt.Sprint(tops) != "[8 0 5 10 0]" {
		t.Errorf("line tops %v, want [8 0 5 10 0]: baseline, baseline, middle, bottom, top", tops)
	}
}

func TestTable_RowSpanAndTableHeight(t *testing.T) {
	cell := mods(height(10))
	tm := tableOf(t, doc(table(nil,
		tr(nil, spans(td(mods(height(50)), ""), 1, 2), td(cell, "")),
		tr(nil, td(cell, "")))), page(100, 100))
	if a, b := pxOf(tm.rows[0].box.h), pxOf(tm.rows[1].box.h); a != 25 || b != 25 {
		t.Errorf("rows %v and %v, want 25 each: the spanning cell's excess spread", a, b)
	}
	tm = tableOf(t, doc(table(mods(height(100)), tr(nil, td(cell, "")), tr(nil, td(cell, "")))), page(100, 200))
	if a, h := pxOf(tm.rows[0].box.h), pxOf(tm.table.borderH()); a != 50 || h != 100 {
		t.Errorf("row %v table %v, want 50 and 100", a, h)
	}
	// A row span stops at the end of its row group.
	tm = tableOf(t, doc(table(nil, tbody(nil, tr(nil, spans(td(nil, "a"), 1, 5))), tbody(nil, tr(nil, td(nil, "a"))))), page(100, 100))
	if rs := tm.cells[0].cell.rowSpan; rs != 1 {
		t.Errorf("row span %d, want 1", rs)
	}
}

func TestTable_CollapseConflicts(t *testing.T) {
	b := func(w float64, bs css.BorderStyle) css.Border {
		return css.Border{Width: px(w), Style: bs, Color: black}
	}
	tests := []struct {
		name      string
		cell, row css.Border
		want      css.BorderStyle
		width     float64
	}{
		{"wider wins", b(1, css.Solid), b(2, css.Dashed), css.Dashed, 2},
		{"style breaks a tie", b(2, css.Dotted), b(2, css.Double), css.Double, 2},
		{"cell over row", b(2, css.Solid), b(2, css.Solid), css.Solid, 2},
		{"hidden suppresses", b(5, css.Solid), css.Border{Style: css.BorderHidden}, css.BorderNone, 0},
		{"none loses", css.Border{}, b(1, css.Inset), css.Inset, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cell := td(nil, "a")
			cell.Style.Border[css.Bottom] = tt.cell
			row := tr(nil, cell)
			row.Style.Border[css.Bottom] = tt.row
			tm := tableOf(t, doc(table(mods(collapse), row, tr(nil, td(nil, "a")))), page(100, 100))
			got := tm.hEdge(0, 1, 0)
			if got.style != tt.want || pxOf(got.w) != tt.width {
				t.Errorf("border %v %v px, want %v %v", got.style, pxOf(got.w), tt.want, tt.width)
			}
			if tt.name == "cell over row" && got.origin != originCell {
				t.Errorf("origin %d, want the cell's", got.origin)
			}
		})
	}
}

func TestTable_CollapseHalves(t *testing.T) {
	cell := mods(border(2, css.Solid, black), width(10), height(10))
	tm := tableOf(t, doc(table(mods(collapse, border(6, css.Solid, red)), tr(nil, td(cell, ""), td(cell, "")))), page(100, 100))
	c := tm.cells[0]
	if got := [4]float64{pxOf(c.bd[0]), pxOf(c.bd[1]), pxOf(c.bd[2]), pxOf(c.bd[3])}; got != [4]float64{3, 1, 3, 3} {
		t.Errorf("cell borders %v, want halves 3 1 3 3", got)
	}
	if got := rect(tm.table); got != [4]float64{0, 0, 6 + 10 + 2 + 10 + 6, 6 + 10 + 6} {
		t.Errorf("table %v, want the borders centred on the grid lines", got)
	}
}

func TestTable_Captions(t *testing.T) {
	root := doc(table(nil,
		caption(mods(func(s *css.Style) { s.CaptionSide = css.CaptionBottom }), txt(sty(), "bb")),
		caption(nil, txt(sty(), "aaaaa")),
		tr(nil, td(nil, "a"))))
	tm := tableOf(t, root, page(200, 100))
	w := tm.wrapper
	if got := pxOf(w.w); got != 50 {
		t.Errorf("wrapper width %v, want 50: the caption's min-content width", got)
	}
	if len(w.kids) != 3 || w.kids[1] != tm.table {
		t.Fatalf("wrapper kids %d, want top caption, table, bottom caption", len(w.kids))
	}
	if got := [3]float64{pxOf(w.kids[0].y), pxOf(tm.table.y), pxOf(w.kids[2].y)}; got != [3]float64{0, 10, 20} {
		t.Errorf("tops %v, want 0 10 20", got)
	}
	if got := pxOf(w.kids[2].borderW()); got != 50 {
		t.Errorf("bottom caption width %v, want the wrapper's 50", got)
	}
}

func TestTable_AnonymousObjects(t *testing.T) {
	cell := func(s string) *Node { return td(nil, s) }
	tests := []struct {
		name  string
		root  *Node
		check func(t *testing.T, rb *box)
	}{
		{"cells in a table get a row and a group", doc(table(nil, cell("a"), cell("b"))), func(t *testing.T, rb *box) {
			w := kid(t, rb, 0, 0)
			tm := w.tdata
			if len(tm.rows) != 1 || tm.nCols != 2 || !tm.groups[0].anon || !tm.rows[0].box.anon {
				t.Errorf("rows %d cols %d, want one anonymous row of two cells", len(tm.rows), tm.nCols)
			}
		}},
		{"text in a row gets a cell", doc(table(nil, tr(nil, txt(sty(), "loose"), cell("b")))), func(t *testing.T, rb *box) {
			tm := kid(t, rb, 0, 0).tdata
			if tm.nCols != 2 || !tm.cells[0].anon {
				t.Errorf("cols %d, want the text in an anonymous cell", tm.nCols)
			}
		}},
		{"white space between rows dropped", doc(table(nil, txt(sty(), "\n  "), tr(nil, cell("a")), txt(sty(), " "), tr(nil, cell("b")))), func(t *testing.T, rb *box) {
			tm := kid(t, rb, 0, 0).tdata
			if len(tm.rows) != 2 || tm.nCols != 1 {
				t.Errorf("rows %d cols %d, want 2 by 1", len(tm.rows), tm.nCols)
			}
		}},
		{"a cell in a block gets a table", doc(el(sty(), txt(sty(), "x"), cell("a"), txt(sty(), " "), cell("b"))), func(t *testing.T, rb *box) {
			p := kid(t, rb, 0, 0)
			if len(p.kids) != 2 || p.kids[1].tpart != partWrapper || !p.kids[1].anon || p.kids[1].tdata.nCols != 2 {
				t.Errorf("kids %d, want the text's anonymous block and an anonymous table of two cells", len(p.kids))
			}
		}},
		{"a cell in an inline gets an inline table", doc(el(sty(), el(inlineSty(), txt(inlineSty(), "x"), cell("a")))), func(t *testing.T, rb *box) {
			p := kid(t, rb, 0, 0)
			span := kid(t, p, 0)
			if len(span.kids) != 2 || span.kids[1].tpart != partWrapper || !span.kids[1].inline {
				t.Errorf("span kids %d, want the text and an inline table", len(span.kids))
			}
		}},
		{"a row in a cell gets a table", doc(table(nil, tr(nil, td(nil, ""))), table(nil, tr(nil, part(css.TableCell, nil, tr(nil, cell("x")))))), func(t *testing.T, rb *box) {
			w := kid(t, rb, 0, 1)
			c := w.tdata.cells[0]
			if len(c.kids) != 1 || c.kids[0].tpart != partWrapper {
				t.Errorf("cell kids %d, want an anonymous table", len(c.kids))
			}
		}},
		{"header and footer move", doc(table(nil, tfoot(nil, tr(nil, cell("f"))), tbody(nil, tr(nil, cell("b"))), thead(nil, tr(nil, cell("h"))))), func(t *testing.T, rb *box) {
			tm := kid(t, rb, 0, 0).tdata
			if tm.headN != 1 || tm.footN != 1 || tm.groups[0].st.Display != css.TableHeaderGroup || tm.groups[2].st.Display != css.TableFooterGroup {
				t.Errorf("head %d foot %d, want the header first and the footer last", tm.headN, tm.footN)
			}
		}},
		{"columns and spans", doc(table(nil, colgroup(nil, col(nil), col(nil)), func() *Node { c := col(nil); c.Span = 3; return c }(), tr(nil, cell("a")))), func(t *testing.T, rb *box) {
			tm := kid(t, rb, 0, 0).tdata
			if tm.nCols != 5 || tm.cols[1].group == nil || tm.cols[4].col == nil {
				t.Errorf("cols %d, want 5 from the column elements", tm.nCols)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, boxes(t, tt.root, page(200, 100)))
		})
	}
}

func TestTable_SpanCaps(t *testing.T) {
	c := td(nil, "a")
	c.ColSpan, c.RowSpan = 1<<30, -5
	tm := tableOf(t, doc(table(nil, tr(nil, c))), page(100, 100))
	if tm.nCols != maxTableColumns || tm.cells[0].cell.rowSpan != 1 {
		t.Errorf("cols %d row span %d, want %d and 1", tm.nCols, tm.cells[0].cell.rowSpan, maxTableColumns)
	}
	row := tr(nil)
	for range maxTableColumns + 5 {
		row.Children = append(row.Children, td(nil, ""))
	}
	res := run(t, doc(table(nil, row)), page(100, 100))
	found := false
	for _, w := range res.Warnings {
		found = found || w.Code == codeTableTooLarge
	}
	if !found {
		t.Error("no warning for the cells past the column cap")
	}
}

// pagedTable returns a table of a header row and n body rows, each of
// height h px, a footer when foot is set.
func pagedTable(n int, h float64, foot bool, m ...func(*css.Style)) *Node {
	cell := mods(height(h))
	tb := table(mods(m...), thead(nil, tr(nil, td(cell, "H"))))
	body := tbody(nil)
	for i := range n {
		body.Children = append(body.Children, tr(nil, td(cell, fmt.Sprint(i))))
	}
	tb.Children = append(tb.Children, body)
	if foot {
		tb.Children = append(tb.Children, tfoot(nil, tr(nil, td(cell, "F"))))
	}
	return tb
}

func TestTable_HeaderRepeats(t *testing.T) {
	tm := tableOf(t, doc(pagedTable(10, 10, false)), page(20, 50))
	if len(tm.seqs) != 3 {
		t.Fatalf("%d runs, want 3 pages", len(tm.seqs))
	}
	var tops []float64
	for _, s := range tm.seqs {
		tops = append(tops, pxOf(tm.top(s[0])), float64(len(s)))
	}
	if fmt.Sprint(tops) != "[0 5 50 5 100 3]" {
		t.Errorf("first rows and row counts per page %v, want a header and 4 rows, then a copy and 4, then a copy and 2", tops)
	}
	if got := pxOf(tm.rows[5].box.y); got != 60 {
		t.Errorf("fifth body row at %v, want 60: below the copy of the header", got)
	}
	res := run(t, doc(pagedTable(10, 10, false)), page(20, 50))
	if len(res.Pages) != 3 {
		t.Fatalf("%d pages, want 3", len(res.Pages))
	}
	for p := 1; p <= 3; p++ {
		ts := texts(res, p)
		if len(ts) == 0 || ts[0].text != "H" || ts[0].y > px(10) {
			t.Fatalf("page %d starts with %v, want the header", p, ts)
		}
		if p > 1 && ts[0].run.Parent != nil {
			t.Errorf("page %d: the header copy hangs from %v, want an artifact", p, ts[0].run.Parent.Role)
		}
	}
	if ts := texts(res, 1); ts[0].run.Parent == nil {
		t.Error("page 1: the header is an artifact, want it tagged")
	}
}

func TestTable_FooterRepeats(t *testing.T) {
	res := run(t, doc(pagedTable(6, 10, true)), page(20, 50))
	var got []string
	for p := range res.Pages {
		var s []string
		for _, x := range texts(res, p+1) {
			s = append(s, x.text)
		}
		got = append(got, strings.Join(s, ""))
	}
	if fmt.Sprint(got) != "[H012F H345F]" {
		t.Errorf("pages %q, want the header and footer on each", got)
	}
}

func TestTable_RowsNotSplit(t *testing.T) {
	tm := tableOf(t, doc(el(sty(height(15))), table(nil, tr(nil, td(mods(height(20)), "a")), tr(nil, td(mods(height(20)), "b")))), page(20, 50))
	if a, b := pxOf(tm.rows[0].box.y), pxOf(tm.rows[1].box.y); a != 15 || b != 50 {
		t.Errorf("rows at %v and %v, want 15 and 50: the second moves whole", a, b)
	}
	// The table moves whole when its first row does not fit.
	tm = tableOf(t, doc(el(sty(height(40))), table(nil, tr(nil, td(mods(height(20)), "a")))), page(20, 50))
	if a := pxOf(tm.table.y); a != 50 {
		t.Errorf("table at %v, want 50", a)
	}
}

func TestTable_TallRowClipped(t *testing.T) {
	root := doc(table(nil, tr(nil, td(mods(height(80)), "a")), tr(nil, td(mods(height(10)), "b"))))
	res := run(t, root, page(20, 50))
	found := false
	for _, w := range res.Warnings {
		found = found || w.Code == codeClipped
	}
	if !found {
		t.Error("no warning for the clipped row")
	}
	tm := tableOf(t, root, page(20, 50))
	if got := pxOf(tm.rows[1].box.y); got != 50 {
		t.Errorf("next row at %v, want 50: the page after the one the tall row is clipped to", got)
	}
}

func TestTable_BreakProperties(t *testing.T) {
	forced := tr(nil, td(nil, "b"))
	forced.Style.BreakBefore = css.Page
	tm := tableOf(t, doc(table(nil, tr(nil, td(nil, "a")), forced)), page(20, 50))
	if got := pxOf(tm.rows[1].box.y); got != 50 {
		t.Errorf("forced row at %v, want 50", got)
	}
	keep := tr(nil, td(mods(height(20)), "b"))
	keep.Style.BreakBefore = css.BreakAvoid
	cell := mods(height(20))
	tm = tableOf(t, doc(table(nil, tr(nil, td(cell, "a")), tr(nil, td(cell, "a")), keep)), page(20, 50))
	if a, b := pxOf(tm.rows[1].box.y), pxOf(tm.rows[2].box.y); a != 50 || b != 70 {
		t.Errorf("rows at %v and %v, want 50 and 70: kept together", a, b)
	}
}

func TestTable_MaxPages(t *testing.T) {
	_, err := Layout(context.Background(), doc(pagedTable(100, 10, true)), ahem(t), Options{Page: page(20, 50), MaxPages: 3})
	if !errors.Is(err, ErrTooManyPages) {
		t.Errorf("error %v, want ErrTooManyPages", err)
	}
}

func TestTable_Structure(t *testing.T) {
	cell := td(nil, "a")
	cell.Role = "TD"
	row := tr(nil, cell)
	row.Role = "TR"
	tb := table(nil, row, tr(nil, td(nil, "b")))
	tb.Role = "Table"
	tb.ID = "t"
	res := run(t, doc(tb), page(100, 100))
	tbl := res.Structure.Kids[0]
	if tbl.Role != "Table" || len(tbl.Kids) != 1 || tbl.Kids[0].Role != "TR" || tbl.Kids[0].Kids[0].Role != "TD" {
		t.Errorf("structure %+v, want Table > TR > TD, the anonymous boxes carrying none", tbl)
	}
	if _, ok := res.Anchors["t"]; !ok {
		t.Error("no anchor for the table's ID")
	}
	linked := td(nil, "a")
	linked.Href = "#c"
	target := td(nil, "b")
	target.ID = "c"
	res = run(t, doc(pagedTable(0, 10, false), table(nil, tr(nil, linked, target))), page(100, 100))
	if a, ok := res.Anchors["c"]; !ok || a.X != px(10) {
		t.Errorf("anchor %+v, want the second cell at 10px", a)
	}
	if len(res.Pages[0].Links) != 1 || res.Pages[0].Links[0].Dest == nil {
		t.Errorf("links %+v, want the cell's link to the other cell", res.Pages[0].Links)
	}
}
