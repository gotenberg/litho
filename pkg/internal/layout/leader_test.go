package layout

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// leaderNode returns a leader of string s in colour c, as the converter
// builds it.
func leaderNode(s string, c css.Color) *Node {
	st := sty(display(css.InlineBlock), font(2), fg(c), func(s *css.Style) { s.WhiteSpace = css.Pre })
	return &Node{Kind: Element, Style: st, Leader: true, Children: []*Node{txt(st, s)}}
}

func TestLeader_Draw(t *testing.T) {
	t.Parallel()
	line := func(st *css.Style, kids ...*Node) *Node { return el(st, kids...) }
	red2 := sty(font(2), fg(red))
	nowrap := sty(font(2), fg(red), func(s *css.Style) { s.WhiteSpace = css.Nowrap })
	rtl := sty(font(2), fg(red), func(s *css.Style) { s.Direction = css.RTL })
	tests := []struct {
		name string
		root *Node
		w, h float64
		want string
	}{
		{"fills the line", doc(line(red2, txt(red2, "X"), leaderNode("X", blue), txt(red2, "X"))), 12, 2, `
			RRBBBBBBBBRR
			RRBBBBBBBBRR`},
		{"copies on the end-edge grid", doc(line(red2, txt(red2, "X"), leaderNode(" X ", blue), txt(red2, "X"))), 16, 2, `
			RR____BB______RR
			RR____BB______RR`},
		{"no room draws no copy", doc(line(nowrap, txt(nowrap, "XXXXX"), leaderNode("X", blue), txt(red2, "X"))), 12, 2, `
			RRRRRRRRRRRR
			RRRRRRRRRRRR`},
		{"moves to the next line with what follows", doc(line(red2, txt(red2, "XXXX"), leaderNode("X", blue), txt(red2, " X"))), 10, 4, `
			RRRRRRRR__
			RRRRRRRR__
			BBBBBB__RR
			BBBBBB__RR`},
		{"right to left", doc(line(rtl, txt(rtl, "X"), leaderNode("X", blue), txt(rtl, "X"))), 8, 2, `
			RRBBBBRR
			RRBBBBRR`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertPixels(t, run(t, tt.root, page(tt.w, tt.h)), 1, tt.want)
		})
	}
}

func TestLeader_NoIntrinsicWidth(t *testing.T) {
	t.Parallel()
	red2 := sty(font(2), fg(red))
	fl := sty(font(2), fg(red), float(css.FloatLeft), bg(lime))
	root := doc(el(fl, txt(red2, "X"), leaderNode("XX", blue)))
	assertPixels(t, run(t, root, page(6, 2)), 1, `
		RR____
		RR____`)
}

func TestAnchors_EmptyInlineBoxes(t *testing.T) {
	t.Parallel()
	red2 := sty(font(2), fg(red))
	mark := func(id string) *Node { n := el(inlineSty(font(2)), nil...); n.ID = id; return n }
	root := doc(el(red2, txt(red2, "XX XX X"), mark("mid"), txt(red2, "X")), el(red2, mark("alone")), el(red2, txt(red2, "X")))
	res, err := Layout(context.Background(), root, ahem(t), Options{Page: page(10, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := res.Anchors["mid"]; !ok || d.Page != 2 || d.X != px(2) {
		t.Errorf("mid: %+v %v, want page 2", d, ok)
	}
	if d, ok := res.Anchors["alone"]; !ok || d.Page != 3 {
		t.Errorf("alone: %+v %v, want page 3", d, ok)
	}
}
