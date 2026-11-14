package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestTurn_VerticalRoot lays a document whose root is vertical-rl out on
// pages turned a quarter turn (CSS Writing Modes 4, 8: the principal
// writing mode is the root's): its block flow runs from the right edge of
// the paper to the left one and breaks onto the next page there, as
// Chromium prints it, on pages of the paper's size.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) prints fifteen
//     paragraphs of a vertical-rl root on five 5 by 3 inch pages, each
//     page's first line at its right margin.
func TestTurn_VerticalRoot(t *testing.T) {
	t.Parallel()
	wm := func(s *css.Style) { s.WritingMode = css.VerticalRL }
	kid := func(c css.Color) *Node { return el(sty(wm, width(2), bg(c))) }
	root := el(sty(wm), el(sty(wm), kid(lime), kid(blue), kid(lime), kid(blue)))
	res := runWith(t, root, Options{Page: page(6, 4), BrowserRules: true})
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	if pg := res.Pages[0]; pg.Width != px(6) || pg.Height != px(4) {
		t.Errorf("page of %v by %v, want the paper's", pg.Width, pg.Height)
	}
	assertPixels(t, res, 1, `
		GGBBGG
		GGBBGG
		GGBBGG
		GGBBGG`)
	assertPixels(t, res, 2, `
		____BB
		____BB
		____BB
		____BB`)
}

// TestTurn_VerticalLRRoot lays a vertical-lr root out on pages reflected
// across their diagonal: its block flow runs from the left edge of the
// paper to the right one, the lines of vertical-lr stacking left to right
// (CSS Writing Modes 4, 3.1), and its glyph runs are reflected back so
// that they read as text set top to bottom.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) draws the first column
//     of a vertical-lr root at the left margin, 8px from the left and top
//     edges of an 800 by 600 window.
func TestTurn_VerticalLRRoot(t *testing.T) {
	t.Parallel()
	wm := func(s *css.Style) { s.WritingMode = css.VerticalLR }
	kid := func(c css.Color) *Node { return el(sty(wm, width(2), bg(c))) }
	root := el(sty(wm), el(sty(wm), kid(lime), kid(blue), kid(lime), kid(blue)))
	res := runWith(t, root, Options{Page: page(6, 4), BrowserRules: true})
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	assertPixels(t, res, 1, `
		GGBBGG
		GGBBGG
		GGBBGG
		GGBBGG`)
	assertPixels(t, res, 2, `
		BB____
		BB____
		BB____
		BB____`)
	text := el(sty(wm), el(sty(wm), txt(sty(wm), "X")))
	runs := texts(runWith(t, text, Options{Page: page(20, 20), BrowserRules: true}), 1)
	if len(runs) != 1 || !runs[0].run.Mirror {
		t.Errorf("%d runs, want one reflected", len(runs))
	}
}
