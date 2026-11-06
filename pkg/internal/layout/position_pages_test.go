package layout

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestPosition_EmptyBoxTakesItsPages locks that an absolutely positioned
// box takes the pages its border box reaches though it paints nothing: it
// adds to the root's scrollable overflow (CSS Overflow 3, 2.2), which the
// pages show. A box a clipping box holds, and a fixed box, take none.
func TestPosition_EmptyBoxTakesItsPages(t *testing.T) {
	t.Parallel()
	nan := math.NaN()
	tall := func(m ...func(*css.Style)) *Node {
		return el(sty(append([]func(*css.Style){abs(0, nan, nan, 0), height(250)}, m...)...))
	}
	tests := []struct {
		name string
		root *Node
		want int
	}{
		{"empty positioned box", el(sty(), tall()), 3},
		{"nested in a positioned box", el(sty(), el(sty(abs(0, nan, nan, 0)), tall())), 3},
		{"under a clipping box", el(sty(), el(sty(height(50), func(s *css.Style) { s.Overflow = css.OverflowHidden; s.Position = css.Relative }), tall())), 1},
		{"fixed", el(sty(), tall(func(s *css.Style) { s.Position = css.Fixed })), 1},
	}
	for _, tt := range tests {
		res := run(t, tt.root, page(100, 100))
		if len(res.Pages) != tt.want {
			t.Errorf("%s: %d pages, want %d", tt.name, len(res.Pages), tt.want)
		}
	}
}
