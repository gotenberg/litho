package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func clipPath(c css.ClipPath) func(*css.Style) {
	return func(s *css.Style) { s.ClipPath = c }
}

// TestClipPath_Shapes locks the basic shapes of CSS Shapes 1, 3.1 against
// their reference box (CSS Masking 1, 5.1), each clipping a red square of
// 4 by 4 pixels with a border of 1 pixel.
func TestClipPath_Shapes(t *testing.T) {
	pct := func(v float64) css.Dimension { return css.Dimension{Kind: css.Percent, Value: v} }
	pxd := func(v float64) css.Dimension { return css.Pts(px(v)) }
	tests := []struct {
		name string
		clip css.ClipPath
		want string
	}{
		{"inset", css.ClipPath{Boxed: true, Shape: &css.Shape{Kind: css.ShapeInset, Inset: [4]css.Dimension{pxd(1), pxd(2), pxd(1), pxd(1)}}}, `
			______
			_RRR__
			_RRR__
			_RRR__
			_RRR__
			______`},
		{"padding box", css.ClipPath{Boxed: true, Box: css.GeoPadding}, `
			______
			_RRRR_
			_RRRR_
			_RRRR_
			_RRRR_
			______`},
		{"content box inset", css.ClipPath{Boxed: true, Box: css.GeoContent, Shape: &css.Shape{Kind: css.ShapeInset, Inset: [4]css.Dimension{pxd(0), pxd(0), pxd(2), pxd(2)}}}, `
			______
			___RR_
			___RR_
			______
			______
			______`},
		{"polygon", css.ClipPath{Boxed: true, Shape: &css.Shape{Kind: css.ShapePolygon, Points: [][2]css.Dimension{{pct(0), pct(0)}, {pct(0.5), pct(0)}, {pct(0.5), pct(1)}, {pct(0), pct(1)}}}}, `
			KKK___
			KRR___
			KRR___
			KRR___
			KRR___
			KKK___`},
		{"path", css.ClipPath{Boxed: true, Shape: &css.Shape{Kind: css.ShapePath, Path: "M3 0 h3 v6 h-3 z"}}, `
			___KKK
			___RRK
			___RRK
			___RRK
			___RRK
			___KKK`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := doc(el(sty(width(4), height(4), bg(red), border(1, css.Solid, black), clipPath(tt.clip))))
			assertPixels(t, run(t, root, page(6, 6)), 1, tt.want)
		})
	}
}

// TestClipPath_EmptyHides locks that a shape enclosing nothing hides the
// element and its content (CSS Masking 1, 5.1).
func TestClipPath_EmptyHides(t *testing.T) {
	clip := css.ClipPath{Boxed: true, Shape: &css.Shape{Kind: css.ShapeInset, Inset: [4]css.Dimension{css.Pts(px(3)), css.Pts(px(3)), css.Pts(px(3)), css.Pts(px(3))}}}
	root := doc(el(sty(width(4), height(2), bg(red), clipPath(clip)), square(2, 2, blue)), square(2, 2, lime))
	assertPixels(t, run(t, root, page(4, 4)), 1, `
		____
		____
		GG__
		GG__`)
}

// TestClipPath_Circle locks a circle's radius: closest-side by default,
// from the centre to the reference box's nearest side.
func TestClipPath_Circle(t *testing.T) {
	half := css.Dimension{Kind: css.Percent, Value: 0.5}
	clip := css.ClipPath{Boxed: true, Shape: &css.Shape{Kind: css.ShapeCircle, Center: [2]css.Dimension{half, half}}}
	root := doc(el(sty(width(20), height(20), bg(red), clipPath(clip))))
	img := render(t, run(t, root, page(20, 20)), 1)
	for _, c := range []struct {
		x, y int
		in   bool
	}{{10, 10, true}, {10, 1, true}, {1, 10, true}, {1, 1, false}, {18, 18, false}} {
		if got := code(img.At(c.x, c.y)) == 'R'; got != c.in {
			t.Errorf("pixel (%d, %d) inside %v, want %v", c.x, c.y, got, c.in)
		}
	}
}

// TestClip_AbsolutelyPositioned locks CSS 2.2, 11.1.2: clip cuts an
// absolutely positioned box, its border and its content, absolutely
// positioned descendants included, to a rectangle from its border box's
// top left, auto its own edge; it does not apply to a box in flow.
func TestClip_AbsolutelyPositioned(t *testing.T) {
	autoEdge := css.Dimension{Kind: css.Auto}
	clip := func(top, right, bottom, left css.Dimension) func(*css.Style) {
		return func(s *css.Style) {
			s.Clip = css.ClipRect{Set: true, Edges: [4]css.Dimension{top, right, bottom, left}}
		}
	}
	pxd := func(v float64) css.Dimension { return css.Pts(px(v)) }
	tests := []struct {
		name string
		root *Node
		want string
	}{
		{"rect", doc(el(sty(abs(0, auto, auto, 0), width(4), height(4), bg(red), clip(pxd(1), pxd(3), pxd(3), pxd(1))),
			square(6, 1, blue, abs(1, auto, auto, 0)))), `
			______
			_BB___
			_RR___
			______`},
		{"auto", doc(el(sty(abs(0, auto, auto, 0), width(4), height(4), bg(red), clip(autoEdge, autoEdge, pxd(2), autoEdge)))), `
			RRRR__
			RRRR__
			______
			______`},
		{"in flow", doc(el(sty(width(4), height(4), bg(red), clip(pxd(1), pxd(3), pxd(3), pxd(1))))), `
			RRRR__
			RRRR__
			RRRR__
			RRRR__`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPixels(t, run(t, tt.root, page(6, 4)), 1, tt.want)
		})
	}
}

// TestClipPath_CircleCorner locks a circle sized to the farthest corner of
// the reference box: from the centre of a square it covers the square.
func TestClipPath_CircleCorner(t *testing.T) {
	half := css.Dimension{Kind: css.Percent, Value: 0.5}
	clip := css.ClipPath{Boxed: true, Shape: &css.Shape{Kind: css.ShapeCircle, Center: [2]css.Dimension{half, half}, Radius: [2]css.ShapeRadius{{Side: css.RadiusFarthestCorner}}}}
	root := doc(el(sty(width(20), height(20), bg(red), clipPath(clip))))
	img := render(t, run(t, root, page(20, 20)), 1)
	if code(img.At(0, 0)) != 'R' || code(img.At(19, 19)) != 'R' {
		t.Errorf("corners %v %v, want red", at(img, 0, 0), at(img, 19, 19))
	}
}
