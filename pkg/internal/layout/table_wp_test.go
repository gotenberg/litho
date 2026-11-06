package layout

import (
	"image"
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// The tests of this file are WeasyPrint's draw tests for tables
// (tests/draw/test_table.py, release 70.0), their HTML rebuilt as node
// trees with Ahem standing for WeasyPrint's test font. WeasyPrint's test
// stylesheet gives tables box-sizing: border-box; wpTable does too.

// wpTable returns a table as WeasyPrint's test stylesheet styles it.
func wpTable(m []func(*css.Style), kids ...*Node) *Node {
	return table(append(mods(func(s *css.Style) { s.BoxSizing = css.BorderBox }), m...), kids...)
}

func TestWeasyPrint_Tables21(t *testing.T) {
	cell := mods(bg(blue), padding(1), border(1, css.Solid, red))
	root := doc(deep(wpTable(mods(func(s *css.Style) { s.Width = pct(1) }),
		tr(nil, td(cell, "abc"), td(cell, "abc")),
		tr(nil, td(cell, "abc"), td(cell, ""))), collapse, font(2), normalLH))
	res := run(t, root, wpPage(25, 11, 1))
	assertPages(t, res, 11, `
		_________________________
		_rrrrrrrrrrrrrrrrrrrrrrr_
		_rBBBBBBBBBBrBBBBBBBBBBr_
		_rBKKKKKKBBBrBKKKKKKBBBr_
		_rBKKKKKKBBBrBKKKKKKBBBr_
		_rBBBBBBBBBBrBBBBBBBBBBr_
		_rrrrrrrrrrrrrrrrrrrrrrr_
		_________________________
		_________________________
		_________________________
		_________________________
		_________________________
		_rrrrrrrrrrrrrrrrrrrrrrr_
		_rBBBBBBBBBBrBBBBBBBBBBr_
		_rBKKKKKKBBBrBBBBBBBBBBr_
		_rBKKKKKKBBBrBBBBBBBBBBr_
		_rBBBBBBBBBBrBBBBBBBBBBr_
		_rrrrrrrrrrrrrrrrrrrrrrr_
		_________________________
		_________________________
		_________________________
		_________________________`, wpLetters)
}

func TestWeasyPrint_Tables9(t *testing.T) {
	cell := mods(border(1, css.Solid, red), width(4), height(2))
	row := func() *Node { return tr(nil, td(cell, ""), td(cell, ""), td(cell, "")) }
	root := doc(wpTable(mods(collapse, fixedLayout),
		thead(mods(border(3, css.Solid, blue)), td(cell, ""), td(cell, ""), td(cell, "")),
		row(), row(), row()))
	res := run(t, root, wpPage(22, 18, 1))
	assertPages(t, res, 18, `
		______________________
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBB____R____R____BBB_
		_BBB____R____R____BBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		__R_____R____R_____R__
		__R_____R____R_____R__
		__RRRRRRRRRRRRRRRRRR__
		__R_____R____R_____R__
		__R_____R____R_____R__
		__RRRRRRRRRRRRRRRRRR__
		______________________
		______________________
		______________________
		______________________
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBB____R____R____BBB_
		_BBB____R____R____BBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		__R_____R____R_____R__
		__RRRRRRRRRRRRRRRRRR__
		______________________
		______________________
		______________________
		______________________
		______________________
		______________________
		______________________`, wpLetters)
}

func TestWeasyPrint_Tables10(t *testing.T) {
	cell := mods(border(1, css.Solid, red), width(4), height(2))
	row := func() *Node { return tr(nil, td(cell, ""), td(cell, ""), td(cell, "")) }
	root := doc(wpTable(mods(collapse, fixedLayout, margin(0, 0, 0, 1)),
		row(), row(), row(),
		tfoot(mods(border(3, css.Solid, blue)), td(cell, ""), td(cell, ""), td(cell, ""))))
	res := run(t, root, wpPage(22, 17, 1))
	assertPages(t, res, 17, `
		______________________
		__RRRRRRRRRRRRRRRRRR__
		__R_____R____R_____R__
		__R_____R____R_____R__
		__RRRRRRRRRRRRRRRRRR__
		__R_____R____R_____R__
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBB____R____R____BBB_
		_BBB____R____R____BBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		______________________
		______________________
		______________________
		______________________
		__RRRRRRRRRRRRRRRRRR__
		__R_____R____R_____R__
		__R_____R____R_____R__
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBB____R____R____BBB_
		_BBB____R____R____BBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		______________________
		______________________
		______________________
		______________________
		______________________`, wpLetters)
}

func TestWeasyPrint_Tables11(t *testing.T) {
	cell := mods(border(1, css.Solid, red), width(4), height(3))
	tb := wpTable(mods(display(css.InlineTable), width(11), collapse, fixedLayout), tr(nil, td(cell, ""), td(cell, "")))
	body := el(sty(font(0), func(s *css.Style) { s.TextAlign = css.AlignRight }), tb)
	res := run(t, el(sty(), body), wpPage(20, 10, 1))
	assertPages(t, res, 10, `
		____________________
		________RRRRRRRRRRR_
		________R____R____R_
		________R____R____R_
		________R____R____R_
		________RRRRRRRRRRR_
		____________________
		____________________
		____________________
		____________________`, wpLetters)
}

func TestWeasyPrint_Tables15(t *testing.T) {
	cell := mods(border(1, css.Solid, red), width(4), height(3))
	row := func() *Node { return tr(nil, td(cell, ""), td(cell, ""), td(cell, "")) }
	root := doc(wpTable(mods(collapse, fixedLayout, margin(0, 0, 0, 1)),
		row(), row(), tr(nil, spans(td(cell, ""), 3, 1)),
		tfoot(mods(border(3, css.Solid, blue)), td(cell, ""), td(cell, ""), td(cell, ""))))
	res := run(t, root, wpPage(22, 18, 1))
	assertPages(t, res, 18, `
		______________________
		__RRRRRRRRRRRRRRRRRR__
		__R_____R____R_____R__
		__R_____R____R_____R__
		__R_____R____R_____R__
		__RRRRRRRRRRRRRRRRRR__
		__R_____R____R_____R__
		__R_____R____R_____R__
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBB____R____R____BBB_
		_BBB____R____R____BBB_
		_BBB____R____R____BBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		______________________
		______________________
		__RRRRRRRRRRRRRRRRRR__
		__R________________R__
		__R________________R__
		__R________________R__
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBB____R____R____BBB_
		_BBB____R____R____BBB_
		_BBB____R____R____BBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		_BBBBBBBBBBBBBBBBBBBB_
		______________________
		______________________
		______________________
		______________________`, wpLetters)
}

// TestWeasyPrint_Tables20 builds the table with box-sizing: content-box:
// WeasyPrint's grid gives the table a 10px content box although its test
// stylesheet asks for border-box.
func TestWeasyPrint_Tables20(t *testing.T) {
	green := mods(bg(lime))
	root := doc(table(mods(width(10), border(1, css.Solid, red)),
		col(green), col(green),
		tbody(green, tr(green), tr(green, td(mods(height(1), bg(blue)), ""))),
		tfoot(green)))
	res := run(t, root, wpPage(20, 5, 1))
	assertPages(t, res, 5, `
		____________________
		_RRRRRRRRRRRR_______
		_RBBBBBBBBBBR_______
		_RRRRRRRRRRRR_______
		____________________`, wpLetters)
}

// tables24 builds WeasyPrint's test_tables_24: borders on rows and cells
// collapsing, cells aligned in the middle, in the direction dir.
func tables24(dir css.Direction) *Node {
	magenta := css.Color{R: 1, B: 1, A: 1}
	cyan := css.Color{G: 1, B: 1, A: 1}
	orange := css.Color{R: 1, G: 165.0 / 255, A: 1}
	green := css.Color{G: 128.0 / 255, A: 1}
	yellow := css.Color{R: 1, G: 1, A: 1}
	left := td(mods(bg(magenta), side(css.Bottom, 5, css.Solid, blue), side(css.Right, 1, css.Solid, green)), "XX")
	right := td(mods(bg(yellow), side(css.Bottom, 1, css.Solid, cyan), side(css.Left, 1, css.Dotted, orange)), "X X")
	row := tr(mods(side(css.Left, 1, css.Solid, red), side(css.Right, 3, css.Solid, lime)), left, right)
	if dir == css.RTL {
		row.Children = []*Node{right, left}
	}
	tb := deep(wpTable(mods(margin(1)), row), collapse, font(2), func(s *css.Style) { s.Direction = dir })
	deep(row, valign(css.Middle))
	row.Style.VerticalAlign = css.VerticalAlign{}
	return doc(tb)
}

func TestWeasyPrint_Tables24(t *testing.T) {
	assertPages(t, run(t, tables24(css.LTR), page(18, 10)), 10, `
		__________________
		_RKKKKgYYYYYYGGG__
		_RKKKKgKKYYKKGGG__
		_BBBBBBKKYYKKGGG__
		_BBBBBBYYYYYYGGG__
		_BBBBBBCCCCCCGGG__
		_BBBBBB___________
		_BBBBBB___________
		__________________
		__________________`, wpLetters)
}

func TestWeasyPrint_Tables24RTL(t *testing.T) {
	assertPages(t, run(t, tables24(css.RTL), page(18, 10)), 10, `
		__________________
		_RKKKKgYYYYYYGGG__
		_RKKKKgKKYYKKGGG__
		_BBBBBBKKYYKKGGG__
		_BBBBBBYYYYYYGGG__
		_BBBBBBCCCCCCGGG__
		_BBBBBB___________
		_BBBBBB___________
		__________________
		__________________`, wpLetters)
}

func TestWeasyPrint_TablesBorderTfoot(t *testing.T) {
	top := mods(side(css.Top, 1, css.Solid, black))
	none := mods(border(0, css.BorderNone, black))
	tb := wpTable(mods(collapse, fg(red)),
		tr(nil, td(top, "A")), tr(nil, td(top, "B")), tr(nil, td(top, "C")), tr(nil, td(none, "D")),
		tfoot(mods(fg(blue)), tr(nil, td(none, "E"))))
	deep(tb, font(2))
	for _, n := range []*Node{tb.Children[4], tb.Children[4].Children[0], tb.Children[4].Children[0].Children[0]} {
		n.Style.Color = blue
	}
	for _, r := range tb.Children[:4] {
		r.Style.Color = red
		r.Children[0].Style.Color = red
	}
	res := run(t, doc(tb), page(12, 12))
	assertPages(t, res, 12, `
		KK__________
		RR__________
		RR__________
		KK__________
		RR__________
		RR__________
		KK__________
		RR__________
		RR__________
		BB__________
		BB__________
		____________
		RR__________
		RR__________
		BB__________
		BB__________
		____________
		____________
		____________
		____________
		____________
		____________
		____________
		____________`, wpLetters)
}

func TestWeasyPrint_TablesColumnBackground(t *testing.T) {
	cell := mods(border(1, css.Solid, black), fg(red), valign(css.AlignTop), font(2))
	root := doc(table(nil, colgroup(nil, col(mods(bg(blue)))),
		tr(nil, td(cell, "a a")), tr(nil, td(cell, "a a"))))
	res := run(t, root, page(8, 4))
	assertPages(t, res, 4, `
		KKKKKKKK
		KRRBBRRK
		KRRBBRRK
		KKKKKKKK
		KKKKKKKK
		KRRBBRRK
		KRRBBRRK
		KKKKKKKK`, wpLetters)
}

// floatTable builds WeasyPrint's test_float_table: a table beside two
// floats, of display d, in the direction dir.
func floatTable(d css.Display, dir css.Direction) *Node {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			img.Set(x, y, color.NRGBA{B: 255, A: 255})
		}
	}
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	pattern := &Picture{Source: &compose.ImageSource{Image: img}, Width: px(4), Height: px(4)}
	left := &Node{Kind: Replaced, Style: inlineSty(float(css.FloatLeft)), Picture: pattern}
	right := &Node{Kind: Replaced, Style: inlineSty(float(css.FloatRight)), Picture: pic(color.NRGBA{B: 255, A: 255}, 4, 4)}
	tb := wpTable(mods(display(d)), tbody(nil, tr(nil, td(nil, "a"))))
	div := el(sty(), left, right, tb)
	return deep(el(sty(), el(sty(), div)), font(2), fg(lime), func(s *css.Style) { s.Direction = dir })
}

func TestWeasyPrint_FloatTable(t *testing.T) {
	for _, d := range []css.Display{css.Table, css.InlineTable} {
		res := run(t, floatTable(d, css.LTR), page(15, 5))
		assertPages(t, res, 5, `
			rBBBGG_____aaaa
			BBBBGG_____aaaa
			BBBB_______aaaa
			BBBB_______aaaa
			_______________`, wpLetters)
	}
}

func TestWeasyPrint_FloatTableRTL(t *testing.T) {
	res := run(t, floatTable(css.Table, css.RTL), page(15, 5))
	assertPages(t, res, 5, `
		rBBB_____GGaaaa
		BBBB_____GGaaaa
		BBBB_______aaaa
		BBBB_______aaaa
		_______________`, wpLetters)
}
