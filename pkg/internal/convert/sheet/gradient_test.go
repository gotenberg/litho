package sheet

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// shadeAt returns the offset an axial shading gives the point (x, y) of
// the path it fills.
func shadeAt(s *typeset.Shading, x, y float64) float64 {
	m := s.Transform
	det := m.A*m.D - m.B*m.C
	u := (m.D*(x-m.E) - m.C*(y-m.F)) / det
	v := (-m.B*(x-m.E) + m.A*(y-m.F)) / det
	dx, dy := s.To.X-s.From.X, s.To.Y-s.From.Y
	return ((u-s.From.X)*dx + (v-s.From.Y)*dy) / (dx*dx + dy*dy)
}

// TestGradientPaths locks a gradient fill as Excel prints it: a linear one
// laid out on the cell as a unit square, its corners at 0 and 1 and,
// at 45 degrees, its top-right and bottom-left corners alike; a path one
// as trapezoids around its inner rectangle, a point coloured by its larger
// distance from that rectangle.
func TestGradientPaths(t *testing.T) {
	white, blue := css.Color{R: 1, G: 1, B: 1, A: 1}, css.Color{B: 1, A: 1}
	stops := []GradientStop{{Position: 0, Color: white}, {Position: 1, Color: blue}}
	x, y, w, h := 10.0, 20.0, 120.0, 40.0
	for _, tc := range []struct {
		degree              float64
		tl, tr, bl, br, mid float64
	}{
		{0, 0, 1, 0, 1, 0.5},
		{90, 0, 0, 1, 1, 0.5},
		{45, 0, 0.5, 0.5, 1, 0.5},
		{135, 0.5, 0, 1, 0.5, 0.5},
	} {
		ps := gradientPaths(x, y, w, h, &Gradient{Degree: tc.degree, Stops: stops})
		if len(ps) != 1 {
			t.Fatalf("%v degrees: %d paths", tc.degree, len(ps))
		}
		s := ps[0].(*typeset.Path).Fill.Shading
		for _, c := range []struct{ x, y, want float64 }{{x, y, tc.tl}, {x + w, y, tc.tr}, {x, y + h, tc.bl}, {x + w, y + h, tc.br}, {x + w/2, y + h/2, tc.mid}} {
			if got := shadeAt(s, c.x, c.y); math.Abs(got-c.want) > 1e-9 {
				t.Errorf("%v degrees: (%v, %v) at %v, want %v", tc.degree, c.x, c.y, got, c.want)
			}
		}
	}
	// A path from the centre: four trapezoids, the top one's offset by
	// the distance from the centre down to the edge.
	ps := gradientPaths(x, y, w, h, &Gradient{Path: true, Left: 0.5, Right: 0.5, Top: 0.5, Bottom: 0.5, Stops: stops})
	if len(ps) != 4 {
		t.Fatalf("path from the centre: %d paths, want 4", len(ps))
	}
	top := ps[2].(*typeset.Path).Fill.Shading
	if got := shadeAt(top, x+w/2, y+h/4); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("top trapezoid at a quarter down %v, want 0.5", got)
	}
	// A path from the top-left corner: the corner is a point, so two
	// trapezoids, bottom and right.
	if ps := gradientPaths(x, y, w, h, &Gradient{Path: true, Stops: stops}); len(ps) != 2 {
		t.Errorf("path from the corner: %d paths, want 2", len(ps))
	}
	// An inner rectangle of its own takes the first colour.
	ps = gradientPaths(x, y, w, h, &Gradient{Path: true, Left: 0.25, Right: 0.75, Top: 0.25, Bottom: 0.75, Stops: stops})
	if len(ps) != 5 || ps[0].(*typeset.Path).Fill.Color != colorOf(white) {
		t.Errorf("inner rectangle: %d paths", len(ps))
	}
}
