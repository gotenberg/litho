package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// calcOf returns calc(f*100% + v px).
func calcOf(f, v float64) css.Dimension {
	return css.Dimension{Kind: css.Calc, Value: px(v), Fraction: f}
}

func TestCalcDraw_BlockSizes(t *testing.T) {
	tests := []struct {
		name string
		mods []func(*css.Style)
		want string
	}{
		{"width", []func(*css.Style){func(s *css.Style) { s.Width = calcOf(0.5, 1) }}, "BBBBB___"},
		{"margin-left", []func(*css.Style){width(2), func(s *css.Style) { s.Margin[css.Left] = calcOf(0.25, -1) }}, "_BB_____"},
		{"border-box width", []func(*css.Style){func(s *css.Style) {
			s.Width, s.BoxSizing = calcOf(0.5, 2), css.BorderBox
		}, padding(0, 1)}, "BBBBBB__"},
		{"negative padding clamps", []func(*css.Style){width(2), func(s *css.Style) { s.Padding[css.Left] = calcOf(0.1, -5) }}, "BB______"},
		{"relative left", []func(*css.Style){width(2), func(s *css.Style) {
			s.Position, s.Left = css.Relative, calcOf(0.5, -1)
		}}, "___BB___"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := el(sty(append([]func(*css.Style){height(1), bg(blue)}, tt.mods...)...))
			assertPixels(t, run(t, doc(b), page(8, 1)), 1, tt.want)
		})
	}
}

func TestCalcDraw_Flex(t *testing.T) {
	// flex-basis calc(50% - 1px) of 8px is 3px; a column gap of
	// calc(25% - 1px) is 1px.
	a := item(auto, 1, bg(blue), func(s *css.Style) { s.FlexBasis = calcOf(0.5, -1) })
	b := item(2, 1, bg(red))
	box := flexBox(width(8), func(s *css.Style) { s.ColumnGap = calcOf(0.25, -1) })
	assertPixels(t, run(t, doc(el(box, a, b)), page(8, 1)), 1, "BBB_RR__")
}

func TestCalc_IntrinsicKeepsLengthPart(t *testing.T) {
	// A float shrinks to its content: the percentage of the child's
	// margin and min-width resolves against zero, their lengths count
	// (CSS Sizing 3, 5.2.1).
	kidSty := sty(height(1), width(1), func(s *css.Style) {
		s.Margin[css.Left] = calcOf(0.5, 2)
	})
	minSty := sty(height(1), func(s *css.Style) { s.MinWidth = calcOf(0.5, 5) })
	rb := boxes(t, doc(el(sty(float(css.FloatLeft)), el(kidSty)), el(sty(float(css.FloatLeft)), el(minSty))), page(100, 10))
	if got := rect(kid(t, rb, 0, 0)); got[2] != 3 {
		t.Errorf("float with a calc() margin is %vpx wide, want 3", got[2])
	}
	if got := rect(kid(t, rb, 0, 1)); got[2] != 5 {
		t.Errorf("float with a calc() min-width is %vpx wide, want 5", got[2])
	}
}

func TestCalc_IndefiniteHeightIsAuto(t *testing.T) {
	// A calc() mixing a percentage of an indefinite height acts as auto
	// (CSS 2.1, 10.5).
	rb := boxes(t, doc(el(sty(), el(sty(func(s *css.Style) { s.Height = calcOf(0.5, 10) }), item(1, 2)))), page(10, 100))
	if got := rect(kid(t, rb, 0, 0, 0)); got[3] != 2 {
		t.Errorf("height %vpx, want the content's 2", got[3])
	}
	rb = boxes(t, doc(el(sty(height(40)), el(sty(func(s *css.Style) { s.Height = calcOf(0.5, 10) })))), page(10, 100))
	if got := rect(kid(t, rb, 0, 0, 0)); got[3] != 30 {
		t.Errorf("height %vpx, want 30 of a definite 40", got[3])
	}
}
