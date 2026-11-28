package layout

import (
	"context"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestWritingMode_Orthogonal lays the text of a box of a vertical writing
// mode along its height, turned a quarter turn: reading bottom to top,
// lines stacking left to right in sideways-lr; top to bottom, lines
// stacking right to left otherwise; the box's width from its line count
// when auto, as Word and Writer draw a cell or a frame of vertical text.
func TestWritingMode_Orthogonal(t *testing.T) {
	t.Parallel()
	wm := func(m css.WritingMode) func(*css.Style) { return func(s *css.Style) { s.WritingMode = m } }
	two := func(m css.WritingMode, mods ...func(*css.Style)) *Node {
		st := sty(append([]func(*css.Style){font(2), wm(m)}, mods...)...)
		r := inlineSty(font(2), fg(red), wm(m))
		b := inlineSty(font(2), fg(blue), wm(m))
		return el(st, el(r, txt(r, "XXX")), txt(st, " "), el(b, txt(b, "XX")))
	}
	inline := func(s *css.Style) { s.Display = css.InlineBlock }
	tests := []struct {
		name string
		root *Node
		want string
	}{
		{"sideways-lr", doc(two(css.SidewaysLR, width(6), height(6))), `
			RR____
			RR____
			RRBB__
			RRBB__
			RRBB__
			RRBB__
			______`},
		{"vertical-rl", doc(two(css.VerticalRL, width(6), height(6))), `
			__BBRR
			__BBRR
			__BBRR
			__BBRR
			____RR
			____RR
			______`},
		{"sideways-rl", doc(two(css.SidewaysRL, width(6), height(6))), `
			__BBRR
			__BBRR
			__BBRR
			__BBRR
			____RR
			____RR
			______`},
		{"an inline block takes the width of its lines", doc(el(sty(font(2)), two(css.SidewaysLR, height(6), inline, bg(lime)), txt(sty(), ""))), `
			RRGG__
			RRGG__
			RRBB__
			RRBB__
			RRBB__
			RRBB__
			______`},
		{"a table cell takes its row's height and the width of its lines", doc(table(mods(font(2)), tr(mods(height(6)), el(sty(display(css.TableCell), font(2), wm(css.SidewaysLR), bg(lime)), txt(sty(font(2), fg(red)), "XXX XX"))))), `
			RRGG__
			RRGG__
			RRRR__
			RRRR__
			RRRR__
			RRRR__
			______`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertPixels(t, run(t, tt.root, page(6, 7)), 1, tt.want)
		})
	}
}

// TestWritingMode_MarginsStayInTheOrthogonalRoot keeps the margins of the
// content of an orthogonal flow root inside the root (CSS Writing Modes
// 4, 7.3): a block's margins collapse through no anonymous block, so the
// content turns about the root's content box and the block lands one
// margin in from its right and top edges.
func TestWritingMode_MarginsStayInTheOrthogonalRoot(t *testing.T) {
	t.Parallel()
	wm := func(s *css.Style) { s.WritingMode = css.VerticalRL }
	kid := sty(wm, margin(1, 1, 1, 1), width(1), height(1), bg(lime))
	root := doc(el(sty(wm, width(6), height(6)), el(kid)))
	assertPixels(t, runWith(t, root, Options{Page: page(6, 7), BrowserRules: true}), 1, `
		______
		____G_
		______
		______
		______
		______
		______`)
}

// TestWritingMode_PhysicalSidesInATurnedFlow keeps the physical sides of
// a box inside a vertical flow on the sides of the page they name (CSS
// Writing Modes 4, 6): in vertical-rl a block's margin-right is its
// block-start margin and its margin-top its inline-start one, and its
// width its block size, however the layout turns the flow onto the page.
// Its margins stay inside the flow root (7.3).
func TestWritingMode_PhysicalSidesInATurnedFlow(t *testing.T) {
	t.Parallel()
	wm := func(s *css.Style) { s.WritingMode = css.VerticalRL }
	kid := sty(wm, margin(1, 2, 0, 0), width(1), height(2), bg(lime))
	root := doc(el(sty(wm, width(6), height(6)), el(kid)))
	assertPixels(t, runWith(t, root, Options{Page: page(6, 7), BrowserRules: true}), 1, `
		______
		___G__
		___G__
		______
		______
		______
		______`)
}

// TestWritingMode_PicturesStandUpright draws the pictures of a vertical
// flow upright: a writing mode turns text, not the content of a replaced
// element (CSS Writing Modes 4, 7), and a background image keeps its
// physical position and size (CSS Backgrounds 3, 3.6 to 3.9). A 2 by 1 px
// picture, red then blue, reads red then blue on the page, as a block at
// the block-start (right) edge of a vertical-rl box and as its background
// at the top left.
func TestWritingMode_PicturesStandUpright(t *testing.T) {
	t.Parallel()
	wm := func(s *css.Style) { s.WritingMode = css.VerticalRL }
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	img.Set(1, 0, color.NRGBA{B: 255, A: 255})
	picture := &Picture{Source: source(img), Width: px(2), Height: px(1)}
	replaced := doc(el(sty(wm, width(6), height(3)), &Node{Kind: Replaced, Style: sty(wm), Picture: picture}))
	assertPixels(t, runWith(t, replaced, Options{Page: page(6, 3), BrowserRules: true}), 1, `
		____RB
		______
		______`)
	background := doc(el(sty(wm, width(6), height(3), func(s *css.Style) {
		s.BackgroundImage = "i.png"
		s.BackgroundRepeat = [2]css.Repeat{css.RepeatNoRepeat, css.RepeatNoRepeat}
	})))
	res, err := Layout(context.Background(), background, ahem(t), Options{Page: page(6, 3), BrowserRules: true, Image: imageLoader()})
	if err != nil {
		t.Fatal(err)
	}
	assertPixels(t, res, 1, `
		RB____
		______
		______`)
}

// TestWritingMode_BlockSizeOfItsContent sizes a vertical block of width
// auto in a horizontal flow by its content: its width is its block size,
// its content's (CSS 2.1, 10.6.3, read in its writing mode; CSS Writing
// Modes 4, 7.3), and it sits at the start of its containing block.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) draws a vertical-rl div
//     of width auto holding one 20px Ahem column 20px wide at the left of
//     the body.
func TestWritingMode_BlockSizeOfItsContent(t *testing.T) {
	t.Parallel()
	wm := func(s *css.Style) { s.WritingMode = css.VerticalRL }
	root := doc(el(sty(wm, height(2), bg(lime)), el(sty(wm, width(3), bg(blue)))))
	assertPixels(t, runWith(t, root, Options{Page: page(6, 2), BrowserRules: true}), 1, `
		BBB___
		BBB___`)
}

// TestWritingMode_CentralBaseline aligns an image in a vertical line of
// upright text on the central baseline, the dominant one there (CSS
// Inline 3, 4.2.1): its middle on the middle of the strut's em box, so a
// picture wider than the strut is the whole line box, where the
// alphabetic baseline would add the strut's descent beside it.
func TestWritingMode_CentralBaseline(t *testing.T) {
	t.Parallel()
	wm := func(s *css.Style) { s.WritingMode = css.VerticalRL }
	img := &Node{Kind: Replaced, Style: inlineSty(wm), Picture: pic(color.NRGBA{R: 255, A: 255}, 20, 2)}
	root := doc(el(sty(wm, height(4), bg(lime)), img))
	assertPixels(t, runWith(t, root, Options{Page: page(24, 4), BrowserRules: true}), 1, `
		RRRRRRRRRRRRRRRRRRRR____
		RRRRRRRRRRRRRRRRRRRR____
		GGGGGGGGGGGGGGGGGGGG____
		GGGGGGGGGGGGGGGGGGGG____`)
}

// TestWritingMode_HorizontalInsideVertical lays a horizontal-tb block
// inside a vertical-rl flow out as an orthogonal flow root (CSS Writing
// Modes 4, 7.3): its content runs on the page as in a horizontal flow,
// its blocks stacking downwards, at the block-start (right) edge of its
// container, its width given, its height its content's.
func TestWritingMode_HorizontalInsideVertical(t *testing.T) {
	t.Parallel()
	wm := func(s *css.Style) { s.WritingMode = css.VerticalRL }
	h := el(sty(width(3)), el(sty(height(1), bg(red))), el(sty(height(1), bg(blue))))
	root := doc(el(sty(wm, width(6), height(4)), h))
	assertPixels(t, runWith(t, root, Options{Page: page(6, 4), BrowserRules: true}), 1, `
		___RRR
		___BBB
		______
		______`)
}

// TestWritingMode_OrthogonalInlineBlockBaseline aligns an inline-block of
// a vertical writing mode in a horizontal line by the bottom of its margin
// box: its lines give no baseline in the line's writing mode (CSS Writing
// Modes 4, 4.3).
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) sets the bottom of a
//     vertical-rl inline-block 64px high on the baseline of the 16px Ahem
//     text around it.
func TestWritingMode_OrthogonalInlineBlockBaseline(t *testing.T) {
	t.Parallel()
	wm := func(s *css.Style) { s.WritingMode = css.VerticalRL }
	ib := el(sty(wm, func(s *css.Style) { s.Display = css.InlineBlock }, height(20), bg(lime)), txt(inlineSty(wm), "X"))
	root := doc(el(sty(), txt(inlineSty(), "X"), ib))
	g := grid(render(t, runWith(t, root, Options{Page: page(40, 30), BrowserRules: true}), 1))
	// The inline-block, one 10px column 20px high, stands on the baseline
	// of the X before it, 8px below the top of its glyph.
	if g[19][15] != 'G' || g[20][15] == 'G' || g[12][5] != 'K' || g[11][5] == 'K' {
		t.Errorf("the inline-block does not end on the baseline:\n%s", strings.Join(g, "\n"))
	}
}

// TestWritingMode_OrthogonalRootFitsItsContainingBlockHeight fits the
// lines of an orthogonal flow root of auto height in its containing
// block's definite height, its available inline space (CSS Writing Modes
// 4, 7.3.1), not in the initial containing block's: a vertical-rl block in
// a box 3 px tall breaks "X X X X" into two lines of three pixels.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 breaks a
//     vertical-rl block of auto size in a 200px tall box into lines 200px
//     long, four lines of 16px/20px text.
func TestWritingMode_OrthogonalRootFitsItsContainingBlockHeight(t *testing.T) {
	t.Parallel()
	wm := func(s *css.Style) { s.WritingMode = css.VerticalRL }
	st := sty(font(1), wm, fg(red))
	root := doc(el(sty(width(6), height(3), bg(lime)), el(st, txt(st, "X X X X"))))
	assertPixels(t, runWith(t, root, Options{Page: page(6, 8), BrowserRules: true}), 1, `
		RRGGGG
		GGGGGG
		RRGGGG
		______
		______
		______
		______
		______`)
}

// TestWritingMode_HorizontalRootFitsItsVerticalContainersWidth fits the
// lines of a horizontal block inside a vertical-lr block of definite width
// in that width: the vertical flow's block size is definite, and so is the
// containing block's height in it, the horizontal root's available inline
// space (CSS Writing Modes 4, 7.3.1), not the initial containing block's.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 breaks the lines
//     of a horizontal block with 10% margins in a vertical-lr block 500px
//     wide and 200px tall at 460px, and at 100px in one 200px wide and
//     500px tall.
func TestWritingMode_HorizontalRootFitsItsVerticalContainersWidth(t *testing.T) {
	t.Parallel()
	h := func(s *css.Style) { s.WritingMode = css.HorizontalTB }
	v := func(s *css.Style) { s.WritingMode = css.VerticalLR }
	st := sty(font(1), h, fg(red))
	root := doc(el(sty(v, width(6), height(3), bg(lime)), el(st, txt(st, "X X X X X X"))))
	assertPixels(t, runWith(t, root, Options{Page: page(10, 4), BrowserRules: true}), 1, `
		RGRGRG____
		RGRGRG____
		GGGGGG____
		__________`)
}

// TestWritingMode_GradientPointsAlongThePage draws the gradient of a box
// inside a vertical flow in the page's directions: its angle is physical
// (CSS Images 3, 3.1.1), so "to right" runs left to right on the page
// whatever the writing mode, as a background image stands upright.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 paints a
//     "to right" gradient of a box in a vertical-lr and in a vertical-rl
//     flow, and of a horizontal box in a vertical-lr flow, red on the left
//     half of the box.
func TestWritingMode_GradientPointsAlongThePage(t *testing.T) {
	t.Parallel()
	for name, wm := range map[string]css.WritingMode{"vertical-rl": css.VerticalRL, "vertical-lr": css.VerticalLR, "sideways-lr": css.SidewaysLR} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			grad := func(s *css.Style) {
				s.BackgroundGradient = &css.Gradient{Angle: 90, Stops: gstops(red, 2.0, blue, 2.0)}
			}
			flow := func(s *css.Style) { s.WritingMode = wm }
			root := doc(el(sty(flow, width(4), height(2)), el(sty(width(4), height(2), grad))))
			assertPixels(t, runWith(t, root, Options{Page: page(4, 2), BrowserRules: true}), 1, `
				RRBB
				RRBB`)
		})
	}
}
