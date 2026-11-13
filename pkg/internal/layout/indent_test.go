package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// CSS Text 3, 8.1: text-indent indents the first line, each-line the
// first line after each forced break as well, and hanging every line but
// those.
func TestTextIndent_HangingEachLine(t *testing.T) {
	tests := []struct {
		name string
		set  func(*css.Style)
		want []float64
	}{
		{"first line", nil, []float64{10, 0, 0}},
		{"hanging", func(s *css.Style) { s.TextIndentHanging = true }, []float64{0, 10, 10}},
		{"each-line", func(s *css.Style) { s.TextIndentEachLine = true }, []float64{10, 0, 10}},
		{"each-line hanging", func(s *css.Style) { s.TextIndentEachLine, s.TextIndentHanging = true, true }, []float64{0, 10, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := sty(func(s *css.Style) { s.TextIndent = css.Pts(px(10)) })
			if tt.set != nil {
				tt.set(st)
			}
			// "aa aa" wraps after its first word at 35px; the forced
			// break starts the third line.
			rb := boxes(t, doc(el(st, txt(st, "aa aa"), br(st), txt(st, "aa"))), page(35, 100))
			lines := kid(t, rb, 0, 0).lines
			if len(lines) != len(tt.want) {
				t.Fatalf("%d lines, want %d", len(lines), len(tt.want))
			}
			for i, l := range lines {
				if got := l.items[0].x / css.Pt; !near(got, tt.want[i]) {
					t.Errorf("line %d starts at %vpx, want %v", i, got, tt.want[i])
				}
			}
		})
	}
}
