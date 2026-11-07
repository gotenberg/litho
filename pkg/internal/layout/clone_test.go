package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestClone_AcrossPages draws a paragraph whose decorations clone at page
// breaks with its whole border on every page, its lines leaving room for
// the bottom edge before the cut and starting below the top edge after
// it.
//
// # Corpus evidence
//
//   - LibreOffice 9.0.0 image: a .fodt paragraph with a 2pt border and
//     4pt padding running over seven pages draws its four borders on every
//     page, the text 4pt inside them.
func TestClone_AcrossPages(t *testing.T) {
	small := func(s *css.Style) { s.FontSize = px(2); s.Orphans, s.Widows = 1, 1 }
	st := sty(small, border(1, css.Solid, red), fg(blue), func(s *css.Style) { s.BoxDecorationBreak = css.DecorationCloneAll })
	p := el(st, txt(st, "a"), br(st), txt(st, "a"), br(st), txt(st, "a"), br(st), txt(st, "a"))
	res := run(t, doc(p), page(6, 8))
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	assertPixels(t, res, 1, `
		RRRRRR
		RBB__R
		RBB__R
		RBB__R
		RBB__R
		RBB__R
		RBB__R
		RRRRRR`)
	assertPixels(t, res, 2, `
		RRRRRR
		RBB__R
		RBB__R
		RRRRRR
		______
		______
		______
		______`)
}

// TestClone_SliceAtPages keeps CSS's clone sliced at page breaks, as
// Chromium prints it.
func TestClone_SliceAtPages(t *testing.T) {
	small := func(s *css.Style) { s.FontSize = px(2); s.Orphans, s.Widows = 1, 1 }
	st := sty(small, border(1, css.Solid, red), fg(blue), func(s *css.Style) { s.BoxDecorationBreak = css.DecorationClone })
	p := el(st, txt(st, "a"), br(st), txt(st, "a"), br(st), txt(st, "a"), br(st), txt(st, "a"))
	res := run(t, doc(p), page(6, 8))
	assertPixels(t, res, 2, `
		RBB__R
		RBB__R
		RRRRRR
		______
		______
		______
		______
		______`)
}
