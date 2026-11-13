package layout

import (
	"image/color"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

var cyan = css.Color{G: 1, B: 1, A: 1}

func blendMode(m css.BlendMode) func(*css.Style) {
	return func(s *css.Style) { s.MixBlendMode = m }
}

func isolate(s *css.Style) { s.Isolation = css.IsolationIsolate }

// TestBlendModes_MatchTypeset locks the conversion of a css blend mode to
// the typeset one by its value.
func TestBlendModes_MatchTypeset(t *testing.T) {
	pairs := [][2]int{
		{int(css.BlendNormal), int(typeset.BlendNormal)}, {int(css.BlendMultiply), int(typeset.BlendMultiply)},
		{int(css.BlendScreen), int(typeset.BlendScreen)}, {int(css.BlendOverlay), int(typeset.BlendOverlay)},
		{int(css.BlendDarken), int(typeset.BlendDarken)}, {int(css.BlendLighten), int(typeset.BlendLighten)},
		{int(css.BlendColorDodge), int(typeset.BlendColorDodge)}, {int(css.BlendColorBurn), int(typeset.BlendColorBurn)},
		{int(css.BlendHardLight), int(typeset.BlendHardLight)}, {int(css.BlendSoftLight), int(typeset.BlendSoftLight)},
		{int(css.BlendDifference), int(typeset.BlendDifference)}, {int(css.BlendExclusion), int(typeset.BlendExclusion)},
		{int(css.BlendHue), int(typeset.BlendHue)}, {int(css.BlendSaturation), int(typeset.BlendSaturation)},
		{int(css.BlendColor), int(typeset.BlendColor)}, {int(css.BlendLuminosity), int(typeset.BlendLuminosity)},
	}
	for _, p := range pairs {
		if p[0] != p[1] {
			t.Errorf("css blend mode %d is typeset's %d", p[0], p[1])
		}
	}
}

// TestMixBlendMode_BlendsWithStackingContext locks CSS Compositing 1, 3.2
// and 3.3: a box of mix-blend-mode multiply blends with what its stacking
// context painted beneath it, cyan over yellow giving lime; a box of
// isolation isolate between them makes an isolated group, so the cyan box
// blends with nothing and shows cyan; and an element that blends paints
// in its stacking context's positioned layers.
func TestMixBlendMode_BlendsWithStackingContext(t *testing.T) {
	child := square(2, 2, cyan, blendMode(css.BlendMultiply))
	for _, c := range []struct {
		name string
		mid  *css.Style
		want color.NRGBA
	}{
		{"backdrop", sty(), color.NRGBA{0, 255, 0, 255}},
		{"isolated", sty(isolate), color.NRGBA{0, 255, 255, 255}},
		// An opacity of 1 makes no stacking context.
		{"opaque", sty(func(s *css.Style) { s.Opacity = 1 }), color.NRGBA{0, 255, 0, 255}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := doc(el(sty(width(4), height(2), bg(yellow)), el(c.mid, child)))
			img := render(t, run(t, root, page(4, 4)), 1)
			if got := at(img, 1, 1); !similar(got, c.want) {
				t.Errorf("pixel %v, want %v", got, c.want)
			}
		})
	}
}

// TestMixBlendMode_Group locks that a blending box of two children
// composites them as one group before it blends: where the second covers
// the first, the multiply shows the second over the backdrop alone.
func TestMixBlendMode_Group(t *testing.T) {
	root := doc(el(sty(width(4), height(2), bg(yellow), rel(0, 0)),
		el(sty(width(4), height(2), abs(0, auto, auto, 0), blendMode(css.BlendMultiply)),
			square(4, 2, red, abs(0, auto, auto, 0)),
			square(2, 2, cyan, abs(0, auto, auto, 2)))))
	img := render(t, run(t, root, page(4, 2)), 1)
	if got := at(img, 0, 1); !similar(got, color.NRGBA{255, 0, 0, 255}) {
		t.Errorf("red over yellow: %v", got)
	}
	if got := at(img, 3, 1); !similar(got, color.NRGBA{0, 255, 0, 255}) {
		t.Errorf("cyan over yellow: %v", got)
	}
}

// TestInlineOpacity_Group locks that an inline box of opacity 0.5 paints
// its background and its text at half alpha, as one group: the text over
// the background shows as the text alone at half alpha.
func TestInlineOpacity_Group(t *testing.T) {
	half := func(s *css.Style) { s.Opacity = 0.5 }
	span := inlineSty(bg(red), fg(blue), half)
	root := doc(el(sty(), txt(sty(), "x"), el(span, txt(span, "x")), el(inlineSty(bg(red)), txt(inlineSty(bg(red)), "x"))))
	img := render(t, run(t, root, page(30, 10)), 1)
	if got := at(img, 15, 5); !similar(got, color.NRGBA{128, 128, 255, 255}) {
		t.Errorf("text in the translucent span: %v", got)
	}
}

// TestBackgroundBlendMode_IsolatedFromBackdrop locks CSS Compositing 1,
// 3.4: the image of a background blends with the background colour,
// cyan multiplied by yellow giving lime, and not with what lies beneath
// the element, here a red parent: the result covers it.
func TestBackgroundBlendMode_IsolatedFromBackdrop(t *testing.T) {
	cyanImage := grad(&css.Gradient{Angle: 180, Stops: gstops(cyan, math.NaN(), cyan, math.NaN())})
	for _, c := range []struct {
		name  string
		color css.Color
		want  color.NRGBA
	}{
		{"over a colour", yellow, color.NRGBA{0, 255, 0, 255}},
		// With no colour the image blends with nothing.
		{"over no colour", css.Color{}, color.NRGBA{0, 255, 255, 255}},
	} {
		t.Run(c.name, func(t *testing.T) {
			box := square(2, 2, c.color, cyanImage, func(s *css.Style) { s.BackgroundBlendMode = css.BlendMultiply })
			root := doc(el(sty(bg(red)), box))
			img := render(t, run(t, root, page(2, 2)), 1)
			if got := at(img, 1, 1); !similar(got, c.want) {
				t.Errorf("pixel %v, want %v", got, c.want)
			}
		})
	}
}

// TestCompositing_DisplayContents locks that an element of display
// contents, which has no box, composites nothing: its opacity, blend
// mode, clip path and mask apply to no group, and its children paint as
// they would without it, as Chromium prints them.
func TestCompositing_DisplayContents(t *testing.T) {
	contents := sty(display(css.Contents), func(s *css.Style) {
		s.Opacity = 0.3
		s.MixBlendMode = css.BlendDifference
		s.ClipPath = css.ClipPath{Boxed: true, Box: css.GeoContent}
		s.Mask = css.Mask{Images: []css.MaskImage{{Gradient: &css.Gradient{Angle: 180, Stops: gstops(css.Color{}, math.NaN(), css.Color{}, math.NaN())}}}}
	})
	root := doc(el(contents, square(2, 2, red)))
	assertPixels(t, run(t, root, page(2, 2)), 1, `
		RR
		RR`)
}
