package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestInlineEdges_EmptyPieces(t *testing.T) {
	// An inline box split by a block: its first piece draws no end edge
	// and makes no line; its last piece makes a line as tall as the box,
	// with no half-leading of its own (CSS 2.1, 9.2.1.1, 9.4.2 and
	// 10.8.1).
	sp := inlineSty(func(s *css.Style) {
		s.FontSize = px(4)
		s.Border[css.Right] = css.Border{Width: px(4), Style: css.Solid, Color: lime}
		s.Margin[css.Right] = css.Pts(px(-4))
	})
	small := func(s *css.Style) { s.FontSize = px(4) }
	tests := []struct {
		name string
		root *Node
	}{
		{"split by a block", doc(el(sty(width(4), bg(red), small), el(sp, txt(sp, " "), el(sty(small)), txt(sp, " "))))},
		{"empty", doc(el(sty(width(4), bg(red), small), el(sp)))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPixels(t, run(t, tt.root, page(6, 6)), 1, `
				GGGG__
				GGGG__
				GGGG__
				GGGG__
				______
				______`)
		})
	}
}
