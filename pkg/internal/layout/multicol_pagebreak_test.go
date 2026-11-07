package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestMulticol_PageBreakInColumns ends the columns' row at a forced page
// break inside them: the row on its page keeps the content before the
// break, filled in order, and the columns resume on the next page, where
// the last row balances, as Writer breaks a page inside a section of
// columns. A column break still breaks the column.
func TestMulticol_PageBreakInColumns(t *testing.T) {
	t.Parallel()
	before := func(v css.Break) func(*css.Style) { return func(s *css.Style) { s.BreakBefore = v } }
	after := func(v css.Break) func(*css.Style) { return func(s *css.Style) { s.BreakAfter = v } }
	nested := el(sty(), strip(blue, before(css.Page)), strip(lime))
	tests := []struct {
		name string
		root *Node
		want string
	}{
		{"break-before page", doc(el(sty(columns(2, 1)), strip(red), strip(red), strip(blue, before(css.Page)), strip(lime), strip(black), strip(red))), `
			RR___
			RR___
			_____
			_____
			BB_KK
			GG_RR
			_____
			_____`},
		{"break-after page", doc(el(sty(columns(2, 1)), strip(red, after(css.Page)), strip(blue), strip(lime))), `
			RR___
			_____
			_____
			_____
			BB_GG
			_____
			_____
			_____`},
		{"page wins over column", doc(el(sty(columns(2, 1)), strip(red, after(css.Column)), strip(blue, before(css.Page)))), `
			RR___
			_____
			_____
			_____
			BB___
			_____
			_____
			_____`},
		{"from the second column", doc(el(sty(columns(2, 1)), strip(red), strip(lime, before(css.Column)), strip(blue, before(css.Page)))), `
			RR_GG
			_____
			_____
			_____
			BB___
			_____
			_____
			_____`},
		{"inside a nested block", doc(el(sty(columns(2, 1)), strip(red), nested)), `
			RR___
			_____
			_____
			_____
			BB_GG
			_____
			_____
			_____`},
		{"column break", doc(el(sty(columns(2, 1)), strip(red), strip(blue, before(css.Column)), strip(lime))), `
			RR_BB
			___GG
			_____
			_____`},
		{"content after the columns follows the last row", doc(el(sty(columns(2, 1)), strip(red), strip(blue, before(css.Page)), strip(lime)), strip(black)), `
			RR___
			_____
			_____
			_____
			BB_GG
			KKKKK
			_____
			_____`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := run(t, tt.root, page(5, 4))
			var rows string
			for i := range res.Pages {
				rows += "\n" + strings.Join(grid(render(t, res, i+1)), "\n")
			}
			if want := normalize(tt.want); rows[1:] != want {
				t.Errorf("pixels:\n%s\nwant:\n%s", rows[1:], want)
			}
		})
	}
}
