package layout

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestMask_Composite locks how two mask layers of a red box composite
// (CSS Masking 1, 6.11): the top layer, opaque on the left half and clear
// on the right, over the bottom one, opaque on the top half and clear on
// the bottom. Each quadrant shows red where the operator keeps it.
func TestMask_Composite(t *testing.T) {
	half := func(angle float64) *css.Gradient {
		return &css.Gradient{Angle: angle, Stops: gstops(black, px(10), css.Color{}, px(10))}
	}
	tests := []struct {
		op   css.MaskComposite
		mode css.MaskMode
		// want holds the top left, top right, bottom left and bottom
		// right quadrants: true where red shows.
		want [4]bool
	}{
		{css.CompositeAdd, css.MaskAlpha, [4]bool{true, true, true, false}},
		{css.CompositeIntersect, css.MaskAlpha, [4]bool{true, false, false, false}},
		{css.CompositeSubtract, css.MaskAlpha, [4]bool{false, false, true, false}},
		{css.CompositeExclude, css.MaskAlpha, [4]bool{false, true, true, false}},
		{css.CompositeIntersect, css.MaskLuminance, [4]bool{false, false, false, false}},
	}
	for _, tt := range tests {
		st := func(s *css.Style) {
			s.Mask = css.Mask{
				Images:     []css.MaskImage{{Gradient: half(90)}, {Gradient: half(180)}},
				Composites: []css.MaskComposite{tt.op},
				Modes:      []css.MaskMode{tt.mode},
			}
		}
		root := doc(el(sty(width(20), height(20), bg(red), st)))
		img := render(t, run(t, root, page(20, 20)), 1)
		for i, q := range [4][2]int{{4, 4}, {15, 4}, {4, 15}, {15, 15}} {
			if got := code(img.At(q[0], q[1])) == 'R'; got != tt.want[i] {
				t.Errorf("op %d mode %d quadrant %d: red %v, want %v (%v)", tt.op, tt.mode, i, got, tt.want[i], at(img, q[0], q[1]))
			}
		}
	}
}

// TestMask_GradientAlpha locks one mask layer of a gradient masking by
// its alpha: clear at the left, opaque at the right.
func TestMask_GradientAlpha(t *testing.T) {
	g := &css.Gradient{Angle: 90, Stops: gstops(css.Color{}, math.NaN(), black, math.NaN())}
	root := doc(el(sty(width(20), height(4), bg(red), func(s *css.Style) { s.Mask = css.Mask{Images: []css.MaskImage{{Gradient: g}}} })))
	img := render(t, run(t, root, page(20, 4)), 1)
	left, right := at(img, 0, 2), at(img, 19, 2)
	if left.G < 230 || right.G > 25 {
		t.Errorf("left %v, right %v: want white to red", left, right)
	}
}
