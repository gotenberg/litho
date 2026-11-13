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

func TestTextPaint_KeepsTheTextUnderItsOutline(t *testing.T) {
	st := sty(func(s *css.Style) {
		s.TextPaint = &css.OfficeTextPaint{NoFill: true, Outline: &css.OfficeOutline{Color: css.Color{R: 1, A: 1}, Width: 0.75}}
	})
	res := run(t, doc(el(st, txt(st, "ab"))), page(200, 100))
	var text *typeset.Text
	var textOpacity float64
	var stroke *typeset.Path
	var walk func([]typeset.Fragment, float64)
	walk = func(fs []typeset.Fragment, op float64) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Text:
				text, textOpacity = v, op
			case *typeset.Path:
				if v.Stroke != nil {
					stroke = v
				}
			case *typeset.Group:
				o := v.Opacity
				if o == 0 {
					o = 1
				}
				walk(v.Content, op*o)
			}
		}
	}
	walk(res.Pages[0].Content, 1)
	if text == nil || text.Parent == nil || textOpacity > 1.0/512 {
		t.Errorf("text %+v at opacity %v, want the tagged text, invisible", text, textOpacity)
	}
	if stroke == nil || stroke.Parent != nil || stroke.Stroke.Color.R != 1 || stroke.Stroke.Width != 0.75 {
		t.Errorf("outline %+v, want a red 0.75 point artifact stroke", stroke)
	}
}

// TestTextEffects_GroupWithTheirBox locks that a translucent or blended box
// holding a run with Office text effects composites the effect bitmaps,
// the run and its underline as one transparency group.
func TestTextEffects_GroupWithTheirBox(t *testing.T) {
	for _, c := range []struct {
		name string
		mod  func(*css.Style)
	}{
		{"opacity", func(s *css.Style) { s.Opacity = 0.5 }},
		{"blend", func(s *css.Style) { s.MixBlendMode = css.BlendMultiply }},
	} {
		t.Run(c.name, func(t *testing.T) {
			run1 := sty(func(s *css.Style) {
				s.TextDecoration = css.Underline
				s.TextEffects = &css.OfficeEffects{Glow: &css.OfficeGlow{Color: css.Color{R: 1, A: 0.4}, Radius: 6}}
			})
			res := run(t, doc(el(sty(c.mod), el(run1, txt(run1, "ab")))), page(200, 100))
			var group *typeset.Group
			var find func([]typeset.Fragment)
			find = func(fs []typeset.Fragment) {
				for _, f := range fs {
					if g, ok := f.(*typeset.Group); ok {
						if g.Transparency != nil && group == nil {
							group = g
						}
						find(g.Content)
					}
				}
			}
			find(res.Pages[0].Content)
			if group == nil {
				t.Fatal("no transparency group")
			}
			var kinds []string
			var walk func([]typeset.Fragment)
			walk = func(fs []typeset.Fragment) {
				for _, f := range fs {
					switch v := f.(type) {
					case *typeset.Image:
						kinds = append(kinds, "image")
					case *typeset.Text:
						kinds = append(kinds, "text")
					case *typeset.Path:
						kinds = append(kinds, "path")
					case *typeset.Group:
						walk(v.Content)
					}
				}
			}
			walk(group.Content)
			want := map[string]bool{"image": false, "text": false, "path": false}
			for _, k := range kinds {
				want[k] = true
			}
			for k, ok := range want {
				if !ok {
					t.Errorf("the group holds no %s: %v", k, kinds)
				}
			}
		})
	}
}
