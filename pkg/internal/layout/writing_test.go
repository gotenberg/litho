package layout

import (
	"context"
	"image"
	"image/color"
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
