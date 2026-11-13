package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestTextEffects_PaintBitmapsUnderTheText(t *testing.T) {
	st := sty(func(s *css.Style) {
		s.TextEffects = &css.OfficeEffects{
			Glow:   &css.OfficeGlow{Color: css.Color{R: 1, A: 0.4}, Radius: 6},
			Shadow: &css.OfficeShadow{Color: css.Color{A: 0.4}, Blur: 3, Dist: 2, Dir: 45, SX: 1, SY: 1},
		}
	})
	res := run(t, doc(el(st, txt(st, "ab"))), page(200, 100))
	var order []typeset.Fragment
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Text, *typeset.Image:
				order = append(order, v)
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[0].Content)
	if len(order) != 3 {
		t.Fatalf("%d fragments, want the shadow, the glow and the text", len(order))
	}
	for i, f := range order[:2] {
		img, ok := f.(*typeset.Image)
		if !ok || img.Parent != nil {
			t.Errorf("fragment %d: %T, want an artifact image", i, f)
		}
	}
	if tx, ok := order[2].(*typeset.Text); !ok || tx.Parent == nil {
		t.Errorf("last fragment %T, want the tagged text above its effects", order[2])
	}
}
