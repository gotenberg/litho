package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// strokes returns the stroked paths of page n of res, nested groups
// included.
func strokes(res *Result, n int) []*compose.Path {
	var out []*compose.Path
	var walk func(fs []compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Path:
				if v.Stroke != nil {
					out = append(out, v)
				}
			case *compose.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[n-1].Content)
	return out
}

// TestStrokeBorders strokes the solid borders of a box whose style asks for
// css.Office.StrokeBorders along the middle of each side, at least
// css.MinBorderStroke wide, the separate borders of a block and the
// collapsed ones of a table alike, and leaves a CSS box's borders filled.
func TestStrokeBorders(t *testing.T) {
	solid := func(w float64, office bool) func(*css.Style) {
		return func(s *css.Style) {
			for i := range s.Border {
				s.Border[i] = css.Border{Width: w, Style: css.Solid, Color: black}
			}
			s.StrokeBorders = office
		}
	}
	tests := []struct {
		name  string
		node  *Node
		n     int
		width float64
	}{
		{"hairline", el(sty(solid(0.05, true), height(4))), 4, css.MinBorderStroke},
		{"thick", el(sty(solid(0.75, true), height(4))), 4, 0.75},
		{"css", el(sty(solid(0.05, false), height(4))), 0, 0},
		{"collapsed cell", table(mods(collapse, func(s *css.Style) { s.StrokeBorders = true }),
			tr(nil, td(mods(solid(0.05, false), height(4)), ""))), 4, css.MinBorderStroke},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(tt.node), page(px(20), px(20)))
			got := strokes(res, 1)
			if len(got) != tt.n {
				t.Fatalf("%d strokes, want %d", len(got), tt.n)
			}
			for _, p := range got {
				if !near(p.Stroke.Width, tt.width) {
					t.Errorf("stroke width %v, want %v", p.Stroke.Width, tt.width)
				}
			}
		})
	}
}

// TestStrokeBorders_Middle puts a stroked top border on the middle of the
// side, from one outer corner to the other.
func TestStrokeBorders_Middle(t *testing.T) {
	st := sty(height(4), func(s *css.Style) {
		s.Border[css.Top] = css.Border{Width: 2, Style: css.Solid, Color: black}
		s.StrokeBorders = true
	})
	res := run(t, doc(el(st)), page(px(20), px(20)))
	got := strokes(res, 1)
	if len(got) != 1 {
		t.Fatalf("%d strokes, want 1", len(got))
	}
	a, z := got[0].Ops[0].P[0], got[0].Ops[1].P[0]
	b := boxes(t, doc(el(st)), page(px(20), px(20)))
	k := kid(t, b, 0, 0)
	if !near(a.Y, k.y+1) || !near(z.Y, k.y+1) || !near(a.X, k.x) || !near(z.X, k.x+k.borderW()) {
		t.Errorf("line from %v to %v, want y %v from %v to %v", a, z, k.y+1, k.x, k.x+k.borderW())
	}
}
