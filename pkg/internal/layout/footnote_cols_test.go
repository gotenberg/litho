package layout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestFootnote_BalancedColumnsKeepBodiesOnTheirPage puts the bodies of the
// calls of every column of a balanced column set on the calls' page, at
// the bottom of the page across the columns, when the page has the room.
func TestFootnote_BalancedColumnsKeepBodiesOnTheirPage(t *testing.T) {
	t.Parallel()
	mc := sty(func(s *css.Style) { s.Columns.Count, s.ColumnGap = 3, css.Pts(px(10)) })
	var paras []*Node
	for i := range 7 {
		paras = append(paras, el(sty(), txt(sty(), "x"), call("c", fmt.Sprintf("n%d", i))))
	}
	for _, n := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d sets", n), func(t *testing.T) {
			var sets []*Node
			for range n {
				sets = append(sets, el(mc, paras...))
			}
			res := run(t, doc(sets...), page(200, 400))
			var bodies []string
			for _, r := range texts(res, 1) {
				if strings.HasPrefix(r.text, "n") {
					bodies = append(bodies, r.text)
				}
			}
			if len(res.Pages) != 1 || len(bodies) != 7*n {
				t.Errorf("%d pages, page 1 bodies %v, want 1 page and %d bodies", len(res.Pages), bodies, 7*n)
			}
		})
	}
}
