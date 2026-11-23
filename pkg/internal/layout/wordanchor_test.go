package layout

import (
	"image/color"
	"strings"
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

// TestWordAnchor_Align moves a float of css.WordAnchor AlignX and AlignY
// left and up by those fractions of its own size, laid out: its bottom
// right corner at its offsets for 1, its middle for 0.5.
func TestWordAnchor_Align(t *testing.T) {
	for _, tt := range []struct {
		name   string
		ax, ay float64
		want   [4]float64
	}{
		{"corner", 1, 1, [4]float64{40, 40, 20, 20}},
		{"middle", 0.5, 0.5, [4]float64{50, 50, 20, 20}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			align := func(s *css.Style) { s.WordAnchor.AlignX, s.WordAnchor.AlignY = tt.ax, tt.ay }
			rb := boxes(t, doc(wpara("a", wordFloat(css.FrameWrapBoth, 60, 60, 20, 20, align))), page(100, 200))
			if got := rect(kid(t, rb, 0, 0, 0)); got != tt.want {
				t.Errorf("float at %v, want %v", got, tt.want)
			}
		})
	}
}

// TestWordAnchor_AlignOnce aligns a block-level float of css.WordAnchor
// AlignX once when its top gap reaches above where it stands, which seeds
// it into the passes that follow: each pass places it where the first
// aligned it.
func TestWordAnchor_AlignOnce(t *testing.T) {
	seeded := func(s *css.Style) {
		s.WordAnchor.AlignX = 1
		s.WrapGap = [4]float64{px(2), 0, 0, 0}
	}
	res := run(t, doc(wpara("aaaa"), wordFloat(css.FrameWrapBoth, 90, 0, 20, 20, seeded), wpara("b")), page(100, 200))
	if got, want := linePlaces(res, 1), "X@70,18 aaaa@0,8 b@0,18 "; got != want {
		t.Errorf("lines %s, want %s", got, want)
	}
}

// TestWordAnchor_TallFloatBreaks lays a float of css.Office.WordAnchor
// without PageClamp that is taller than its page out across the pages it
// reaches, a line that would cross a page's end moved to the next, where
// one with PageClamp is laid out whole on its anchor's page.
func TestWordAnchor_TallFloatBreaks(t *testing.T) {
	for _, tt := range []struct {
		name  string
		clamp bool
		want  float64
	}{
		// Lines of 10 px on pages of 95 px: past each page's end, the line
		// that would cross it starts the next page.
		{"breaks", false, 250},
		{"whole", true, 240},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lines := make([]*Node, 0, 25)
			for range 25 {
				lines = append(lines, el(sty(), txt(sty(), "X")))
			}
			f := el(sty(func(s *css.Style) {
				s.Float = css.FloatLeft
				s.WordAnchor = css.WordAnchor{Set: true}
				s.PageClamp = tt.clamp
			}, width(20)), lines...)
			rb := boxes(t, doc(wpara("a", f)), page(100, 95))
			fb := kid(t, rb, 0, 0, 0)
			if got := rect(kid(t, fb, 24))[1]; got != tt.want {
				t.Errorf("last line at %v px, want %v", got, tt.want)
			}
		})
	}
}

// writer marks a float of css.WordAnchor.Writer.
func writer(s *css.Style) { s.WordAnchor.Writer = true }

// TestWordAnchor_WriterLineBottom wraps a line whose bottom a float of
// css.WordAnchor.Writer overlaps by half a pixel, which Word's trim lets
// run beside it unwrapped.
func TestWordAnchor_WriterLineBottom(t *testing.T) {
	res := run(t, doc(wpara("aa bb cc dd", wordFloat(css.FrameWrapBoth, 0, 9.5, 40, 20, writer))), page(100, 100))
	if got, want := linePlaces(res, 1), "X@0,17.5 aa bb @40,8 cc dd@40,18 "; got != want {
		t.Errorf("lines %s, want %s", got, want)
	}
}

// TestWordAnchor_WriterStaysOnPage leaves the paragraph of a float of
// css.WordAnchor.Writer that would end below its page's area on its page,
// the float moved up into the page box.
func TestWordAnchor_WriterStaysOnPage(t *testing.T) {
	res := run(t, doc(wpara("a"), wpara("b"), wpara("c"), wpara("d", wordFloat(css.FrameWrapBoth, 60, 0, 40, 30, writer))), page(100, 50))
	if len(res.Pages) != 1 {
		t.Fatalf("%d pages, want 1", len(res.Pages))
	}
	if got, want := linePlaces(res, 1), "X@60,28 a@0,8 b@0,18 c@0,28 d@0,38 "; got != want {
		t.Errorf("lines %s, want %s", got, want)
	}
}

// TestWordAnchor_WriterRise places a float of css.WordAnchor.Writer from
// the top of its paragraph's area, Rise above its first line, and from
// the page area's top when the paragraph starts the page. The float
// touches the line before its paragraph, which goes below it, finding no
// room on its right.
func TestWordAnchor_WriterRise(t *testing.T) {
	rise := func(s *css.Style) { s.WordAnchor.Rise = px(10) }
	spaced := func() *Node {
		p := wpara("b", wordFloat(css.FrameWrapRight, 60, 0, 40, 10, writer, rise))
		p.Style.Margin[css.Top] = css.Pts(px(10))
		return p
	}
	res := run(t, doc(wpara("a"), spaced()), page(100, 100))
	if got, want := linePlaces(res, 1), "X@60,18 a@0,28 b@0,48 "; got != want {
		t.Errorf("lines %s, want %s", got, want)
	}
	res = run(t, doc(spaced()), page(100, 100))
	if got, want := linePlaces(res, 1), "X@60,8 b@0,18 "; got != want {
		t.Errorf("first paragraph: lines %s, want %s", got, want)
	}
}

// TestWordAnchor_WriterEmptyLine moves an empty line that a float of
// css.WordAnchor.Writer of no wrap overlaps below it, as a line of text.
func TestWordAnchor_WriterEmptyLine(t *testing.T) {
	empty := el(sty(), br(sty()))
	res := run(t, doc(wpara("a", wordFloat(css.FrameWrapNone, 0, 15, 40, 20, writer)), empty, wpara("b")), page(100, 100))
	if got, want := linePlaces(res, 1), "X@0,23 a@0,8 b@0,53 "; got != want {
		t.Errorf("lines %s, want %s", got, want)
	}
}

// TestWordAnchor_WriterIndents starts the lines of a paragraph of
// css.Office.WriterIndents beside a float that takes their start its
// indent past the float's edge, and keeps its indent where a piece of the
// line is left before the float.
func TestWordAnchor_WriterIndents(t *testing.T) {
	for _, tc := range []struct {
		name string
		x    float64
		want string
	}{
		{"float at the start", 0, "X@0,8 aa @60,8 bb @60,18 cc@20,28 "},
		{"float inside", 40, "X@40,8 aa @20,8 bb @80,8 cc@20,18 "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := wpara("aa bb cc", wordFloat(css.FrameWrapBoth, tc.x, 0, 40, 20, writer))
			p.Style.Margin[css.Left] = css.Pts(px(20))
			p.Style.WriterIndents = true
			res := run(t, doc(p), page(100, 100))
			if got := linePlaces(res, 1); got != tc.want {
				t.Errorf("lines %s, want %s", got, tc.want)
			}
		})
	}
}

// TestWordAnchor_FromFirstColumn places a float of css.Office.WordAnchor
// in the second column of a multi-column container at its offset from
// the first column under FromFirstColumn, as Word places a frame
// positioned from the page, and from its own column without it.
func TestWordAnchor_FromFirstColumn(t *testing.T) {
	for _, tt := range []struct {
		first bool
		want  string
	}{{false, "X@70,"}, {true, "X@10,"}} {
		f := wordFloat(css.FrameWrapNone, 10, 0, 20, 10, func(s *css.Style) { s.WordAnchor.FromFirstColumn = tt.first })
		mc := el(sty(width(100), height(20), func(s *css.Style) {
			s.Columns.Count = 2
			s.Columns.Fill = css.FillAuto
			s.ColumnGap = css.Pts(px(20))
		}), wpara("a"), wpara("b"), wpara("c", f))
		res := run(t, doc(mc), page(200, 200))
		if got := linePlaces(res, 1); !strings.HasPrefix(got, tt.want) && !strings.Contains(got, " "+tt.want) {
			t.Errorf("FromFirstColumn %v: %s, want %s", tt.first, got, tt.want)
		}
	}
}
