package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestOverflowClip_RelativeChild(t *testing.T) {
	t.Parallel()
	// A 2px box with a 1px padding clips a child shifted 2px up and left,
	// 6px square.
	child := el(sty(width(6), height(6), bg(blue), func(s *css.Style) {
		s.Position = css.Relative
		s.Top, s.Left = css.Pts(px(-2)), css.Pts(px(-2))
	}))
	tests := []struct {
		name string
		want string
	}{
		{"padding box", `
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
				s.Overflow = css.OverflowHidden
				s.Margin[css.Top], s.Margin[css.Left] = css.Pts(px(2)), css.Pts(px(1))
			})
			assertPixels(t, run(t, doc(el(clip, child)), PageBox{Width: px(8), Height: px(8)}), 1, tt.want)
		})
	}
}
