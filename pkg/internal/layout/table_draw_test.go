package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

var yellow = css.Color{R: 1, G: 1, A: 1}

func TestTableDraw_Separated(t *testing.T) {
	cell := mods(width(2), height(2), border(1, css.Solid, blue), bg(lime))
	root := doc(table(mods(border(1, css.Solid, red), spacing(1, 1)), tr(nil, td(cell, ""), td(cell, ""))))
	assertPixels(t, run(t, root, page(13, 8)), 1, `
		RRRRRRRRRRRRR
		R___________R
		R_BBBB_BBBB_R
		R_BGGB_BGGB_R
		R_BGGB_BGGB_R
		R_BBBB_BBBB_R
		R___________R
		RRRRRRRRRRRRR`)
}

func TestTableDraw_EmptyCellsHide(t *testing.T) {
	cell := mods(width(2), height(2), border(1, css.Solid, blue), func(s *css.Style) { s.EmptyCells = css.Hide })
	full := td(cell, "a")
	deep(full, font(1), fg(black))
	root := doc(table(mods(border(1, css.Solid, red), spacing(1, 1)), tr(mods(bg(yellow)), full, td(cell, ""))))
	assertPixels(t, run(t, root, page(13, 8)), 1, `
		RRRRRRRRRRRRR
		R___________R
		R_BBBB______R
		R_BKyB______R
		R_ByyB______R
		R_BBBB______R
		R___________R
		RRRRRRRRRRRRR`)
}

func TestTableDraw_Collapsed(t *testing.T) {
	cell := mods(width(2), height(2), border(1, css.Solid, red), bg(lime))
	row := func() *Node { return tr(nil, td(cell, ""), td(cell, "")) }
	root := doc(table(mods(collapse, border(3, css.Solid, blue)), row(), row()))
	assertPixels(t, run(t, root, page(11, 11)), 1, `
		BBBBBBBBBBB
		BBBBBBBBBBB
		BBBBBBBBBBB
		BBBGGRGGBBB
		BBBGGRGGBBB
		BBBRRRRRBBB
		BBBGGRGGBBB
		BBBGGRGGBBB
		BBBBBBBBBBB
		BBBBBBBBBBB
		BBBBBBBBBBB`)
}

func TestTableDraw_BackgroundLayers(t *testing.T) {
	cell := mods(width(2), height(2))
	root := doc(table(mods(bg(red), spacing(1, 1)),
		col(mods(bg(blue))),
		tr(mods(bg(lime)), td(cell, ""), td(cell, "")),
		tr(nil, td(cell, ""), td(append(cell, bg(yellow)), ""))))
	assertPixels(t, run(t, root, page(7, 7)), 1, `
		RRRRRRR
		RGGRGGR
		RGGRGGR
		RRRRRRR
		RBBRyyR
		RBBRyyR
		RRRRRRR`)
}

func TestTableDraw_HeaderOnTwoPages(t *testing.T) {
	cell := mods(width(4), height(2))
	root := doc(table(nil,
		thead(mods(bg(blue)), tr(nil, td(cell, ""))),
		tbody(nil, tr(mods(bg(lime)), td(cell, "")), tr(mods(bg(red)), td(cell, "")), tr(mods(bg(yellow)), td(cell, "")))))
	res := run(t, root, page(4, 6))
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	assertPixels(t, res, 1, `
		BBBB
		BBBB
		GGGG
		GGGG
		RRRR
		RRRR`)
	assertPixels(t, res, 2, `
		BBBB
		BBBB
		yyyy
		yyyy
		____
		____`)
}
