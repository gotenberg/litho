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
