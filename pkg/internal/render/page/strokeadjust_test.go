package page

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/stroke"
)

func TestStrokeAdjustment(t *testing.T) {
	t.Parallel()
	// A 1-unit line along user y 50 runs over rows 49.5 to 50.5, half of
	// rows 49 and 50 unadjusted. Adjusted, it paints one row solid.
	img, _ := draw(t, fixture{content: "0 G 1 w 10 50 m 90 50 l S"})
	if row49, row50 := img.Pix[(49*100+50)*4], img.Pix[(50*100+50)*4]; !((row49 == 0 && row50 == 255) || (row49 == 255 && row50 == 0)) {
		t.Errorf("rows 49 and 50 = %d and %d, want one black and one white", row49, row50)
	}
	// A line thinner than a pixel still paints one solid row.
	img, _ = draw(t, fixture{content: "0 G 0.2 w 10 30.5 m 90 30.5 l S"})
	expect(t, img, 0, map[[2]int]rgb{{50, 69}: black, {50, 68}: paper, {50, 70}: paper})
	// A vertical line snaps its column the same way, under a scaled CTM.
	img, _ = draw(t, fixture{content: "2 0 0 2 0 0 cm 0 G 0.1 w 20.2 10 m 20.2 40 l S"})
	expect(t, img, 0, map[[2]int]rgb{{40, 50}: black, {39, 50}: paper, {41, 50}: paper})
}

func TestRectangleFillAdjustment(t *testing.T) {
	t.Parallel()
	// A rectangle a fifth of a pixel tall paints one solid row; its edges
	// round to pixel boundaries.
	img, _ := draw(t, fixture{content: "0 g 10.4 50.1 79.4 0.2 re f"})
	expect(t, img, 0, map[[2]int]rgb{{50, 50}: black, {10, 50}: black, {89, 50}: black, {9, 50}: paper, {90, 50}: paper, {50, 49}: paper, {50, 51}: paper})
}

func TestAdjustOutlineLeavesUnadjustable(t *testing.T) {
	var curve geom.Path
	curve.MoveTo(geom.Point{X: 0, Y: 0.5})
	curve.CubicTo(geom.Point{X: 1, Y: 1}, geom.Point{X: 2, Y: 1}, geom.Point{X: 3, Y: 0.5})
	var line geom.Path
	line.MoveTo(geom.Point{X: 0, Y: 0.5})
	line.LineTo(geom.Point{X: 3, Y: 0.5})
	var slanted geom.Path
	slanted.MoveTo(geom.Point{X: 0, Y: 0.5})
	slanted.LineTo(geom.Point{X: 3, Y: 0.5})
	slanted.LineTo(geom.Point{X: 5, Y: 2})
	style := stroke.Style{Width: 1}
	for name, tt := range map[string]struct {
		p geom.Path
		m geom.Matrix
	}{
		"curve":           {curve, geom.Identity},
		"slanted segment": {slanted, geom.Identity},
		"skewed matrix":   {line, geom.Matrix{1, 0.5, 0, 1, 0, 0}},
		"rotated matrix":  {line, geom.Matrix{0.6, 0.8, -0.8, 0.6, 0, 0}},
	} {
		outline, err := stroke.Outline(tt.p.Transform(mustInvert(t, tt.m)), style, tt.m, 0)
		if err != nil {
			t.Fatal(err)
		}
		got := adjustOutline(outline, tt.p, style, tt.m)
		for i := range got.Pts {
			if got.Pts[i] != outline.Pts[i] {
				t.Errorf("%s: point %d moved from %v to %v", name, i, outline.Pts[i], got.Pts[i])
				break
			}
		}
	}
	// A horizontal line on a half pixel moves to whole rows.
	outline, err := stroke.Outline(line, style, geom.Identity, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pt := range adjustOutline(outline, line, style, geom.Identity).Pts {
		if pt.Y != 0 && pt.Y != 1 {
			t.Errorf("adjusted point %v is off the rows 0 to 1", pt)
		}
	}
	if _, ok := adjustedRect(line); ok {
		t.Error("an open line was adjusted as a rectangle")
	}
}

func mustInvert(t *testing.T, m geom.Matrix) geom.Matrix {
	t.Helper()
	inv, ok := m.Invert()
	if !ok {
		t.Fatalf("matrix %v is singular", m)
	}
	return inv
}

// BenchmarkAdjustOutline adjusts the stroke of a table grid of 40 rules.
func BenchmarkAdjustOutline(b *testing.B) {
	var grid geom.Path
	for i := range 20 {
		v := 10.5 + float64(i)*20.3
		grid.MoveTo(geom.Point{X: 10, Y: v})
		grid.LineTo(geom.Point{X: 400, Y: v})
		grid.MoveTo(geom.Point{X: v, Y: 10})
		grid.LineTo(geom.Point{X: v, Y: 400})
	}
	style := stroke.Style{Width: 0.5}
	outline, err := stroke.Outline(grid, style, geom.Identity, 0)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		adjustOutline(outline, grid, style, geom.Identity)
	}
}
