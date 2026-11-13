package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestIntrinsic_KeywordWidthClampedByMinWidth locks CSS Sizing 3, 5.2 with
// CSS 2.1, 10.4: a width of min-content is clamped by min-width like any
// width, so a shrink-to-fit parent takes the clamped size, the border-box
// padding counted once.
func TestIntrinsic_KeywordWidthClampedByMinWidth(t *testing.T) {
	inner := el(sty(display(css.InlineBlock), padding(10), borderBox, func(s *css.Style) {
		s.Width = css.Dimension{Kind: css.MinContent}
		s.MinWidth = css.Pts(px(200))
	}), el(sty(width(20), height(20))))
	rb := boxes(t, doc(el(sty(), el(sty(display(css.InlineBlock)), inner))), page(400, 100))
	if got := kid(t, rb, 0, 0, 0).w / css.Pt; !near(got, 200) {
		t.Errorf("wrapper width %v, want 200", got)
	}
}
