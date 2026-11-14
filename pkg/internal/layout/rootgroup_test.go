package layout

import (
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestRootGroup_OpacityAndClip locks CSS Compositing 1, 9.1: the root's
// opacity applies to the canvas its background paints, which shows over
// the white page at half its colour; outside the root's clip-path the
// page shows, the canvas clipped away with the content.
func TestRootGroup_OpacityAndClip(t *testing.T) {
	half := func(s *css.Style) { s.Opacity = 0.5 }
	root := el(sty(bg(blue), half), el(sty(height(2))))
	img := render(t, run(t, root, page(4, 4)), 1)
	if got := at(img, 2, 3); !similar(got, color.NRGBA{128, 128, 255, 255}) {
		t.Errorf("canvas %v, want blue at half opacity", got)
	}
	clip := clipPath(css.ClipPath{Boxed: true, Shape: &css.Shape{Kind: css.ShapeInset, Inset: [4]css.Dimension{css.Pts(0), css.Pts(px(2)), css.Pts(0), css.Pts(0)}}})
	root = el(sty(bg(blue), width(4), height(4), clip))
	assertPixels(t, run(t, root, page(4, 4)), 1, `
		BB__
		BB__
		BB__
		BB__`)
}
