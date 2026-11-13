package layout

import (
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
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

// TestBoxShadow_EmptyBox locks that a box of no width and no height casts
// its outer shadow, grown by its spread (CSS Backgrounds 3, 7.1).
func TestBoxShadow_EmptyBox(t *testing.T) {
	root := doc(el(sty(width(0), height(0), shadow(2, 2, 0, 2, red, false))))
	assertPixels(t, run(t, root, page(5, 5)), 1, `
		RRRR_
		RRRR_
		RRRR_
		RRRR_
		_____`)
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
		in, dark, light typeset.Color
	}{
		// Too dark to darken (Chromium 9.0.0 print): the lighter shade and
		// its lighter.
		{typeset.Color{A: 1}, typeset.Color{R: 0x54 / 255.0, G: 0x54 / 255.0, B: 0x54 / 255.0, A: 1}, typeset.Color{R: 0xa8 / 255.0, G: 0xa8 / 255.0, B: 0xa8 / 255.0, A: 1}},
		{typeset.Color{R: 0x21 / 255.0, A: 1}, typeset.Color{R: 117 / 255.0, A: 1}, typeset.Color{R: 201 / 255.0, A: 1}},
		{typeset.Color{G: 0x30 / 255.0, A: 1}, typeset.Color{A: 1}, typeset.Color{G: 132 / 255.0, A: 1}},
		{typeset.Color{B: 1, A: 1}, typeset.Color{B: 171 / 255.0, A: 1}, typeset.Color{B: 1, A: 1}},
	}
	for _, tt := range tests {
		d, l := shades(tt.in, true)
		if !colorNear(d, tt.dark) || !colorNear(l, tt.light) {
			t.Errorf("shades(%v) = %v, %v; want %v, %v", tt.in, d, l, tt.dark, tt.light)
		}
	}
	// Off the Blink rendering, black darkens to black.
	if d, l := shades(typeset.Color{A: 1}, false); d != (typeset.Color{A: 1}) || !colorNear(l, typeset.Color{R: 0x54 / 255.0, G: 0x54 / 255.0, B: 0x54 / 255.0, A: 1}) {
		t.Errorf("shades(black, false) = %v, %v; want black and #545454", d, l)
	}
	// #8d99ae, measured on Chromium's groove: (73, 79, 90) and (207, 225, 255).
	d, l := shades(typeset.Color{R: 141 / 255.0, G: 153 / 255.0, B: 174 / 255.0, A: 1}, true)
	if math.Abs(d.R*255-73) > 1 || math.Abs(l.G*255-225) > 1 || math.Abs(l.B*255-255) > 1 {
		t.Errorf("shades of #8d99ae = %v, %v", d, l)
	}
}

func colorNear(a, b typeset.Color) bool {
	return math.Abs(a.R-b.R) < 1e-6 && math.Abs(a.G-b.G) < 1e-6 && math.Abs(a.B-b.B) < 1e-6 && a.A == b.A
}

func TestDashStroke_FitsTheSide(t *testing.T) {
	// The Chromium reference of the html borders fixture: a 4px dashed
	// side 170px long has 15 dashes of 8px; a dotted one 22 dots.
	w := px(4)
	st := dashStroke(css.Dashed, typeset.Color{A: 1}, w, px(170), false)
	if dash, gap := st.Dash[0], st.Dash[1]; math.Abs(dash-px(8)) > 1e-9 || math.Abs(15*dash+14*gap-px(170)) > 1e-9 {
		t.Errorf("dashed pattern %v", st.Dash)
	}
	st = dashStroke(css.Dotted, typeset.Color{A: 1}, w, px(166), false)
	if pitch := st.Dash[1]; st.Dash[0] != 0 || st.Cap != typeset.RoundCap || math.Abs(21*pitch-px(166)) > 1e-9 {
		t.Errorf("dotted pattern %v, cap %v", st.Dash, st.Cap)
	}
}

func TestDashStroke_ThinDotsAreSquare(t *testing.T) {
	// Under 3px the dots are squares a width long, butt-capped.
	w := px(1)
	st := dashStroke(css.Dotted, typeset.Color{A: 1}, w, px(9), false)
	if st.Cap == typeset.RoundCap || st.Dash[0] != w || math.Abs(5*w+4*st.Dash[1]-px(9)) > 1e-9 {
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
	area := typeset.Rect{Width: 8, Height: 12}
	tiles := e.tiles(st, pic(color.NRGBA{B: 255, A: 255}, 4, 4), area, area, 0)
	if len(tiles) == 0 || tiles[0].Width != 8 || tiles[0].Height != 8 {
		t.Errorf("tiles %v, want 8 by 8", tiles)
	}
}

func TestOutline_UnderPositionedDescendants(t *testing.T) {
	// The outline of an in-flow block paints with its stacking context's
	// content, under the positioned boxes, as Chromium paints it: CSS 2.1,
	// Appendix E lets an implementation draw outlines before step 10.
	//
	// # Corpus evidence
	//
	//   - probe, html: Chromium 9.4.1 paints the blue outline of an
	//     absolutely positioned box over the red outline of the grid item
	//     holding it where both lie (wpt
	//     positioned-grid-descendant-containing-block-008).
	line := func(c css.Color) func(*css.Style) {
		return func(s *css.Style) { s.Outline = css.Border{Width: px(1), Style: css.Solid, Color: c} }
	}
	child := square(2, 2, lime, abs(1, auto, auto, 1), line(blue))
	root := doc(el(sty(width(4), height(4), margin(1), line(red)), child))
	assertPixels(t, run(t, root, page(6, 6)), 1, `
		BBBBRR
		BGGB_R
		BGGB_R
		BBBB_R
		R____R
		RRRRRR`)
}

func TestOutline_AfterDescendantOutlines(t *testing.T) {
	// A box's outline paints after the outlines of the blocks inside it
	// ([painter.outlines]): the red outline of the outer box covers the
	// blue one of its child and the black one of the child's sibling where
	// they lie, the sibling's covers the child's top edge, and the child's
	// bottom edge covers the sibling's box; the lime outline of the
	// child's own child shows nowhere.
	line := func(c css.Color) func(*css.Style) {
		return func(s *css.Style) { s.Outline = css.Border{Width: px(1), Style: css.Solid, Color: c} }
	}
	inner := el(sty(height(1), line(lime)))
	child := el(sty(line(blue)), inner)
	sib := el(sty(height(1), line(black)))
	root := doc(el(sty(width(4), margin(1), line(red)), child, sib))
	assertPixels(t, run(t, root, page(6, 5)), 1, `
		RRRRRR
		RKKKKR
		RBBBBR
		RRRRRR
		______`)
	root = doc(el(sty(width(4), margin(1)), el(sty(line(blue)), el(sty(height(2), line(lime))))))
	assertPixels(t, run(t, root, page(6, 4)), 1, `
		BBBBBB
		B____B
		B____B
		BBBBBB`)
}

func TestShadowSigma_ChromiumBoxes(t *testing.T) {
	// A 10px blur is a deviation of 5px, 15.625 pixels at 300 pixels an
	// inch: three boxes of 29 pixels, as Chromium draws it.
	s := shadowSigma(px(10))
	if d := boxWidth(s * chromiumScale); d != 29 {
		t.Errorf("box width at 300 pixels an inch %d, want 29", d)
	}
	if want := math.Sqrt(210) / chromiumScale; math.Abs(s-want) > 1e-12 {
		t.Errorf("deviation %v, want %v", s, want)
	}
	if s := shadowSigma(px(0.4)); s != 0 {
		t.Errorf("a 0.4px blur has deviation %v, want none: Chromium's boxes are narrower than 2 pixels", s)
	}
	for _, d := range []int{2, 3, 8, 9, 30} {
		if got := boxWidth(math.Sqrt(boxVariance(d))); got != d {
			t.Errorf("boxWidth of the deviation of %d boxes is %d", d, got)
		}
	}
}

func TestBoxShadow_BlurredInsetFadesAtThePaddingEdge(t *testing.T) {
	// Chromium covers an inner shadow 5px past the padding box before a
	// 12px blur, so its edge holds 30% of the colour where CSS Backgrounds
	// 3, 7.1 asks for half.
	root := doc(el(sty(width(60), height(60), margin(10), bg(css.Color{R: 1, G: 1, B: 1, A: 1}), shadow(0, 0, 12, 0, css.Color{A: 1}, true))))
	img := render(t, run(t, root, page(80, 80)), 1)
	if c := at(img, 10, 40); c.G < 160 || c.G > 205 {
		t.Errorf("at the padding edge: %v, want about 70%% of white", c)
	}
	if c := at(img, 40, 40); c.G < 250 {
		t.Errorf("in the middle: %v, want white", c)
	}
}

// shadowImages returns the images of the first page of res, in paint
// order, and the SHA-256 of their samples.
func shadowImages(t *testing.T, res *Result) []string {
	t.Helper()
	var sums []string
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Image:
				img, err := v.Source.Decode(t.Context())
				if err != nil {
					t.Fatalf("Decode: %v", err)
				}
				n, ok := img.(*image.NRGBA)
				if !ok {
					t.Fatalf("image %T, want NRGBA", img)
				}
				sum := sha256.Sum256(n.Pix)
				sums = append(sums, fmt.Sprintf("%dx%d %x", n.Rect.Dx(), n.Rect.Dy(), sum[:8]))
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[0].Content)
	return sums
}

func TestShadows_SameBytesOnEveryArchitecture(t *testing.T) {
	st := sty(width(40), height(30), margin(20), bg(blue), border(2, css.Solid, lime),
		func(s *css.Style) {
			s.BorderRadius[css.TopLeft] = css.Radius{H: css.Pts(px(9)), V: css.Pts(px(9))}
		},
		shadow(5, 3, 14, 2, red, false), shadow(-2, 4, 9, 1, css.Color{G: 0.5, A: 0.6}, true),
		func(s *css.Style) {
			s.TextShadow = []css.Shadow{{X: px(3), Y: px(2), Blur: px(5), Color: css.Color{B: 1, A: 0.8}}}
		})
	got := shadowImages(t, run(t, doc(el(st, txt(st, "ab"))), page(100, 100)))
	want := []string{
		"69x62 8b77fc880ef10b4c",
		"89x78 625ec3c923ed60f3",
		"78x57 2bec32ecf5fc4de2",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("shadow images:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestFloat_PositionedDescendantsPaintInParentContext(t *testing.T) {
	// CSS 2.1, Appendix E, step 5: a float paints as if it made a
	// stacking context, but its positioned descendants take part in the
	// enclosing one: a z-index 10 box inside a float paints over a
	// positioned box that follows the float.
	inner := square(2, 2, lime, rel(0, 0), zIndex(10))
	root := doc(el(sty(width(4), height(2)),
		el(sty(float(css.FloatLeft), width(2), height(2), bg(red)), inner),
		square(3, 2, blue, rel(0, 0))))
	assertPixels(t, run(t, root, page(4, 2)), 1, `
		GGB_
		GGB_`)
}
