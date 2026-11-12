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

// TestWordRules_WidowsKeepNext moves a heading kept with a paragraph that
// moves whole for its widows along with it, as Word does.
//
// # Corpus evidence
//
//   - litho/docx paper-a4-landscape: Word prints the heading "3. Checking
//     a conversion" and the three lines of the paragraph it keeps with at
//     the top of page 3, where the heading and two lines fit on page 2.
func TestWordRules_WidowsKeepNext(t *testing.T) {
	st := sty(width(10), func(s *css.Style) { s.WordRules = 15 })
	head := sty(width(10), func(s *css.Style) { s.WordRules = 15; s.BreakAfter = css.BreakAvoidPage })
	rb := boxes(t, doc(el(st, txt(st, "a")), el(head, txt(head, "a")), el(st, txt(st, "a a a"))), page(10, 40))
	if got := kid(t, rb, 0, 1).lines[0].y / css.Pt; !near(got, 40) {
		t.Errorf("heading at %v, want 40", got)
	}
	if got := kid(t, rb, 0, 2).lines[0].y / css.Pt; !near(got, 50) {
		t.Errorf("paragraph's first line at %v, want 50", got)
	}
}
