package layout

import (
	"image/color"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

func shadow(x, y, blur, spread float64, c css.Color, inset bool) func(*css.Style) {
	return func(s *css.Style) {
		s.BoxShadow = append(s.BoxShadow, css.Shadow{X: px(x), Y: px(y), Blur: px(blur), Spread: px(spread), Color: c, Inset: inset})
	}
}

func overflowHidden(s *css.Style) { s.Overflow = css.OverflowHidden }

func TestBoxShadow_Outer(t *testing.T) {
	root := doc(el(sty(width(5), height(5), margin(1), bg(blue), shadow(3, 3, 0, 0, red, false))))
	assertPixels(t, run(t, root, page(10, 10)), 1, `
		__________
		_BBBBB____
		_BBBBB____
		_BBBBB____
		_BBBBBRRR_
		_BBBBBRRR_
		____RRRRR_
		____RRRRR_
		____RRRRR_
		__________`)
}

func TestBoxShadow_OuterSpreadUnderTransparentBox(t *testing.T) {
	// The shadow does not show under the border box, even a transparent
	// one (CSS Backgrounds 3, 7.1).
	root := doc(el(sty(width(4), height(4), margin(2), shadow(0, 0, 0, 1, red, false))))
	assertPixels(t, run(t, root, page(8, 8)), 1, `
		________
		_RRRRRR_
		_R____R_
		_R____R_
		_R____R_
		_R____R_
		_RRRRRR_
		________`)
}

func TestBoxShadow_Inset(t *testing.T) {
	root := doc(el(sty(width(6), height(6), margin(1), bg(blue), border(1, css.Solid, lime),
		shadow(2, 2, 0, 0, red, true), shadow(-1, -1, 0, 0, css.Color{A: 1}, true))))
	assertPixels(t, run(t, root, page(10, 10)), 1, `
		__________
		_GGGGGGGG_
		_GRRRRRRG_
		_GRRRRRRG_
		_GRRBBBKG_
		_GRRBBBKG_
		_GRRBBBKG_
		_GRRKKKKG_
		_GGGGGGGG_
		__________`)
}

func TestBoxShadow_Blurred(t *testing.T) {
	root := doc(el(sty(width(20), height(20), margin(20), shadow(0, 0, 8, 0, red, false))))
	img := render(t, run(t, root, page(60, 60)), 1)
	if c := at(img, 17, 30); c.G > 200 || c.G < 40 {
		t.Errorf("3px out of the box: %v, want a partly transparent red", c)
	}
	if c := at(img, 10, 30); c.G <= at(img, 17, 30).G {
		t.Errorf("the shadow fades outward: %v at 10px, %v at 17px", c, at(img, 17, 30))
	}
	if c := at(img, 30, 30); code(c) != '_' {
		t.Errorf("under the box: %v, want the page", c)
	}
	if c := at(img, 1, 30); code(c) != '_' {
		t.Errorf("far from the box: %v, want the page", c)
	}
}

func TestOutline_Offset(t *testing.T) {
	root := doc(el(sty(width(4), height(1), margin(3), border(1, css.Solid, red), func(s *css.Style) {
		s.Outline = css.Border{Width: px(1), Style: css.Solid, Color: blue}
		s.OutlineOffset = px(1)
	})))
	assertPixels(t, run(t, root, page(12, 9)), 1, `
		____________
		_BBBBBBBBBB_
		_B________B_
		_B_RRRRRR_B_
		_B_R____R_B_
		_B_RRRRRR_B_
		_B________B_
		_BBBBBBBBBB_
		____________`)
}

func TestReplaced_ObjectFit(t *testing.T) {
	tests := []struct {
		fit  css.ObjectFit
		pos  [2]css.Dimension
		want string
	}{
		{css.FitContain, [2]css.Dimension{{Kind: css.Percent, Value: 0.5}, {Kind: css.Percent, Value: 0.5}}, `
			______
			_RRRR_
			_BBBB_
			_BBBB_
			_RRRR_
			______`},
		{css.FitNone, [2]css.Dimension{{Kind: css.Percent}, {Kind: css.Percent, Value: 1}}, `
			______
			_RRRR_
			_RRRR_
			_RRRR_
			_BBRR_
			______`},
	}
	for _, tt := range tests {
		img := &Node{Kind: Replaced, Picture: pic(color.NRGBA{B: 255, A: 255}, 2, 1), Style: sty(width(4), height(4), margin(1), bg(red), func(s *css.Style) {
			s.ObjectFit, s.ObjectPosition = tt.fit, tt.pos
		})}
		assertPixels(t, run(t, doc(img), page(6, 6)), 1, tt.want)
	}
}

func TestCanvas_Propagation(t *testing.T) {
	none := func(s *css.Style) { s.Display = css.DisplayNone }
	paint := func(s *css.Style) { s.Contain = css.ContainPaint }
	tests := []struct {
		name string
		root *Node
		want string
	}{
		{"root without a box", el(sty(bg(red), none)), `
			____
			____`},
		{"body with paint containment", el(sty(), el(sty(height(1), bg(red), paint))), `
			RRRR
			____`},
		{"root with paint containment", el(sty(paint), el(sty(height(1), bg(red)))), `
			RRRR
			____`},
		{"body propagates", el(sty(), el(sty(height(1), bg(red)))), `
			RRRR
			RRRR`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPixels(t, run(t, tt.root, page(4, 2)), 1, tt.want)
		})
	}
}

func TestOverflow_BodyPropagatesToViewport(t *testing.T) {
	root := el(sty(), el(sty(height(1), overflowHidden), el(sty(width(2), height(3), bg(red)))))
	assertPixels(t, run(t, root, page(3, 3)), 1, `
		RR_
		RR_
		RR_`)
}

func TestOverflow_ClippedContentAddsNoPage(t *testing.T) {
	root := doc(el(sty(height(2), overflowHidden), txt(sty(), "X"), br(sty()), txt(sty(), "X"), br(sty()), txt(sty(), "X")))
	res := run(t, root, page(10, 15))
	if len(res.Pages) != 1 {
		t.Errorf("%d pages, want 1: the clipped lines add none", len(res.Pages))
	}
}

func TestInline_DecorationsInTreeOrder(t *testing.T) {
	// The span's shadow, cast one em left, covers the text before it, as
	// box-shadow-overlapping-001 asks.
	span := el(inlineSty(fg(lime), shadow(-10, 0, 0, 0, lime, false)), txt(inlineSty(fg(lime)), "X"))
	root := doc(el(sty(fg(red)), txt(inlineSty(fg(red)), "X"), span))
	img := render(t, run(t, root, page(20, 10)), 1)
	if c := code(at(img, 5, 5)); c != 'G' {
		t.Errorf("the first glyph shows %c, want the span's shadow over it", c)
	}
}

func TestShades_Blink(t *testing.T) {
	tests := []struct {
		in, dark, light compose.Color
	}{
		{compose.Color{A: 1}, compose.Color{A: 1}, compose.Color{R: 0x54 / 255.0, G: 0x54 / 255.0, B: 0x54 / 255.0, A: 1}},
		{compose.Color{B: 1, A: 1}, compose.Color{B: 2.0 / 3, A: 1}, compose.Color{B: 1, A: 1}},
	}
	for _, tt := range tests {
		d, l := shades(tt.in)
		if !colorNear(d, tt.dark) || !colorNear(l, tt.light) {
			t.Errorf("shades(%v) = %v, %v; want %v, %v", tt.in, d, l, tt.dark, tt.light)
		}
	}
	// #8d99ae, measured on Chromium's groove: (73, 79, 90) and (207, 225, 255).
	d, l := shades(compose.Color{R: 141 / 255.0, G: 153 / 255.0, B: 174 / 255.0, A: 1})
	if math.Abs(d.R*255-73) > 1 || math.Abs(l.G*255-225) > 1 || math.Abs(l.B*255-255) > 1 {
		t.Errorf("shades of #8d99ae = %v, %v", d, l)
	}
}

func colorNear(a, b compose.Color) bool {
	return math.Abs(a.R-b.R) < 1e-6 && math.Abs(a.G-b.G) < 1e-6 && math.Abs(a.B-b.B) < 1e-6 && a.A == b.A
}

func TestDashStroke_FitsTheSide(t *testing.T) {
	// The Chromium reference of the html borders fixture: a 4px dashed
	// side 170px long has 15 dashes of 8px; a dotted one 22 dots.
	w := px(4)
	st := dashStroke(css.Dashed, compose.Color{A: 1}, w, px(170), false)
	if dash, gap := st.Dash[0], st.Dash[1]; math.Abs(dash-px(8)) > 1e-9 || math.Abs(15*dash+14*gap-px(170)) > 1e-9 {
		t.Errorf("dashed pattern %v", st.Dash)
	}
	st = dashStroke(css.Dotted, compose.Color{A: 1}, w, px(166), false)
	if pitch := st.Dash[1]; st.Dash[0] != 0 || st.Cap != compose.RoundCap || math.Abs(21*pitch-px(166)) > 1e-9 {
		t.Errorf("dotted pattern %v, cap %v", st.Dash, st.Cap)
	}
}

func TestDashStroke_ThinDotsAreSquare(t *testing.T) {
	// Under 3px the dots are squares a width long, butt-capped.
	w := px(1)
	st := dashStroke(css.Dotted, compose.Color{A: 1}, w, px(9), false)
	if st.Cap == compose.RoundCap || st.Dash[0] != w || math.Abs(5*w+4*st.Dash[1]-px(9)) > 1e-9 {
		t.Errorf("thin dotted pattern %v, cap %v", st.Dash, st.Cap)
	}
}

func TestBackgroundRound_KeepsRatio(t *testing.T) {
	// CSS Backgrounds 3, 3.4: round on x only with an auto height scales
	// the height with the width.
	st := sty(func(s *css.Style) {
		s.BackgroundRepeat = [2]css.Repeat{css.RepeatRound, css.RepeatRepeat}
		s.BackgroundSize = css.BackgroundSize{Width: css.Pts(6), Height: css.Dimension{Kind: css.Auto}}
	})
	e := &engine{}
	area := compose.Rect{Width: 8, Height: 12}
	tiles := e.tiles(st, pic(color.NRGBA{B: 255, A: 255}, 4, 4), area, area, 0)
	if len(tiles) == 0 || tiles[0].Width != 8 || tiles[0].Height != 8 {
		t.Errorf("tiles %v, want 8 by 8", tiles)
	}
}
