package geom

import (
	"math"
	"testing"
)

func near(a, b Point) bool {
	return math.Abs(a.X-b.X) < 1e-9 && math.Abs(a.Y-b.Y) < 1e-9
}

// TestMatrixMulAppliesLeftFirst pins the order "m cm" follows: the new
// matrix maps user space into the space the old one maps to the page.
func TestMatrixMulAppliesLeftFirst(t *testing.T) {
	t.Parallel()
	scale := Matrix{2, 0, 0, 2, 0, 0}
	move := Matrix{1, 0, 0, 1, 10, 20}
	p := Point{X: 1, Y: 1}
	if got, want := scale.Mul(move).Apply(p), move.Apply(scale.Apply(p)); !near(got, want) {
		t.Errorf("scale.Mul(move) maps %v to %v, want %v", p, got, want)
	}
	if got := scale.Mul(move).Apply(p); !near(got, Point{X: 12, Y: 22}) {
		t.Errorf("got %v, want (12, 22)", got)
	}
}

func TestMatrixInvert(t *testing.T) {
	t.Parallel()
	m := Matrix{2, 1, -1, 3, 5, -7}
	inv, ok := m.Invert()
	if !ok {
		t.Fatal("Invert reported a regular matrix singular")
	}
	p := Point{X: 3.5, Y: -2}
	if got := inv.Apply(m.Apply(p)); !near(got, p) {
		t.Errorf("inverse round trip gave %v, want %v", got, p)
	}
	if _, ok := (Matrix{1, 2, 2, 4, 0, 0}).Invert(); ok {
		t.Error("Invert accepted a singular matrix")
	}
	if _, ok := (Matrix{math.Inf(1), 0, 0, 1, 0, 0}).Invert(); ok {
		t.Error("Invert accepted a matrix holding infinity")
	}
}

func TestPathTransformAndBounds(t *testing.T) {
	t.Parallel()
	var p Path
	p.MoveTo(Point{X: 0, Y: 0})
	p.CubicTo(Point{X: 0, Y: 10}, Point{X: 10, Y: 10}, Point{X: 10, Y: 0})
	p.Close()
	q := p.Transform(Matrix{1, 0, 0, 1, 5, 5})
	r, ok := q.Bounds()
	if !ok {
		t.Fatal("Bounds reported no point")
	}
	if r.Min != (Point{X: 5, Y: 5}) || r.Max != (Point{X: 15, Y: 15}) {
		t.Errorf("bounds = %+v, want (5,5)-(15,15)", r)
	}
	if p.Pts[0] != (Point{}) {
		t.Error("Transform modified the source path")
	}
	if _, ok := (Path{}).Bounds(); ok {
		t.Error("an empty path reported bounds")
	}
}
