package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// columns returns a multi-column container style of n columns and a gap of
// gap pixels, 5 pixels wide.
func columns(n int, gap float64) func(*css.Style) {
	return func(s *css.Style) {
		s.Columns.Count = n
		s.ColumnGap = css.Pts(px(gap))
		s.Width = css.Pts(px(5))
	}
}

// strip returns a block one pixel tall of background c.
func strip(c css.Color, mods ...func(*css.Style)) *Node {
	return el(sty(append([]func(*css.Style){height(1), bg(c)}, mods...)...))
}

func TestMulticol_ColumnLayout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		count     int
		width     float64
		u, g      float64
		n         int
		w         float64
		widthAuto bool
	}{
		{count: 3, u: 300, g: 0, n: 3, w: 100, widthAuto: true},
		{count: 2, u: 100, g: 20, n: 2, w: 40, widthAuto: true},
		{count: 0, width: 100, u: 300, g: 0, n: 3, w: 100},
		{count: 0, width: 100, u: 350, g: 0, n: 3, w: 350.0 / 3},
		{count: 2, width: 100, u: 350, g: 0, n: 2, w: 175},
		{count: 5, width: 200, u: 100, g: 10, n: 1, w: 100},
		{count: 0, width: 0, u: 75, g: 0, n: 100, w: 0.75},
		{count: 4, u: 10, g: 20, n: 4, w: 0, widthAuto: true},
	}
	for _, tt := range tests {
		c := css.Columns{Count: tt.count, Width: css.Pts(tt.width)}
		if tt.widthAuto {
			c.Width = css.Dimension{Kind: css.Auto}
		}
		n, w := columnLayout(&c, tt.u, tt.g)
		if n != tt.n || !near(w, tt.w) {
			t.Errorf("%+v in %v gap %v: got %d columns of %v, want %d of %v", c, tt.u, tt.g, n, w, tt.n, tt.w)
		}
	}
}

func TestMulticol_Balances(t *testing.T) {
	t.Parallel()
	res := run(t, doc(el(sty(columns(2, 1)), strip(red), strip(blue), strip(lime), strip(black))), page(5, 4))
	assertPixels(t, res, 1, `
		RR_GG
		BB_KK
		_____
		_____`)
}

func TestMulticol_Rule(t *testing.T) {
	t.Parallel()
	rule := func(s *css.Style) {
		s.Columns.RuleWidth, s.Columns.RuleStyle, s.Columns.RuleColor = px(1), css.Solid, red
	}
	res := run(t, doc(el(sty(columns(2, 1), rule), strip(blue), strip(lime), strip(blue), strip(lime))), page(5, 3))
	assertPixels(t, res, 1, `
		BBRBB
		GGRGG
		_____`)
	// A rule runs between columns that both hold content alone.
	res = run(t, doc(el(sty(columns(2, 1), rule, height(2), func(s *css.Style) { s.Columns.Fill = css.FillAuto }), strip(blue))), page(5, 3))
	assertPixels(t, res, 1, `
		BB___
		_____
		_____`)
}

func TestMulticol_Spanner(t *testing.T) {
	t.Parallel()
	span := func(s *css.Style) { s.Columns.SpanAll = true }
	res := run(t, doc(el(sty(columns(2, 1)), strip(red), strip(red), strip(black, span), strip(blue), strip(blue))), page(5, 4))
	assertPixels(t, res, 1, `
		RR_RR
		KKKKK
		BB_BB
		_____`)
	// A spanner inside a block splits it: the pieces share its height,
	// the last taking what the first leaves, balanced in its columns.
	res = run(t, doc(el(sty(columns(2, 1)), el(sty(height(4), bg(lime)), strip(red), strip(red), strip(black, span)))), page(5, 4))
	assertPixels(t, res, 1, `
		RR_RR
		KKKKK
		GG_GG
		_____`)
}

func TestMulticol_ForcedBreak(t *testing.T) {
	t.Parallel()
	brk := func(s *css.Style) { s.BreakBefore = css.Column }
	res := run(t, doc(el(sty(columns(2, 1)), strip(red), strip(blue, brk), strip(lime))), page(5, 3))
	assertPixels(t, res, 1, `
		RR_BB
		___GG
		_____`)
}

func TestMulticol_FillAuto(t *testing.T) {
	t.Parallel()
	auto := func(s *css.Style) { s.Columns.Fill = css.FillAuto }
	res := run(t, doc(el(sty(columns(2, 1), height(2), auto), strip(red), strip(blue), strip(lime))), page(5, 3))
	assertPixels(t, res, 1, `
		RR_GG
		BB___
		_____`)
}

func TestMulticol_Paginates(t *testing.T) {
	t.Parallel()
	res := run(t, doc(el(sty(columns(2, 1)), strip(red), strip(blue), strip(lime), strip(black), strip(red), strip(blue))), page(5, 2))
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2: a row of columns per page", len(res.Pages))
	}
	assertPixels(t, res, 1, "RR_GG\nBB_KK")
	// The last row balances.
	assertPixels(t, res, 2, "RR_BB\n_____")
}

func TestMulticol_MonolithicMoves(t *testing.T) {
	t.Parallel()
	auto := func(s *css.Style) { s.Columns.Fill = css.FillAuto }
	clip := func(s *css.Style) { s.Overflow = css.OverflowHidden }
	res := run(t, doc(el(sty(columns(2, 1), height(2), auto), strip(red), el(sty(height(2), bg(blue), clip)))), page(5, 3))
	assertPixels(t, res, 1, `
		RR_BB
		___BB
		_____`)
}

func TestMulticol_Anchors(t *testing.T) {
	t.Parallel()
	target := strip(lime)
	target.ID = "t"
	res := run(t, doc(el(sty(columns(2, 1)), strip(red), strip(blue), target, strip(black))), page(5, 4))
	d, ok := res.Anchors["t"]
	if !ok || !near(d.X, px(3)) || !near(d.Y, 0) {
		t.Errorf("anchor in the second column: got %+v, want x 3px, y 0", d)
	}
}

func TestMulticol_CloneDecoration(t *testing.T) {
	t.Parallel()
	auto := func(s *css.Style) { s.Columns.Fill = css.FillAuto }
	clone := func(s *css.Style) {
		s.BoxDecorationBreak = css.DecorationClone
		s.Border[css.Top] = css.Border{Width: px(1), Style: css.Solid, Color: red}
		s.Border[css.Bottom] = css.Border{Width: px(1), Style: css.Solid, Color: red}
	}
	// Each column's fragment holds the box's top and bottom borders: the
	// box grows by them at the break.
	res := run(t, doc(el(sty(columns(2, 1), height(3), auto), el(sty(height(2), bg(blue), clone)))), page(5, 4))
	assertPixels(t, res, 1, `
		RR_RR
		BB_BB
		RR_RR
		_____`)
}

// TestMulticol_CloneFillsItsColumns locks that a cloned box whose
// fragments, each with its top and bottom borders, fill its columns
// exactly grows by the borders of the breaks between them alone, and
// starts no fragment past its last column (CSS Fragmentation 3, 5.4).
func TestMulticol_CloneFillsItsColumns(t *testing.T) {
	t.Parallel()
	cols := func(s *css.Style) {
		s.Columns.Count, s.Columns.Fill = 3, css.FillAuto
		s.ColumnGap = css.Pts(px(10))
		s.Width = css.Pts(px(320))
	}
	clone := func(s *css.Style) { s.BoxDecorationBreak = css.DecorationClone }
	rb := boxes(t, doc(el(sty(cols, height(100)), el(sty(width(80), height(240), border(10, css.Solid, black), clone)))), page(400, 200))
	if got := kid(t, rb, 0, 0, 0).h; !near(got, px(280)) {
		t.Errorf("content height %vpx, want 280px: 240px and the borders of two breaks", got/px(1))
	}
}

// TestMulticol_CloneOutlinesEachFragment locks that a box cloning its
// decorations draws its outline round each column's fragment, whole past
// the column's edges (CSS Fragmentation 3, 5.4).
func TestMulticol_CloneOutlinesEachFragment(t *testing.T) {
	t.Parallel()
	cols := func(s *css.Style) {
		s.Columns.Count, s.Columns.Fill = 2, css.FillAuto
		s.ColumnGap = css.Pts(px(3))
		s.Width = css.Pts(px(9))
	}
	clone := func(s *css.Style) {
		s.BoxDecorationBreak = css.DecorationClone
		s.Outline = css.Border{Width: px(1), Style: css.Solid, Color: red}
	}
	res := run(t, doc(el(sty(cols, height(3), margin(1, 0, 0, 1)), el(sty(height(6), bg(blue), clone)))), page(11, 6))
	assertPixels(t, res, 1, `
		RRRRR_RRRRR
		RBBBR_RBBBR
		RBBBR_RBBBR
		RBBBR_RBBBR
		RRRRR_RRRRR
		___________`)
}

func TestMulticol_BalancesOverAbsoluteBoxes(t *testing.T) {
	t.Parallel()
	abs := el(sty(width(2), func(s *css.Style) { s.Position = css.Absolute; s.Top = css.Pts(0) }), strip(blue), strip(blue), strip(blue), strip(blue))
	rel := el(sty(func(s *css.Style) { s.Position = css.Relative }), strip(red), strip(red), abs)
	// The flow alone balances to one row in each column; the absolutely
	// positioned box, 4 rows tall, balances the columns to 2 rows, which
	// the block after the container follows.
	res := run(t, doc(el(sty(columns(2, 1)), rel), strip(lime)), page(5, 6))
	rows := grid(render(t, res, 1))
	if rows[0][3:] != "BB" || rows[2][4] != 'G' || rows[3][4] != '_' {
		t.Errorf("pixels:\n%s\nwant the second column to start blue and the next block on the third row", strings.Join(rows, "\n"))
	}
}

func TestMulticol_BaselineInItsColumn(t *testing.T) {
	t.Parallel()
	// The container's first line lies at the top of its second column:
	// its baseline aligns there, not one column height lower.
	text := sty(font(1), fg(red))
	mc := el(sty(func(s *css.Style) {
		s.Columns.Count, s.Columns.Fill = 2, css.FillAuto
		s.ColumnGap, s.Width, s.Height = css.Pts(0), css.Pts(px(2)), css.Pts(px(2))
	}), strip(blue), strip(blue), el(text, txt(text, "X")))
	row := el(sty(display(css.Flex), func(s *css.Style) { s.AlignItems = css.BoxBaseline }), el(text, txt(text, "X")), mc)
	assertPixels(t, run(t, doc(row), page(4, 3)), 1, `
		RBR_
		_B__
		____`)
}

func TestMulticol_ClippedPositionedChildStaysInColumn(t *testing.T) {
	t.Parallel()
	// A clipping box 3 rows tall in columns 2 rows tall stays whole in the
	// first column, its relatively positioned child with it.
	child := el(sty(width(4), height(4), bg(blue), func(s *css.Style) { s.Position = css.Relative }))
	clip := el(sty(height(3), bg(red), func(s *css.Style) { s.Overflow = css.OverflowHidden }), child)
	mc := el(sty(columns(2, 1), height(2), func(s *css.Style) { s.Columns.Fill = css.FillAuto }), clip)
	assertPixels(t, run(t, doc(mc), page(5, 4)), 1, `
		BB___
		BB___
		BB___
		_____`)
}

// TestMulticol_MarkerBySpannerLine locks that the outside marker of a
// multi-column list item sits by its first line box, here in a spanner
// after a column set of no line (CSS Lists 3, 3.1; CSS Multicol 1, 5.1):
// the spanner's line is in the item's own flow, which no column moves,
// and the marker paints once.
func TestMulticol_MarkerBySpannerLine(t *testing.T) {
	t.Parallel()
	item := sty(display(css.ListItem), columns(3, 0), func(s *css.Style) {
		s.ListStyleType = css.Decimal
		s.Width = css.Pts(px(90))
		s.Margin[css.Left] = css.Pts(px(40))
	})
	span := sty(func(s *css.Style) { s.Columns.SpanAll = true })
	res := run(t, doc(el(item, el(sty(height(30))), el(span, txt(span, "x")), txt(item, "y"))), page(200, 100))
	var marks []placedText
	var x placedText
	for _, r := range texts(res, 1) {
		switch {
		case strings.HasPrefix(r.text, "1."):
			marks = append(marks, r)
		case r.text == "x":
			x = r
		}
	}
	if len(marks) != 1 || x.run == nil || !near(marks[0].y, x.y) {
		t.Errorf("markers %+v, want one on the baseline of the spanner's line %v", marks, x.y)
	}
}

// TestMulticol_OutlineUnderSpannerOutline locks that the outline of a
// multi-column container paints before those of the boxes inside it, a
// spanner's among them, as in a container without columns (CSS 2.1,
// Appendix E, step 10, in tree order).
func TestMulticol_OutlineUnderSpannerOutline(t *testing.T) {
	t.Parallel()
	outline := func(c css.Color) func(*css.Style) {
		return func(s *css.Style) { s.Outline = css.Border{Width: px(1), Style: css.Solid, Color: c} }
	}
	span := func(s *css.Style) { s.Columns.SpanAll = true }
	res := run(t, doc(el(sty(columns(1, 0), margin(1, 0, 0, 1), outline(red)), strip(lime, height(2), span, outline(blue)))), page(7, 4))
	assertPixels(t, res, 1, `
		BBBBBBB
		BGGGGGB
		BGGGGGB
		BBBBBBB`)
}

// TestMulticol_BalancesToTheLayoutUnit locks that a balanced column height
// is the least that fits on the grid of Chromium's layout unit, a 64th of
// a CSS pixel: 250 and 5 pixels of content in three columns of a container
// 100 pixels tall balance to 85 pixels exactly, not to where the search
// stops above it (CSS Multicol 1, 7.1 balances "as far as possible").
func TestMulticol_BalancesToTheLayoutUnit(t *testing.T) {
	t.Parallel()
	mc := el(sty(columns(3, 0), height(100), func(s *css.Style) { s.Width = css.Pts(px(300)) }), el(sty(height(250))), el(sty(height(5))))
	rb := boxes(t, doc(mc), page(400, 300))
	b := kid(t, rb, 0, 0)
	if b.mc == nil || len(b.mc.sets) == 0 || len(b.mc.sets[0].rows) == 0 {
		t.Fatal("no column row")
	}
	if h := b.mc.sets[0].rows[0].h; h != px(85) {
		t.Errorf("column height %v pt, want %v", h, px(85))
	}
}

// TestMulticol_LastRuleReachesTheContentEdge locks that the rule of the
// last row of columns runs down to the content edge of a container taller
// than its balanced content, as Chromium draws it (CSS Multicol 1, 4.5),
// while a row before a spanner keeps its own height.
func TestMulticol_LastRuleReachesTheContentEdge(t *testing.T) {
	t.Parallel()
	rule := func(s *css.Style) {
		s.Columns.RuleWidth, s.Columns.RuleStyle, s.Columns.RuleColor = px(1), css.Solid, red
	}
	span := func(s *css.Style) { s.Columns.SpanAll = true }
	res := run(t, doc(el(sty(columns(2, 1), rule, height(5)), strip(blue), strip(lime), strip(black, span), strip(blue), strip(lime))), page(5, 6))
	assertPixels(t, res, 1, `
		BBRGG
		KKKKK
		BBRGG
		__R__
		__R__
		_____`)
}

// TestMulticol_InlineBlockCutsItsColumns locks that a multi-column inline
// block, which sits on its line's page whole, still cuts its content at
// its columns: a block balanced over two columns paints half in each (CSS
// Multicol 1, 2), whatever its vertical alignment moves it by.
func TestMulticol_InlineBlockCutsItsColumns(t *testing.T) {
	t.Parallel()
	top := func(s *css.Style) { s.VerticalAlign = css.VerticalAlign{Kind: css.AlignTop} }
	mc := el(sty(columns(2, 1), display(css.InlineBlock), top, margin(1, 0, 0, 0)), strip(blue, height(2)))
	res := run(t, doc(mc), page(5, 3))
	assertPixels(t, res, 1, `
		_____
		BB_BB
		_____`)
}

// TestMulticol_BalancesOverAvoidBoxes locks that balanced columns are no
// shorter than the tallest box that avoids breaks inside it, in the flow
// or in an absolutely positioned box, so that box fits a column, as
// Chromium balances (CSS Fragmentation 3, 4.4).
//
// # Corpus evidence
//
//   - probe, html: a 200 px avoid box above a 100 px block in two columns
//     balances to 200 px in Chromium; a 160 px avoid box in a wrapper with
//     40 px after it balances to 160 px; a 200 px avoid box in an
//     absolutely positioned box after a 100 px block balances to 200 px.
func TestMulticol_BalancesOverAvoidBoxes(t *testing.T) {
	t.Parallel()
	rel := func(s *css.Style) { s.Position = css.Relative }
	absl := func(s *css.Style) { s.Position = css.Absolute; s.Top = css.Pts(0) }
	tests := []struct {
		name string
		kids []*Node
		want float64
	}{
		{"in the flow", []*Node{el(sty(height(200), avoid)), el(sty(height(100)))}, 200},
		{"in a wrapper", []*Node{el(sty(), el(sty(height(160), avoid)), el(sty(height(20)))), el(sty(height(20)))}, 160},
		{"in an absolute box", []*Node{el(sty(height(100))), el(sty(height(200), rel), el(sty(absl), el(sty(height(200), avoid))))}, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := el(sty(columns(2, 0), func(s *css.Style) { s.Width = css.Pts(px(200)) }), tt.kids...)
			rb := boxes(t, doc(mc), page(400, 300))
			b := kid(t, rb, 0, 0)
			if b.mc == nil || len(b.mc.sets) == 0 || len(b.mc.sets[0].rows) == 0 {
				t.Fatal("no column row")
			}
			if h := b.mc.sets[0].rows[0].h; !near(h, px(tt.want)) {
				t.Errorf("column height %v pt, want %v", h, px(tt.want))
			}
		})
	}
}

// TestMulticol_RelativeInlineBackgroundInItsColumn locks that the
// background of a relatively positioned inline box paints in the column
// of its line, moved by its offset after the columns cut the flow, as its
// text does (CSS Fragmentation 3, 5.5), even when the offset reaches past
// the column.
func TestMulticol_RelativeInlineBackgroundInItsColumn(t *testing.T) {
	t.Parallel()
	small := func(s *css.Style) { s.FontSize = px(2) }
	span := el(inlineSty(small, bg(red), fg(blue), padding(0, 0, 0, 1), func(s *css.Style) {
		s.Position = css.Relative
		s.Top = css.Pts(px(2))
	}), txt(inlineSty(small, fg(blue)), "X"))
	mc := el(sty(small, columns(3, 0), height(2), func(s *css.Style) {
		s.Width = css.Pts(px(12))
		s.Columns.Fill = css.FillAuto
	}), br(inlineSty(small)), span, br(inlineSty(small)), txt(inlineSty(small, fg(lime)), "X"))
	assertPixels(t, run(t, doc(mc), page(12, 4)), 1, `
		________GG__
		________GG__
		____RBB_____
		____RBB_____`)
}
