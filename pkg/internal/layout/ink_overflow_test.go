package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestPaging_LineInkOverflowsIntoTheMargin locks that what a line paints
// past the page area, an underline below it here, shows in the page
// margin rather than being cut at the page area: a line sits on one page
// whole (CSS Fragmentation 3, 4.4, a line box is monolithic), and its ink
// overflow is painted with it, as browsers print it. A box that spans the
// cut is still split there.
func TestPaging_LineInkOverflowsIntoTheMargin(t *testing.T) {
	lime := css.Color{G: 1, A: 1}
	u := sty(func(s *css.Style) {
		s.TextDecoration = css.Underline
		s.TextDecorationColor = lime
		s.TextUnderlineOffset = css.Pts(px(4))
		s.TextDecorationThickness = css.Pts(px(2))
	})
	pb := page(10, 14)
	pb.Margin[css.Bottom] = px(4)
	res := run(t, doc(el(u, txt(sty(fg(css.Color{})), "a"))), pb)
	rows := grid(render(t, res, 1))
	for r, row := range rows {
		if r >= 10 && strings.Contains(row, "G") {
			return
		}
	}
	t.Errorf("no underline in the bottom margin:\n%s", strings.Join(rows, "\n"))
}
