package layout

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestBackground_LocalAttachment(t *testing.T) {
	// A scroll container's local background is positioned in its
	// scrollable overflow area: at its bottom right, past the clip, the
	// image does not show (CSS Backgrounds 3, 3.5).
	tests := []struct {
		name   string
		attach css.Attachment
		want   string
	}{
		{"scroll", css.AttachScroll, "____\n__RB"},
		{"local", css.AttachLocal, "____\n____"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := doc(el(sty(width(4), height(2), func(s *css.Style) {
				s.Overflow = css.OverflowHidden
				s.BackgroundImage = "i.png"
				s.BackgroundAttachment = tt.attach
				s.BackgroundRepeat = [2]css.Repeat{css.RepeatNoRepeat, css.RepeatNoRepeat}
				s.BackgroundPosition = [2]css.Dimension{{Kind: css.Percent, Value: 1}, {Kind: css.Percent, Value: 1}}
			}), el(sty(width(8), height(4)))))
			res, err := Layout(context.Background(), root, ahem(t), Options{Page: page(4, 2), Image: imageLoader()})
			if err != nil {
				t.Fatal(err)
			}
			assertPixels(t, res, 1, tt.want)
		})
	}
}

func TestBackground_NearZeroGradientTiles(t *testing.T) {
	// Tiles of a fifth of a pixel blend into the gradient's colour.
	g := &css.Gradient{Angle: 180, Stops: []css.GradientStop{
		{Color: lime, Position: css.Dimension{Kind: css.Auto}},
		{Color: lime, Position: css.Dimension{Kind: css.Auto}},
	}}
	root := doc(el(sty(width(3), height(2), func(s *css.Style) {
		s.BackgroundGradient = g
		s.BackgroundSize = css.BackgroundSize{Width: css.Pts(px(0.2)), Height: css.Pts(px(0.2))}
	})))
	assertPixels(t, run(t, root, page(4, 2)), 1, "GGG_\nGGG_")
}
