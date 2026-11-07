package layout

import (
	"image/color"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestOfficeShapes draws what the converters build for a shape: an
// absolutely positioned box turned by a transform, holding text or a
// picture, page-anchored or not, about its centre, as ODF draw:transform
// turns a frame (ODF 1.3 Part 3, 19.229); and a box positioned inside an inline block,
// as the text of a shape sits in its vector box.
func TestOfficeShapes(t *testing.T) {
	t.Parallel()
	turn := func(s *css.Style) {
		s.Transform = []css.TransformFunc{{Kind: css.TransformRotate, V: [6]float64{math.Pi / 2}}}
	}
	anchor := func(s *css.Style) { s.PageAnchor = true }
	redText := func(s string, m ...func(*css.Style)) *Node {
		st := sty(append([]func(*css.Style){font(2), fg(red)}, m...)...)
		return el(st, txt(st, s))
	}
	img := &Node{Kind: Replaced, Style: sty(), Picture: pic(color.NRGBA{B: 255, A: 255}, 4, 2)}
	rel := func(s *css.Style) { s.Position = css.Relative }
	inlineBox := func(s *css.Style) { s.Display = css.InlineBlock }
	tests := []struct {
		name string
		root *Node
		want string
	}{
		{"turned text", doc(el(sty(abs(1, math.NaN(), math.NaN(), 1), width(4), height(2), turn), redText("XX"))), `
			__RR__
			__RR__
			__RR__
			__RR__
			______
			______`},
		{"turned picture", doc(el(sty(abs(1, math.NaN(), math.NaN(), 1), width(4), height(2), turn), img)), `
			__BB__
			__BB__
			__BB__
			__BB__
			______
			______`},
		{"turned on the page", doc(el(sty(height(1)), el(sty(abs(1, math.NaN(), math.NaN(), 1), anchor, width(4), height(2), turn), img))), `
			__BB__
			__BB__
			__BB__
			__BB__
			______
			______`},
		{"positioned in an inline block", doc(el(sty(font(2)), el(sty(inlineBox, rel, width(6), height(4), bg(lime)), el(sty(abs(2, math.NaN(), math.NaN(), 2), width(2), height(2), bg(red)))))), `
			GGGGGG
			GGGGGG
			GGRRGG
			GGRRGG
			______
			______`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertPixels(t, run(t, tt.root, page(6, 6)), 1, tt.want)
		})
	}
}
