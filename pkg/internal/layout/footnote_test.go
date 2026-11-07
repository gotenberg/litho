package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// call returns a footnote call drawing label in red whose body is a block
// of text in blue.
func call(label, body string) *Node {
	cs := inlineSty(font(2), fg(red))
	bs := sty(font(2), fg(blue))
	n := el(cs, txt(cs, label))
	n.Footnote = el(bs, txt(bs, body))
	return n
}

func TestFootnote_Draw(t *testing.T) {
	t.Parallel()
	red2 := sty(font(2), fg(red))
	area := sty(font(2), func(s *css.Style) { s.MaxHeight = css.Pts(px(2)) })
	tests := []struct {
		name string
		root *Node
		pb   PageBox
		want string
	}{
		{"area at the bottom", doc(el(red2, txt(red2, "X"), call("X", "XX"))), PageBox{Width: px(6), Height: px(6)}, `
			RRRR__
			RRRR__
			______
			______
			BBBB__
			BBBB__`},
		{"the page shrinks for the area", doc(el(red2, txt(red2, "X"), call("X", "XX")), el(red2, txt(red2, "XX")), el(red2, txt(red2, "X"))), PageBox{Width: px(6), Height: px(6)}, `
			RRRR__
			RRRR__
			RRRR__
			RRRR__
			BBBB__
			BBBB__
			RR____
			RR____
			______
			______
			______
			______`},
		{"max-height moves a body on", doc(el(red2, call("X", "X"), call("X", "XX"))), PageBox{Width: px(6), Height: px(4), Footnote: area}, `
			RRRR__
			RRRR__
			BB____
			BB____
			______
			______
			BBBB__
			BBBB__`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := run(t, tt.root, tt.pb)
			var rows string
			for i := range res.Pages {
				rows += "\n" + strings.Join(grid(render(t, res, i+1)), "\n")
			}
			if want := normalize(tt.want); rows[1:] != want {
				t.Errorf("pixels:\n%s\nwant:\n%s", rows[1:], want)
			}
		})
	}
}

// normalize returns the rows of a pixel grid literal, trimmed, one a line.
func normalize(want string) string {
	var rows []string
	for _, l := range strings.Split(want, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			rows = append(rows, l)
		}
	}
	return strings.Join(rows, "\n")
}

func TestFootnote_OffTheCutsFollowsTheFlow(t *testing.T) {
	t.Parallel()
	red2 := sty(font(2), fg(red))
	ib := sty(font(2), fg(red), display(css.InlineBlock))
	root := doc(el(red2, txt(red2, "X"), el(ib, call("X", "XX"))))
	assertPixels(t, run(t, root, PageBox{Width: px(6), Height: px(4)}), 2, `
		______
		______
		BBBB__
		BBBB__`)
}
