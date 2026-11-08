package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// noteCall returns a note call drawing label in red whose body is a block
// of text in blue.
func noteCall(label, body string) *Node {
	n := call(label, body)
	ns := *n.Footnote.Style
	ns.Note = "note"
	n.Footnote.Style = &ns
	n.Footnote.Children[0].Style = &ns
	return n
}

func TestNote_Draw(t *testing.T) {
	t.Parallel()
	red2 := sty(font(2), fg(red))
	abs := sty(font(2), func(s *css.Style) {
		s.Position = css.Absolute
		s.Right, s.Bottom = css.Pts(0), css.Pts(0)
		s.Width = css.Pts(px(4))
	})
	flex := sty(font(2), display(css.Flex))
	tests := []struct {
		name string
		root *Node
		pb   PageBox
		want string
	}{
		{"area at the top pushes the flow down", doc(el(red2, txt(red2, "X"), noteCall("X", "XX"))), PageBox{Width: px(6), Height: px(6)}, `
			BBBB__
			BBBB__
			RRRR__
			RRRR__
			______
			______`},
		{"an absolute area takes no room", doc(el(red2, txt(red2, "X"), noteCall("X", "XX"))), PageBox{Width: px(6), Height: px(4), NoteArea: abs}, `
			RRRR__
			RRRR__
			__BBBB
			__BBBB`},
		{"a flex area lays the bodies out in a row", doc(el(red2, noteCall("X", "X"), noteCall("X", "X"))), PageBox{Width: px(6), Height: px(4), NoteArea: flex}, `
			BBBB__
			BBBB__
			RRRR__
			RRRR__`},
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

func TestNote_NextPageWithoutRoom(t *testing.T) {
	t.Parallel()
	red2 := sty(font(2), fg(red))
	root := doc(el(red2, txt(red2, "XXX")), el(red2, txt(red2, "X"), noteCall("X", "XX")))
	assertPixels(t, run(t, root, PageBox{Width: px(6), Height: px(4)}), 2, `
		BBBB__
		BBBB__
		______
		______`)
}
