package layout

import (
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestBlockWidths_Resolve(t *testing.T) {
	tests := []struct {
		name string
		mods []func(*css.Style)
		want [4]float64
	}{
		{"auto width fills", nil, [4]float64{0, 0, 100, 0}},
		{"margins and padding", []func(*css.Style){margin(0, 10, 0, 5), padding(0, 3, 0, 2)}, [4]float64{5, 0, 85, 0}},
		{"auto margins centre", []func(*css.Style){width(40), margin(0, auto)}, [4]float64{30, 0, 40, 0}},
		{"auto left margin", []func(*css.Style){width(40), margin(0, 0, 0, auto)}, [4]float64{60, 0, 40, 0}},
		{"over-constrained drops right margin", []func(*css.Style){width(40), margin(0, 50, 0, 20)}, [4]float64{20, 0, 40, 0}},
		{"too wide ignores auto", []func(*css.Style){width(140), margin(0, auto)}, [4]float64{0, 0, 140, 0}},
		{"percentage", []func(*css.Style){func(s *css.Style) { s.Width = css.Dimension{Kind: css.Percent, Value: 0.25} }}, [4]float64{0, 0, 25, 0}},
		{"max-width", []func(*css.Style){func(s *css.Style) { s.MaxWidth = css.Pts(px(30)) }, margin(0, auto)}, [4]float64{35, 0, 30, 0}},
		{"min-width wins over max", []func(*css.Style){width(10), func(s *css.Style) {
			s.MinWidth, s.MaxWidth = css.Pts(px(50)), css.Pts(px(20))
		}}, [4]float64{0, 0, 50, 0}},
		{"border-box", []func(*css.Style){width(40), padding(5), func(s *css.Style) { s.BoxSizing = css.BorderBox }}, [4]float64{0, 0, 40, 10}},
		{"content-box", []func(*css.Style){width(40), padding(5)}, [4]float64{0, 0, 50, 10}},
		{"height", []func(*css.Style){height(12), border(1, css.Solid, black)}, [4]float64{0, 0, 100, 14}},
		{"min-height", []func(*css.Style){func(s *css.Style) { s.MinHeight = css.Pts(px(7)) }}, [4]float64{0, 0, 100, 7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := boxes(t, doc(el(sty(tt.mods...))), page(100, 100))
			if got := rect(kid(t, rb, 0, 0)); got != tt.want {
				t.Errorf("border box %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBlockWidths_RTLOverConstrained(t *testing.T) {
	rb := boxes(t, el(sty(func(s *css.Style) { s.Direction = css.RTL }),
		el(sty(func(s *css.Style) { s.Direction = css.RTL }, width(40), margin(0, 20, 0, 50)))), page(100, 100))
	if got := rect(kid(t, rb, 0)); got[0] != 40 {
		t.Errorf("x = %v, want 40: the left margin gives way in a right-to-left block", got[0])
	}
}

func TestMarginCollapse_Forms(t *testing.T) {
	box10 := func(mods ...func(*css.Style)) *Node {
		return el(sty(append([]func(*css.Style){height(10)}, mods...)...))
	}
	tests := []struct {
		name string
		root *Node
		path []int
		want float64 // border box top in px
	}{
		{"siblings take the larger", doc(box10(margin(0, 0, 20, 0)), box10(margin(30, 0, 0, 0))), []int{0, 1}, 40},
		{"negative and positive add", doc(box10(margin(0, 0, 20, 0)), box10(margin(-5, 0, 0, 0))), []int{0, 1}, 25},
		{"two negatives take the most negative", doc(box10(margin(0, 0, -4, 0)), box10(margin(-9, 0, 0, 0))), []int{0, 1}, 1},
		{"parent and first child", doc(el(sty(margin(10, 0)), box10(margin(25, 0)))), []int{0, 0}, 25},
		{"parent with padding keeps them apart", doc(el(sty(margin(10, 0), padding(1, 0)), box10(margin(25, 0)))), []int{0, 0, 0}, 36},
		{"parent with border keeps them apart", doc(el(sty(margin(10, 0), border(2, css.Solid, black)), box10(margin(5, 0)))), []int{0, 0, 0}, 17},
		{"last child and parent bottom", doc(el(sty(margin(0, 0, 10, 0)), box10(margin(0, 0, 30, 0))), box10()), []int{0, 1}, 40},
		{"empty block collapses through", doc(box10(margin(0, 0, 10, 0)), el(sty(margin(15, 0, 20, 0))), box10(margin(5, 0, 0, 0))), []int{0, 2}, 30},
		{"bfc root keeps its child's margin", doc(el(sty(func(s *css.Style) { s.Overflow = css.OverflowHidden }), box10(margin(8, 0)))), []int{0, 0, 0}, 8},
		{"root margin", el(sty(margin(6, 0)), el(sty(height(4)))), []int{0}, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := boxes(t, tt.root, page(100, 200))
			b := kid(t, rb, tt.path...)
			if got := rect(b)[1]; got != tt.want {
				t.Errorf("top %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMarginCollapse_ParentHeight(t *testing.T) {
	// The child's margins collapse through its parent, which is as tall as
	// the child alone.
	rb := boxes(t, doc(el(sty(), el(sty(height(10), margin(7, 0))))), page(100, 100))
	p := kid(t, rb, 0, 0)
	if got := rect(p); got != [4]float64{0, 7, 100, 10} {
		t.Errorf("parent %v, want [0 7 100 10]", got)
	}
}

func TestClearance_PlacesBelowFloat(t *testing.T) {
	rb := boxes(t, doc(
		el(sty(float(css.FloatLeft), width(20), height(30))),
		el(sty(height(5), func(s *css.Style) { s.Clear = css.ClearLeft })),
	), page(100, 100))
	if got := rect(kid(t, rb, 0, 1)); got[1] != 30 {
		t.Errorf("cleared top %v, want 30", got[1])
	}
}

func TestFloats_Placement(t *testing.T) {
	f := func(side css.Float, w, h float64) *Node { return el(sty(float(side), width(w), height(h))) }
	tests := []struct {
		name  string
		kids  []*Node
		index int
		want  [2]float64
	}{
		{"left float", []*Node{f(css.FloatLeft, 20, 10)}, 0, [2]float64{0, 0}},
		{"right float", []*Node{f(css.FloatRight, 20, 10)}, 0, [2]float64{80, 0}},
		{"second left float beside the first", []*Node{f(css.FloatLeft, 20, 10), f(css.FloatLeft, 30, 10)}, 1, [2]float64{20, 0}},
		{"too wide moves below", []*Node{f(css.FloatLeft, 60, 10), f(css.FloatLeft, 60, 10)}, 1, [2]float64{0, 10}},
		{"right beside left", []*Node{f(css.FloatLeft, 60, 10), f(css.FloatRight, 30, 10)}, 1, [2]float64{70, 0}},
		{"no higher than an earlier float", []*Node{f(css.FloatLeft, 60, 20), f(css.FloatRight, 60, 10), f(css.FloatLeft, 10, 10)}, 2, [2]float64{0, 20}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := boxes(t, doc(tt.kids...), page(100, 100))
			got := rect(kid(t, rb, 0, tt.index))
			if [2]float64{got[0], got[1]} != tt.want {
				t.Errorf("float at %v, want %v", got[:2], tt.want)
			}
		})
	}
}

func TestFloats_ShrinkToFit(t *testing.T) {
	rb := boxes(t, doc(el(sty(float(css.FloatLeft), padding(0, 2)), txt(sty(), "ab abc"))), page(100, 100))
	if got := rect(kid(t, rb, 0, 0)); got[2] != 64 || got[3] != 10 {
		t.Errorf("float %v, want 64 by 10: the max-content width of its text", got)
	}
	rb = boxes(t, doc(el(sty(float(css.FloatLeft)), txt(sty(), "ab abc"))), page(45, 100))
	if got := rect(kid(t, rb, 0, 0)); got[2] != 45 || got[3] != 20 {
		t.Errorf("float %v, want 45 by 20: the available width", got)
	}
	rb = boxes(t, doc(el(sty(float(css.FloatLeft)), txt(sty(), "ab abc"))), page(20, 100))
	if got := rect(kid(t, rb, 0, 0)); got[2] != 30 {
		t.Errorf("float %v, want 30 wide: its min-content width", got)
	}
}

func TestFloats_BFCShrinksBeside(t *testing.T) {
	rb := boxes(t, doc(
		el(sty(float(css.FloatLeft), width(30), height(20))),
		el(sty(height(10), func(s *css.Style) { s.Overflow = css.OverflowHidden })),
	), page(100, 100))
	if got := rect(kid(t, rb, 0, 1)); got != [4]float64{30, 0, 70, 10} {
		t.Errorf("BFC %v, want [30 0 70 10]", got)
	}
	rb = boxes(t, doc(
		el(sty(float(css.FloatLeft), width(30), height(20))),
		el(sty(width(80), height(10), func(s *css.Style) { s.Overflow = css.OverflowHidden })),
	), page(100, 100))
	if got := rect(kid(t, rb, 0, 1)); got != [4]float64{0, 20, 80, 10} {
		t.Errorf("BFC %v, want [0 20 80 10]: too wide beside the float", got)
	}
}

func TestFloats_LinesShortenBeside(t *testing.T) {
	rb := boxes(t, doc(
		el(sty(float(css.FloatLeft), width(30), height(15))),
		el(sty(), txt(sty(), "aa bb cc")),
	), page(70, 100))
	p := kid(t, rb, 0, 1)
	if len(p.lines) != 3 {
		t.Fatalf("%d lines, want 3", len(p.lines))
	}
	for i, wantX := range []float64{30, 30, 0} {
		if got := p.lines[i].x / css.Pt; !near(got, wantX) {
			t.Errorf("line %d at x %v, want %v", i, got, wantX)
		}
	}
}

func TestFloats_ContainedByBFCHeight(t *testing.T) {
	rb := boxes(t, doc(el(sty(func(s *css.Style) { s.Overflow = css.OverflowHidden }), el(sty(float(css.FloatLeft), width(10), height(25))))), page(100, 100))
	if got := rect(kid(t, rb, 0, 0))[3]; got != 25 {
		t.Errorf("BFC height %v, want 25: it holds its float (CSS 2.1, 10.6.7)", got)
	}
}

func TestRelative_PaintOffset(t *testing.T) {
	root := doc(el(sty(width(2), height(2), bg(red), func(s *css.Style) {
		s.Position = css.Relative
		s.Left, s.Top = css.Pts(px(1)), css.Pts(px(1))
	})))
	res := run(t, root, page(4, 4))
	assertPixels(t, res, 1, `
		____
		_RR_
		_RR_
		____`)
}

func TestInlineBlock_ShrinkAndBaseline(t *testing.T) {
	ib := el(sty(display(css.InlineBlock), padding(0, 1)), txt(sty(), "ab"))
	rb := boxes(t, doc(el(sty(), txt(sty(), "x "), ib)), page(100, 100))
	b := kid(t, rb, 0, 0, 1)
	if got := rect(b); got != [4]float64{20, 0, 22, 10} {
		t.Errorf("inline block %v, want [20 0 22 10]", got)
	}
}

func TestReplaced_Sizing(t *testing.T) {
	img := func(mods ...func(*css.Style)) *Node {
		return &Node{Kind: Replaced, Style: sty(mods...), Picture: pic(color.NRGBA{R: 255, A: 255}, 40, 20)}
	}
	tests := []struct {
		name string
		mods []func(*css.Style)
		want [2]float64
	}{
		{"intrinsic", nil, [2]float64{40, 20}},
		{"width keeps ratio", []func(*css.Style){width(20)}, [2]float64{20, 10}},
		{"height keeps ratio", []func(*css.Style){height(40)}, [2]float64{80, 40}},
		{"both", []func(*css.Style){width(10), height(30)}, [2]float64{10, 30}},
		{"max-width", []func(*css.Style){func(s *css.Style) { s.MaxWidth = css.Pts(px(20)) }}, [2]float64{20, 10}},
		{"min-height", []func(*css.Style){func(s *css.Style) { s.MinHeight = css.Pts(px(30)) }}, [2]float64{60, 30}},
		{"max-width and max-height", []func(*css.Style){func(s *css.Style) {
			s.MaxWidth, s.MaxHeight = css.Pts(px(30)), css.Pts(px(10))
		}}, [2]float64{20, 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := boxes(t, doc(img(tt.mods...)), page(100, 100))
			b := kid(t, rb, 0, 0)
			if got := [2]float64{b.w / css.Pt, b.h / css.Pt}; !near(got[0], tt.want[0]) || !near(got[1], tt.want[1]) {
				t.Errorf("size %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReplaced_Draws(t *testing.T) {
	root := doc(&Node{Kind: Replaced, Style: sty(margin(1)), Picture: pic(color.NRGBA{B: 255, A: 255}, 3, 2)})
	res := run(t, root, page(5, 4))
	assertPixels(t, res, 1, `
		_____
		_BBB_
		_BBB_
		_____`)
}

func TestFloats_ShrinkToFitBlockChildren(t *testing.T) {
	rb := boxes(t, doc(el(sty(float(css.FloatLeft)), el(sty(), txt(sty(), "aaaa")), el(sty(margin(0, 3)), txt(sty(), "bb cc")))), page(100, 100))
	if got := rect(kid(t, rb, 0, 0)); got[2] != 56 {
		t.Errorf("float %v, want 56 wide: the widest child with its margins", got)
	}
}

func TestInlineBlock_BaselineFromNestedBlock(t *testing.T) {
	ib := el(sty(display(css.InlineBlock)), el(sty(), txt(sty(), "a")), el(sty(), txt(sty(), "b")))
	rb := boxes(t, doc(el(sty(), txt(sty(), "x"), ib)), page(100, 100))
	p := kid(t, rb, 0, 0)
	if len(p.lines) != 1 || !near(p.lines[0].h/css.Pt, 20) || !near(p.lines[0].baseline/css.Pt, 18) {
		t.Errorf("line height %v baseline %v, want 20 and 18: the inline block's last line on the baseline", p.lines[0].h/css.Pt, p.lines[0].baseline/css.Pt)
	}
}

func TestReplaced_ConstraintTable(t *testing.T) {
	inf := 1e9
	tests := []struct {
		w, h, minW, maxW, minH, maxH float64
		want                         [2]float64
	}{
		{40, 20, 0, inf, 0, inf, [2]float64{40, 20}},
		{40, 20, 0, 20, 0, 5, [2]float64{10, 5}},
		{40, 20, 0, 10, 0, 50, [2]float64{10, 5}},
		{40, 20, 80, inf, 50, inf, [2]float64{100, 50}},
		{40, 20, 100, inf, 30, inf, [2]float64{100, 50}},
		{40, 20, 50, inf, 0, 10, [2]float64{50, 10}},
		{40, 20, 0, 30, 25, inf, [2]float64{30, 25}},
		{40, 20, 0, inf, 0, 10, [2]float64{20, 10}},
		{40, 20, 0, inf, 30, inf, [2]float64{60, 30}},
	}
	for _, tt := range tests {
		w, h := constrainRatio(tt.w, tt.h, tt.minW, tt.maxW, tt.minH, tt.maxH)
		if !near(w, tt.want[0]) || !near(h, tt.want[1]) {
			t.Errorf("constrainRatio(%+v) = %v, %v, want %v", tt, w, h, tt.want)
		}
	}
}

func TestReplaced_InlinePercentageHeight(t *testing.T) {
	// An inline replaced box resolves a percentage height against a
	// definite containing block height, and as auto against an indefinite
	// one (CSS 2.1, 10.5).
	half := func(s *css.Style) { s.Height = pct(0.5) }
	for _, tc := range []struct {
		parent []func(*css.Style)
		want   [2]float64
	}{{[]func(*css.Style){height(50)}, [2]float64{50, 25}}, {nil, [2]float64{40, 20}}} {
		img := &Node{Kind: Replaced, Style: inlineSty(half), Picture: pic(color.NRGBA{R: 255, A: 255}, 40, 20)}
		rb := boxes(t, doc(el(sty(tc.parent...), img)), page(100, 100))
		var found *box
		var walk func(b *box)
		walk = func(b *box) {
			if b.kind == kindReplaced {
				found = b
			}
			for _, k := range b.kids {
				walk(k)
			}
		}
		walk(rb)
		if found == nil {
			t.Fatal("no replaced box")
		}
		if got := [2]float64{found.w / css.Pt, found.h / css.Pt}; !near(got[0], tc.want[0]) || !near(got[1], tc.want[1]) {
			t.Errorf("parent %d mods: size %v, want %v", len(tc.parent), got, tc.want)
		}
	}
}
