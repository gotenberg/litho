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

// TestAlignContent_CentresABlockLevelControlsLine centres the line of a
// form control laid out as a block on the page under align-content:
// center, as an inline-block's (CSS Align 3, 5.1): its one line is no
// content a page break cuts. A plain block keeps its line at its top.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 centres the
//     value of a display: block text field 40px tall, as it centres an
//     inline one.
func TestAlignContent_CentresABlockLevelControlsLine(t *testing.T) {
	t.Parallel()
	top := func(ctl *Control) float64 {
		st := sty(display(css.Block), width(40), height(30), func(s *css.Style) { s.AlignContent = css.BoxCenter })
		n := &Node{Kind: Element, Style: st, Control: ctl, Children: []*Node{txt(st, "a")}}
		rb := boxes(t, doc(n), page(100, 100))
		b := ofNode(rb, n)
		return b.lines[0].y - b.contentY()
	}
	if got := top(&Control{Kind: ControlTextField}); !near(got, px(10)) {
		t.Errorf("text field: line %.2f below the top, want %.2f", got, px(10))
	}
	if got := top(nil); !near(got, 0) {
		t.Errorf("block: line %.2f below the top, want 0", got)
	}
}
