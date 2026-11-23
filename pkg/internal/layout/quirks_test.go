package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestLineQuirks_NoStrut lays out lines by the line height calculation
// quirk and the blocks ignore line-height quirk (Quirks Mode Standard, 3.3
// and 3.4) in quirks and limited-quirks mode: no strut, an empty inline
// box without inline-axis border or padding adds nothing, a line holding a
// line break alone is as tall as the break's line height; no-quirks mode
// keeps the strut.
func TestLineQuirks_NoStrut(t *testing.T) {
	lh := func(v float64) func(*css.Style) {
		return func(s *css.Style) { s.LineHeight = css.Pts(px(v)) }
	}
	block := sty(lh(40))
	for _, tc := range []struct {
		name         string
		kids         func() []*Node
		quirks, none float64
	}{
		{"small span", func() []*Node { return []*Node{el(inlineSty(lh(10)), txt(inlineSty(lh(10)), "x"))} }, 10, 40},
		{"root text", func() []*Node {
			return []*Node{txt(sty(lh(40)), "x"), el(inlineSty(lh(10)), txt(inlineSty(lh(10)), "y"))}
		}, 40, 40},
		{"empty span", func() []*Node {
			return []*Node{el(inlineSty(lh(80))), el(inlineSty(lh(10)), txt(inlineSty(lh(10)), "y"))}
		}, 10, 80},
		{"padded empty span", func() []*Node {
			return []*Node{el(inlineSty(lh(80), padding(0, 1, 0, 0))), el(inlineSty(lh(10)), txt(inlineSty(lh(10)), "y"))}
		}, 80, 80},
		{"break alone", func() []*Node { return []*Node{br(inlineSty(lh(5)))} }, 5, 40},
		{"break then text", func() []*Node { return []*Node{br(inlineSty(lh(50))), txt(sty(lh(40)), "x")} }, 90, 80},
	} {
		for _, mode := range []css.DocumentMode{css.Quirks, css.LimitedQuirks, css.NoQuirks} {
			root := doc(el(block, tc.kids()...))
			b := kid(t, boxesWith(t, root, Options{Page: page(200, 200), Mode: mode}), 0)
			want := tc.quirks
			if mode == css.NoQuirks {
				want = tc.none
			}
			if got := rect(b)[3]; got != want {
				t.Errorf("%s, mode %d: height %v, want %v", tc.name, mode, got, want)
			}
		}
	}
}
