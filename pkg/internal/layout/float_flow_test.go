package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func clearing(c css.Clear) func(*css.Style) { return func(s *css.Style) { s.Clear = c } }

var hidden = func(s *css.Style) { s.Overflow = css.OverflowHidden }

func TestClearance_AbsorbsChildMargins(t *testing.T) {
	// CSS 2.1, 9.5.2: the hypothetical position counts the child's margin
	// collapsing with the cleared box's; the child then sits at its top.
	rb := boxes(t, doc(el(sty(height(100)),
		el(sty(float(css.FloatLeft), width(50), height(20))),
		el(sty(clearing(css.ClearLeft), margin(20, 0, 0, 0)),
			el(sty(height(10), margin(-200, 0, 0, 0)))),
	)), page(100, 200))
	if got := rect(kid(t, rb, 0, 0, 1, 0)); got[1] != 20 {
		t.Errorf("child top %v, want 20: at the cleared top", got[1])
	}
}

func TestFloats_PlacementRules(t *testing.T) {
	tests := []struct {
		name string
		root *Node
		path []int
		want [2]float64
	}{
		{
			// Rule 3: a float outside the containing block still bars a
			// wider float from overlapping it.
			"right float outside the containing block",
			doc(el(sty(float(css.FloatLeft), width(100), height(100)),
				el(sty(float(css.FloatRight), width(10), height(30))),
				el(sty(margin(0, 20, 0, 0)), el(sty(float(css.FloatLeft), width(95), height(5)))))),
			[]int{0, 0, 1, 0},
			[2]float64{0, 30},
		},
		{
			// 9.5.2: clear on a float puts it below the floats it clears.
			"cleared float",
			doc(el(sty(float(css.FloatRight), width(10), height(10))),
				el(sty(float(css.FloatRight), clearing(css.ClearRight), width(40), height(10)))),
			[]int{0, 1},
			[2]float64{60, 10},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rect(kid(t, boxes(t, tt.root, page(100, 200)), tt.path...))
			if [2]float64{got[0], got[1]} != tt.want {
				t.Errorf("float at %v, want %v", got[:2], tt.want)
			}
		})
	}
}

func TestFloats_TallLineAvoidsFloatBelowStrut(t *testing.T) {
	// The second line is 30 tall: the left float 15 below its top leaves
	// no room for it though the strut's band does, so it moves below both
	// floats.
	ib := func() *Node { return el(sty(display(css.InlineBlock), width(60), height(30))) }
	rb := boxes(t, doc(
		el(sty(float(css.FloatRight), width(30), height(45))),
		el(sty(float(css.FloatLeft), width(80), height(20))),
		el(sty(), ib(), ib()),
	), page(100, 200))
	p := kid(t, rb, 0, 2)
	if len(p.lines) != 2 {
		t.Fatalf("%d lines, want 2", len(p.lines))
	}
	if got := p.lines[1].y / css.Pt; !near(got, 65) {
		t.Errorf("second line at %v, want 45: below both floats", got)
	}
}

func TestFloats_AnchoredAtLineEndSitBeside(t *testing.T) {
	// A float met at the end of the line's content goes at the line's top
	// when it fits beside it (CSS 2.1, 9.5.1, rule 8).
	rb := boxes(t, doc(el(sty(),
		txt(sty(), "aa "),
		el(sty(float(css.FloatLeft), width(20), height(20))),
	)), page(100, 200))
	p := kid(t, rb, 0, 0)
	if got := rect(kid(t, p, 1)); got[0] != 0 || got[1] != 0 {
		t.Errorf("float at %v, want [0 0]", got[:2])
	}
	if got := p.lines[0].x / css.Pt; !near(got, 20) {
		t.Errorf("line at x %v, want 20: beside the float", got)
	}
}

func TestFloats_MaxContentSumsFloatsAndLine(t *testing.T) {
	rb := boxes(t, doc(el(sty(float(css.FloatLeft)),
		el(sty(float(css.FloatRight), width(30), height(10))),
		el(sty(display(css.InlineBlock), width(40), height(10))),
	)), page(200, 200))
	if got := rect(kid(t, rb, 0, 0))[2]; got != 70 {
		t.Errorf("float width %v, want 70: its float beside its line", got)
	}
}

func TestFloats_BFCRootNarrowsAtSameTop(t *testing.T) {
	// Beside the first float the box is 70 wide, wraps to two lines and
	// grows beside the second float; 40 wide, it fits beside both at the
	// same top rather than moving down.
	rb := boxes(t, doc(
		el(sty(float(css.FloatLeft), width(30), height(10))),
		el(sty(float(css.FloatLeft), clearing(css.ClearLeft), width(60), height(20))),
		el(sty(hidden), txt(sty(), "aaaa aaaa")),
	), page(100, 200))
	if got := rect(kid(t, rb, 0, 2)); got != [4]float64{60, 0, 40, 20} {
		t.Errorf("BFC %v, want [60 0 40 20]", got)
	}
}

func TestMarginCollapse_MinHeightCutsChildMargin(t *testing.T) {
	rb := boxes(t, doc(
		el(sty(func(s *css.Style) { s.MinHeight = css.Pts(px(50)) }), el(sty(height(10), margin(0, 0, 100, 0)))),
		el(sty(height(10))),
	), page(100, 300))
	if got := rect(kid(t, rb, 0, 0))[3]; got != 50 {
		t.Errorf("parent height %v, want 50", got)
	}
	if got := rect(kid(t, rb, 0, 1))[1]; got != 50 {
		t.Errorf("next top %v, want 50: the child's margin is cut off", got)
	}
}

func TestFloats_MoveWithCollapsedMargins(t *testing.T) {
	// The float sits in an empty box whose margins collapse with the
	// parent's and the next sibling's: it lands where they resolve.
	rb := boxes(t, doc(el(sty(margin(10, 0, 0, 0)),
		el(sty(), el(sty(float(css.FloatLeft), width(10), height(10)))),
		el(sty(margin(30, 0, 0, 0)), txt(sty(), "a")),
	)), page(100, 200))
	if got := rect(kid(t, rb, 0, 0, 0, 0))[1]; got != 30 {
		t.Errorf("float top %v, want 30", got)
	}
}

func TestFloats_NewContextSeparatesFromFloat(t *testing.T) {
	// The new formatting context does not fit beside the float placed
	// under the pending margins: it goes below the float, its margin cut,
	// and the float stays.
	rb := boxes(t, doc(el(sty(hidden, width(100), height(100)),
		el(sty(margin(-20, 0, 0, 0)),
			el(sty(), el(sty(float(css.FloatLeft), width(100), height(50)))),
			el(sty(hidden, margin(500, 0, 0, 0), width(100), height(10)))),
	)), page(100, 200))
	outer := kid(t, rb, 0, 0, 0)
	if got := rect(kid(t, outer, 0, 0))[1]; got != -20 {
		t.Errorf("float top %v, want -20", got)
	}
	if got := rect(kid(t, outer, 1))[1]; got != 30 {
		t.Errorf("new context top %v, want 30: below the float", got)
	}
}

func TestTableRows_BaselineBelowCellBottom(t *testing.T) {
	// A line 2px tall in a 10px font has its baseline 4px down, below the
	// cell's bottom: the row takes the cell's height, not the baseline's.
	lh := func(s *css.Style) { s.LineHeight = css.Pts(px(2)) }
	tm := tableOf(t, doc(table(mods(spacing(0, 0)), tr(nil, td(mods(lh), "a")))), page(100, 100))
	if got := tm.table.borderH() / css.Pt; !near(got, 2) {
		t.Errorf("table height %v, want 2", got)
	}
}

func TestFloats_LinePushedPastCutBreaksAgain(t *testing.T) {
	// Beside the float the line holds one word; pushed to the next page,
	// where no float narrows it, it is broken again and holds both.
	rb := boxes(t, doc(
		el(sty(height(25))),
		el(sty(float(css.FloatRight), width(50), height(4))),
		el(sty(), txt(sty(), "aaaa aaaa")),
	), page(100, 30))
	p := kid(t, rb, 0, 2)
	if len(p.lines) != 1 {
		t.Fatalf("%d lines, want 1", len(p.lines))
	}
	if got := p.lines[0].y / css.Pt; !near(got, 30) || !near(p.lines[0].w/css.Pt, 100) {
		t.Errorf("line at y %v, %v wide, want 30 and 100", got, p.lines[0].w/css.Pt)
	}
}

func TestFloats_PushedFirstLineLeavesBlockTop(t *testing.T) {
	// CSS 2.1, 9.5: a line too wide to fit beside a float moves below it
	// inside its block; the block, whose top margin had not resolved yet,
	// keeps its top. Chromium draws the block from 0, its line at 50.
	rb := boxes(t, doc(
		el(sty(width(300), height(100)),
			el(sty(float(css.FloatLeft), width(100), height(50))),
			el(sty(display(css.InlineBlock), width(250), height(20)))),
	), page(400, 300))
	b := kid(t, rb, 0, 0)
	if got := rect(b); got[1] != 0 {
		t.Errorf("block top %v, want 0", got[1])
	}
	if len(b.lines) != 1 || !near(b.lines[0].y/css.Pt, 50) {
		t.Errorf("lines %v, want one at 50", b.lines)
	}
}

func TestFormattingContextBelowFloat_KeepsParentTop(t *testing.T) {
	// CSS 2.1, 9.5: a box that establishes a formatting context and does
	// not fit beside a float moves below it; that is no clearance, so the
	// parent whose top margin collapses with the box's keeps its top
	// where the margins resolve, beside the float.
	rb := boxes(t, doc(el(sty(width(100)),
		el(sty(),
			el(sty(float(css.FloatLeft), width(50), height(50))),
			el(sty(hidden, width(70), height(30)))),
	)), page(200, 200))
	if got := rect(kid(t, rb, 0, 0, 0)); got[1] != 0 || got[3] != 80 {
		t.Errorf("parent at y %v height %v, want 0 and 80", got[1], got[3])
	}
	if got := rect(kid(t, rb, 0, 0, 0, 1)); got[1] != 50 {
		t.Errorf("formatting context at y %v, want 50: below the float", got[1])
	}
}

func TestFormattingContextBesideFloat_Margins(t *testing.T) {
	// CSS 2.1, 9.5 leaves the placement beside floats to the user agent:
	// a margin may lie over the float, never moves the box into it, and
	// only a positive one narrows the space beside it.
	tests := []struct {
		name   string
		rtl    bool
		w      float64
		margin [4]float64
		want   [3]float64 // x, y, width of the border box
	}{
		{"auto width, negative start margin", false, 0, [4]float64{0, 0, 0, -20}, [3]float64{50, 0, 50}},
		{"auto width, positive start margin", false, 0, [4]float64{0, 0, 0, 20}, [3]float64{50, 0, 50}},
		{"auto width, margin wider than the float", false, 0, [4]float64{0, 0, 0, -60}, [3]float64{-60, 40, 160}},
		{"rtl, negative end margin", true, 0, [4]float64{0, 0, 0, -20}, [3]float64{50, 0, 50}},
		{"rtl, fixed width, negative start margin", true, 40, [4]float64{0, -20, 0, 0}, [3]float64{60, 0, 40}},
		{"rtl, start margin past the float", true, 0, [4]float64{0, 51, 0, 0}, [3]float64{0, 40, 49}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.margin
			mods := []func(*css.Style){hidden, height(30), margin(m[0], m[1], m[2], m[3])}
			if tt.w > 0 {
				mods = append(mods, width(tt.w))
			}
			outer := []func(*css.Style){width(100)}
			if tt.rtl {
				outer = append(outer, rtl)
			}
			rb := boxes(t, doc(el(sty(outer...),
				el(sty(float(css.FloatLeft), width(50), height(40))),
				el(sty(mods...)))), page(200, 200))
			got := rect(kid(t, rb, 0, 0, 1))
			if [3]float64{got[0], got[1], got[2]} != tt.want {
				t.Errorf("box at %v, want x, y, width %v", got, tt.want)
			}
		})
	}
}

func TestClearance_MarginsThroughClearedBoxStayInParent(t *testing.T) {
	// CSS 2.1, 8.3.1: a parent's bottom margin collapses with its last
	// child's only if that margin does not collapse with a top margin
	// that has clearance; the margins after an empty cleared box stay in,
	// through the empty boxes white space between the children makes.
	for _, ws := range []bool{false, true} {
		kids := []*Node{
			el(sty(float(css.FloatRight), width(10), height(1))),
			el(sty(clearing(css.ClearRight))),
			el(sty(margin(99, 0, 0, 0))),
		}
		if ws {
			kids = []*Node{kids[0], txt(sty(), " "), kids[1], txt(sty(), " "), kids[2], txt(sty(), " ")}
		}
		rb := boxes(t, doc(el(sty(width(100)), kids...)), page(200, 200))
		if got := rect(kid(t, rb, 0, 0)); got[3] != 100 {
			t.Errorf("white space %v: parent height %v, want 100", ws, got[3])
		}
	}
}

func TestClearance_HypotheticalPositionCountsEmptyContent(t *testing.T) {
	// CSS 2.1, 9.5.2: the hypothetical position counts the margins that
	// collapse through an empty descendant, which a layout decides (a
	// percentage padding of a fit-content box is 0): past the float, the
	// box takes no clearance; short of it, clearance places it at the
	// float's bottom and the margins are spent.
	fit := func(s *css.Style) { s.Width = css.Dimension{Kind: css.FitContent} }
	pct := func(s *css.Style) { s.Padding[css.Top] = css.Dimension{Kind: css.Percent, Value: 1} }
	for _, m := range []float64{100, 75} {
		rb := boxes(t, doc(el(sty(display(css.InlineBlock)),
			el(sty(float(css.FloatLeft), width(100), height(100))),
			el(sty(clearing(css.ClearBoth)),
				el(sty(fit), el(sty(pct, margin(0, 0, m, 0))))),
		)), page(200, 200))
		if got := rect(kid(t, rb, 0, 0)); got[3] != 100 {
			t.Errorf("margin %v: inline-block height %v, want 100", m, got[3])
		}
	}
}

func TestClearance_PendingFloatNeedsClearance(t *testing.T) {
	// CSS 2.1, 9.5.2: a float placed under margins still pending moves
	// down with them, so a box whose top margin collapses with them
	// cannot get past it without clearance: the parent and the float
	// stay at the top and the box goes below the float.
	rb := boxes(t, doc(el(sty(width(100)),
		el(sty(float(css.FloatLeft), width(50), height(64))),
		el(sty(clearing(css.ClearLeft), margin(64, 0, 64, 0), height(64))),
	)), page(200, 400))
	if got := rect(kid(t, rb, 0, 0)); got[1] != 0 {
		t.Errorf("parent at y %v, want 0", got[1])
	}
	if got := rect(kid(t, rb, 0, 0, 1)); got[1] != 64 {
		t.Errorf("cleared box at y %v, want 64: below the float", got[1])
	}
}

func TestClearance_EmptyClearedBoxSpendsItsMargins(t *testing.T) {
	// CSS 2.1, 9.5.2 and 8.3.1: clearance puts the top border edge of an
	// empty box at the float's bottom with its own margins above it; a
	// following margin no larger than those moves nothing, and the
	// margins of empty content under clearance collapse into it.
	rb := boxes(t, doc(el(sty(width(100)),
		el(sty(float(css.FloatLeft), width(50), height(64))),
		el(sty(clearing(css.ClearLeft), margin(32, 0, 32, 0)), el(sty(margin(16, 0, 16, 0)))),
		el(sty(margin(16, 0, 0, 0), height(10))),
	)), page(200, 400))
	if got := rect(kid(t, rb, 0, 0, 2)); got[1] != 64 {
		t.Errorf("box after the cleared box at y %v, want 64", got[1])
	}
}

func TestMargins_ZeroHeightBoxCollapsesThrough(t *testing.T) {
	// CSS 2.1, 8.3.1: an empty box of zero height has adjoining top and
	// bottom margins, which collapse with its parent's top margin and
	// its next sibling's: the sibling sits at the parent's top.
	rb := boxes(t, doc(el(sty(width(100), border(1, css.Solid, black)),
		el(sty(height(20)),
			el(sty(height(0), margin(40, 0, 40, 0))),
			el(sty(height(20), margin(40, 0, 0, 0)))),
	)), page(200, 200))
	parent, sib := rect(kid(t, rb, 0, 0, 0)), rect(kid(t, rb, 0, 0, 0, 1))
	if parent[1] != 41 || sib[1] != 41 {
		t.Errorf("parent at y %v, sibling at %v, want both 41", parent[1], sib[1])
	}
}

func TestFloats_PendingFloatsMoveTogether(t *testing.T) {
	// CSS 2.1, 9.5.1 and 9.5.2: floats placed while the parent's top
	// waits on collapsing margins sit at its top wherever it lands; a
	// float that clears an earlier one meets it there, so with a
	// negative margin as tall as that float the two coincide.
	rb := boxes(t, doc(el(sty(width(200), height(200)),
		el(sty(float(css.FloatRight), width(50), height(50))),
		el(sty(margin(50, 0, 50, 0))),
		el(sty(float(css.FloatRight), clearing(css.ClearRight), width(50), height(50), margin(-50, 0, 0, 0))),
	)), page(300, 300))
	a, b := rect(kid(t, rb, 0, 0, 0)), rect(kid(t, rb, 0, 0, 2))
	if a[1] != 50 || b[1] != 50 {
		t.Errorf("floats at y %v and %v, want both 50", a[1], b[1])
	}
}

func TestFormattingContextBesideFloat_StretchFillsMarginBoxSpace(t *testing.T) {
	// CSS Sizing 4, 6.1: a stretch width fills the space the margin box is
	// laid out in, which beside a float a positive margin may lie over:
	// the border box fills the 100 beside the float.
	stretch := func(s *css.Style) { s.Width = css.Dimension{Kind: css.Stretch} }
	rb := boxes(t, doc(el(sty(width(200)),
		el(sty(float(css.FloatLeft), width(100), height(50))),
		el(sty(hidden, stretch, height(25), margin(0, 0, 0, 10), padding(0, 10, 0, 10))),
	)), page(300, 300))
	if got := rect(kid(t, rb, 0, 0, 1)); got[0] != 100 || got[2] != 100 {
		t.Errorf("box at x %v width %v, want 100 and 100", got[0], got[2])
	}
}

func TestFloats_PercentHeightInAnonymousBlock(t *testing.T) {
	// CSS 2.1, 9.2.1.1: a percentage that would refer to an anonymous
	// block refers to its closest non-anonymous ancestor. A float among
	// white space after a block sits in an anonymous block; its 50%
	// height resolves against the 200 tall parent.
	half := func(s *css.Style) { s.Height = css.Dimension{Kind: css.Percent, Value: 0.5} }
	rb := boxes(t, doc(el(sty(width(100), height(200)),
		el(sty(height(10))),
		el(sty(float(css.FloatLeft), width(50), half)),
		txt(sty(), " "),
	)), page(300, 300))
	var f *box
	for _, k := range kid(t, rb, 0, 0).kids {
		for _, kk := range append([]*box{k}, k.kids...) {
			if kk.float != css.FloatNone {
				f = kk
			}
		}
	}
	if f == nil {
		t.Fatal("no float")
	}
	if got := rect(f); got[3] != 100 {
		t.Errorf("float height %v, want 100", got[3])
	}
}
