package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// chained returns a link of the chain "c" at index i: a page-anchored box of
// w px wide at left, top px, h px tall when h is positive, holding kids.
func chained(i int, left, top, w, h float64, kids ...*Node) *Node {
	mods := []func(*css.Style){func(s *css.Style) {
		s.Position = css.Absolute
		s.PageAnchor = true
		s.Left, s.Top = css.Pts(px(left)), css.Pts(px(top))
		s.Chain, s.ChainIndex = "c", i
	}, width(w)}
	if h > 0 {
		mods = append(mods, height(h))
	}
	return el(sty(mods...), kids...)
}

// TestChain flows the content of a chain's first link into the next links,
// in index order whatever the tree order, at each link's width, cut at
// each link's height; a later link's own children are ignored, and a link
// without a height takes what is left.
func TestChain(t *testing.T) {
	p := func(s string) *Node { return el(sty(), txt(sty(), s)) }
	tests := []struct {
		name  string
		links []*Node
		want  string
	}{
		{"two links", []*Node{
			chained(0, 0, 0, 50, 20, p("a"), p("b"), p("c"), p("d")),
			chained(1, 0, 50, 50, 20, p("x")),
		}, "a@0,8 b@0,18 c@0,58 d@0,68 "},
		{"tree order reversed", []*Node{
			chained(1, 50, 0, 50, 20),
			chained(0, 0, 0, 50, 20, p("a"), p("b"), p("c")),
		}, "c@50,8 a@0,8 b@0,18 "},
		{"narrower link", []*Node{
			chained(0, 0, 0, 40, 10, p("aa bb cc")),
			chained(1, 0, 50, 20, 30),
		}, "aa @0,8 bb @0,58 cc@0,68 "},
		{"auto last link", []*Node{
			chained(0, 0, 0, 50, 10, p("a"), p("b"), p("c")),
			chained(1, 0, 50, 50, 0),
		}, "a@0,8 b@0,58 c@0,68 "},
		{"inline-block links in flow", []*Node{
			el(sty(), flowLink(0, display(css.InlineBlock), p("a"), p("b")), flowLink(1, display(css.InlineBlock))),
		}, "a@0,8 b@50,8 "},
		{"content past the last link", []*Node{
			chained(0, 0, 0, 50, 10, p("a"), p("b")),
			chained(1, 0, 50, 50, 10),
		}, "a@0,8 b@0,58 "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(tt.links...), page(100, 100))
			if got := linePlaces(res, 1); got != tt.want {
				t.Errorf("lines %s, want %s", got, tt.want)
			}
		})
	}
}

// flowLink returns an in-flow link of the chain "c" at index i, 50 px wide
// and 10 px tall, changed by mod, holding kids.
func flowLink(i int, mod func(*css.Style), kids ...*Node) *Node {
	return el(sty(width(50), height(10), mod, func(s *css.Style) { s.Chain, s.ChainIndex = "c", i }), kids...)
}
