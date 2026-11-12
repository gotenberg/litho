package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestWordRules_Widows moves a paragraph that cannot leave two lines on
// either side of a page break whole to the next page under
// css.Style.WordRules, as Word does, where CSS keeps its orphans and
// leaves a widow.
//
// # Corpus evidence
//
//   - litho/docx paper-letter-portrait: Word prints the three lines of
//     the paragraph "Headings keep with the paragraph..." at the top of
//     page 2, where two of them fit at the foot of page 1.
func TestWordRules_Widows(t *testing.T) {
	tests := []struct {
		name string
		word int
		want float64
	}{
		{"css", 0, 10},
		{"word", 15, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := sty(width(10), func(s *css.Style) { s.WordRules = tt.word })
			rb := boxes(t, doc(el(st, txt(st, "a")), el(st, txt(st, "a a a"))), page(10, 30))
			if got := kid(t, rb, 0, 1).lines[0].y / css.Pt; !near(got, tt.want) {
				t.Errorf("second paragraph's first line at %v, want %v", got, tt.want)
			}
		})
	}
}
