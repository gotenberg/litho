package layout

import (
	"image/color"
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
	// The height is a content height: the borders of the pieces take
	// none of it (CSS 2.1, 10.5).
	edge := func(s *css.Style) {
		s.Border[css.Top] = css.Border{Width: px(1), Style: css.Solid, Color: blue}
		s.Border[css.Bottom] = s.Border[css.Top]
	}
	rb := boxes(t, doc(el(sty(columns(2, 1)), el(sty(height(4), bg(lime), edge), strip(red), strip(red), strip(black, span)))), page(5, 6))
	sum := 0.0
	var walk func(b *box)
	walk = func(b *box) {
		if b.st.BackgroundColor == lime {
			sum += b.h
		}
		if b.mc == nil {
			for _, k := range b.kids {
				walk(k)
			}
			return
		}
		for _, set := range b.mc.sets {
			for _, k := range set.kids {
				walk(k)
			}
		}
	}
	walk(rb)
	if !near(sum, px(4)) {
		t.Errorf("pieces' content heights sum to %vpx, want 4px", sum/px(1))
	}
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

func TestMulticol_FillAutoUnderMaxHeight(t *testing.T) {
	t.Parallel()
	// CSS Multicol 1, 7.1 fills the columns in turn up to the max-height;
	// the container's height stays auto, its tallest column's (CSS 2.1,
	// 10.6.3), so the blue box after it starts below one strip.
	auto := func(s *css.Style) { s.Columns.Fill = css.FillAuto }
	maxH := func(s *css.Style) { s.MaxHeight = css.Pts(px(3)) }
	res := run(t, doc(el(sty(columns(2, 1), maxH, auto), strip(red)), strip(blue)), page(5, 3))
	assertPixels(t, res, 1, `
		RR___
		BBBBB
		_____`)
}

func TestMulticol_SpannerInTransformDoesNotSpan(t *testing.T) {
	t.Parallel()
	// A column-span: all box inside a transformed block runs in the
	// columns, as Chromium lays it out (holdsSpanner).
	span := func(s *css.Style) { s.Columns.SpanAll = true }
	tf := func(s *css.Style) {
		s.Transform = []css.TransformFunc{{Kind: css.TransformScale, V: [6]float64{1, 1}}}
	}
	auto := func(s *css.Style) { s.Columns.Fill = css.FillAuto }
	res := run(t, doc(el(sty(columns(2, 1), height(2), auto), el(sty(tf), el(sty(span, width(2), height(4), bg(lime)))))), page(5, 3))
	assertPixels(t, res, 1, `
		GG_GG
		GG_GG
		_____`)
}

func TestMulticol_FloatAvoidingBreaksMovesWhole(t *testing.T) {
	t.Parallel()
	// CSS Fragmentation 3, 3.2 and 4.4: a float with break-inside: avoid
	// that a column cut would split starts the next column whole.
	avoid := func(s *css.Style) { s.BreakInside = css.InsideAvoid }
	auto := func(s *css.Style) { s.Columns.Fill = css.FillAuto }
	res := run(t, doc(el(sty(columns(2, 1), height(3), auto),
		el(sty(height(2), bg(red))),
		el(sty(float(css.FloatLeft), width(2), height(2), bg(lime), avoid)))), page(5, 3))
	assertPixels(t, res, 1, `
		RR_GG
		RR_GG
		_____`)
}

func TestMulticol_CloneBorderBoxHeightIsTheFragmentsSum(t *testing.T) {
	t.Parallel()
	// CSS Fragmentation 3, 5.4: each fragment of a cloned box holds its
	// borders whole; a border-box height of 4 over columns 2 tall is two
	// fragments of 2, as Chromium sizes it, not one spilling into a third
	// column.
	clone := func(s *css.Style) {
		s.BoxDecorationBreak = css.DecorationClone
		s.BoxSizing = css.BorderBox
	}
	auto := func(s *css.Style) { s.Columns.Fill = css.FillAuto }
	mc := el(sty(columns(3, 0), height(2), auto),
		el(sty(height(4), bg(lime), border(1, css.Solid, blue), clone)))
	rb := boxes(t, doc(mc), page(6, 3))
	if got := rect(kid(t, rb, 0, 0, 0)); got[3] != 4 {
		t.Errorf("border box height %v, want 4: the box does not grow", got[3])
	}
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

// TestMulticol_CloneOfAutoHeightKeepsRoomForItsEdges locks that the
// content of a cloned box of auto height leaves room at each column's end
// for the box's bottom border, and starts below its top border in the
// next column (CSS Fragmentation 3, 5.4), while the box around it fills
// its columns: the 3px child of a box of 1px borders in columns 3px tall
// takes 1px of each column.
func TestMulticol_CloneOfAutoHeightKeepsRoomForItsEdges(t *testing.T) {
	t.Parallel()
	auto := func(s *css.Style) { s.Columns.Fill = css.FillAuto }
	clone := func(s *css.Style) {
		s.BoxDecorationBreak = css.DecorationClone
		s.Border[css.Top] = css.Border{Width: px(1), Style: css.Solid, Color: red}
		s.Border[css.Bottom] = css.Border{Width: px(1), Style: css.Solid, Color: red}
	}
	res := run(t, doc(el(sty(columns(3, 1), width(8), height(3), auto), el(sty(bg(lime)), el(sty(clone), el(sty(height(3), bg(blue))))))), page(8, 4))
	assertPixels(t, res, 1, `
		RR_RR_RR
		BB_BB_BB
		RR_RR_RR
		________`)
	// The box around a cloned box of padding alone fills the room its
	// padding keeps in each column.
	pad := func(s *css.Style) {
		s.BoxDecorationBreak = css.DecorationClone
		s.Padding = dims(1, 0, 1, 0)
	}
	res = run(t, doc(el(sty(columns(3, 1), width(8), height(3), auto), el(sty(bg(lime)), el(sty(pad), el(sty(height(3), bg(blue))))))), page(8, 4))
	assertPixels(t, res, 1, `
		GG_GG_GG
		BB_BB_BB
		GG_GG_GG
		________`)
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

// TestMulticol_OutlineOverSpannerOutline locks that the outline of a
// multi-column container paints after those of the boxes inside it, a
// spanner's among them, as in a container without columns
// ([painter.outlines]).
func TestMulticol_OutlineOverSpannerOutline(t *testing.T) {
	t.Parallel()
	outline := func(c css.Color) func(*css.Style) {
		return func(s *css.Style) { s.Outline = css.Border{Width: px(1), Style: css.Solid, Color: c} }
	}
	span := func(s *css.Style) { s.Columns.SpanAll = true }
	res := run(t, doc(el(sty(columns(1, 0), margin(1, 0, 0, 1), outline(red)), strip(lime, height(2), span, outline(blue)))), page(7, 4))
	assertPixels(t, res, 1, `
		RRRRRRR
		RGGGGGR
		RGGGGGR
		RRRRRRR`)
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

func TestMulticol_FirstBaselineIsTheHighestColumns(t *testing.T) {
	// CSS Align 3, 9.1: the first baseline set of a multi-column container
	// is that of the column whose baseline is the highest. A first column
	// that starts with a 10px block and a second that starts with a line
	// give the second column's baseline; the outside marker of a list
	// item still sits on the first line of the flow (CSS Lists 3, 3.5).
	auto := func(s *css.Style) {
		s.Columns.Fill = css.FillAuto
		s.Width = css.Pts(px(100))
		s.Orphans, s.Widows = 1, 1
	}
	mc := el(sty(columns(2, 0), auto, height(25)), el(sty(height(10))), txt(sty(), "a"), br(sty()), txt(sty(), "b"), br(sty()), txt(sty(), "c"))
	b := kid(t, boxes(t, doc(mc), page(200, 100)), 0, 0)
	first, ok := firstBaseline(b)
	flow, ok2 := markerBaseline(b)
	if !ok || !ok2 {
		t.Fatal("no baseline")
	}
	// 10px Ahem lines of line-height 1: baselines 8px below a line's top.
	if !near((first-b.contentY())/css.Pt, 8) || !near((flow-b.contentY())/css.Pt, 18) {
		t.Errorf("baselines %v and %v px below the top, want 8 (the second column) and 18 (the first line)", (first-b.contentY())/css.Pt, (flow-b.contentY())/css.Pt)
	}
}

// TestMulticol_MonolithicOverflowsItsColumnWhole locks the overflow of a
// monolithic piece taller than its column (CSS Fragmentation 3, 4.1 and
// 4.4): an image and an inline block draw whole in the column they start
// in, a box around the image that ends before the image's bottom ends in
// that column, and the content after them starts at the top of the next
// column, as Chromium draws them.
//
// # Corpus evidence
//
//   - wpt, css/css-break/block-max-height-004.html: the 80 pixel image in a
//     30 pixel column, cut across three columns before.
func TestMulticol_MonolithicOverflowsItsColumnWhole(t *testing.T) {
	t.Parallel()
	auto := func(s *css.Style) { s.Columns.Fill = css.FillAuto }
	maxH := func(s *css.Style) { s.MaxHeight = css.Pts(px(4)) }
	img := &Node{Kind: Replaced, Style: sty(width(1), height(8)), Picture: pic(color.NRGBA{B: 255, A: 255}, 1, 8)}
	res := run(t, doc(el(sty(columns(2, 1), height(3), auto), el(sty(bg(lime), maxH), img, strip(red, width(1), height(8))))), page(9, 10))
	assertPixels(t, res, 1, `
		BG_R__R__
		BG_R__R__
		BG_R__R__
		BG_______
		B________
		B________
		B________
		B________
		_________
		_________`)
	zero := func(s *css.Style) { s.LineHeight = css.Dimension{Kind: css.Number, Value: 0} }
	ib := el(sty(display(css.InlineBlock), width(1), height(8), bg(blue)))
	res = run(t, doc(el(sty(columns(2, 1), height(3), auto), strip(lime), el(sty(zero), ib), strip(red))), page(9, 10))
	assertPixels(t, res, 1, `
		GG_B__RR_
		___B_____
		___B_____
		___B_____
		___B_____
		___B_____
		___B_____
		___B_____
		_________
		_________`)
}

// TestMulticol_BalanceKeepsAnAvoidedEmptyBlockWhole locks that balancing
// does not cut the blank height of an empty block whose break-before is
// avoid: the cut would separate it from the block before as the avoided
// break does (CSS Fragmentation 3, 3.1 and 4.4), so the columns grow to
// hold both, as Chromium balances them.
func TestMulticol_BalanceKeepsAnAvoidedEmptyBlockWhole(t *testing.T) {
	t.Parallel()
	avoid := func(s *css.Style) { s.BreakBefore = css.BreakAvoid }
	res := run(t, doc(el(sty(columns(2, 1)),
		strip(red), strip(blue), strip(blue), strip(lime, avoid), strip(black), strip(black))), page(5, 5))
	assertPixels(t, res, 1, `
		RR_KK
		BB_KK
		BB___
		GG___
		_____`)
}

func TestMulticol_NegativeMarginKeepsTheColumnReached(t *testing.T) {
	t.Parallel()
	// The lime block ends 1 pixel into the second column; the blue block,
	// pulled back 2 pixels by its top margin, stays in that column above
	// its top, as Chromium lays it out (CSS Fragmentation 3, 2 does not
	// say), rather than in the first column's slice.
	auto := func(s *css.Style) { s.Columns.Fill = css.FillAuto }
	res := run(t, doc(el(sty(columns(2, 1), height(3), margin(2, 0, 0, 0), auto),
		strip(lime, height(4)), strip(blue, margin(-2, 0, 0, 0)))), page(5, 5))
	assertPixels(t, res, 1, `
		_____
		___BB
		GG_GG
		GG___
		GG___`)
}

func TestMulticol_PositionedBoxesAloneAreLaidOut(t *testing.T) {
	t.Parallel()
	// A column set of absolutely positioned boxes alone lays them out and
	// paints them (CSS 2.1, 10.1): the container is their containing
	// block, and they take no room in its columns.
	box := square(1, 1, blue, abs(0, auto, auto, 3))
	mc := el(sty(columns(2, 1), height(2), func(s *css.Style) { s.Position = css.Relative }), box)
	assertPixels(t, run(t, doc(mc), page(5, 3)), 1, `
		___B_
		_____
		_____`)
}

// fillAuto makes a multi-column container fill its columns in turn.
func fillAuto(s *css.Style) { s.Columns.Fill = css.FillAuto }

// positioned makes a style relatively positioned in place.
func positioned(s *css.Style) { s.Position = css.Relative }

func TestMulticol_ContainerHoldsPositionedBoxOutsideItsColumns(t *testing.T) {
	t.Parallel()
	// A box fragments in the flow of its containing block (CSS Position
	// 3, 3.5.3; CSS Fragmentation 3, 5.5). The container's columns
	// fragment its content, not the container: a box it holds overflows
	// it whole.
	box := square(1, 4, blue, abs(0, auto, auto, 0))
	mc := el(sty(columns(2, 1), height(2), fillAuto, positioned), strip(red), strip(red), strip(red), box)
	assertPixels(t, run(t, doc(mc), page(5, 5)), 1, `
		BR_RR
		BR___
		B____
		B____
		_____`)
	// Its static position lies in the second column, after the first
	// column's content, and the box overflows that column whole.
	box = square(1, 3, blue, abs(auto, auto, auto, auto))
	mc = el(sty(columns(2, 1), height(2), fillAuto, positioned), strip(red), strip(red), strip(red), box)
	assertPixels(t, run(t, doc(mc), page(5, 5)), 1, `
		RR_RR
		RR_B_
		___B_
		___B_
		_____`)
}

func TestMulticol_FixedBoxAtItsColumn(t *testing.T) {
	t.Parallel()
	// A fixed box at its static position sits where its column paints it
	// (CSS 2.1, 10.3.7 and 10.6.4).
	fixed := square(1, 1, blue, func(s *css.Style) { s.Position = css.Fixed })
	mc := el(sty(columns(2, 1), height(2), fillAuto), strip(red), strip(red), strip(red), fixed)
	assertPixels(t, run(t, doc(mc), page(5, 3)), 1, `
		RR_RR
		RR_B_
		_____`)
}

func TestMulticol_NestedContainerHoldsBoxCutByOuterColumns(t *testing.T) {
	t.Parallel()
	// A box whose containing block is a container nested in another is
	// cut by the outer container's columns, as the inner container is
	// (CSS Fragmentation 3, 5.5).
	box := square(1, 4, blue, abs(0, auto, auto, 0))
	inner := el(sty(columns(2, 0), width(2), positioned), box)
	outer := el(sty(columns(2, 1), height(2), fillAuto), inner)
	assertPixels(t, run(t, doc(outer), page(5, 3)), 1, `
		B__B_
		B__B_
		_____`)
}

func TestMulticol_NegativeLayerOutsideColumnsPaintsUnder(t *testing.T) {
	t.Parallel()
	// A box of a negative layer outside the columns paints under the
	// content of the columns (CSS 2.1, Appendix E, step 3).
	under := square(2, 1, red, abs(0, auto, auto, 0), zIndex(-1))
	mc := el(sty(columns(2, 1), height(2), fillAuto, positioned), under, strip(blue))
	assertPixels(t, run(t, doc(mc), page(5, 3)), 1, `
		BB___
		_____
		_____`)
}

func TestMulticol_PositionedSpannerPaintsItsPositionedBoxes(t *testing.T) {
	t.Parallel()
	// A box held by a relatively positioned spanner paints (CSS 2.1,
	// Appendix E), over the spanner and past its end.
	box := square(1, 2, blue, abs(0, auto, auto, 3))
	span := el(sty(height(1), bg(red), positioned, func(s *css.Style) { s.Columns.SpanAll = true }), box)
	mc := el(sty(columns(2, 1)), span)
	assertPixels(t, run(t, doc(mc), page(5, 3)), 1, `
		RRRBR
		___B_
		_____`)
}

func TestMulticol_RelativeOffsetMovesFragments(t *testing.T) {
	t.Parallel()
	// Relative positioning applies to each fragment after fragmentation
	// (CSS Fragmentation 3, 5.5): a block cut over two columns moves down
	// in both, each piece cut at its column first.
	block := square(2, 4, blue, rel(0, 1))
	mc := el(sty(columns(2, 1), height(2), fillAuto), block)
	assertPixels(t, run(t, doc(mc), page(5, 4)), 1, `
		_____
		BB_BB
		BB_BB
		_____`)
}

func TestMulticol_RuleOfEveryRowContentRunsThrough(t *testing.T) {
	t.Parallel()
	// A block cut over every column of an inner container's first row
	// holds content in each: the row draws its rule (CSS Multicol 1, 4.5),
	// as the last row does.
	rule := func(s *css.Style) {
		s.Columns.RuleWidth, s.Columns.RuleStyle, s.Columns.RuleColor = px(1), css.Solid, red
	}
	inner := el(sty(columns(2, 1), width(3), rule), el(sty(height(8))))
	outer := el(sty(columns(2, 1), width(7), height(2), fillAuto), inner)
	assertPixels(t, run(t, doc(outer), page(7, 3)), 1, `
		_R___R_
		_R___R_
		_______`)
}

func TestMulticol_RuleRunsToTheCut(t *testing.T) {
	t.Parallel()
	// A container taller than its balanced columns draws its last rule to
	// its content edge, or to the end of the page that cuts it before.
	rule := func(s *css.Style) {
		s.Columns.RuleWidth, s.Columns.RuleStyle, s.Columns.RuleColor = px(1), css.Solid, red
	}
	mc := el(sty(columns(2, 1), height(4), rule), strip(blue), strip(blue))
	assertPixels(t, run(t, doc(mc), page(5, 3)), 1, `
		BBRBB
		__R__
		__R__`)
	// An outer column cuts it the same way.
	inner := el(sty(columns(2, 1), width(5), height(6), rule), strip(blue), strip(blue))
	outer := el(sty(columns(2, 1), width(11), height(3), fillAuto), inner)
	assertPixels(t, run(t, doc(outer), page(11, 4)), 1, `
		BBRBB______
		__R________
		__R________
		___________`)
}

func TestMulticol_AvoidingBoxMovesToTheNextOuterColumn(t *testing.T) {
	t.Parallel()
	// A box that avoids breaks inside it and does not fit the room an
	// outer column leaves an inner container below other content moves to
	// the next outer column, where the inner columns start at its top (CSS
	// Fragmentation 3, 4.4).
	avoid := square(1, 2, blue, func(s *css.Style) { s.BreakInside = css.InsideAvoid })
	inner := el(sty(columns(2, 1), width(3), fillAuto), avoid)
	outer := el(sty(columns(2, 1), width(7), height(3), fillAuto), strip(red, height(2)), inner)
	assertPixels(t, run(t, doc(outer), page(7, 4)), 1, `
		RRR_B__
		RRR_B__
		_______
		_______`)
}
