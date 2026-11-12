package layout

import (
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// wordFloat returns a float of css.Office.WordAnchor of w by h px at x, y
// px from its paragraph, wrapped by mode, holding the letter X.
func wordFloat(mode css.FrameWrap, x, y, w, h float64, mods ...func(*css.Style)) *Node {
	return el(sty(append([]func(*css.Style){func(s *css.Style) {
		s.Float = css.FloatLeft
		s.WordAnchor = css.WordAnchor{Set: true, X: px(x), Y: px(y)}
		s.FrameWrap = mode
	}, width(w), height(h)}, mods...)...), txt(sty(), "X"))
}

// wpara returns a paragraph of the text s holding kids before it.
func wpara(s string, kids ...*Node) *Node {
	return el(sty(), append(kids, txt(sty(), s))...)
}

// TestWordAnchor_Sides places a float of css.Office.WordAnchor at its
// offsets and sets the lines beside it on the sides its mode takes: both
// sides, each a piece of the line filled in turn, a word that does not fit
// a piece going to the next; one side; or none.
func TestWordAnchor_Sides(t *testing.T) {
	tests := []struct {
		name string
		mode css.FrameWrap
		want string
	}{
		{"both", css.FrameWrapBoth, "X@30,8 aa @0,8 bbb @50,8 cc @0,18 dd ee @50,18 ff gg@0,28 "},
		{"left", css.FrameWrapLeft, "X@30,8 aa @0,8 bbb @0,18 cc dd ee @0,28 ff gg@0,38 "},
		{"right", css.FrameWrapRight, "X@30,8 aa @50,8 bbb @50,18 cc dd ee @0,28 ff gg@0,38 "},
		{"largest", css.FrameWrapLargest, "X@30,8 aa @50,8 bbb @50,18 cc dd ee @0,28 ff gg@0,38 "},
		{"none", css.FrameWrapNone, "X@30,8 aa bbb cc @0,28 dd ee ff @0,38 gg@0,48 "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(wpara("aa bbb cc dd ee ff gg", wordFloat(tt.mode, 30, 0, 20, 20))), page(100, 100))
			if got := linePlaces(res, 1); got != tt.want {
				t.Errorf("lines %s, want %s", got, tt.want)
			}
		})
	}
}

// TestWordAnchor_Contour wraps the lines beside a float of
// css.Office.WrapPolygon along the part of its contour over each line,
// grown by the top and bottom gaps, kept the left and right gaps away.
func TestWordAnchor_Contour(t *testing.T) {
	// A triangle pointing right: its right edge runs from the top-left
	// corner to the middle of the right side and back.
	tri := func(s *css.Style) {
		s.WrapPolygon = []css.WrapPoint{{X: 0, Y: 0}, {X: 1, Y: 0.5}, {X: 0, Y: 1}}
		s.WrapGap = [4]float64{0, px(5), 0, 0}
	}
	res := run(t, doc(wpara("a b c d e f g h i j k l m n o p q", wordFloat(css.FrameWrapBoth, 0, 0, 40, 40, tri))), page(60, 100))
	if got, want := linePlaces(res, 1), "X@0,8 a b @23,8 c @43,18 d @45,28 e f @25,38 g h i @0,48 j k l @0,58 m n o @0,68 p q@0,78 "; got != want {
		t.Errorf("lines %s, want %s", got, want)
	}
}

// TestWordAnchor_Overlap places a float of css.WordAnchor NoOverlap that
// would overlap an earlier one right of it, and one that has no room
// there below it.
func TestWordAnchor_Overlap(t *testing.T) {
	no := func(s *css.Style) { s.WordAnchor.NoOverlap = true }
	tests := []struct {
		name string
		w    float64
		want [4]float64
	}{
		{"right of it", 30, [4]float64{40, 10, 30, 30}},
		{"below it", 70, [4]float64{20, 30, 70, 30}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := wordFloat(css.FrameWrapBoth, 10, 0, 30, 30)
			second := wordFloat(css.FrameWrapBoth, 20, 10, tt.w, 30, no)
			second.Kind, second.Children = Replaced, nil
			second.Picture = pic(color.Black, 3, 3)
			rb := boxes(t, doc(wpara("a", first, second)), page(100, 200))
			if got := rect(kid(t, rb, 0, 0, 1)); got != tt.want {
				t.Errorf("second float at %v, want %v", got, tt.want)
			}
		})
	}
}

// TestWordAnchor_LineBottom leaves a line whose bottom overlaps a float of
// css.Office.WordAnchor by less than wordBandTrim beside none of it.
func TestWordAnchor_LineBottom(t *testing.T) {
	for _, tc := range []struct {
		y    float64
		want string
	}{
		{9.5, "X@0,17.5 aa bb cc @0,8 dd@40,18 "},
		{8, "X@0,16 aa bb @40,8 cc dd@40,18 "},
	} {
		res := run(t, doc(wpara("aa bb cc dd", wordFloat(css.FrameWrapBoth, 0, tc.y, 40, 20))), page(100, 100))
		if got := linePlaces(res, 1); got != tc.want {
			t.Errorf("float at %v px: lines %s, want %s", tc.y, got, tc.want)
		}
	}
}

// TestWordAnchor_LinesBefore wraps the lines before the paragraph of a
// float of css.Office.WordAnchor whose top gap reaches them, and keeps the
// float where it was placed when that wrapping moves its paragraph down.
func TestWordAnchor_LinesBefore(t *testing.T) {
	gap := func(s *css.Style) { s.WrapGap = [4]float64{px(2), 0, 0, 0} }
	res := run(t, doc(wpara("aaaa aaaa"), wpara("b", wordFloat(css.FrameWrapBoth, 60, 0, 40, 20, gap))), page(100, 100))
	if got, want := linePlaces(res, 1), "X@60,18 aaaa @0,8 aaaa@0,18 b@0,28 "; got != want {
		t.Errorf("lines %s, want %s", got, want)
	}
}

// TestWordAnchor_NextPage moves a paragraph whose float of
// css.Office.WordAnchor would end below its page's area to the next page,
// the float with it.
func TestWordAnchor_NextPage(t *testing.T) {
	res := run(t, doc(wpara("a"), wpara("b"), wpara("c"), wpara("d", wordFloat(css.FrameWrapBoth, 60, 0, 40, 30))), page(100, 50))
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	if got, want := linePlaces(res, 2), "X@60,8 d@0,8 "; got != want {
		t.Errorf("page 2 lines %s, want %s; page 1 %s", got, want, linePlaces(res, 1))
	}
}

// TestWordAnchor_FromColumn measures a float's offset from the column,
// whatever the indent of its paragraph.
func TestWordAnchor_FromColumn(t *testing.T) {
	indented := wpara("a", wordFloat(css.FrameWrapBoth, 60, 0, 40, 10))
	indented.Style.Margin[css.Left] = css.Pts(px(20))
	res := run(t, doc(indented), page(100, 100))
	if got, want := linePlaces(res, 1), "X@60,8 a@20,8 "; got != want {
		t.Errorf("lines %s, want %s", got, want)
	}
}
