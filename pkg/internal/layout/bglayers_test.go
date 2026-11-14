package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// solid returns a gradient of the one colour c.
func solid(c css.Color) *css.Gradient {
	return &css.Gradient{Angle: 180, Stops: gstops(c, 0.0, c, 1.0)}
}

// TestBackgroundLayers_PaintBottomUp locks CSS Backgrounds 3, 2.2: the
// layers paint from the last up, each with its own size, position,
// repeat and clip, over the colour clipped by the bottom layer.
func TestBackgroundLayers_PaintBottomUp(t *testing.T) {
	t.Parallel()
	root := doc(el(sty(width(4), height(4), padding(1), bg(lime), func(s *css.Style) {
		// The top layer: blue, 2 by 2 in the padding box's top left
		// corner, no repeat.
		s.BackgroundGradient = solid(blue)
		s.BackgroundSize = css.BackgroundSize{Width: css.Pts(px(2)), Height: css.Pts(px(2))}
		s.BackgroundRepeat = [2]css.Repeat{css.RepeatNoRepeat, css.RepeatNoRepeat}
		// The bottom layer: red over the content box; the colour takes
		// its clip and stays hidden beneath it.
		s.BackgroundRest = &css.BackgroundLists{
			Images:  []css.LayerImage{{Gradient: solid(red)}},
			Repeats: [][2]css.Repeat{{css.RepeatRepeat, css.RepeatRepeat}},
			Sizes:   []css.BackgroundSize{{Width: css.Dimension{Kind: css.Auto}, Height: css.Dimension{Kind: css.Auto}}},
			Clips:   []css.BoxEdge{css.ContentEdge},
		}
	})))
	res := run(t, root, page(6, 6))
	assertPixels(t, res, 1, `
		BB____
		BBRRR_
		_RRRR_
		_RRRR_
		_RRRR_
		______`)
}

// TestBackgroundLayers_InlineSlicedAsOneStrip locks CSS Backgrounds 3,
// 3.7 and CSS Fragmentation 3, 5.4: under box-decoration-break: slice
// the background of an inline box broken over lines is positioned in its
// fragments laid end to end, a 4px gradient red then blue showing its red
// half on the first line and its blue half on the second; a cloned box
// positions it in each fragment.
func TestBackgroundLayers_InlineSlicedAsOneStrip(t *testing.T) {
	for _, tt := range []struct {
		name  string
		brk   css.BoxDecorationBreak
		lines string
	}{
		{"slice", css.DecorationSlice, "RR\nRR\nBB\nBB"},
		{"clone", css.DecorationClone, "RR\nRR\nRR\nRR"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sp := inlineSty(func(s *css.Style) {
				s.FontSize = px(2)
				s.BoxDecorationBreak = tt.brk
				s.Color = css.Color{}
				s.BackgroundGradient = &css.Gradient{Angle: 90, Stops: gstops(red, 2.0, blue, 2.0)}
			})
			root := doc(el(sty(width(2), func(s *css.Style) { s.FontSize = px(2) }), el(sp, txt(sp, "X X"))))
			assertPixels(t, run(t, root, page(2, 4)), 1, tt.lines)
		})
	}
}
