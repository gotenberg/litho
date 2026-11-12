package chart

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

func TestErrorAmounts_Kinds(t *testing.T) {
	vals := Data{Values: nums(2, 4, 6), Cached: true}
	tests := []struct {
		name     string
		e        ErrorBars
		j        int
		up, down float64
	}{
		{"fixed", ErrorBars{Kind: ErrorFixed, Plus: 1, Minus: 0.5}, 0, 1, 0.5},
		{"percent", ErrorBars{Kind: ErrorPercent, Plus: 10}, 1, 0.4, 0.4},
		{"margin", ErrorBars{Kind: ErrorMargin, Plus: 50}, 0, 3, 3},
		{"custom", ErrorBars{Kind: ErrorCustom, PlusData: Data{Values: nums(1, 2, 3), Cached: true}, MinusData: Data{Values: nums(9), Cached: true}}, 1, 2, math.NaN()},
		{"std dev", ErrorBars{Kind: ErrorStdDev, Plus: 2}, 2, 4, 4},
		{"std err", ErrorBars{Kind: ErrorStdErr, Plus: 1}, 2, 2 / math.Sqrt(3), 2 / math.Sqrt(3)},
		{"variance", ErrorBars{Kind: ErrorVariance, Plus: 1}, 2, 4, 4},
	}
	rd := &renderer{c: &Chart{}}
	same := func(a, b float64) bool { return math.IsNaN(a) && math.IsNaN(b) || math.Abs(a-b) < 1e-9 }
	for _, tt := range tests {
		up, down := rd.errorAmounts(tt.e, vals, seriesErrorStats(tt.e, vals), tt.j)
		if !same(up, tt.up) || !same(down, tt.down) {
			t.Errorf("%s: %v, %v; want %v, %v", tt.name, up, down, tt.up, tt.down)
		}
	}
}

func TestRender_ErrorBarsWithCaps(t *testing.T) {
	green := compose.Color{G: 1, A: 1}
	c := barChart(1, 2)
	c.Groups[0].Series[0].ErrorBars = []ErrorBars{{Kind: ErrorFixed, Plus: 0.5, Minus: 0.5, Up: true, Line: &Stroke{Color: green, Width: 1}}}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	var lines []compose.Rect
	walk(frags, 0, func(fr compose.Fragment, _ int) {
		if p, ok := fr.(*compose.Path); ok && p.Stroke != nil && near(p.Stroke.Color, green) {
			lines = append(lines, bounds(p))
		}
	})
	// Two points, each a bar above it and its cap.
	if len(lines) != 4 {
		t.Fatalf("got %d green lines, want 4", len(lines))
	}
	if lines[0].Width != 0 || lines[0].Height <= 0 || math.Abs(lines[1].Width-2*errorCap) > 1e-9 || lines[1].Height != 0 {
		t.Errorf("bar %+v and cap %+v, want a vertical bar and a cap %v wide", lines[0], lines[1], 2*errorCap)
	}
}

func TestRender_SeriesWideErrorBarsStayLinear(t *testing.T) {
	// A margin or a standard deviation reads every value of the series:
	// once per point, that is about 2e9 reads here, seconds where once
	// per series takes milliseconds.
	green := compose.Color{G: 1, A: 1}
	vals := make([]float64, MaxPoints)
	for i := range vals {
		vals[i] = float64(i % 7)
	}
	line := &Stroke{Color: green, Width: 1}
	c := barChart(vals...)
	c.Groups[0].Series[0].ErrorBars = []ErrorBars{
		{Kind: ErrorStdDev, Plus: 1, Up: true, NoCap: true, Line: line},
		{Kind: ErrorMargin, Plus: 10, Down: true, NoCap: true, Line: line},
	}
	fonts := ahem(t)
	done := make(chan []compose.Fragment, 1)
	go func() { done <- Render(context.Background(), fonts, c, chartRect) }()
	select {
	case frags := <-done:
		n := 0
		walk(frags, 0, func(fr compose.Fragment, _ int) {
			if p, ok := fr.(*compose.Path); ok && p.Stroke != nil && near(p.Stroke.Color, green) {
				n++
			}
		})
		if n != 2*MaxPoints {
			t.Errorf("got %d error bars, want %d", n, 2*MaxPoints)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Render does not return within 3 seconds")
	}
}
