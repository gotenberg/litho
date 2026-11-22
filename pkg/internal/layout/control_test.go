package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestControl_DefaultSize sizes form controls under field-sizing: fixed
// by the HTML Standard's rules in Blink's arithmetic (15.5.15 to
// 15.5.17), in Ahem: an average character width of 0.982 em and a maximum
// of 1 em.
func TestControl_DefaultSize(t *testing.T) {
	ahemSize := func(size float64) func(*css.Style) {
		return func(s *css.Style) { s.FontSize = px(size) }
	}
	for name, tc := range map[string]struct {
		c    *Control
		mods []func(*css.Style)
		w, h float64
	}{
		// avg 9.82 rounds up to 10, max 10: 19 × 10 + 10.
		"text field of size 20 at 10px": {&Control{Kind: ControlTextField, Chars: 20}, nil, 200, 10},
		// avg 39.28 stays, max 40: ceil(4 × 39.28 + 40).
		"text field of size 5 at 40px": {&Control{Kind: ControlTextField, Chars: 5}, []func(*css.Style){ahemSize(40)}, 198, 40},
		// 13.333px is read at 13.328125: avg 13.088 stays, max 13.
		"text field at 13.333px": {&Control{Kind: ControlTextField, Chars: 20}, []func(*css.Style){ahemSize(40.0 / 3)}, 262, 40.0 / 3},
		"textarea of 10 by 3":    {&Control{Kind: ControlTextArea, Chars: 10, Rows: 3}, nil, 115, 30},
		"drop-down box":          {&Control{Kind: ControlDropDown, Labels: []ControlLabel{{Text: "ab"}, {Text: "abc"}}}, nil, 50, 12},
		"list box":               {&Control{Kind: ControlListBox, Rows: 3, Labels: []ControlLabel{{Text: "abcd"}, {Text: "a", Indent: px(40)}}}, nil, 54, 39},
		"author width kept":      {&Control{Kind: ControlTextField, Chars: 20}, []func(*css.Style){width(50)}, 50, 10},
		"content sizing":         {&Control{Kind: ControlTextField, Chars: 20}, []func(*css.Style){func(s *css.Style) { s.FieldSizing = css.FieldContent }}, 0, 0},
		"border box": {&Control{Kind: ControlTextField, Chars: 20}, []func(*css.Style){func(s *css.Style) {
			s.BoxSizing = css.BorderBox
			s.Padding[css.Left] = css.Pts(px(3))
			s.Border[css.Right] = css.Border{Width: px(2), Style: css.Solid}
		}}, 200, 10},
	} {
		st := sty(append([]func(*css.Style){display(css.InlineBlock)}, tc.mods...)...)
		ctl := &Node{Kind: Element, Style: st, Control: tc.c}
		rb := boxes(t, doc(el(sty(), ctl)), page(800, 200))
		b := ofNode(rb, ctl)
		if b == nil {
			t.Fatalf("%s: no box of the control", name)
		}
		if !near(b.w/css.Pt, tc.w) || !near(b.h/css.Pt, tc.h) {
			t.Errorf("%s: content box %.3f by %.3f px, want %v by %v", name, b.w/css.Pt, b.h/css.Pt, tc.w, tc.h)
		}
	}
}

// ofNode returns the first box of root's tree that node generated.
func ofNode(root *box, node *Node) *box {
	if root.node == node {
		return root
	}
	for _, k := range root.kids {
		if b := ofNode(k, node); b != nil {
			return b
		}
	}
	return nil
}
