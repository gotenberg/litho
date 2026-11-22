package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestAlignContent_CentresAControlsLine centres the line of an inline-block
// under align-content: center in a box taller than it (CSS Align 3, 5.1),
// and leaves it at the top under normal.
func TestAlignContent_CentresAControlsLine(t *testing.T) {
	t.Parallel()
	top := func(a css.BoxAlign) float64 {
		ib := el(sty(display(css.InlineBlock), width(40), height(30), func(s *css.Style) { s.AlignContent = a }), txt(sty(), "a"))
		rb := boxes(t, doc(el(sty(), ib)), page(100, 100))
		b := ofNode(rb, ib)
		return b.lines[0].y - b.contentY()
	}
	if got := top(css.BoxNormal); !near(got, 0) {
		t.Errorf("normal: line %.2f below the top, want 0", got)
	}
	if got := top(css.BoxCenter); !near(got, px(10)) {
		t.Errorf("center: line %.2f below the top, want %.2f", got, px(10))
	}
}
