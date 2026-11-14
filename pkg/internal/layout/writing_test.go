package layout

import (
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
