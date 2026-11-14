package sheet

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// sparkValues returns the numbers v as chart values, NaN as an empty one.
func sparkValues(v ...float64) []chart.Value {
	out := make([]chart.Value, len(v))
	for i, x := range v {
		if !math.IsNaN(x) {
			out[i] = chart.Value{Kind: chart.Number, Number: x}
		}
	}
	return out
}

// sparkPaths returns the paths a group draws for sparkline 0 in box.
func sparkPaths(t *testing.T, g *SparkGroup, box typeset.Rect) []*typeset.Path {
	t.Helper()
	var out []*typeset.Path
	for _, f := range g.Picture(0).Vector(t.Context(), box) {
		if p, ok := f.(*typeset.Path); ok {
			out = append(out, p)
		}
	}
	return out
}

// TestSparkGroup_Columns locks a column sparkline (MS-XLSX, 2.6.90): one
// column a value from 0, in slots across the cell less its insets, a
// negative one in the negative colour, the first in its own, the axis
// where 0 is.
func TestSparkGroup_Columns(t *testing.T) {
	t.Parallel()
	red := typeset.Color{R: 1, A: 1}
	green := typeset.Color{G: 1, A: 1}
	g := &SparkGroup{Kind: SparkColumn, ShowNegative: true, Negative: red, ShowFirst: true, First: green, ShowAxis: true,
		Count: 1, Values: func(int) []chart.Value { return sparkValues(2, -1, math.NaN(), 1) }}
	box := typeset.Rect{X: 0, Y: 0, Width: 4*10 + 2*sparkInsetX, Height: 30 + 2*sparkInsetY}
	ps := sparkPaths(t, g, box)
	if len(ps) != 4 {
		t.Fatalf("%d paths, want the axis and three columns", len(ps))
	}
	if ps[0].Stroke == nil || ps[0].Ops[0].P[0].Y != sparkInsetY+20 {
		t.Errorf("axis at %v, want y %g", ps[0].Ops[0].P[0], sparkInsetY+20)
	}
	first, neg := ps[1], ps[2]
	if first.Fill.Color != green || neg.Fill.Color != red {
		t.Errorf("colours %v %v", first.Fill.Color, neg.Fill.Color)
	}
	// The first column spans slot 0's middle 0.7, from 0 (y 22.5) to 2
	// (y 2.5).
	if p := first.Ops[0].P[0]; math.Abs(p.X-(sparkInsetX+1.5)) > 1e-9 || math.Abs(p.Y-sparkInsetY) > 1e-9 {
		t.Errorf("first column at %v", p)
	}
}

// TestSparkGroup_LineGapsAndGroupAxis locks a line broken at an empty
// cell shown as a gap, joined across it when spanned, and the group's
// value axis shared by its sparklines.
func TestSparkGroup_LineGapsAndGroupAxis(t *testing.T) {
	t.Parallel()
	vals := [][]chart.Value{sparkValues(0, 1, math.NaN(), 2, 3), sparkValues(0, 10)}
	box := typeset.Rect{Width: 100, Height: 20}
	moves := func(g *SparkGroup) int {
		n := 0
		for _, op := range sparkPaths(t, g, box)[0].Ops {
			if op.Kind == typeset.MoveTo {
				n++
			}
		}
		return n
	}
	g := &SparkGroup{Count: 2, Values: func(i int) []chart.Value { return vals[i] }}
	if n := moves(g); n != 2 {
		t.Errorf("a gap draws %d pieces, want 2", n)
	}
	g = &SparkGroup{Count: 2, Empty: SparkSpan, Values: func(i int) []chart.Value { return vals[i] }}
	if n := moves(g); n != 1 {
		t.Errorf("a span draws %d pieces, want 1", n)
	}
	g = &SparkGroup{Count: 2, MaxAxis: SparkGroupAxis, Values: func(i int) []chart.Value { return vals[i] }}
	// The last point, 3 of a group maximum of 10, sits 0.3 of the way up.
	ops := sparkPaths(t, g, box)[0].Ops
	want := box.Height - sparkInsetY - 0.3*(box.Height-2*sparkInsetY)
	if y := ops[len(ops)-1].P[0].Y; math.Abs(y-want) > 1e-9 {
		t.Errorf("last point at y %g, want %g", y, want)
	}
}
