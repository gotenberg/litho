package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestInlineBox_SplitByBidiReordering checks CSS Writing Modes 3, 2.4.5:
// bidi reordering splits an inline box into one border box a fragment, and
// its edges go to its outer fragments in visual order.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 draws a bordered span split by an outer
//     right-to-left word as two boxes, the left border on the left-most one
//     and the right border on the right-most one, whatever the span's
//     direction.
func TestInlineBox_SplitByBidiReordering(t *testing.T) {
	sm := func(s *css.Style) { s.FontSize = px(4) }
	over := func(s *css.Style) { s.Direction = css.RTL; s.UnicodeBidi = css.BidiOverride }
	clear := css.Color{}
	for _, tc := range []struct {
		name string
		dir  css.Direction
	}{{"ltr", css.LTR}, {"rtl", css.RTL}} {
		t.Run(tc.name, func(t *testing.T) {
			span := inlineSty(sm, func(s *css.Style) {
				s.Direction = tc.dir
				s.Border = [4]css.Border{{}, {Width: px(1), Style: css.Solid, Color: red}, {}, {Width: px(1), Style: css.Solid, Color: red}}
			}, fg(blue), bg(black))
			b := inlineSty(sm, over, fg(lime))
			c := inlineSty(sm, over, fg(clear))
			// "a" sits at level 0; the overridden "b" inside the span and
			// "c" after it share level 1 and swap: a c b. The span's
			// fragments are "a" and "b": its background stops short of "c",
			// whose glyph is transparent.
			root := doc(el(sty(sm), el(span, txt(span, "a"), el(b, txt(b, "b"))), el(c, txt(c, "c"))))
			res := run(t, root, page(14, 4))
			assertPixels(t, res, 1, `
				RBBBB____GGGGR
				RBBBB____GGGGR
				RBBBB____GGGGR
				RBBBB____GGGGR`)
		})
	}
}
