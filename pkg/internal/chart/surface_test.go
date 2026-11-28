package chart

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// surfaceChart returns a surface of two series over three categories, its
// value axis fixed from 0 to 10 by 5: two bands, filled red and blue, the
// first series low, the second high.
func surfaceChart(wireframe bool, view View3D) *Chart {
	axes := plainAxes()
	axes[0].BetweenCategories = true
	axes[1].Min, axes[1].Max, axes[1].MajorUnit = 0, 10, 5
	nan := math.NaN()
	axes = append(axes, &Axis{Kind: SeriesAxis, Labels: LabelNone, Min: nan, Max: nan, MajorUnit: nan, MinorUnit: nan})
	g := &Group{Kind: Surface, Rows: true, Wireframe: wireframe, XAxis: 0, YAxis: 1, ZAxis: 2,
		Bands: []Frame{{Fill: &Paint{Color: red}, Border: &Stroke{Color: red, Width: 1}}, {Fill: &Paint{Color: blue}, Border: &Stroke{Color: blue, Width: 1}}}}
	g.Series = []*Series{
		{Name: "A", Values: Data{Values: nums(1, 2, 1), Cached: true}},
		{Name: "B", Values: Data{Values: nums(9, 8, 9), Cached: true}},
	}
	return &Chart{Rules: OfficeRules, Axes: axes, Groups: []*Group{g}, View3D: &view}
}

// strokesOf returns the stroked paths of frags in colour col.
func strokesOf(frags []typeset.Fragment, col typeset.Color) []*typeset.Path {
	var out []*typeset.Path
	walk(frags, 0, func(fr typeset.Fragment, _ int) {
		if p, ok := fr.(*typeset.Path); ok && p.Stroke != nil && near(p.Stroke.Color, col) {
			out = append(out, p)
		}
	})
	return out
}

// TestRender_SurfaceFillsItsBands locks that a surface, seen from above as
// a contour or turned, fills the part of its grid in each band of the
// value axis with that band's fill: the cells between a low and a high
// series cross the band bound at 5 halfway, so each band takes about half
// the contour's area.
func TestRender_SurfaceFillsItsBands(t *testing.T) {
	for _, view := range []View3D{{RotX: 90, Depth: 100}, {RotX: 15, RotY: 20, Depth: 100, Perspective: 30}} {
		frags := Render(context.Background(), ahem(t), surfaceChart(false, view), chartRect)
		area := func(col typeset.Color) float64 {
			a := 0.0
			for _, p := range paths(frags, col) {
				b := bounds(p)
				a += b.Width * b.Height
			}
			return a
		}
		lo, hi := area(red), area(blue)
		if lo == 0 || hi == 0 {
			t.Fatalf("view %+v: band areas %.1f and %.1f, want both filled", view, lo, hi)
		}
		if view.RotX == 90 && math.Abs(lo-hi) > 0.1*(lo+hi) {
			t.Errorf("contour band areas %.1f and %.1f, want about equal", lo, hi)
		}
	}
}

// TestRender_SurfaceWireframe locks that a wireframe surface draws its
// grid's lines in its bands' colours, the line between two bands in the
// lower one's, and fills nothing.
func TestRender_SurfaceWireframe(t *testing.T) {
	frags := Render(context.Background(), ahem(t), surfaceChart(true, View3D{RotX: 90, Depth: 100}), chartRect)
	if len(paths(frags, red))+len(paths(frags, blue)) != 0 {
		t.Error("a wireframe fills its bands")
	}
	low, high := strokesOf(frags, red), strokesOf(frags, blue)
	if len(low) == 0 || len(high) == 0 {
		t.Fatalf("%d low and %d high lines, want both", len(low), len(high))
	}
	// The bound at 5 crosses each cell halfway between the series: its
	// lines run across the grid, the category direction, in red.
	across := 0
	for _, p := range low {
		if b := bounds(p); b.Height < 0.5 && b.Width > 1 {
			across++
		}
	}
	if across == 0 {
		t.Error("no line between the bands in the lower band's colour")
	}
}

// TestLegend_SurfaceListsItsBands locks that a surface's legend lists its
// bands, each named by its bounds and keyed by its fill.
func TestLegend_SurfaceListsItsBands(t *testing.T) {
	c := surfaceChart(false, View3D{RotX: 15, RotY: 20, Depth: 100, Perspective: 30})
	c.Legend = &Legend{Position: LegendBottom, Style: TextStyle{Size: 10}}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	got := texts(frags)
	for _, want := range []string{"0-5", "5-10"} {
		if !slices.Contains(got, want) {
			t.Errorf("texts %q lack the band %q", got, want)
		}
	}
}

// TestClipLevel keeps the part of a triangle on one side of a level, the
// crossing points interpolated along its edges.
func TestClipLevel(t *testing.T) {
	tri := []gridPoint{{0, 0, 0}, {2, 0, 10}, {0, 2, 0}}
	above := clipLevel(tri, 5, true)
	below := clipLevel(tri, 5, false)
	if len(above) != 3 || len(below) != 4 {
		t.Fatalf("parts of %d and %d corners, want 3 and 4", len(above), len(below))
	}
	for _, q := range above {
		if q.v < 5-1e-9 {
			t.Errorf("point %+v below the level", q)
		}
	}
	if !slices.Contains(above, gridPoint{1, 0, 5}) {
		t.Errorf("parts %+v miss the crossing at u 1", above)
	}
}
