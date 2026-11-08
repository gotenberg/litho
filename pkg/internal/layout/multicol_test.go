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
