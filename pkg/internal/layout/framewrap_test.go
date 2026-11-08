package layout

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// wrapFrame returns a page-anchored box of w by h px at left, top px of
// the page box that the flow wraps around by mode.
func wrapFrame(mode css.FrameWrap, left, top, w, h float64, mods ...func(*css.Style)) *Node {
	return el(sty(append([]func(*css.Style){func(s *css.Style) {
		s.Position = css.Absolute
		s.PageAnchor = true
		s.Left, s.Top = css.Pts(px(left)), css.Pts(px(top))
		s.FrameWrap = mode
	}, width(w), height(h), bg(red)}, mods...)...))
}

// linePlaces returns where the text runs of page n of res start, in px.
func linePlaces(res *Result, n int) string {
	var out string
	for _, r := range texts(res, n) {
		out += fmt.Sprintf("%s@%g,%g ", r.text, r.x/css.Pt, r.y/css.Pt)
	}
	return out
}

// TestFrameWrap wraps the lines of the flow around a page-anchored box by
// its css.Office.FrameWrap, the lines before its anchor included: the
// text keeps to the side the mode names or the one with more room, or
// moves below the box, and runs through it by default.
func TestFrameWrap(t *testing.T) {
	tests := []struct {
		name string
		mode css.FrameWrap
		left float64
		gap  float64
		want string
	}{
		{"through", css.FrameWrapThrough, 0, 0, "a@0,8 b@0,18 c@0,28 d@0,38 "},
		{"none", css.FrameWrapNone, 0, 0, "a@0,8 b@0,43 c@0,53 d@0,63 "},
		{"right", css.FrameWrapRight, 0, 0, "a@0,8 b@50,18 c@50,28 d@50,38 "},
		{"left beside a right box", css.FrameWrapLeft, 50, 0, "a@0,8 b@0,18 c@0,28 d@0,38 "},
		{"largest", css.FrameWrapLargest, 0, 0, "a@0,8 b@50,18 c@50,28 d@50,38 "},
		{"parallel with a gap", css.FrameWrapParallel, 0, 5, "a@0,8 b@55,18 c@55,28 d@55,38 "},
		{"dynamic without room", css.FrameWrapDynamic, 10, 0, "a@0,8 b@0,43 c@0,53 d@0,63 "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gap := func(s *css.Style) { s.WrapGap = [4]float64{px(tt.gap), px(tt.gap), px(tt.gap), px(tt.gap)} }
			frame := wrapFrame(tt.mode, tt.left, 15, 50, 20, gap)
			p := func(s string, kids ...*Node) *Node { return el(sty(), append([]*Node{txt(sty(), s)}, kids...)...) }
			res := run(t, doc(p("a"), p("b"), p("c"), p("d", frame)), page(100, 100))
			if got := linePlaces(res, 1); got != tt.want {
				t.Errorf("lines %s, want %s", got, tt.want)
			}
		})
	}
}

// TestFrameWrap_FullPage moves the flow past a wrapped box that covers its
// page's area, on a page AnchorPage names: the page holds the box alone.
func TestFrameWrap_FullPage(t *testing.T) {
	frame := wrapFrame(css.FrameWrapDynamic, 0, 0, 100, 100, func(s *css.Style) { s.AnchorPage = 2 })
	var kids []*Node
	for i := range 15 {
		kids = append(kids, el(sty(), txt(sty(), string(rune('a'+i)))))
	}
	kids[0].Children = append(kids[0].Children, frame)
	res := run(t, doc(kids...), page(100, 100))
	if len(res.Pages) != 3 {
		t.Fatalf("%d pages, want 3", len(res.Pages))
	}
	if got := linePlaces(res, 2); got != "" {
		t.Errorf("page 2 lines %s, want none", got)
	}
	if got := texts(res, 3); len(got) != 5 || got[0].text != "k" {
		t.Errorf("page 3 lines %s, want k to o", linePlaces(res, 3))
	}
}

// TestPageTop starts a table no higher than its distance from its page's
// top edge, and leaves it where the flow puts it when that is lower.
func TestPageTop(t *testing.T) {
	tests := []struct {
		name   string
		before int
		want   string
	}{
		{"moved down", 1, "a@0,18 b@0,48 "},
		{"flow lower", 5, "a@0,18 a@0,28 a@0,38 a@0,48 a@0,58 b@0,68 "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var kids []*Node
			for range tt.before {
				kids = append(kids, el(sty(), txt(sty(), "a")))
			}
			tb := table(mods(func(s *css.Style) { s.PageTop = px(40) }), tr(nil, td(nil, "b")))
			kids = append(kids, tb)
			res := run(t, doc(kids...), PageBox{Width: px(100), Height: px(200), Margin: [4]float64{px(10), 0, px(10), 0}})
			if got := linePlaces(res, 1); got != tt.want {
				t.Errorf("lines %s, want %s", got, tt.want)
			}
		})
	}
}
