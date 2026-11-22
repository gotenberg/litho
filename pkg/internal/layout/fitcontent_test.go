package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestFitContentFunction_ClampsTheArgumentByTheContent(t *testing.T) {
	// CSS Sizing 4, 3.2: fit-content(L) is min(max-content, max(min-content,
	// L)). Two floats of 2 and 3 pixels give a min-content of 3 and a
	// max-content of 5: the red box takes 4, 3 and 5 pixels.
	tests := []struct {
		limit float64
		want  string
	}{
		{4, "RRRR__"},
		{1, "RRR___"},
		{9, "RRRRR_"},
	}
	for _, tt := range tests {
		box := el(sty(height(1), bg(red), func(s *css.Style) {
			s.Width = css.Dimension{Kind: css.FitContentOf, Value: px(tt.limit)}
		}), square(2, 0, blue, float(css.FloatLeft)), square(3, 0, blue, float(css.FloatLeft)))
		assertPixels(t, run(t, doc(box), page(6, 1)), 1, tt.want)
	}
}

func TestFitContentFunction_Contributes(t *testing.T) {
	// CSS Sizing 3, 5.2 and CSS Sizing 4, 3.2: a width of fit-content(4px)
	// over content of min-content 3 and max-content 5 contributes 4 to its
	// shrink-to-fit parent, which then shows none of its own green.
	child := el(sty(height(1), bg(red), func(s *css.Style) {
		s.Width = css.Dimension{Kind: css.FitContentOf, Value: px(4)}
	}), square(2, 0, blue, float(css.FloatLeft)), square(3, 0, blue, float(css.FloatLeft)))
	parent := el(sty(height(1), bg(lime), float(css.FloatLeft)), child)
	assertPixels(t, run(t, doc(parent), page(6, 1)), 1, "RRRR__")
}
