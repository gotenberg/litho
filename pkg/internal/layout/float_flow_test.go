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
			[]int{0, 0, 1, 0}, [2]float64{0, 30},
		},
		{
			// 9.5.2: clear on a float puts it below the floats it clears.
			"cleared float",
			doc(el(sty(float(css.FloatRight), width(10), height(10))),
				el(sty(float(css.FloatRight), clearing(css.ClearRight), width(40), height(10)))),
			[]int{0, 1}, [2]float64{60, 10},
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
