package slides

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestShape_Shadow checks that a shadow paints a copy of the shape's fill
// under it, moved by its offsets, scaled about the shape's centre and at
// the shadow's opacity, and that a blur only warns.
func TestShape_Shadow(t *testing.T) {
	box := compose.Rect{X: 100, Y: 100, Width: 200, Height: 100}
	for _, tc := range []struct {
		name   string
		shadow Shadow
		want   compose.Rect
		warns  int
	}{
		{"offset", Shadow{Color: css.Color{A: 1}, Opacity: 0.5, OffsetX: 10, OffsetY: 20}, compose.Rect{X: 110, Y: 120, Width: 200, Height: 100}, 0},
		{"scaled", Shadow{Color: css.Color{A: 1}, Opacity: 0.5, ScaleX: 0.5, ScaleY: 2}, compose.Rect{X: 150, Y: 50, Width: 100, Height: 200}, 0},
		{"blurred", Shadow{Color: css.Color{A: 1}, Opacity: 0.5, OffsetX: 5, Blur: 4}, compose.Rect{X: 105, Y: 100, Width: 200, Height: 100}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := &Shape{Box: box, Fill: Fill{Kind: FillSolid, Color: red}, Shadow: &tc.shadow}
			doc := convert(t, deck(&Slide{Shapes: []*Shape{sh}}), office.Options{})
			var ps []placedPath
			paths(doc.Pages[0].Content, compose.Identity, &ps)
			if len(ps) != 2 {
				t.Fatalf("%d paths, want the shadow then the shape", len(ps))
			}
			if got := ps[0].bounds(); !rectNear(got, tc.want, 0.01) {
				t.Errorf("shadow bounds %+v, want %+v", got, tc.want)
			}
			if ps[0].p.Fill == nil || !near(ps[0].p.Fill.Color.A, 0.5, 1e-9) {
				t.Errorf("shadow fill %+v, want alpha 0.5", ps[0].p.Fill)
			}
			if got := ps[1].bounds(); !rectNear(got, box, 0.01) {
				t.Errorf("shape bounds %+v, want %+v", got, box)
			}
			if n := len(doc.Warnings); n != tc.warns {
				t.Errorf("%d warnings %v, want %d", n, doc.Warnings, tc.warns)
			}
		})
	}
	t.Run("none", func(t *testing.T) {
		sh := &Shape{Box: box, Fill: Fill{Kind: FillSolid, Color: red}, Shadow: &Shadow{Color: css.Color{A: 1}}}
		doc := convert(t, deck(&Slide{Shapes: []*Shape{sh}}), office.Options{})
		var ps []placedPath
		paths(doc.Pages[0].Content, compose.Identity, &ps)
		if len(ps) != 1 {
			t.Fatalf("%d paths, want the shape alone for a shadow of opacity 0", len(ps))
		}
	})
}
