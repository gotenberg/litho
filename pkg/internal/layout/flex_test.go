package layout

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// Style modifiers of flex layout.

func flexBox(mods ...func(*css.Style)) *css.Style {
	return sty(append([]func(*css.Style){display(css.Flex)}, mods...)...)
}

func dir(d css.FlexDirection) func(*css.Style) { return func(s *css.Style) { s.FlexDirection = d } }
func wrapMode(w css.FlexWrap) func(*css.Style) { return func(s *css.Style) { s.FlexWrap = w } }
func flexF(grow, shrink, basis float64) func(*css.Style) {
	return func(s *css.Style) {
		s.FlexGrow, s.FlexShrink = grow, shrink
		s.FlexBasis = css.Dimension{Kind: css.Auto}
		if basis >= 0 {
			s.FlexBasis = css.Pts(px(basis))
		}
	}
}
func justify(a css.BoxAlign) func(*css.Style)    { return func(s *css.Style) { s.JustifyContent = a } }
func alignItems(a css.BoxAlign) func(*css.Style) { return func(s *css.Style) { s.AlignItems = a } }
func alignSelf(a css.BoxAlign) func(*css.Style)  { return func(s *css.Style) { s.AlignSelf = a } }
func alignContent(a css.BoxAlign) func(*css.Style) {
	return func(s *css.Style) { s.AlignContent = a }
}
func gaps(row, col float64) func(*css.Style) {
	return func(s *css.Style) { s.RowGap, s.ColumnGap = css.Pts(px(row)), css.Pts(px(col)) }
}
func order(n int) func(*css.Style) { return func(s *css.Style) { s.Order = n } }
func rtl(s *css.Style)             { s.Direction = css.RTL }

// item returns a block of w by h px, auto for a NaN size.
func item(w, h float64, mods ...func(*css.Style)) *Node {
	ms := []func(*css.Style){}
	if w == w {
		ms = append(ms, width(w))
	}
	if h == h {
		ms = append(ms, height(h))
	}
	return el(sty(append(ms, mods...)...))
}

// flexRects lays root out on a 100 by 100 px page and returns the border
// boxes of the kids of the box at path.
func flexRects(t *testing.T, root *Node, path ...int) [][4]float64 {
	t.Helper()
	c := kid(t, boxes(t, root, page(100, 100)), path...)
	var out [][4]float64
	for _, k := range c.kids {
		out = append(out, rect(k))
	}
	return out
}

func eqRects(a, b [][4]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFlex_RowLayout(t *testing.T) {
	tests := []struct {
		name  string
		cont  []func(*css.Style)
		items []*Node
		want  [][4]float64
	}{
		{"items sit side by side, stretched", nil,
			[]*Node{item(10, auto), item(20, 5)},
			[][4]float64{{0, 0, 10, 5}, {10, 0, 20, 5}}},
		{"flex-grow shares the free space", nil,
			[]*Node{item(10, 5, flexF(1, 1, -1)), item(10, 5, flexF(3, 1, -1))},
			[][4]float64{{0, 0, 30, 5}, {30, 0, 70, 5}}},
		{"flex-shrink weighs by the base size", nil,
			[]*Node{item(100, 5, flexF(0, 1, -1), func(s *css.Style) { s.MinWidthAuto = false }), item(50, 5, flexF(0, 1, -1), func(s *css.Style) { s.MinWidthAuto = false })},
			[][4]float64{{0, 0, 66.667, 5}, {66.667, 0, 33.333, 5}}},
		{"max-width freezes a growing item", nil,
			[]*Node{item(auto, 5, flexF(1, 1, 0), func(s *css.Style) { s.MaxWidth = css.Pts(px(20)) }), item(auto, 5, flexF(1, 1, 0))},
			[][4]float64{{0, 0, 20, 5}, {20, 0, 80, 5}}},
		{"justify-content: center", []func(*css.Style){justify(css.BoxCenter)},
			[]*Node{item(20, 5), item(20, 5)},
			[][4]float64{{30, 0, 20, 5}, {50, 0, 20, 5}}},
		{"justify-content: space-between", []func(*css.Style){justify(css.BoxSpaceBetween)},
			[]*Node{item(20, 5), item(20, 5), item(20, 5)},
			[][4]float64{{0, 0, 20, 5}, {40, 0, 20, 5}, {80, 0, 20, 5}}},
		{"justify-content: space-evenly", []func(*css.Style){justify(css.BoxSpaceEvenly)},
			[]*Node{item(20, 5), item(20, 5)},
			[][4]float64{{20, 0, 20, 5}, {60, 0, 20, 5}}},
		{"auto margins take the free space", nil,
			[]*Node{item(20, 5), item(20, 5, margin(0, 0, 0, auto))},
			[][4]float64{{0, 0, 20, 5}, {80, 0, 20, 5}}},
		{"row-reverse starts at the right", []func(*css.Style){dir(css.FlexRowReverse)},
			[]*Node{item(20, 5), item(30, 5)},
			[][4]float64{{80, 0, 20, 5}, {50, 0, 30, 5}}},
		{"rtl row starts at the right", []func(*css.Style){rtl},
			[]*Node{item(20, 5), item(30, 5)},
			[][4]float64{{80, 0, 20, 5}, {50, 0, 30, 5}}},
		{"gap separates items", []func(*css.Style){gaps(0, 10)},
			[]*Node{item(20, 5), item(20, 5)},
			[][4]float64{{0, 0, 20, 5}, {30, 0, 20, 5}}},
		{"align-items: center in a definite height", []func(*css.Style){height(20), alignItems(css.BoxCenter)},
			[]*Node{item(20, 10), item(20, 4)},
			[][4]float64{{0, 5, 20, 10}, {20, 8, 20, 4}}},
		{"align-self: flex-end", []func(*css.Style){height(20)},
			[]*Node{item(20, 10, alignSelf(css.BoxFlexEnd)), item(20, auto)},
			[][4]float64{{0, 10, 20, 10}, {20, 0, 20, 20}}},
		{"order sorts the items", nil,
			[]*Node{item(20, 5, order(2)), item(30, 5, order(1))},
			[][4]float64{{0, 0, 30, 5}, {30, 0, 20, 5}}},
		{"wrap breaks lines", []func(*css.Style){wrapMode(css.Wrap), gaps(2, 0)},
			[]*Node{item(60, 5), item(60, 10), item(30, 4)},
			[][4]float64{{0, 0, 60, 5}, {0, 7, 60, 10}, {60, 7, 30, 4}}},
		{"wrap-reverse stacks lines upwards", []func(*css.Style){wrapMode(css.WrapReverse), height(30), alignContent(css.BoxFlexStart)},
			[]*Node{item(60, 5), item(60, 10)},
			[][4]float64{{0, 25, 60, 5}, {0, 15, 60, 10}}},
		{"align-content: space-between", []func(*css.Style){wrapMode(css.Wrap), height(30), alignContent(css.BoxSpaceBetween)},
			[]*Node{item(60, 5), item(60, 10)},
			[][4]float64{{0, 0, 60, 5}, {0, 20, 60, 10}}},
		{"align-content: normal stretches the lines", []func(*css.Style){wrapMode(css.Wrap), height(30)},
			[]*Node{item(60, auto), item(60, auto)},
			[][4]float64{{0, 0, 60, 15}, {0, 15, 60, 15}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := flexRects(t, doc(el(flexBox(tt.cont...), tt.items...)), 0, 0)
			if !eqRects(got, tt.want) {
				t.Errorf("items %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFlex_ColumnLayout(t *testing.T) {
	col := dir(css.FlexColumn)
	tests := []struct {
		name  string
		cont  []func(*css.Style)
		items []*Node
		want  [][4]float64
	}{
		{"items stack, stretched across", []func(*css.Style){col},
			[]*Node{item(auto, 10), item(30, 5)},
			[][4]float64{{0, 0, 100, 10}, {0, 10, 30, 5}}},
		{"flex-grow fills a definite height", []func(*css.Style){col, height(50)},
			[]*Node{item(auto, 10, flexF(1, 1, -1)), item(auto, 10)},
			[][4]float64{{0, 0, 100, 40}, {0, 40, 100, 10}}},
		{"flex: 1 in an auto height keeps the content height", []func(*css.Style){col},
			[]*Node{item(auto, 10, func(s *css.Style) { s.FlexGrow, s.FlexBasis = 1, css.Dimension{Kind: css.Percent} })},
			[][4]float64{{0, 0, 100, 10}}},
		{"max-height bounds an auto height column", []func(*css.Style){col, func(s *css.Style) { s.MaxHeight = css.Pts(px(20)) }},
			[]*Node{item(auto, 15, func(s *css.Style) { s.MinHeightAuto = false }), item(auto, 15, func(s *css.Style) { s.MinHeightAuto = false })},
			[][4]float64{{0, 0, 100, 10}, {0, 10, 100, 10}}},
		{"column-reverse starts at the bottom", []func(*css.Style){dir(css.FlexColumnReverse), height(50)},
			[]*Node{item(auto, 10), item(auto, 20)},
			[][4]float64{{0, 40, 100, 10}, {0, 20, 100, 20}}},
		{"align-items: center centres across", []func(*css.Style){col, alignItems(css.BoxCenter)},
			[]*Node{item(40, 10)},
			[][4]float64{{30, 0, 40, 10}}},
		{"justify-content: flex-end", []func(*css.Style){col, height(50), justify(css.BoxFlexEnd)},
			[]*Node{item(auto, 10)},
			[][4]float64{{0, 40, 100, 10}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := flexRects(t, doc(el(flexBox(tt.cont...), tt.items...)), 0, 0)
			if !eqRects(got, tt.want) {
				t.Errorf("items %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFlex_ContainerHeight(t *testing.T) {
	tests := []struct {
		name string
		root *Node
		want float64
	}{
		{"row takes its tallest item", el(flexBox(), item(10, 5), item(10, 12)), 12},
		{"wrapped lines add up", el(flexBox(wrapMode(css.Wrap), gaps(3, 0)), item(60, 5), item(60, 12)), 20},
		{"column sums its items", el(flexBox(dir(css.FlexColumn), gaps(4, 0)), item(10, 5), item(10, 12)), 21},
		{"min-height stretches the single line", el(flexBox(func(s *css.Style) { s.MinHeight = css.Pts(px(30)) }), item(10, auto)), 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := boxes(t, doc(tt.root), page(100, 100))
			c := kid(t, rb, 0, 0)
			if got := rect(c)[3]; got != tt.want {
				t.Errorf("height %v, want %v", got, tt.want)
			}
			if tt.name == "min-height stretches the single line" {
				if got := rect(c.kids[0])[3]; got != 30 {
					t.Errorf("stretched item height %v, want 30", got)
				}
			}
		})
	}
}

func TestFlex_AutoMinimumSize(t *testing.T) {
	// A shrinking item does not go below its min-content width, the
	// longest word, unless min-width says otherwise (CSS Flexbox 1, 4.5).
	word := func(mods ...func(*css.Style)) *Node {
		return el(sty(append([]func(*css.Style){width(60)}, mods...)...), txt(sty(), "aaaa"))
	}
	got := flexRects(t, doc(el(flexBox(width(50)), word(), word())), 0, 0)
	if want := [][4]float64{{0, 0, 40, 10}, {40, 0, 40, 10}}; !eqRects(got, want) {
		t.Errorf("auto minimum %v, want %v", got, want)
	}
	got = flexRects(t, doc(el(flexBox(width(50)), word(func(s *css.Style) { s.MinWidthAuto = false }), word(func(s *css.Style) { s.MinWidthAuto = false }))), 0, 0)
	if want := [][4]float64{{0, 0, 25, 10}, {25, 0, 25, 10}}; !eqRects(got, want) {
		t.Errorf("min-width: 0 %v, want %v", got, want)
	}
}

func TestFlex_AnonymousItems(t *testing.T) {
	// Text runs become anonymous items; white space between items does
	// not (CSS Flexbox 1, 4).
	rb := boxes(t, doc(el(flexBox(), txt(sty(), " "), item(10, 10), txt(sty(), " ab "), item(10, 10), txt(sty(), "\n"))), page(100, 100))
	c := kid(t, rb, 0, 0)
	if len(c.kids) != 3 || !c.kids[1].anon || !c.kids[1].flexItem {
		t.Fatalf("kids: %d, want the two items around an anonymous one", len(c.kids))
	}
	if got := rect(c.kids[1]); got != [4]float64{10, 0, 20, 10} {
		t.Errorf("anonymous item %v", got)
	}
}

func TestFlex_Baseline(t *testing.T) {
	// align-items: baseline lines the first lines up; an inline flex
	// container sits on its first baseline.
	small := el(sty(font(10)), txt(sty(font(10)), "a"))
	big := el(sty(font(20)), txt(sty(font(20)), "b"))
	rb := boxes(t, doc(el(flexBox(alignItems(css.BoxBaseline)), small, big)), page(100, 100))
	c := kid(t, rb, 0, 0)
	if got := rect(c.kids[0]); got != [4]float64{0, 8, 10, 10} {
		t.Errorf("small item %v, want its baseline at 16", got)
	}
	if fy, ok := flexBaseline(c, false); !ok || !near(fy-c.y, px(16)) {
		t.Errorf("container baseline %v %v, want 16px", (fy-c.y)/css.Pt, ok)
	}
}

func TestFlex_StaticPosition(t *testing.T) {
	abs := el(sty(width(10), height(10), func(s *css.Style) { s.Position = css.Absolute }))
	rb := boxes(t, doc(el(flexBox(height(40), justify(css.BoxCenter), alignItems(css.BoxFlexEnd), func(s *css.Style) { s.Position = css.Relative }), abs)), page(100, 100))
	if got := rect(kid(t, rb, 0, 0, 0)); got != [4]float64{45, 30, 10, 10} {
		t.Errorf("absolutely positioned child %v, want centred at the bottom", got)
	}
}

func TestFlex_Intrinsic(t *testing.T) {
	// A floated row container shrinks to the sum of its items; a column
	// to the widest.
	row := el(flexBox(float(css.FloatLeft), gaps(0, 5)), item(10, 5), item(20, 5))
	colm := el(flexBox(float(css.FloatLeft), dir(css.FlexColumn)), item(10, 5), item(20, 5))
	rb := boxes(t, doc(row, colm), page(100, 100))
	if got := rect(kid(t, rb, 0, 0))[2]; got != 35 {
		t.Errorf("row width %v, want 35", got)
	}
	if got := rect(kid(t, rb, 0, 1))[2]; got != 20 {
		t.Errorf("column width %v, want 20", got)
	}
}

func TestFlex_InlineFlex(t *testing.T) {
	inl := el(sty(display(css.InlineFlex)), item(10, 5), item(20, 5))
	rb := boxes(t, doc(el(sty(), txt(sty(), "a"), inl)), page(100, 100))
	c := kid(t, rb, 0, 0)
	var flex *box
	for _, l := range c.lines {
		for _, it := range l.items {
			if it.atomic != nil {
				flex = it.atomic
			}
		}
	}
	if flex == nil || flex.flex == nil {
		t.Fatal("no inline flex container on the line")
	}
	if got := rect(flex); got[2] != 30 || got[0] != 10 {
		t.Errorf("inline flex %v, want 30 wide after the a", got)
	}
}

func TestFlex_RowBreaksBetweenLines(t *testing.T) {
	// Two wrapped lines of 30px items that avoid breaks inside on 50px
	// pages: the second line moves to page 2 whole.
	avoid := func(s *css.Style) { s.BreakInside = css.InsideAvoid }
	res := run(t, doc(el(flexBox(wrapMode(css.Wrap)), item(60, 30, bg(red), avoid), item(60, 30, bg(blue), avoid))), page(100, 50))
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	rb := boxes(t, doc(el(flexBox(wrapMode(css.Wrap)), item(60, 30, avoid), item(60, 30, avoid))), page(100, 50))
	if got := rect(kid(t, rb, 0, 0, 1)); got[1] != 50 {
		t.Errorf("second line top %v, want 50", got[1])
	}
}

func TestFlex_RowItemsBreakInside(t *testing.T) {
	// A row of text items taller than a page breaks inside its items,
	// line by line.
	text := func() *Node {
		return el(sty(), txt(sty(), "a a a a a a a a"))
	}
	res, err := Layout(context.Background(), doc(el(flexBox(), text(), text())), ahem(t), Options{Page: page(20, 50)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 2 {
		t.Errorf("%d pages, want 2", len(res.Pages))
	}
}

func TestFlex_ColumnForcedBreak(t *testing.T) {
	rb := boxes(t, doc(el(flexBox(dir(css.FlexColumn)), item(auto, 10), item(auto, 10, func(s *css.Style) { s.BreakBefore = css.Page }))), page(100, 50))
	if got := rect(kid(t, rb, 0, 0, 1)); got[1] != 50 {
		t.Errorf("item after break-before: page at %v, want 50", got[1])
	}
}

func TestFlexDraw_PaintOrder(t *testing.T) {
	// Items paint in order-modified document order: the later one in
	// order paints over the earlier where negative margins overlap them.
	a := item(4, 3, bg(red), order(2), margin(0, 0, 0, -2))
	b := item(4, 3, bg(blue), order(1))
	assertPixels(t, run(t, doc(el(flexBox(width(6)), a, b)), page(6, 3)), 1, `
		BBRRRR
		BBRRRR
		BBRRRR`)
}

func TestFlexDraw_Stretch(t *testing.T) {
	assertPixels(t, run(t, doc(el(flexBox(height(3), gaps(0, 1)), item(2, auto, bg(lime)), item(2, 1, bg(blue)), item(1, 1, bg(red), alignSelf(css.BoxCenter)))), page(7, 3)), 1, `
		GG_BB__
		GG____R
		GG_____`)
}
