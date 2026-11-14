package layout

import (
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func ratio(w, h float64) func(*css.Style) {
	return func(s *css.Style) { s.AspectRatio = css.Ratio(w, h, false) }
}

func autoRatio(w, h float64) func(*css.Style) {
	return func(s *css.Style) { s.AspectRatio = css.Ratio(w, h, true) }
}

func maxH(v float64) func(*css.Style) { return func(s *css.Style) { s.MaxHeight = css.Pts(px(v)) } }
func maxW(v float64) func(*css.Style) { return func(s *css.Style) { s.MaxWidth = css.Pts(px(v)) } }
func minH(v float64) func(*css.Style) {
	return func(s *css.Style) { s.MinHeight, s.MinHeightAuto = css.Pts(px(v)), false }
}
func minW(v float64) func(*css.Style) {
	return func(s *css.Style) { s.MinWidth, s.MinWidthAuto = css.Pts(px(v)), false }
}
func borderBox(s *css.Style) { s.BoxSizing = css.BorderBox }

// TestAspectRatio_Sizes holds a box with a preferred aspect ratio to CSS
// Sizing 4, 4.2 and 4.3: an automatic size follows the other through the
// ratio, a definite height giving the width and else the width giving
// the height; the height grows to the content's while min-height is auto
// and the box is not a scroll container, and an automatic width keeps
// within the minimum and maximum heights through the ratio. Each case
// sits alone in a 400px wide, 300px tall relatively positioned block.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) lays each case
//     out at the size the test wants, read by getBoundingClientRect.
func TestAspectRatio_Sizes(t *testing.T) {
	text := "a b c d e f g h i j k l m n o p q r s t u v w x y z"
	tests := []struct {
		name string
		box  *Node
		want [2]float64
	}{
		{"both auto: the width gives the height", el(sty(ratio(2, 1))), [2]float64{400, 200}},
		{"a definite height gives the width", el(sty(ratio(2, 1), height(100))), [2]float64{200, 100}},
		{"a percentage of a definite height gives the width", el(sty(ratio(2, 1), func(s *css.Style) { s.Height = css.Dimension{Kind: css.Percent, Value: 0.5} })), [2]float64{300, 150}},
		{"the used height gives the width", el(sty(ratio(1, 1), height(100), maxH(50))), [2]float64{50, 50}},
		{"the used width gives the height", el(sty(ratio(1, 1), width(100), maxW(50))), [2]float64{50, 50}},
		{"min-width applies to the dependent width", el(sty(ratio(1, 1), height(100), minW(150))), [2]float64{150, 100}},
		{"min-height gives the width", el(sty(ratio(1, 1), height(100), minH(200))), [2]float64{200, 200}},
		{"max-height bounds an automatic width", el(sty(ratio(1, 1), maxH(50))), [2]float64{50, 50}},
		{"max-height bounds the height a width gives", el(sty(ratio(2, 1), width(100), maxH(30))), [2]float64{100, 30}},
		{"content taller than the ratio", el(sty(ratio(8, 1), width(100)), txt(sty(), text)), [2]float64{100, 60}},
		{"a scroll container keeps the ratio", el(sty(ratio(8, 1), width(100), hidden), txt(sty(), text)), [2]float64{100, 12.5}},
		{"min-height 0 keeps the ratio", el(sty(ratio(8, 1), width(100), minH(0)), txt(sty(), text)), [2]float64{100, 12.5}},
		{"the content's min-content width", el(sty(ratio(1, 1), height(10), display(css.InlineBlock)), txt(sty(), "abcdefghij")), [2]float64{100, 10}},
		{"border-box relates the border box", el(sty(ratio(1, 1), width(100), padding(10), borderBox)), [2]float64{100, 100}},
		{"content-box relates the content box", el(sty(ratio(1, 1), width(100), padding(10))), [2]float64{120, 120}},
		{"auto && ratio relates the content box", el(sty(autoRatio(1, 1), width(100), padding(10), borderBox)), [2]float64{100, 100}},
		{"a float takes the width of its height", el(sty(ratio(2, 1), height(100), float(css.FloatLeft))), [2]float64{200, 100}},
		{"a percentage width counts as auto in a contribution", el(sty(display(css.InlineBlock), height(20)), el(sty(ratio(1, 1), height(100), func(s *css.Style) { s.Width = css.Dimension{Kind: css.Percent, Value: 0.5} }))), [2]float64{100, 20}},
		{"an inline block takes the width of its height", el(sty(ratio(2, 1), height(50), display(css.InlineBlock))), [2]float64{100, 50}},
		{"a degenerate ratio is auto", el(sty(ratio(0, 1), width(100))), [2]float64{100, 0}},
		{"an absolute box takes the width of its height", el(sty(ratio(2, 1), height(100), abs(0, auto, auto, 0))), [2]float64{200, 100}},
		{"an absolute box between left and right", el(sty(ratio(2, 1), abs(auto, 0, auto, 0))), [2]float64{400, 200}},
		{"an absolute box between top and bottom", el(sty(ratio(2, 1), abs(0, auto, 0, 0))), [2]float64{600, 300}},
		{"an absolute box between all four insets: the width comes first", el(sty(ratio(1, 1), abs(0, 0, 0, 0))), [2]float64{400, 400}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := el(sty(width(400), height(300), func(s *css.Style) { s.Position = css.Relative }), tt.box)
			rb := boxes(t, doc(cb), page(800, 800))
			b := kid(t, rb, 0, 0, 0)
			if b.kind == kindBlock && b.anon {
				b = kid(t, b, 0)
			}
			if got := rect(b); got[2] != tt.want[0] || got[3] != tt.want[1] {
				t.Errorf("box %v by %v, want %v by %v", got[2], got[3], tt.want[0], tt.want[1])
			}
		})
	}
}

// TestAspectRatio_MarginsDoNotCollapseThrough holds that the block axis a
// ratio gives is not an auto height for margin collapsing (CSS Sizing 4,
// 4.2.1): the last child's bottom margin stays inside the box.
func TestAspectRatio_MarginsDoNotCollapseThrough(t *testing.T) {
	rb := boxes(t, doc(
		el(sty(ratio(2, 1), width(100)), el(sty(height(10), margin(0, 0, 20, 0)))),
		el(sty(height(1))),
	), page(400, 400))
	if got := rect(kid(t, rb, 0, 1)); got[1] != 50 {
		t.Errorf("next block at y %v, want 50", got[1])
	}
}

// TestAspectRatio_Items holds flex and grid items with a preferred aspect
// ratio to CSS Flexbox 1, 9.2, 3B and 4.5 and CSS Sizing 4, 4.2: a
// definite cross size, stretched or not, gives the main size, the
// automatic minimum takes the size the ratio gives, and a column item's
// width follows its used height.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) lays each case
//     out at the size the test wants, read by getBoundingClientRect.
func TestAspectRatio_Items(t *testing.T) {
	tests := []struct {
		name      string
		container *css.Style
		item      *css.Style
		want      [2]float64
	}{
		{"row: a definite height gives the width", flexBox(width(400), height(150), alignItems(css.BoxFlexStart)), sty(ratio(2, 1), height(100)), [2]float64{200, 100}},
		{"row: a stretched height gives the width", flexBox(width(400), height(100)), sty(ratio(2, 1)), [2]float64{200, 100}},
		{"row: the automatic minimum width", flexBox(width(50)), sty(ratio(1, 1), height(100)), [2]float64{100, 100}},
		{"row: the automatic minimum of a stretched height", flexBox(width(0), height(100)), sty(ratio(1, 1)), [2]float64{100, 100}},
		{"row: the used width gives the height", flexBox(width(100)), sty(ratio(1, 1), width(50), minW(0), flexF(1, 1, -1)), [2]float64{100, 100}},
		{"column: a stretched width gives the height", flexBox(width(400), dir(css.FlexColumn)), sty(ratio(2, 1)), [2]float64{400, 200}},
		{"column: the automatic minimum height", flexBox(width(400), height(50), dir(css.FlexColumn)), sty(ratio(1, 1), width(100)), [2]float64{100, 100}},
		{"column: the width follows the used height", flexBox(width(400), height(200), dir(css.FlexColumn), alignItems(css.BoxStart)), sty(ratio(1, 1), minH(100), flexF(1, 0, -1)), [2]float64{149.5, 149.5}},
		{"grid: the column width gives the height", sty(display(css.Grid), cols(tpx(200))), sty(ratio(2, 1)), [2]float64{200, 100}},
		{"grid: a definite height gives the width", sty(display(css.Grid), cols(tpx(200))), sty(ratio(1, 1), height(50)), [2]float64{50, 50}},
		{"grid: a fixed width gives the height", sty(display(css.Grid)), sty(ratio(2, 1), width(100)), [2]float64{100, 50}},
		{"grid: a percentage height sizes the row as auto", sty(display(css.Grid), width(100)), sty(ratio(4, 1), width(100), func(s *css.Style) { s.Height = css.Dimension{Kind: css.Percent, Value: 0.5} }), [2]float64{100, 12.5}},
		{"grid: a stretched width wins over the row", sty(display(css.Grid), cols(tpx(200)), rowsT(tpx(50))), sty(ratio(1, 1)), [2]float64{200, 200}},
		{"grid: a stretched height gives the width", sty(display(css.Grid), cols(tpx(200)), rowsT(tpx(50)), func(s *css.Style) { s.JustifyItems = css.BoxStart }), sty(ratio(1, 1)), [2]float64{50, 50}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kids := []*Node{el(tt.item)}
			if tt.name == "column: the width follows the used height" {
				kids = append(kids, el(sty(flexF(1, 0, 1))))
			}
			rb := boxes(t, doc(el(tt.container, kids...)), page(800, 800))
			if got := rect(kid(t, rb, 0, 0, 0)); got[2] != tt.want[0] || got[3] != tt.want[1] {
				t.Errorf("item %v by %v, want %v by %v", got[2], got[3], tt.want[0], tt.want[1])
			}
		})
	}
}

// TestAspectRatio_Replaced holds a replaced element to CSS Sizing 4, 4.1:
// a <ratio> replaces its natural ratio, auto && <ratio> keeps it, and a
// picture without one takes the specified ratio.
func TestAspectRatio_Replaced(t *testing.T) {
	tests := []struct {
		name string
		st   *css.Style
		want [2]float64
	}{
		{"a ratio replaces the natural one", sty(ratio(1, 1), width(100)), [2]float64{100, 100}},
		{"auto keeps the natural one", sty(autoRatio(1, 1), width(100)), [2]float64{100, 250}},
		{"no specified size: the natural width", sty(ratio(1, 1)), [2]float64{20, 20}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &Node{Kind: Replaced, Style: tt.st, Picture: pic(color.NRGBA{G: 255, A: 255}, 20, 50)}
			rb := boxes(t, doc(n), page(400, 400))
			b := kid(t, rb, 0, 0)
			for b.kind != kindReplaced && len(b.kids) > 0 {
				b = b.kids[0]
			}
			if got := rect(b); got[2] != tt.want[0] || got[3] != tt.want[1] {
				t.Errorf("picture %v by %v, want %v by %v", got[2], got[3], tt.want[0], tt.want[1])
			}
		})
	}
}

// TestAspectRatio_Table holds that aspect-ratio applies to the table box
// and not to the internal boxes of a table (CSS Sizing 4, 4.1).
func TestAspectRatio_Table(t *testing.T) {
	cell := el(sty(display(css.TableCell), ratio(4, 1), height(50), width(20)))
	tbl := el(sty(display(css.Table), ratio(2, 1), width(100)), el(sty(display(css.TableRow)), cell))
	rb := boxes(t, doc(tbl), page(400, 400))
	var table, td *box
	var walk func(b *box)
	walk = func(b *box) {
		switch b.tpart {
		case partTable:
			table = b
		case partCell:
			td = b
		}
		for _, k := range b.kids {
			walk(k)
		}
	}
	walk(rb)
	if table == nil || td == nil {
		t.Fatal("no table or cell box")
	}
	if got := rect(table); got[2] != 100 || got[3] != 50 {
		t.Errorf("table %v by %v, want 100 by 50", got[2], got[3])
	}
	if got := rect(td); got[2] != 100 || got[3] != 50 {
		t.Errorf("cell %v by %v, want 100 by 50: the ratio does not apply", got[2], got[3])
	}
}

// TestAspectRatio_PercentHeightInIntrinsicWidth locks that a percentage
// height resolves while the widths are measured when the styles of the
// ancestors make it definite (CSS Sizing 3, 5.2.1; CSS 2.1, 10.5), so a
// box with an aspect ratio gives its min-content width through it; an
// indefinite chain counts as auto.
func TestAspectRatio_PercentHeightInIntrinsicWidth(t *testing.T) {
	pct := func(s *css.Style) { s.Height = css.Dimension{Kind: css.Percent, Value: 1} }
	minContent := func(s *css.Style) { s.Width = css.Dimension{Kind: css.MinContent} }
	for _, tt := range []struct {
		name  string
		outer func(*css.Style)
		want  float64
	}{
		{"definite", height(100), 100},
		{"indefinite", func(*css.Style) {}, 0},
	} {
		rb := boxes(t, doc(el(sty(minContent, tt.outer), el(sty(pct), el(sty(pct, ratio(1, 1)))))), page(400, 400))
		if got := rect(kid(t, rb, 0, 0))[2]; got != tt.want {
			t.Errorf("%s: width %v, want %v", tt.name, got, tt.want)
		}
	}
}
