package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestAbsolute_IntrinsicHeightKeepsContentHeight(t *testing.T) {
	t.Parallel()
	for _, k := range []css.DimKind{css.FitContent, css.MaxContent, css.MinContent} {
		box := sty(bg(red), func(s *css.Style) {
			s.Position = css.Absolute
			s.Top, s.Bottom = css.Dimension{Kind: css.Percent, Value: 0.5}, css.Pts(0)
			s.Height = css.Dimension{Kind: k}
			s.Margin[css.Top], s.Margin[css.Bottom] = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
		})
		root := doc(el(box, el(sty(width(2), height(6)))))
		// The content, 6px tall, overflows the 4px between the insets: the
		// auto margins share -2px, the box starts 1px above the middle.
		assertPixels(t, run(t, root, PageBox{Width: px(4), Height: px(8)}), 1, `
			____
			____
			____
			RR__
			RR__
			RR__
			RR__
			RR__`)
	}
}
