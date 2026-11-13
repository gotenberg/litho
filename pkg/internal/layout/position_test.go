package layout

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// abs makes a style absolutely positioned at the offsets top, right,
// bottom and left in px, NaN for auto.
func abs(v ...float64) func(*css.Style) {
	return func(s *css.Style) {
		s.Position = css.Absolute
		d := dims(v...)
		s.Top, s.Right, s.Bottom, s.Left = d[0], d[1], d[2], d[3]
	}
}

// rel makes a style relatively positioned by left and top px.
func rel(left, top float64) func(*css.Style) {
	return func(s *css.Style) {
		s.Position = css.Relative
		s.Left, s.Top = css.Pts(px(left)), css.Pts(px(top))
	}
}

func zIndex(z int) func(*css.Style) { return func(s *css.Style) { s.ZIndex = css.ZIndex{Level: z} } }

// square returns an empty block of w by h px of colour c, changed by mods.
func square(w, h float64, c css.Color, m ...func(*css.Style)) *Node {
	return el(sty(append([]func(*css.Style){width(w), height(h), bg(c)}, m...)...))
}

func TestPosition_Widths(t *testing.T) {
	// CSS 2.1, 10.3.7: the offsets, the width and the margins solve
	// against the containing block, the padding box of the nearest
	// positioned ancestor or the page area.
	tests := []struct {
		name string
		mods []func(*css.Style)
		want [4]float64
	}{
		{"left and width", mods(abs(1, auto, auto, 2), width(3), height(1)), [4]float64{2, 1, 3, 1}},
		{"right and width", mods(abs(0, 1, auto, auto), width(3), height(1)), [4]float64{6, 0, 3, 1}},
		{"left and right stretch", mods(abs(0, 2, auto, 3), height(1)), [4]float64{3, 0, 5, 1}},
		{"bottom and height", mods(abs(auto, auto, 1, 0), width(1), height(2)), [4]float64{0, 7, 1, 2}},
		{"top and bottom stretch", mods(abs(2, auto, 3, 0), width(1)), [4]float64{0, 2, 1, 5}},
		{"auto margins centre", mods(abs(0, 0, 0, 0), width(4), height(2), margin(auto)), [4]float64{3, 4, 4, 2}},
		{"static position", mods(abs(auto), width(1), height(1)), [4]float64{0, 3, 1, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := doc(square(10, 3, blue), el(sty(tt.mods...)))
			rb := boxes(t, root, page(10, 10))
			if got := rect(kid(t, rb, 0, 1)); got != tt.want {
				t.Errorf("border box %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPosition_ShrinkToFit(t *testing.T) {
	root := doc(el(sty(abs(0, 0, auto, auto)), txt(sty(), "aa")))
	rb := boxes(t, root, page(100, 50))
	if got := rect(kid(t, rb, 0, 0)); got != [4]float64{80, 0, 20, 10} {
		t.Errorf("border box %v, want 20px wide at the right", got)
	}
}

func TestPosition_ContainingBlock(t *testing.T) {
	// The padding box of a relatively positioned ancestor holds the box,
	// which moves with it.
	inner := square(2, 2, red, abs(auto, 0, 0, auto))
	root := doc(square(1, 1, lime), el(sty(rel(1, 0), width(4), height(3), padding(1), bg(blue)), inner))
	assertPixels(t, run(t, root, page(8, 6)), 1, `
		G_______
		_BBBBBB_
		_BBBBBB_
		_BBBBBB_
		_BBBBRR_
		_BBBBRR_`)
}

func TestPosition_StaticInline(t *testing.T) {
	// A box that was inline sits where its inline box would have; one that
	// was a block starts below the line (CSS Position 3, 4.1).
	inl := sty(abs(auto), width(1), height(1), bg(red), func(s *css.Style) { s.OuterInline = true })
	blk := sty(abs(auto), width(1), height(1), bg(lime))
	root := doc(el(sty(font(2), fg(blue)), txt(sty(font(2), fg(blue)), "a"), el(inl), el(blk), txt(sty(font(2), fg(blue)), "a")))
	assertPixels(t, run(t, root, page(6, 4)), 1, `
		BBRB__
		BBBB__
		G_____
		______`)
}

func TestPosition_StackOrder(t *testing.T) {
	// CSS 2.1, Appendix E: a negative stack level paints below the in-flow
	// blocks, a positive one above the later positioned boxes, which paint
	// above the in-flow content in tree order.
	root := doc(
		square(2, 2, red, abs(0, auto, auto, 0), zIndex(-1)),
		square(4, 2, blue),
		square(2, 2, lime, abs(0, auto, auto, 2), zIndex(2)),
		square(2, 2, red, abs(0, auto, auto, 3)),
	)
	assertPixels(t, run(t, root, page(6, 2)), 1, `
		BBGGR_
		BBGGR_`)
}

func TestPosition_ZAutoLiftsDescendants(t *testing.T) {
	// A positioned box with z-index auto creates no stacking context: its
	// child of negative stack level paints below the root's in-flow blocks.
	child := square(2, 2, red, abs(0, auto, auto, 0), zIndex(-1))
	root := doc(el(sty(rel(0, 0), width(2), height(2)), child), square(2, 2, blue, margin(-2, 0, 0, 0)))
	assertPixels(t, run(t, root, page(2, 2)), 1, `
		BB
		BB`)
}

func TestPosition_Fragments(t *testing.T) {
	// An absolutely positioned box laid out in the flow's column breaks at
	// the page cuts like in-flow content.
	f := sty(font(2), fg(blue))
	lone := func(s *css.Style) { s.Orphans, s.Widows = 1, 1 }
	root := doc(el(sty(abs(1, auto, auto, 0), width(2), font(2), fg(blue), lone), txt(f, "a a")))
	res := run(t, root, page(2, 3))
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	assertPixels(t, res, 1, "__\nBB\nBB")
	assertPixels(t, res, 2, "BB\nBB\n__")
}

func TestPosition_FixedRepeats(t *testing.T) {
	// CSS 2.1, 9.6.1: a fixed box paints on every page.
	fixed := square(1, 1, red, func(s *css.Style) { s.Position = css.Fixed; s.Bottom = css.Pts(0); s.Right = css.Pts(0) })
	root := doc(fixed, square(2, 5, blue))
	res := run(t, root, page(2, 3))
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	assertPixels(t, res, 1, "BB\nBB\nBR")
	assertPixels(t, res, 2, "BB\nBB\n_R")
}

func TestPosition_RelativeInline(t *testing.T) {
	// CSS 2.1, 9.4.3: relative positioning moves an inline box and its
	// content.
	f := sty(font(2), fg(blue))
	span := el(inlineSty(font(2), fg(red), rel(1, 1)), txt(inlineSty(font(2), fg(red)), "a"))
	root := doc(el(f, txt(f, "a"), span))
	assertPixels(t, run(t, root, page(6, 3)), 1, `
		BB____
		BB_RR_
		___RR_`)
}

func TestPosition_Transform(t *testing.T) {
	// CSS Transforms 1: the box paints through its transform, about its
	// origin, and may paint past the page area into the margins.
	tests := []struct {
		name string
		fns  []css.TransformFunc
		want string
	}{
		{"translate", []css.TransformFunc{{Kind: css.TransformTranslate, X: css.Pts(px(1)), Y: css.Pts(px(2))}}, "____\n____\n_BB_\n_BB_"},
		{"rotate", []css.TransformFunc{{Kind: css.TransformRotate, V: [6]float64{math.Pi / 2}}}, "BB__\nBB__\n____\n____"},
		{"scale", []css.TransformFunc{{Kind: css.TransformScale, V: [6]float64{2, 1}}}, "BBBB\n____\n____\n____"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sq := square(2, 1, blue, margin(0, 0, 0, 1), func(s *css.Style) { s.Transform = tt.fns })
			switch tt.name {
			case "translate":
				sq = square(2, 2, blue, func(s *css.Style) { s.Transform = tt.fns })
			case "rotate":
				sq = square(2, 4, blue, func(s *css.Style) {
					s.Transform = tt.fns
					s.TransformOrigin = [2]css.Dimension{css.Pts(px(1)), css.Pts(px(1))}
				})
			}
			pb := page(4, 4)
			assertPixels(t, run(t, doc(sq), pb), 1, tt.want)
		})
	}
}

func TestPosition_SingularTransformPaintsNothing(t *testing.T) {
	sq := square(2, 2, red, func(s *css.Style) {
		s.Transform = []css.TransformFunc{{Kind: css.TransformScale, V: [6]float64{0, 1}}}
	})
	assertPixels(t, run(t, doc(sq), page(2, 2)), 1, "__\n__")
}

func TestPosition_RelativeTableParts(t *testing.T) {
	// CSS Position 3, 3.3: a relatively positioned row moves with the
	// cells that originate in it, and paints over the positioned box
	// before it in tree order (CSS 2.1, Appendix E, step 8). A percentage
	// top of a row whose row group specifies no height behaves as auto, as
	// Chromium lays it out.
	topPct := func(s *css.Style) { s.Position, s.Top = css.Relative, pct(1) }
	cell := func(c css.Color) *Node { return td(mods(width(2), height(2), bg(c)), "") }
	root := doc(
		square(2, 2, red, abs(0, auto, auto, 2)),
		table(mods(spacing(0, 0)), tbody(nil,
			tr(mods(rel(2, 0)), cell(lime)),
			tr(mods(topPct), cell(blue)),
		)),
	)
	assertPixels(t, run(t, root, page(6, 4)), 1, `
		__GG__
		__GG__
		BB____
		BB____`)
}

func TestPosition_TablePartHoldsAbsolute(t *testing.T) {
	// CSS Position 3, 2: a positioned row group holds its absolutely
	// positioned descendants, which move with it.
	inner := square(1, 1, lime, abs(0, auto, auto, 1))
	c := td(mods(width(2), height(2), bg(blue)), "")
	c.Children = []*Node{inner}
	root := doc(table(mods(spacing(0, 0)), tbody(mods(rel(1, 0)), tr(nil, c))))
	assertPixels(t, run(t, root, page(4, 2)), 1, `
		_BG_
		_BB_`)
}

func TestPosition_StaticInlineOnEmptyLine(t *testing.T) {
	// CSS 2.1, 10.3.7: a box that was inline-level, alone on its line,
	// sits where the empty line would put its inline box: beside the float
	// there, at the point text-align takes the line's content to.
	tests := []struct {
		name  string
		align css.TextAlign
		dir   func(*css.Style)
		side  css.Float
		want  string
	}{
		{"start beside a left float", css.AlignStart, nil, css.FloatLeft, "RRBB__"},
		{"centre beside a left float", css.AlignCenter, nil, css.FloatLeft, "RR__BB"},
		{"rtl start beside a right float", css.AlignStart, rtl, css.FloatRight, "__BBRR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := mods(width(6), func(s *css.Style) { s.TextAlign = tt.align })
			if tt.dir != nil {
				ms = append(ms, tt.dir)
			}
			inl := square(2, 1, blue, abs(auto), func(s *css.Style) { s.OuterInline = true })
			sp := func() *Node { return txt(sty(), " ") }
			root := doc(el(sty(ms...), sp(), square(2, 1, red, float(tt.side)), sp(), inl, sp()))
			assertPixels(t, run(t, root, page(6, 1)), 1, tt.want)
		})
	}
}

func TestPosition_TransformedTableParts(t *testing.T) {
	// CSS Transforms 1, 3: a row group, a row and a cell are transformable;
	// a column is not.
	shift := func(s *css.Style) {
		s.Transform = []css.TransformFunc{{Kind: css.TransformTranslate, X: css.Pts(px(2)), Y: css.Pts(px(1))}}
	}
	cell := func(m ...func(*css.Style)) *Node { return td(mods(append(m, width(2), height(1), bg(blue))...), "") }
	tests := []struct {
		name string
		root *Node
	}{
		{"row group", table(mods(spacing(0, 0)), tbody(mods(shift), tr(nil, cell())))},
		{"row", table(mods(spacing(0, 0)), tr(mods(shift), cell()))},
		{"cell", table(mods(spacing(0, 0)), tr(nil, cell(shift)))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPixels(t, run(t, doc(tt.root), page(4, 2)), 1, "____\n__BB")
		})
	}
	t.Run("column", func(t *testing.T) {
		root := doc(table(mods(spacing(0, 0)), colgroup(nil, col(mods(shift))), tr(nil, cell())))
		assertPixels(t, run(t, root, page(4, 2)), 1, "BB__\n____")
	})
}
