package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestLineRuleMultiple_SlackAtPageEnd lets the room a proportional line
// spacing adds below the glyphs of a page's last line hang past the page
// cut, as Writer does: the line stays on the page when its glyphs fit,
// and the next line starts at the next page's top. A CSS line height, or
// glyphs that do not fit, move the line down.
func TestLineRuleMultiple_SlackAtPageEnd(t *testing.T) {
	tests := []struct {
		name  string
		rule  css.LineRule
		pageH float64
		page1 []string
		page2 float64
	}{
		{"office", css.LineRuleMultiple, 50, []string{"a", "b", "c"}, 0},
		{"glyphs past the cut", css.LineRuleMultiple, 45, []string{"a", "b"}, 0},
		{"css", css.LineRuleCSS, 50, []string{"a", "b"}, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := func(s *css.Style) {
				s.LineRule = tt.rule
				s.LineHeight = css.Dimension{Kind: css.Number, Value: 2}
				s.Widows, s.Orphans = 1, 1
			}
			res := run(t, doc(el(sty(m, width(10)), txt(sty(m), "a b c d"))), page(20, tt.pageH))
			got, _ := lineTexts(res, 1)
			if len(got) != len(tt.page1) {
				t.Fatalf("page 1 lines %q, want %q", got, tt.page1)
			}
			for i := range got {
				if trimSpaces(got[i]) != tt.page1[i] {
					t.Fatalf("page 1 lines %q, want %q", got, tt.page1)
				}
			}
			_, pos := lineTexts(res, 2)
			// The glyphs of the first line of page 2 sit at the line's
			// top, plus the half-leading under the CSS rule.
			if len(pos) == 0 || !near(pos[0][1], tt.page2+8) {
				t.Errorf("page 2 lines at %v, want the first baseline at %v", pos, tt.page2+8)
			}
		})
	}
}
