package chart

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

func TestTrendCurve_Fits(t *testing.T) {
	xs := []float64{1, 2, 3, 4, 5}
	tests := []struct {
		name string
		t    Trendline
		ys   []float64
		f    func(float64) float64
	}{
		{"linear", Trendline{Kind: TrendLinear}, []float64{3, 5, 7, 9, 11}, func(x float64) float64 { return 2*x + 1 }},
		{"linear through intercept", Trendline{Kind: TrendLinear, HasIntercept: true, Intercept: 0}, []float64{2, 4, 6, 8, 10}, func(x float64) float64 { return 2 * x }},
		{"polynomial", Trendline{Kind: TrendPolynomial, Order: 2}, []float64{2, 5, 10, 17, 26}, func(x float64) float64 { return x*x + 1 }},
		{"exponential", Trendline{Kind: TrendExponential}, []float64{2 * math.Exp(0.5), 2 * math.Exp(1), 2 * math.Exp(1.5), 2 * math.Exp(2), 2 * math.Exp(2.5)}, func(x float64) float64 { return 2 * math.Exp(0.5*x) }},
		{"logarithmic", Trendline{Kind: TrendLogarithmic}, []float64{1, 1 + 3*math.Log(2), 1 + 3*math.Log(3), 1 + 3*math.Log(4), 1 + 3*math.Log(5)}, func(x float64) float64 { return 3*math.Log(x) + 1 }},
		{"power", Trendline{Kind: TrendPower}, []float64{3, 3 * math.Pow(2, 1.5), 3 * math.Pow(3, 1.5), 3 * math.Pow(4, 1.5), 3 * math.Pow(5, 1.5)}, func(x float64) float64 { return 3 * math.Pow(x, 1.5) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pts := trendCurve(tt.t, xs, tt.ys)
			if len(pts) < 2 || pts[0][0] != 1 || pts[len(pts)-1][0] != 5 {
				t.Fatalf("curve spans %v", pts)
			}
			for _, p := range pts {
				if math.Abs(p[1]-tt.f(p[0])) > 1e-6*math.Max(1, math.Abs(p[1])) {
					t.Fatalf("curve at %v = %v, want %v", p[0], p[1], tt.f(p[0]))
				}
			}
		})
	}
}

func TestTrendCurve_ExtrapolatesAndRefuses(t *testing.T) {
	pts := trendCurve(Trendline{Kind: TrendLinear, Forward: 2, Backward: 1}, []float64{1, 2, 3}, []float64{1, 2, 3})
	if len(pts) != 2 || pts[0][0] != 0 || pts[1][0] != 5 {
		t.Errorf("extrapolated curve = %v, want x from 0 to 5", pts)
	}
	if pts := trendCurve(Trendline{Kind: TrendLinear}, []float64{1}, []float64{1}); pts != nil {
		t.Errorf("one point fits %v", pts)
	}
	if pts := trendCurve(Trendline{Kind: TrendLinear}, []float64{2, 2, 2}, []float64{1, 2, 3}); pts != nil {
		t.Errorf("a vertical cloud fits %v", pts)
	}
	if pts := trendCurve(Trendline{Kind: TrendPower}, []float64{-1, -2}, []float64{1, 2}); pts != nil {
		t.Errorf("a power fit of negative x fits %v", pts)
	}
}

func TestTrendCurve_MovingAverage(t *testing.T) {
	xs := []float64{1, 2, 4, 8, 16}
	ys := []float64{2, 4, 6, 8, 10}
	tests := []struct {
		m    MovingType
		want [][2]float64
	}{
		{MovingPrior, [][2]float64{{4, 4}, {8, 6}, {16, 8}}},
		{MovingAveragedAbscissa, [][2]float64{{7.0 / 3, 4}, {14.0 / 3, 6}, {28.0 / 3, 8}}},
		{MovingCentral, [][2]float64{{2, 4}, {4, 6}, {8, 8}}},
	}
	for _, tt := range tests {
		got := trendCurve(Trendline{Kind: TrendMovingAverage, Period: 3, Moving: tt.m}, xs, ys)
		if !slices.EqualFunc(got, tt.want, func(a, b [2]float64) bool { return math.Abs(a[0]-b[0]) < 1e-9 && math.Abs(a[1]-b[1]) < 1e-9 }) {
			t.Errorf("moving type %d: %v, want %v", tt.m, got, tt.want)
		}
	}
}

func TestRender_TrendlineAndLegendEntry(t *testing.T) {
	green := compose.Color{G: 1, A: 1}
	c := barChart(1, 2, 3)
	c.Groups[0].Series[0].Trendlines = []Trendline{{Kind: TrendLinear, Line: &Stroke{Color: green, Width: 1}}}
	c.Legend = &Legend{Position: LegendBottom, Style: TextStyle{Size: 10}}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	var lines []*compose.Path
	walk(frags, 0, func(fr compose.Fragment, _ int) {
		if p, ok := fr.(*compose.Path); ok && p.Stroke != nil && near(p.Stroke.Color, green) {
			lines = append(lines, p)
		}
	})
	if len(lines) != 2 {
		t.Fatalf("got %d green lines, want the trendline and its legend key", len(lines))
	}
	if !slices.Contains(texts(frags), "Linear (S)") {
		t.Errorf("texts %q lack the trendline's legend entry", texts(frags))
	}
	// The line, drawn after the legend, runs from the first category's
	// centre to the last's.
	b := bounds(lines[1])
	if math.Abs(b.Width-384*2.0/3) > 1e-6 {
		t.Errorf("trendline %v wide, want two categories", b.Width)
	}
}
