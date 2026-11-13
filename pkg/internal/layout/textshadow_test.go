package layout

import (
	"strings"
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

func TestTextShadow_BlurredIsABitmapUnderInvisibleText(t *testing.T) {
	st := sty(func(s *css.Style) {
		s.TextShadow = []css.Shadow{{X: px(2), Y: px(3), Blur: px(4), Color: css.Color{B: 1, A: 1}}}
	})
	res := run(t, doc(el(st, txt(st, "a"))), page(100, 100))
	var kinds []string
	var copyAt typeset.Point
	var walk func([]typeset.Fragment, float64)
	walk = func(fs []typeset.Fragment, opacity float64) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Image:
				if v.Parent != nil {
					t.Errorf("the shadow's bitmap belongs to %v, want an artifact", v.Parent)
				}
				kinds = append(kinds, "image")
			case *typeset.Text:
				if opacity < 1 || v.Invisible {
					kinds = append(kinds, "invisible text")
					copyAt = v.Origin
					if v.Parent != nil {
						t.Errorf("the shadow's text belongs to %v, want an artifact", v.Parent)
					}
				} else {
					kinds = append(kinds, "text")
					if !near(copyAt.X-v.Origin.X, px(2)) || !near(copyAt.Y-v.Origin.Y, px(3)) {
						t.Errorf("the shadow's copy at %v, the text at %v: want it 2px right and 3px down", copyAt, v.Origin)
					}
				}
			case *typeset.Group:
				o := v.Opacity
				if o == 0 {
					o = 1
				}
				walk(v.Content, opacity*o)
			}
		}
	}
	walk(res.Pages[0].Content, 1)
	if got := strings.Join(kinds, ", "); got != "image, invisible text, text" {
		t.Errorf("painted %s, want the blurred bitmap, the shadow's text invisible, then the text", got)
	}
	img := render(t, res, 1)
	if c := at(img, 11, 11); c.B < 200 || c.R > 220 || c.R < 20 {
		t.Errorf("past the glyph, inside its shadow: %v, want a blurred blue", c)
	}
}
