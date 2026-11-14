package layout

import (
	"context"
	"math"
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
	if !math.IsNaN(w) {
		ms = append(ms, width(w))
	}
	if !math.IsNaN(h) {
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
		{
			"items sit side by side, stretched", nil,
			[]*Node{item(10, auto), item(20, 5)},
			[][4]float64{{0, 0, 10, 5}, {10, 0, 20, 5}},
		},
		{
			"flex-grow shares the free space", nil,
			[]*Node{item(10, 5, flexF(1, 1, -1)), item(10, 5, flexF(3, 1, -1))},
			[][4]float64{{0, 0, 30, 5}, {30, 0, 70, 5}},
		},
		{
			"flex-shrink weighs by the base size", nil,
			[]*Node{item(100, 5, flexF(0, 1, -1), func(s *css.Style) { s.MinWidthAuto = false }), item(50, 5, flexF(0, 1, -1), func(s *css.Style) { s.MinWidthAuto = false })},
			[][4]float64{{0, 0, 66.667, 5}, {66.667, 0, 33.333, 5}},
		},
		{
			"max-width freezes a growing item", nil,
			[]*Node{item(auto, 5, flexF(1, 1, 0), func(s *css.Style) { s.MaxWidth = css.Pts(px(20)) }), item(auto, 5, flexF(1, 1, 0))},
			[][4]float64{{0, 0, 20, 5}, {20, 0, 80, 5}},
		},
		{
			"justify-content: center",
			[]func(*css.Style){justify(css.BoxCenter)},
			[]*Node{item(20, 5), item(20, 5)},
			[][4]float64{{30, 0, 20, 5}, {50, 0, 20, 5}},
		},
		{
			"justify-content: space-between",
			[]func(*css.Style){justify(css.BoxSpaceBetween)},
			[]*Node{item(20, 5), item(20, 5), item(20, 5)},
			[][4]float64{{0, 0, 20, 5}, {40, 0, 20, 5}, {80, 0, 20, 5}},
		},
		{
			"justify-content: space-evenly",
			[]func(*css.Style){justify(css.BoxSpaceEvenly)},
			[]*Node{item(20, 5), item(20, 5)},
			[][4]float64{{20, 0, 20, 5}, {60, 0, 20, 5}},
		},
		{
			"auto margins take the free space", nil,
			[]*Node{item(20, 5), item(20, 5, margin(0, 0, 0, auto))},
			[][4]float64{{0, 0, 20, 5}, {80, 0, 20, 5}},
		},
		{
			"row-reverse starts at the right",
			[]func(*css.Style){dir(css.FlexRowReverse)},
			[]*Node{item(20, 5), item(30, 5)},
			[][4]float64{{80, 0, 20, 5}, {50, 0, 30, 5}},
		},
		{
			"rtl row starts at the right",
			[]func(*css.Style){rtl},
			[]*Node{item(20, 5), item(30, 5)},
			[][4]float64{{80, 0, 20, 5}, {50, 0, 30, 5}},
		},
		{
			"gap separates items",
			[]func(*css.Style){gaps(0, 10)},
			[]*Node{item(20, 5), item(20, 5)},
			[][4]float64{{0, 0, 20, 5}, {30, 0, 20, 5}},
		},
		{
			"align-items: center in a definite height",
			[]func(*css.Style){height(20), alignItems(css.BoxCenter)},
			[]*Node{item(20, 10), item(20, 4)},
			[][4]float64{{0, 5, 20, 10}, {20, 8, 20, 4}},
		},
		{
			"align-self: flex-end",
			[]func(*css.Style){height(20)},
			[]*Node{item(20, 10, alignSelf(css.BoxFlexEnd)), item(20, auto)},
			[][4]float64{{0, 10, 20, 10}, {20, 0, 20, 20}},
		},
		{
			"order sorts the items", nil,
			[]*Node{item(20, 5, order(2)), item(30, 5, order(1))},
			[][4]float64{{0, 0, 30, 5}, {30, 0, 20, 5}},
		},
		{
			"wrap breaks lines",
			[]func(*css.Style){wrapMode(css.Wrap), gaps(2, 0)},
			[]*Node{item(60, 5), item(60, 10), item(30, 4)},
			[][4]float64{{0, 0, 60, 5}, {0, 7, 60, 10}, {60, 7, 30, 4}},
		},
		{
			"wrap-reverse stacks lines upwards",
			[]func(*css.Style){wrapMode(css.WrapReverse), height(30), alignContent(css.BoxFlexStart)},
			[]*Node{item(60, 5), item(60, 10)},
			[][4]float64{{0, 25, 60, 5}, {0, 15, 60, 10}},
		},
		{
			"align-content: space-between",
			[]func(*css.Style){wrapMode(css.Wrap), height(30), alignContent(css.BoxSpaceBetween)},
			[]*Node{item(60, 5), item(60, 10)},
			[][4]float64{{0, 0, 60, 5}, {0, 20, 60, 10}},
		},
		{
			"align-content: normal stretches the lines",
			[]func(*css.Style){wrapMode(css.Wrap), height(30)},
			[]*Node{item(60, auto), item(60, auto)},
			[][4]float64{{0, 0, 60, 15}, {0, 15, 60, 15}},
		},
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
		{
			"items stack, stretched across",
			[]func(*css.Style){col},
			[]*Node{item(auto, 10), item(30, 5)},
			[][4]float64{{0, 0, 100, 10}, {0, 10, 30, 5}},
		},
		{
			"flex-grow fills a definite height",
			[]func(*css.Style){col, height(50)},
			[]*Node{item(auto, 10, flexF(1, 1, -1)), item(auto, 10)},
			[][4]float64{{0, 0, 100, 40}, {0, 40, 100, 10}},
		},
		{
			"flex: 1 in an auto height takes the content height, not the height",
			[]func(*css.Style){col},
			[]*Node{item(auto, 10, func(s *css.Style) { s.FlexGrow, s.FlexBasis = 1, css.Dimension{Kind: css.Percent} })},
			[][4]float64{{0, 0, 100, 0}},
		},
		{
			"max-height bounds an auto height column",
			[]func(*css.Style){col, func(s *css.Style) { s.MaxHeight = css.Pts(px(20)) }},
			[]*Node{item(auto, 15, func(s *css.Style) { s.MinHeightAuto = false }), item(auto, 15, func(s *css.Style) { s.MinHeightAuto = false })},
			[][4]float64{{0, 0, 100, 10}, {0, 10, 100, 10}},
		},
		{
			"column-reverse starts at the bottom",
			[]func(*css.Style){dir(css.FlexColumnReverse), height(50)},
			[]*Node{item(auto, 10), item(auto, 20)},
			[][4]float64{{0, 40, 100, 10}, {0, 20, 100, 20}},
		},
		{
			"align-items: center centres across",
			[]func(*css.Style){col, alignItems(css.BoxCenter)},
			[]*Node{item(40, 10)},
			[][4]float64{{30, 0, 40, 10}},
		},
		{
			"justify-content: flex-end",
			[]func(*css.Style){col, height(50), justify(css.BoxFlexEnd)},
			[]*Node{item(auto, 10)},
			[][4]float64{{0, 40, 100, 10}},
		},
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

func TestFlex_WrapReverseBaselineFromTheTopLine(t *testing.T) {
	// CSS Flexbox 1, 8.5: a row container takes its first baseline from
	// its startmost line, the one nearest the block start: under
	// wrap-reverse the last line, drawn at the top.
	line := func(size float64) *Node { return el(sty(width(60), font(size)), txt(sty(font(size)), "a")) }
	rb := boxes(t, doc(el(flexBox(wrapMode(css.WrapReverse), width(100)), line(10), line(20))), page(100, 100))
	c := kid(t, rb, 0, 0)
	top := c.kids[1]
	if top.y >= c.kids[0].y {
		t.Fatalf("second line at %v, first at %v: want the second on top", top.y, c.kids[0].y)
	}
	want, _ := firstBaseline(top)
	if fy, ok := flexBaseline(c, false); !ok || !near(fy, want) {
		t.Errorf("container baseline %v, want the top line's %v", fy, want)
	}
}

func TestFlex_ColumnReverseBaselineFromTheTopItem(t *testing.T) {
	// CSS Flexbox 1, 8.5: a column container takes its first baseline
	// from its startmost item, the one nearest the block start: under
	// column-reverse the last item, drawn at the top.
	item := func(size float64) *Node { return el(sty(font(size)), txt(sty(font(size)), "a")) }
	rb := boxes(t, doc(el(flexBox(dir(css.FlexColumnReverse)), item(10), item(20))), page(100, 100))
	c := kid(t, rb, 0, 0)
	top := c.kids[1]
	if top.y >= c.kids[0].y {
		t.Fatalf("second item at %v, first at %v: want the second on top", top.y, c.kids[0].y)
	}
	want, _ := firstBaseline(top)
	if fy, ok := flexBaseline(c, false); !ok || !near(fy, want) {
		t.Errorf("container baseline %v, want the top item's %v", fy, want)
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

// TestFlex_BasisKeywords holds the sizing keywords of flex-basis to CSS
// Flexbox 1, 7.2.3 and CSS Sizing 3, 3.1: min-content, max-content and
// fit-content size the content box as width would, and stretch takes the
// line's room.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) gives an item
//     of flex-shrink 0 its min-content width for min-content, its
//     max-content width for max-content and for fit-content in a wide
//     container, the container's width for fit-content in a narrow one
//     and for stretch.
func TestFlex_BasisKeywords(t *testing.T) {
	basis := func(k css.DimKind) func(*css.Style) {
		return func(s *css.Style) { s.FlexBasis, s.FlexShrink = css.Dimension{Kind: k}, 0 }
	}
	tests := []struct {
		name  string
		k     css.DimKind
		cw, w float64
	}{
		{"min-content", css.MinContent, 400, 80},
		{"max-content", css.MaxContent, 400, 160},
		{"fit-content in room", css.FitContent, 400, 160},
		{"fit-content narrowed", css.FitContent, 100, 100},
		{"stretch", css.Stretch, 400, 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := boxes(t, doc(el(flexBox(width(tt.cw)), el(sty(basis(tt.k)), txt(sty(), "aaaa bbbbbbbb cc")))), page(800, 800))
			if got := rect(kid(t, rb, 0, 0, 0)); got[2] != tt.w {
				t.Errorf("item %v wide, want %v", got[2], tt.w)
			}
		})
	}
}

// TestFlex_TableItemCaptions holds a table flex item to CSS Flexbox 1, 4:
// the table wrapper is the item, its captions count as the table box's
// padding, so the table box takes the item's size less the captions, in
// a row stretched and in a column flexed or sized by its height.
//
// # Corpus evidence
//
//   - wpt: css-flexbox/table-as-item-stretch-cross-size, -2 and -3 and
//     table-as-item-flex-cross-size draw a 100px square in Chromium 156.
func TestFlex_TableItemCaptions(t *testing.T) {
	t.Parallel()
	tbl := func(m ...func(*css.Style)) *Node {
		return table(mods(append([]func(*css.Style){spacing(0, 0)}, m...)...),
			caption(mods(height(10))),
			caption(mods(height(20), func(s *css.Style) { s.CaptionSide = css.CaptionBottom })),
			tr(nil, td(nil, "")))
	}
	tests := []struct {
		name string
		cont *css.Style
		item *Node
		want float64
	}{
		{"row: stretched", flexBox(width(100), height(100)), tbl(flexF(1, 1, -1)), 70},
		{"column: flexed", flexBox(width(100), height(100), dir(css.FlexColumn)), tbl(flexF(1, 1, -1)), 70},
		{"column: a height of its own", flexBox(width(100), dir(css.FlexColumn)), tbl(height(90)), 90},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := kid(t, boxes(t, doc(el(tt.cont, tt.item)), page(200, 200)), 0, 0, 0)
			if w.tdata == nil {
				t.Fatalf("item of part %v, want a table wrapper", w.tpart)
			}
			tb := w.tdata.table
			if got := tb.borderH() / css.Pt; !near(got, tt.want) {
				t.Errorf("table box %v tall, want %v", got, tt.want)
			}
		})
	}
}

// TestFlex_StretchedTableKeepsItsMinContentWidth holds a table item a
// column stretches to CSS 2.1, 17.5.2: a table is never narrower than
// its min-content width, so it overflows a narrower container.
//
// # Corpus evidence
//
//   - wpt: css-flexbox/table-as-item-stretch-cross-size-4 (a table holding
//     a 100px block in a 50px column) draws a 100px square in Chromium 156.
func TestFlex_StretchedTableKeepsItsMinContentWidth(t *testing.T) {
	t.Parallel()
	tb := table(nil, tr(nil, part(css.TableCell, nil, el(sty(width(100), height(100))))))
	w := kid(t, boxes(t, doc(el(flexBox(width(50), dir(css.FlexColumn)), tb)), page(200, 200)), 0, 0, 0)
	if got := rect(w); got[2] != 100 {
		t.Errorf("table item %v wide, want 100", got[2])
	}
}

// TestFlex_PercentageHeightDoesNotStretch holds a row item of a cyclic
// percentage height to CSS Flexbox 1, 9.4, step 11: only a computed cross
// size of auto stretches, and a percentage of an indefinite height
// behaves as auto for sizing alone.
//
// # Corpus evidence
//
//   - wpt: css-flexbox/stretch-requires-computed-auto-size draws its
//     reference in Chromium 156.
func TestFlex_PercentageHeightDoesNotStretch(t *testing.T) {
	t.Parallel()
	half := func(s *css.Style) { s.Height = css.Dimension{Kind: css.Percent, Value: 0.5} }
	got := flexRects(t, doc(el(flexBox(), item(10, 30), item(10, math.NaN(), half))), 0, 0)
	if want := [][4]float64{{0, 0, 10, 30}, {10, 0, 10, 0}}; !eqRects(got, want) {
		t.Errorf("rects = %v, want %v", got, want)
	}
}

// TestFlex_PercentageBasisOfIndefiniteMain holds a percentage flex-basis
// in a column of indefinite height to CSS Flexbox 1, 7.2.3: the used flex
// basis is content, not the item's height.
func TestFlex_PercentageBasisOfIndefiniteMain(t *testing.T) {
	t.Parallel()
	basis := func(s *css.Style) {
		s.FlexBasis = css.Dimension{Kind: css.Percent, Value: 0}
		s.FlexGrow, s.FlexShrink = 0, 0
	}
	got := flexRects(t, doc(el(flexBox(width(50), dir(css.FlexColumn)), el(sty(height(300), minH(0), basis), item(10, 100)))), 0, 0)
	if want := [][4]float64{{0, 0, 50, 100}}; !eqRects(got, want) {
		t.Errorf("rects = %v, want %v", got, want)
	}
}

// TestFlex_PercentageOfAnIndefiniteColumnItem holds the children of an
// item of a column of auto height: a percentage height resolves against
// an item whose flex basis is definite of its own, and behaves as auto in
// one of auto height (CSS Flexbox 1, 9.8; CSS 2.1, 10.5).
func TestFlex_PercentageOfAnIndefiniteColumnItem(t *testing.T) {
	t.Parallel()
	half := func(s *css.Style) { s.Height = css.Dimension{Kind: css.Percent, Value: 0.5} }
	tests := []struct {
		name string
		item *Node
		want float64
	}{
		{"an auto height", el(flexBox(dir(css.FlexColumn)), item(10, 40), item(10, math.NaN(), half)), 0},
		{"a length height", el(sty(height(40)), item(10, math.NaN(), half)), 20},
		{"a length basis", el(sty(flexF(0, 0, 40)), item(10, math.NaN(), half)), 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			it := kid(t, boxes(t, doc(el(flexBox(width(50), dir(css.FlexColumn)), tt.item)), page(100, 100)), 0, 0, 0)
			c := it.kids[len(it.kids)-1]
			if got := rect(c)[3]; got != tt.want {
				t.Errorf("child %v tall, want %v", got, tt.want)
			}
		})
	}
}

// TestFlex_ColumnWrapWidthFromFlexedLines locks CSS Flexbox 1, 9.9.2: an
// inline multi-line column of a definite height is as wide as its lines
// once its items flex: three 60px items in a 100px column take three
// lines.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 makes such a
//     column of three 50 by 60px items 150px wide.
func TestFlex_ColumnWrapWidthFromFlexedLines(t *testing.T) {
	t.Parallel()
	col := func(kids ...*Node) *Node {
		return el(flexBox(func(s *css.Style) { s.Display = css.InlineFlex }, dir(css.FlexColumn), wrapMode(css.Wrap), height(100)), kids...)
	}
	for _, tc := range []struct {
		name string
		root *Node
		want float64
	}{
		{"three lines", col(item(50, 60, flexF(0, 0, -1)), item(50, 60, flexF(0, 0, -1)), item(50, 60, flexF(0, 0, -1))), 150},
	} {
		var fb *box
		var walk func(b *box)
		walk = func(b *box) {
			if b.flex != nil && fb == nil {
				fb = b
			}
			for _, k := range b.kids {
				walk(k)
			}
		}
		walk(boxes(t, doc(el(sty(), tc.root)), page(300, 300)))
		if fb == nil {
			t.Fatalf("%s: no flex container", tc.name)
		}
		if got := rect(fb)[2]; got != tc.want {
			t.Errorf("%s: container %vpx wide, want %v", tc.name, got, tc.want)
		}
	}
}
