package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestOverflowClip_Margin(t *testing.T) {
	t.Parallel()
	// A 2px box with a 1px padding clips a child shifted 2px up and left,
	// 6px square.
	child := el(sty(width(6), height(6), bg(blue), func(s *css.Style) {
		s.Position = css.Relative
		s.Top, s.Left = css.Pts(px(-2)), css.Pts(px(-2))
	}))
	// CSS Overflow 3, 3.2: the margin applies to a box of overflow clip
	// and has no effect on a scroll container, which clips at its padding
	// box.
	tests := []struct {
		name   string
		cm     css.ClipMargin
		hidden bool
		want   string
	}{
		{"padding box", css.ClipMargin{}, false, `
			________
			________
			_BBBB___
			_BBBB___
			_BBBB___
			_BBBB___
			________
			________`},
		{"content box", css.ClipMargin{Box: css.ClipContentBox}, false, `
			________
			________
			________
			__BB____
			__BB____
			________
			________
			________`},
		{"padding box and a margin", css.ClipMargin{Margin: px(1)}, false, `
			________
			BBBBBB__
			BBBBBB__
			BBBBBB__
			BBBBBB__
			BBBBBB__
			BBBBBB__
			________`},
		{"scroll container", css.ClipMargin{Box: css.ClipContentBox, Margin: px(1)}, true, `
			________
			________
			_BBBB___
			_BBBB___
			_BBBB___
			_BBBB___
			________
			________`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			clip := sty(width(2), height(2), padding(1), func(s *css.Style) {
				if tt.hidden {
					s.Overflow = css.OverflowHidden
				} else {
					s.SetClipOnly()
				}
				s.OverflowClipMargin = tt.cm
				s.Margin[css.Top], s.Margin[css.Left] = css.Pts(px(2)), css.Pts(px(1))
			})
			assertPixels(t, run(t, doc(el(clip, child)), PageBox{Width: px(8), Height: px(8)}), 1, tt.want)
		})
	}
}
