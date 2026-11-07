package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestTextShadow_PaintsAnOffsetCopy(t *testing.T) {
	st := sty(func(s *css.Style) {
		s.TextShadow = []css.Shadow{{X: px(2), Y: px(3), Color: css.Color{B: 1, A: 1}}}
	})
	res := run(t, doc(el(st, txt(st, "a"))), page(100, 100))
	var runs []*compose.Text
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Text:
				runs = append(runs, v)
			case *compose.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[0].Content)
	if len(runs) != 2 || runs[0].Parent != nil || !near(runs[0].Origin.X-runs[1].Origin.X, px(2)) || !near(runs[0].Origin.Y-runs[1].Origin.Y, px(3)) {
		t.Fatalf("runs %+v: want the shadow, an artifact, 2px right and 3px down, then the text", runs)
	}
}
