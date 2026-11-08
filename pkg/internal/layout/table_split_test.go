package layout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// The tests of this file cover rows split across pages (CSS Fragmentation
// 3, 4.4), the first ones WeasyPrint's draw tests for split rows
// (tests/draw/test_table.py, release 70.0) rebuilt with Ahem.

func TestWeasyPrint_TablesSplitRow(t *testing.T) {
	cell := mods(border(1, css.Solid, black), fg(red), valign(css.AlignTop))
	two := td(cell, "a")
	two.Children = append(two.Children, br(sty()), txt(sty(fg(red)), "a"))
	root := doc(deep(table(nil, tr(nil, td(cell, "a"), two)), font(2)))
	assertPages(t, run(t, root, page(8, 4)), 4, `
		KKKKKKKK
		KRRKKRRK
		KRRKKRRK
		K__KK__K
		K__KKRRK
		K__KKRRK
		KKKKKKKK
		________`, wpLetters)
}

func TestWeasyPrint_Tables17(t *testing.T) {
	cell := mods(border(2, css.Solid, red), padding(1))
	root := doc(deep(table(mods(collapse), tr(nil, td(cell, "a a a a"), td(cell, "a"))), font(2), fg(blue)))
	assertPages(t, run(t, root, wpPage(16, 10, 1)), 10, `
		________________
		_RRRRRRRRRRRRRR_
		_RRRRRRRRRRRRRR_
		_RR____RR____RR_
		_RR_BB_RR_BB_RR_
		_RR_BB_RR_BB_RR_
		_RR_BB_RR____RR_
		_RR_BB_RR____RR_
		_RR____RR____RR_
		________________
		________________
		_RR_BB_RR____RR_
		_RR_BB_RR____RR_
		_RR_BB_RR____RR_
		_RR_BB_RR____RR_
		_RR____RR____RR_
		_RRRRRRRRRRRRRR_
		_RRRRRRRRRRRRRR_
		________________
		________________`, wpLetters)
}

func TestWeasyPrint_Tables18(t *testing.T) {
	cell := mods(border(1, css.Solid, red), padding(1))
	root := doc(deep(table(mods(border(1, css.Solid, red), spacing(1, 1)), tr(nil, td(cell, "a a a a"))), font(2), fg(blue)))
	assertPages(t, run(t, root, wpPage(12, 11, 1)), 11, `
		____________
		_RRRRRRRRRR_
		_R________R_
		_R_RRRRRR_R_
		_R_R____R_R_
		_R_R_BB_R_R_
		_R_R_BB_R_R_
		_R_R_BB_R_R_
		_R_R_BB_R_R_
		_R_R____R_R_
		____________
		____________
		_R_R_BB_R_R_
		_R_R_BB_R_R_
		_R_R_BB_R_R_
		_R_R_BB_R_R_
		_R_R____R_R_
		_R_RRRRRR_R_
		_R________R_
		_RRRRRRRRRR_
		____________
		____________`, wpLetters)
}

func TestWeasyPrint_Tables22(t *testing.T) {
	cell := mods(bg(blue), border(1, css.Solid, red))
	root := doc(deep(table(mods(collapse),
		thead(nil, tr(nil, td(cell, "abcde"), td(cell, "abcde"))),
		tbody(nil, tr(nil, td(cell, "abc abc"), td(cell, "")))), font(2), wpBreaks))
	assertPages(t, run(t, root, wpPage(25, 9, 1)), 9, `
		_________________________
		_rrrrrrrrrrrrrrrrrrrrrrr_
		_rKKKKKKKKKKrKKKKKKKKKKr_
		_rKKKKKKKKKKrKKKKKKKKKKr_
		_rrrrrrrrrrrrrrrrrrrrrrr_
		_rKKKKKKBBBBrBBBBBBBBBBr_
		_rKKKKKKBBBBrBBBBBBBBBBr_
		_rBBBBBBBBBBrBBBBBBBBBBr_
		_________________________
		_________________________
		_rrrrrrrrrrrrrrrrrrrrrrr_
		_rKKKKKKKKKKrKKKKKKKKKKr_
		_rKKKKKKKKKKrKKKKKKKKKKr_
		_rrrrrrrrrrrrrrrrrrrrrrr_
		_rKKKKKKBBBBrBBBBBBBBBBr_
		_rKKKKKKBBBBrBBBBBBBBBBr_
		_rrrrrrrrrrrrrrrrrrrrrrr_
		_________________________`, wpLetters)
}

// wpBreaks gives the orphans and widows of WeasyPrint's test stylesheet.
func wpBreaks(s *css.Style) { s.Orphans, s.Widows = 1, 1 }

// splitTable returns a table of a header row, one body row of n lines of
// text and a footer row, every line 10px tall.
func splitTable(n int, m ...func(*css.Style)) *Node {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprint(i % 10)
	}
	body := td(mods(width(10)), strings.Join(lines, " "))
	tb := table(mods(m...), thead(nil, tr(nil, td(nil, "H"))), tbody(nil, tr(nil, body)), tfoot(nil, tr(nil, td(nil, "F"))))
	return deep(tb, font(10), wpBreaks)
}

func TestTableSplit_HeaderAndFooterRepeat(t *testing.T) {
	// Pages 50px tall hold a header, three lines and a footer: the row's
	// seven lines take three pages.
	res := run(t, doc(splitTable(7)), page(20, 50))
	var got []string
	for p := range res.Pages {
		var s []string
		for _, x := range texts(res, p+1) {
			s = append(s, strings.TrimSpace(x.text))
		}
		got = append(got, strings.Join(s, ""))
	}
	// The content of the split row paints before the copies of the header.
	if fmt.Sprint(got) != "[H012F 345HF 6HF]" {
		t.Errorf("pages %q, want the row's lines under a header and over a footer on each", got)
	}
	tm := tableOf(t, doc(splitTable(7)), page(20, 50))
	if len(tm.seqs) != 3 {
		t.Fatalf("%d runs, want 3", len(tm.seqs))
	}
	var pieces []string
	for _, s := range tm.seqs {
		for _, pr := range s {
			if pr.piece {
				pieces = append(pieces, fmt.Sprintf("%v-%v %v%v", pxOf(pr.y0), pxOf(pr.y1), pr.cutTop, pr.cutBottom))
			}
		}
	}
	if want := "[10-40 falsetrue 60-90 truetrue 110-120 truefalse]"; fmt.Sprint(pieces) != want {
		t.Errorf("pieces %v, want %v", pieces, want)
	}
}

func TestTableSplit_AvoidMovesRow(t *testing.T) {
	// A row that avoids breaks inside moves whole to the next page when it
	// fits there; one that does not avoid them breaks.
	for _, avoid := range []bool{false, true} {
		row := tr(nil, td(mods(width(10)), "a a a a"))
		if avoid {
			row.Style.BreakInside = css.InsideAvoid
		}
		root := doc(el(sty(height(30))), deep(table(nil, row), font(10)))
		tm := tableOf(t, root, page(20, 50))
		got := pxOf(tm.rows[0].box.y)
		want := 30.0
		if avoid {
			want = 50
		}
		if got != want || avoid == tm.cells[0].cell.split {
			t.Errorf("avoid %v: row at %v, split %v, want %v", avoid, got, tm.cells[0].cell.split, want)
		}
	}
}

func TestTableSplit_OrphansAndWidows(t *testing.T) {
	// Two orphans and two widows: of five lines, with room for three the
	// break leaves three on the first page; with room for one, the row
	// moves to the next page whole.
	for _, tc := range []struct{ above, first, second int }{{20, 3, 2}, {35, 0, 5}} {
		cell := td(mods(width(10)), "a b c d e")
		cell.Style.Orphans, cell.Style.Widows = 2, 2
		root := doc(el(sty(height(float64(tc.above)))), deep(table(nil, tr(nil, cell)), font(10)))
		res := run(t, root, page(20, 50))
		if n := len(texts(res, 1)); n != tc.first {
			t.Errorf("%dpx above: %d lines on page 1, want %d", tc.above, n, tc.first)
		}
		if n := len(texts(res, 2)); n != tc.second {
			t.Errorf("%dpx above: %d lines on page 2, want %d", tc.above, n, tc.second)
		}
	}
}

func TestTableSplit_InsertStrut(t *testing.T) {
	// The cell's lines below the cut move down; a block the cut crosses
	// grows.
	inner := el(sty(), txt(sty(), "a b c"))
	root := doc(deep(table(nil, tr(nil, td(mods(width(10)), ""))), font(10)))
	root.Children[0].Children[0].Children[0].Children[0].Children = []*Node{inner}
	tm := tableOf(t, root, page(20, 1000))
	c := tm.cells[0]
	blk := c.kids[0]
	c.insertStrut(blk.lines[1].y, px(7), 0)
	if got := []float64{pxOf(blk.lines[0].y), pxOf(blk.lines[1].y), pxOf(blk.lines[2].y), pxOf(blk.h)}; fmt.Sprint(got) != "[0 17 27 37]" {
		t.Errorf("lines at %v and block height %v, want [0 17 27 37]", got[:3], got[3])
	}
}

func TestTable_CaptionStaysWhenTableMoves(t *testing.T) {
	// The first row does not fit below the caption: the table moves to the
	// next page and its caption stays, as Chromium breaks between them
	// (WPT css/CSS2/pagination/table-page-break-inside-avoid-8).
	tb := table(nil, caption(mods(height(5)), el(sty(height(5)))), tr(nil, td(mods(height(20)), "a")))
	root := doc(el(sty(height(35))), tb)
	rb := boxes(t, root, page(20, 50))
	w := kid(t, rb, 0, 1)
	if w.tdata == nil {
		t.Fatal("no table wrapper")
	}
	if c, tt := pxOf(w.kids[0].y), pxOf(w.tdata.table.y); c != 35 || tt != 50 {
		t.Errorf("caption at %v and table at %v, want 35 and 50", c, tt)
	}
}

// TestTableSplit_NestedTableRows splits a row whose cell holds a table
// taller than the page between the nested table's rows, as Writer splits
// it, rather than moving the outer table to the next page whole.
func TestTableSplit_NestedTableRows(t *testing.T) {
	cell := mods(fg(red), valign(css.AlignTop))
	var rows []*Node
	for range 3 {
		rows = append(rows, tr(nil, td(cell, "a")))
	}
	inner := td(nil, "")
	inner.Children = []*Node{table(nil, rows...)}
	root := doc(deep(el(sty(), txt(sty(fg(blue)), "b")), font(2)), deep(table(nil, tr(nil, inner)), font(2)))
	assertPages(t, run(t, root, page(4, 4)), 4, `
		BB__
		BB__
		RR__
		RR__
		RR__
		RR__
		RR__
		RR__`, wpLetters)
}

// TestTableSplit_ClonedCellBorders draws the borders of a split cell whole
// on each page when the cell, or the table in the collapsing model, clones
// its decorations at a break, as Writer draws a split row.
func TestTableSplit_ClonedCellBorders(t *testing.T) {
	clone := func(s *css.Style) { s.BoxDecorationBreak = css.DecorationCloneAll }
	two := func(m []func(*css.Style)) *Node {
		c := td(m, "a")
		c.Children = append(c.Children, br(sty()), txt(sty(fg(red)), "a"), br(sty()), txt(sty(fg(red)), "a"))
		return c
	}
	cell := mods(border(1, css.Solid, black), fg(red), valign(css.AlignTop), clone)
	want := `
		KKKK____
		KRRK____
		KRRK____
		KKKK____
		KKKK____
		KRRK____
		KRRK____
		KKKK____`
	tests := []struct {
		name string
		root *Node
	}{
		{"separated", doc(deep(table(nil, tr(nil, two(cell))), font(2)))},
		{"collapsed", doc(deep(table(mods(collapse, clone), tr(nil, two(mods(border(1, css.Solid, black), fg(red), valign(css.AlignTop))))), font(2)))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, tt.root, page(8, 4))
			if len(res.Pages) < 2 {
				t.Fatalf("%d pages, want at least 2", len(res.Pages))
			}
			var rows string
			for i := range 2 {
				rows += "\n" + strings.Join(grid(render(t, res, i+1)), "\n")
			}
			if got := strings.ReplaceAll(rows[1:], "B", "K"); got != normalize(want) {
				t.Errorf("pixels:\n%s\nwant:\n%s", got, normalize(want))
			}
		})
	}
}
