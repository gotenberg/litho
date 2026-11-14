package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// sticky returns the style of a sticky box of the insets top, right,
// bottom and left in pixels, NaN for auto.
func sticky(insets ...float64) func(*css.Style) {
	return func(s *css.Style) {
		s.Position = css.Sticky
		d := dims(insets...)
		s.Top, s.Right, s.Bottom, s.Left = d[0], d[1], d[2], d[3]
	}
}

func clipped(s *css.Style) { s.Overflow = css.OverflowHidden }

// TestSticky_OffsetAtTheInitialScrollPosition locks CSS Position 3, 3.4 at
// the initial scroll position: a sticky box moves the least that keeps it
// inside its scroll container's scrollport less its insets, never out of
// its containing block, an auto margin counting as 0; a box already
// inside stays where it is laid out, whatever its insets.
func TestSticky_OffsetAtTheInitialScrollPosition(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		root *Node
		want string
	}{
		{
			// bottom: 1px pulls a box 6px down a 4px scroll container up
			// to its bottom less 1px.
			"bottom inset", doc(el(sty(width(4), height(4), clipped, bg(red)), el(sty(height(8)),
				el(sty(height(6))), el(sty(height(1), bg(blue), sticky(auto, auto, 1, auto)))))),
			`RRRR
			 RRRR
			 BBBB
			 RRRR
			 ____
			 ____`,
		},
		{
			// top: 3px moves a box 1px down its scroll container to 3px.
			"top inset", doc(el(sty(width(4), height(4), clipped, bg(red)), el(sty(height(8)),
				el(sty(height(1))), el(sty(height(1), bg(blue), sticky(3, auto, auto, auto)))))),
			`RRRR
			 RRRR
			 RRRR
			 BBBB
			 ____
			 ____`,
		},
		{
			// A box inside its rectangle does not move by its insets as
			// a relative one would.
			"inside", doc(el(sty(width(4), height(6)), el(sty(height(2), bg(blue), sticky(1, auto, auto, 1))))),
			`____
			 BBBB
			 BBBB
			 ____
			 ____
			 ____`,
		},
		{
			// A right: 0 box pushed 6px right by an auto margin comes back
			// to the scrollport's right edge.
			"auto margin", doc(el(sty(width(4), height(2), clipped, bg(red)), el(sty(width(10)),
				el(sty(width(2), height(2), bg(blue), sticky(auto, 0, auto, auto), func(s *css.Style) {
					s.Margin[css.Left] = css.Dimension{Kind: css.Auto}
				}))))),
			`RRBB
			 RRBB
			 ____
			 ____
			 ____
			 ____`,
		},
		{
			// The containing block, 3px tall, holds the box back.
			"containing block", doc(el(sty(width(4), height(6)), el(sty(height(3)),
				el(sty(height(1), bg(blue), sticky(4, auto, auto, auto)))))),
			`____
			 ____
			 BBBB
			 ____
			 ____
			 ____`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertPixels(t, run(t, tt.root, PageBox{Width: px(4), Height: px(6)}), 1, tt.want)
		})
	}
}

// TestSticky_ViewportIsThePageArea locks the viewport of a sticky box with
// no scroll container: the area of the page its top is on, so a bottom: 0
// box at the top of the second page stays there; inside a fixed box, the
// page that box is fixed to, so the same box moves up its page.
func TestSticky_ViewportIsThePageArea(t *testing.T) {
	t.Parallel()
	flow := doc(el(sty(height(4))), el(sty(height(2), bg(blue), sticky(auto, auto, 0, auto))))
	res := run(t, flow, PageBox{Width: px(2), Height: px(4)})
	assertPixels(t, res, 2, `
		BB
		BB
		__
		__`)
	fixed := doc(el(sty(width(2), height(4), func(s *css.Style) { s.Position = css.Fixed }),
		el(sty(height(4))), el(sty(height(2), bg(blue), sticky(auto, auto, 0, auto)))))
	assertPixels(t, run(t, fixed, PageBox{Width: px(2), Height: px(4)}), 1, `
		__
		__
		BB
		BB`)
}
