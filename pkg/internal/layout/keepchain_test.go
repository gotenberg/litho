package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestBreakAvoid_ChainFromPageTop leaves the break where it falls when the
// run of boxes avoiding a break after one another starts its page, as
// Writer ignores a keep-with-next chain that fills its page; a run that
// starts below other content moves to the next page whole.
func TestBreakAvoid_ChainFromPageTop(t *testing.T) {
	keep := func(s *css.Style) { s.BreakAfter = css.BreakAvoidPage }
	whole := func(s *css.Style) { s.BreakInside = css.InsideAvoid }
	blk := func(h float64, c css.Color, m ...func(*css.Style)) *Node {
		return el(sty(append([]func(*css.Style){height(h), width(2), bg(c)}, m...)...))
	}
	tests := []struct {
		name         string
		kids         []*Node
		page1, page2 string
	}{
		{"chain from the top", []*Node{blk(1, red, keep), blk(1, blue, keep), blk(1, lime, keep), blk(2, red, whole)}, `
			RR__
			BB__
			GG__
			____`, `
			RR__
			RR__
			____
			____`},
		{"chain below content", []*Node{blk(2, red), blk(1, blue, keep), blk(2, lime, whole)}, `
			RR__
			RR__
			____
			____`, `
			BB__
			GG__
			GG__
			____`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(tt.kids...), page(4, 4))
			if len(res.Pages) != 2 {
				t.Fatalf("%d pages, want 2", len(res.Pages))
			}
			assertPixels(t, res, 1, tt.page1)
			assertPixels(t, res, 2, tt.page2)
		})
	}
}
