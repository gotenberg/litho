package layout

import (
	"image/color"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestInlineEffects_OneGroupOverLines locks CSS Color 4, 15: an inline box
// of opacity 0.4 broken over two lines that overlap composites once, so
// where its second line paints over its first the colour is the one of
// the lines that do not overlap.
func TestInlineEffects_OneGroupOverLines(t *testing.T) {
	sp := inlineSty(bg(blue), fg(blue), func(s *css.Style) { s.Opacity = 0.4; s.LineHeight = css.Pts(px(5)) })
	root := doc(el(sty(width(20), func(s *css.Style) { s.LineHeight = css.Pts(px(5)) }), el(sp, txt(sp, "XX XX"))))
	img := render(t, run(t, root, page(20, 20)), 1)
	alone, overlap := at(img, 5, 1), at(img, 5, 5)
	if !similar(alone, color.NRGBA{153, 153, 255, 255}) || !similar(overlap, alone) {
		t.Errorf("first line %v, overlap %v: want one translucent blue", alone, overlap)
	}
}

// TestInlineEffects_ReachFloat locks CSS Color 4, 15: the float inside an
// inline box of opacity 0 is part of its rendering and is not drawn.
func TestInlineEffects_ReachFloat(t *testing.T) {
	sp := inlineSty(func(s *css.Style) { s.Opacity = 0 })
	root := doc(el(sty(width(10), height(10), bg(lime)), el(sp, el(sty(width(10), height(10), bg(red), float(css.FloatLeft))))))
	img := render(t, run(t, root, page(10, 10)), 1)
	if got := at(img, 5, 5); !similar(got, color.NRGBA{0, 255, 0, 255}) {
		t.Errorf("pixel %v, want lime", got)
	}
}

// TestInlineEffects_ClipPathFirstFragment locks the reference box of a
// clip-path on an inline box broken over lines: its first fragment, as
// Chromium draws it, so what it paints on its second line is clipped
// away (CSS Masking 1, 5.1).
func TestInlineEffects_ClipPathFirstFragment(t *testing.T) {
	sp := inlineSty(fg(blue), clipPath(css.ClipPath{Boxed: true, Box: css.GeoBorder}))
	root := doc(el(sty(width(20)), txt(sty(), "X"), el(sp, txt(sp, "X XX"))))
	img := render(t, run(t, root, page(20, 20)), 1)
	if got := code(img.At(15, 5)); got != 'B' {
		t.Errorf("first line: %c, want blue", got)
	}
	for _, x := range []int{5, 15} {
		if got := code(img.At(x, 15)); got != '_' {
			t.Errorf("second line at %d: %c, want clipped away", x, got)
		}
	}
}

// TestInlineEffects_MaskSliced locks CSS Masking 1, 6 with CSS
// Fragmentation 3, 5.4: the mask of an inline box under
// box-decoration-break slice is laid over its fragments end to end, a
// gradient keeping the first 10 pixels of the strip keeps the first line and
// masks the second.
func TestInlineEffects_MaskSliced(t *testing.T) {
	g := &css.Gradient{Angle: 90, Stops: gstops(black, math.NaN(), black, 10.0, css.Color{}, 10.0, css.Color{}, math.NaN())}
	sp := inlineSty(fg(blue), func(s *css.Style) { s.Mask = css.Mask{Images: []css.MaskImage{{Gradient: g}}} })
	root := doc(el(sty(width(10)), el(sp, txt(sp, "X X"))))
	img := render(t, run(t, root, page(10, 20)), 1)
	if first, second := code(img.At(5, 5)), code(img.At(5, 15)); first != 'B' || second != '_' {
		t.Errorf("first line %c, second %c: want blue, then masked", first, second)
	}
}
