package layout

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func avoid(s *css.Style) { s.BreakInside = css.InsideAvoid }

func TestFragmentation_MarginsTruncateAtBreaks(t *testing.T) {
	tests := []struct {
		name string
		root *Node
		path []int
		top  float64
	}{
		{"margin within the page", doc(el(sty(height(10))), el(sty(height(5), margin(5, 0)))), []int{0, 1}, 15},
		{"margin across the cut", doc(el(sty(height(25))), el(sty(height(5), margin(10, 0)))), []int{0, 1}, 30},
		{"margin after a filled page", doc(el(sty(height(30))), el(sty(height(5), margin(5, 0)))), []int{0, 1}, 30},
		{"root margin taller than pages", el(sty(margin(100, 0)), el(sty(height(5)))), nil, 30},
		{"margin at a content edge stays", el(sty(margin(100, 0)), el(sty(height(5), margin(8, 0)))), []int{0}, 38},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := boxes(t, tt.root, page(10, 30))
			if got := rect(kid(t, rb, tt.path...))[1]; got != tt.top {
				t.Errorf("top %v, want %v", got, tt.top)
			}
		})
	}
}

func TestFragmentation_TrailingMarginTakesNoPage(t *testing.T) {
	root := el(sty(), el(sty(margin(0, 0, 8, 0)), el(sty(height(30), bg(red)))))
	if res := run(t, root, page(10, 30)); len(res.Pages) != 1 {
		t.Errorf("%d pages, want 1: the bottom margin truncates at the end", len(res.Pages))
	}
}

func TestFragmentation_AvoidInsideTallerThanPage(t *testing.T) {
	// An avoid box too tall for any page moves past a sibling, where the
	// break violates no rule, then breaks before its avoid child.
	root := doc(para()(1), el(sty(avoid), el(sty(height(10))), el(sty(avoid), el(sty(height(20))), el(sty(height(10))))))
	rb := boxes(t, root, page(10, 30))
	if got := rect(kid(t, rb, 0, 1))[1]; got != 30 {
		t.Errorf("outer top %v, want 30", got)
	}
	if got := rect(kid(t, rb, 0, 1, 1))[1]; got != 60 {
		t.Errorf("inner top %v, want 60", got)
	}
	// As a first child there is nothing to move past: it stays.
	root = doc(el(sty(avoid), el(sty(height(10))), el(sty(avoid), el(sty(height(20))), el(sty(height(10))))))
	rb = boxes(t, root, page(10, 30))
	if got := rect(kid(t, rb, 0, 0))[1]; got != 0 {
		t.Errorf("first child top %v, want 0", got)
	}
}

func TestInline_LineBreakClears(t *testing.T) {
	f := el(sty(float(css.FloatLeft), width(20), height(10), margin(0, 0, 20, 0)))
	clear := inlineSty(func(s *css.Style) { s.Clear = css.ClearBoth })
	rb := boxes(t, doc(el(sty(), f, br(clear), txt(inlineSty(), "a"))), page(100, 100))
	b := kid(t, rb, 0, 0)
	if len(b.lines) != 2 {
		t.Fatalf("%d lines, want 2", len(b.lines))
	}
	if got := b.lines[1].y / css.Pt; !near(got, 30) {
		t.Errorf("line after the break at %v, want 30, below the float's margin", got)
	}
}

func TestFragmentation_TallLines(t *testing.T) {
	// An inline block taller than the page overflows with its line; what
	// follows starts below its full height, the strut's 2 px descent under
	// the baseline the inline block sits on included.
	ib := el(sty(display(css.InlineBlock), width(5), height(70), bg(red)))
	root := doc(el(sty(), ib), el(sty(height(5), bg(blue))))
	rb := boxes(t, root, page(10, 30))
	if got := rect(kid(t, rb, 0, 1))[1]; got != 72 {
		t.Errorf("next block at %v, want 72", got)
	}
	res := run(t, root, page(10, 30))
	if len(res.Pages) != 3 {
		t.Fatalf("%d pages, want 3", len(res.Pages))
	}
	// A line taller than the page for its text alone takes one page.
	tall := para(func(s *css.Style) { s.LineHeight = css.Pts(px(50)) })(1)
	rb = boxes(t, doc(tall, el(sty(height(5)))), page(10, 30))
	if got := rect(kid(t, rb, 0, 1))[1]; got != 30 {
		t.Errorf("block after a tall text line at %v, want 30", got)
	}
}

func TestFragmentation_ForcedSide(t *testing.T) {
	brk := func(v css.Break) func(*css.Style) { return func(s *css.Style) { s.BreakBefore = v } }
	for _, tt := range []struct {
		name string
		v    css.Break
		rtl  bool
		want []int
	}{
		{"right after a right page", css.RightPage, false, []int{1, 0, 1}},
		{"left after a right page", css.LeftPage, false, []int{1, 1}},
		{"left after a left page in rtl", css.LeftPage, true, []int{1, 0, 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := doc(para()(1), para(brk(tt.v))(1))
			if tt.rtl {
				root.Style.Direction = css.RTL
			}
			got := pageLines(run(t, root, page(10, 30)))
			if len(got) != len(tt.want) {
				t.Fatalf("lines per page %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("lines per page %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestPageAt_SpreadMargins(t *testing.T) {
	after := func(s *css.Style) { s.BreakAfter = css.Page }
	root := el(sty(), el(sty(), el(sty(height(2), bg(red), after)), el(sty(height(2), bg(red)))))
	opts := Options{Page: page(4, 4), PageAt: func(i int) PageBox {
		pb := page(4, 4)
		if i%2 == 0 {
			pb.Margin[css.Top], pb.Margin[css.Right] = px(2), px(2)
		} else {
			pb.Margin[css.Left], pb.Margin[css.Bottom] = px(2), px(2)
		}
		return pb
	}}
	res, err := Layout(context.Background(), root, ahem(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	assertPixels(t, res, 1, "____\n____\nRR__\nRR__")
	assertPixels(t, res, 2, "__RR\n__RR\n____\n____")
}

func TestPageAt_HeightsVary(t *testing.T) {
	root := el(sty(), el(sty(), el(sty(height(6), bg(red)))))
	opts := Options{Page: page(2, 4), PageAt: func(i int) PageBox {
		if i == 0 {
			return page(2, 2)
		}
		return page(2, 4)
	}}
	res, err := Layout(context.Background(), root, ahem(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 2 || res.Pages[0].Height != px(2) || res.Pages[1].Height != px(4) {
		t.Fatalf("%d pages, want a 2 px page then a 4 px one", len(res.Pages))
	}
	assertPixels(t, res, 1, "RR\nRR")
	assertPixels(t, res, 2, "RR\nRR\nRR\nRR")
}

func TestPageAt_InvalidBoxFails(t *testing.T) {
	for _, bad := range []int{0, 1} {
		opts := Options{Page: page(2, 2), PageAt: func(i int) PageBox {
			if i == bad {
				return PageBox{}
			}
			return page(2, 2)
		}}
		root := el(sty(), el(sty(height(6))))
		if _, err := Layout(context.Background(), root, ahem(t), opts); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("bad page %d: error %v, want ErrInvalidOptions", bad, err)
		}
	}
}

func TestFragmentation_ContainerFollowsItsContent(t *testing.T) {
	// The child's margin crosses the cut, so the section's content starts
	// on page 2: the section moves there whole, its padding and the
	// child's margin, at a content edge, kept.
	root := doc(el(sty(height(25))), el(sty(padding(3, 0)), el(sty(height(5), margin(5, 0)))))
	rb := boxes(t, root, page(10, 30))
	if got := rect(kid(t, rb, 0, 1))[1]; got != 30 {
		t.Errorf("section top %v, want 30", got)
	}
	if got := rect(kid(t, rb, 0, 1, 0))[1]; got != 38 {
		t.Errorf("child top %v, want 38", got)
	}
}

func TestFragmentation_ViewportClipsOverflow(t *testing.T) {
	// The body's overflow goes to the viewport, which clips each page to
	// its area: the inline block overflowing the body takes no page.
	ib := el(sty(display(css.InlineBlock), width(5), height(70), bg(red)))
	hidden := func(s *css.Style) { s.Overflow = css.OverflowHidden }
	root := el(sty(), el(sty(height(1), hidden), el(sty(), ib)))
	if res := run(t, root, page(10, 30)); len(res.Pages) != 1 {
		t.Errorf("%d pages, want 1", len(res.Pages))
	}
	root = el(sty(), el(sty(height(1)), el(sty(), ib)))
	if res := run(t, root, page(10, 30)); len(res.Pages) != 3 {
		t.Errorf("%d pages, want 3: visible overflow takes its pages", len(res.Pages))
	}
}
