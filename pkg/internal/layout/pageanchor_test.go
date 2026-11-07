package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestPageAnchor positions a page-anchored box in the page box of the page
// its static position falls on, over the margins, and paints it there
// alone.
func TestPageAnchor(t *testing.T) {
	abs := func(top, left float64) func(*css.Style) {
		return func(s *css.Style) {
			s.Position = css.Absolute
			s.PageAnchor = true
			s.Top, s.Left = css.Pts(px(top)), css.Pts(px(left))
		}
	}
	pb := PageBox{Width: px(8), Height: px(8), Margin: [4]float64{px(2), px(2), px(2), px(2)}}
	tall := el(sty(height(5), bg(blue)))
	frame := el(sty(abs(1, 6), width(2), height(2), bg(red)))
	anchor := el(sty(height(1)), frame)
	res := run(t, doc(tall, anchor), pb)
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	assertPixels(t, res, 1, `
		________
		________
		__BBBB__
		__BBBB__
		__BBBB__
		__BBBB__
		________
		________`)
	assertPixels(t, res, 2, `
		________
		______RR
		__BBBBRR
		________
		________
		________
		________
		________`)
}

// TestPageAnchor_Page puts a page-anchored box on the page its AnchorPage
// names, adding the pages the flow does not reach, and counts a box whose
// height runs past its page area by the page it sits on alone.
func TestPageAnchor_Page(t *testing.T) {
	pb := PageBox{Width: px(8), Height: px(8), Margin: [4]float64{px(2), px(2), px(2), px(2)}}
	tests := []struct {
		name  string
		page  int
		top   float64
		pages int
		on    int
		want  string
	}{
		{"third page", 3, 1, 3, 3, `
			________
			______RR
			______RR
			________
			________
			________
			________
			________`},
		{"own page", 0, 5, 1, 1, `
			________
			________
			__BB____
			________
			________
			______RR
			______RR
			________`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := el(sty(func(s *css.Style) {
				s.Position = css.Absolute
				s.PageAnchor = true
				s.AnchorPage = tt.page
				s.Top, s.Left = css.Pts(px(tt.top)), css.Pts(px(6))
			}, width(2), height(2), bg(red)))
			res := run(t, doc(el(sty(height(1), width(2), bg(blue)), frame)), pb)
			if len(res.Pages) != tt.pages {
				t.Fatalf("%d pages, want %d", len(res.Pages), tt.pages)
			}
			assertPixels(t, res, tt.on, tt.want)
		})
	}
}

// TestPageClamp keeps a box positioned from its anchor inside the page box
// of the anchor's page: a box above the page moves down to its top edge, a
// box below its bottom edge moves up, a box taller than the page keeps its
// top on the page's top edge, and none adds a page.
func TestPageClamp(t *testing.T) {
	pb := PageBox{Width: px(8), Height: px(8), Margin: [4]float64{px(2), px(2), px(2), px(2)}}
	tests := []struct {
		name string
		top  float64
		h    float64
		want string
	}{
		{"above", -9, 2, `
			__RR____
			__RR____
			__BB____
			________
			________
			________
			________
			________`},
		{"below", 7, 2, `
			________
			________
			__BB____
			________
			________
			________
			__RR____
			__RR____`},
		{"taller", -1, 12, `
			__RR____
			__RR____
			__RR____
			__RR____
			__RR____
			__RR____
			__RR____
			__RR____`},
		{"inside", 1, 2, `
			________
			________
			__BB____
			__RR____
			__RR____
			________
			________
			________`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := el(sty(func(s *css.Style) {
				s.Position = css.Absolute
				s.PageClamp = true
				s.Top = css.Pts(px(tt.top))
			}, width(2), height(tt.h), bg(red)))
			anchor := el(sty(height(1), width(2), bg(blue), func(s *css.Style) { s.Position = css.Relative }), frame)
			res := run(t, doc(anchor), pb)
			if len(res.Pages) != 1 {
				t.Fatalf("%d pages, want 1", len(res.Pages))
			}
			assertPixels(t, res, 1, tt.want)
		})
	}
}
