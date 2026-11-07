package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func clipText(s *css.Style) { s.BackgroundClip = css.TextEdge }

func transparent(s *css.Style) { s.Color = css.Color{} }

// rowsOf returns n rows of the pattern line, then m blank rows of its
// width.
func rowsOf(line string, n, m int) string {
	rows := strings.Repeat(line+"\n", n) + strings.Repeat(strings.Repeat("_", len(line))+"\n", m)
	return strings.TrimSuffix(rows, "\n")
}

func TestBackgroundClip_Text(t *testing.T) {
	// The background paints under the glyphs alone, Ahem's squares here,
	// of the box, its inline blocks and its relatively positioned boxes
	// (CSS Backgrounds 4, 2.4).
	ib := sty(display(css.InlineBlock), transparent)
	rel := inlineSty(transparent, func(s *css.Style) {
		s.Position = css.Relative
		s.Left = css.Pts(px(-10))
	})
	block := func(kids ...*Node) *Node {
		return doc(el(sty(width(30), bg(lime), clipText, transparent), kids...))
	}
	both := rowsOf("GGGGGGGGGG__________GGGGGGGGGG", 10, 10)
	tests := []struct {
		name string
		root *Node
		want string
	}{
		{"block", block(txt(sty(transparent), "X X")), both},
		{"inline block", block(txt(sty(transparent), "X "), el(ib, txt(ib, "X"))), both},
		{"relative", block(txt(sty(transparent), "X "), el(rel, txt(rel, "X"))),
			rowsOf("GGGGGGGGGGGGGGGGGGGG__________", 10, 10)},
		{"no text", doc(el(sty(width(30), height(10), bg(lime), clipText))), rowsOf(strings.Repeat("_", 30), 20, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPixels(t, run(t, tt.root, page(30, 20)), 1, tt.want)
		})
	}
}
