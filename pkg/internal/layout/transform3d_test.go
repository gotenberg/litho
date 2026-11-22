package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// rotateY180 is rotateY(180deg): the box turned about its vertical axis,
// its back towards the viewer.
var rotateY180 = []css.TransformFunc{{Kind: css.TransformMatrix3D, M: &[16]float64{-1, 0, 0, 0, 0, 1, 0, 0, 0, 0, -1, 0, 0, 0, 0, 1}}}

func TestTransform3D_PerspectiveScalesATranslationAlongZ(t *testing.T) {
	// CSS Transforms 2, 7 and 12: a box moved 1px towards the viewer under
	// its parent's 2px perspective is drawn d/(d-z) = 2 times larger about
	// the parent's perspective origin, its centre.
	child := square(2, 2, blue, margin(1, 0, 0, 1), func(s *css.Style) {
		s.Transform = []css.TransformFunc{{Kind: css.TransformTranslate, X: css.Pts(0), Y: css.Pts(0), V: [6]float64{px(1)}}}
	})
	parent := el(sty(width(4), height(4), func(s *css.Style) {
		s.Display = css.FlowRoot
		s.Space3D = &css.Space3D{Perspective: px(2)}
	}), child)
	assertPixels(t, run(t, doc(parent), page(4, 4)), 1, "BBBB\nBBBB\nBBBB\nBBBB")
}

func TestTransform3D_PerspectiveOrigin(t *testing.T) {
	// CSS Transforms 2, 8: the same scale about a perspective origin at
	// the parent's top left corner.
	child := square(2, 2, blue, func(s *css.Style) {
		s.Transform = []css.TransformFunc{{Kind: css.TransformTranslate, X: css.Pts(0), Y: css.Pts(0), V: [6]float64{px(1)}}}
	})
	parent := el(sty(width(4), height(4), func(s *css.Style) {
		s.Space3D = &css.Space3D{Perspective: px(2), OriginSet: true, Origin: [2]css.Dimension{css.Pts(0), css.Pts(0)}}
	}), child)
	assertPixels(t, run(t, doc(parent), page(4, 4)), 1, "BBBB\nBBBB\nBBBB\nBBBB")
}

func TestTransform3D_HiddenBackface(t *testing.T) {
	// CSS Transforms 2, 11: a box turned to show its back is drawn unless
	// its back face is hidden.
	turned := func(hidden bool) *Node {
		return square(2, 2, blue, func(s *css.Style) {
			s.Transform = rotateY180
			s.Space3D = &css.Space3D{BackfaceHidden: hidden}
			if !hidden {
				s.Space3D = nil
			}
		})
	}
	assertPixels(t, run(t, doc(turned(false)), page(2, 2)), 1, "BB\nBB")
	assertPixels(t, run(t, doc(turned(true)), page(2, 2)), 1, "__\n__")
}

func TestTransform3D_PreserveAccumulatesTheContext(t *testing.T) {
	// CSS Transforms 2, 9 and 11: inside a parent of transform-style:
	// preserve-3d turned a half turn, a child turned a half turn more
	// faces the viewer and is drawn; a flat parent flattens its child,
	// whose own half turn then shows its hidden back.
	tests := []struct {
		name     string
		preserve bool
		opacity  float64
		want     string
	}{
		{"preserve-3d", true, 1, "BB\nBB"},
		{"flat", false, 1, "__\n__"},
		{"preserve-3d made flat by opacity", true, 0.5, "__\n__"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			child := square(2, 2, blue, func(s *css.Style) {
				s.Transform = rotateY180
				s.Space3D = &css.Space3D{BackfaceHidden: true}
			})
			parent := el(sty(width(2), height(2), func(s *css.Style) {
				s.Transform = rotateY180
				s.Opacity = tt.opacity
				if tt.preserve {
					s.Space3D = &css.Space3D{Preserve3D: true}
				}
			}), child)
			assertPixels(t, run(t, doc(parent), page(2, 2)), 1, tt.want)
		})
	}
}

func TestTransform3D_FlatPlaneHoldsItsContext(t *testing.T) {
	// CSS Transforms 2, 9: a parent of preserve-3d turned a quarter about
	// x projects to no area and draws nothing of its own, but its child,
	// turned a quarter more, lies flat again and is drawn.
	quarterX := []css.TransformFunc{{Kind: css.TransformMatrix3D, M: &[16]float64{1, 0, 0, 0, 0, 0, -1, 0, 0, 1, 0, 0, 0, 0, 0, 1}}}
	child := square(2, 2, blue, func(s *css.Style) { s.Transform = quarterX })
	parent := el(sty(width(2), height(2), bg(red), func(s *css.Style) {
		s.Transform = quarterX
		s.Space3D = &css.Space3D{Preserve3D: true}
	}), child)
	assertPixels(t, run(t, doc(parent), page(2, 2)), 1, "BB\nBB")
}
