package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestTextShadow_PaintsAnOffsetCopy(t *testing.T) {
	st := sty(func(s *css.Style) {
		s.TextShadow = []css.Shadow{{X: px(2), Y: px(3), Color: css.Color{B: 1, A: 1}}}
	})
	res := run(t, doc(el(st, txt(st, "a"))), page(100, 100))
	var runs []*typeset.Text
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Text:
				runs = append(runs, v)
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[0].Content)
	if len(runs) != 2 || runs[0].Parent != nil || !near(runs[0].Origin.X-runs[1].Origin.X, px(2)) || !near(runs[0].Origin.Y-runs[1].Origin.Y, px(3)) {
		t.Fatalf("runs %+v: want the shadow, an artifact, 2px right and 3px down, then the text", runs)
	}
}
