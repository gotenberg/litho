package chart

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestStatsOf(t *testing.T) {
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
	s := statsOf([]float64{3, 5, 6, 7, 7, 8, 9, 20}, true)
	if !near(s.q1, 5.25) || !near(s.median, 7) || !near(s.q3, 8.75) || !near(s.mean, 8.125) || s.low != 3 || s.high != 9 || len(s.outliers) != 1 || s.outliers[0] != 20 {
		t.Errorf("exclusive stats %+v, want quartiles 5.25, 7, 8.75, mean 8.125, whiskers 3 to 9, outlier 20", s)
	}
	s = statsOf([]float64{5, 20, 21, 23, 24, 26, 27, 30}, true)
	if !near(s.q1, 20.25) || s.low != 20 || len(s.outliers) != 1 {
		t.Errorf("exclusive stats %+v, want the low quartile 20.25, whisker 20, outlier 5", s)
	}
	s = statsOf([]float64{1, 2, 3, 4}, false)
	if !near(s.q1, 1.75) || !near(s.median, 2.5) || !near(s.q3, 3.25) {
		t.Errorf("inclusive stats %+v, want quartiles 1.75, 2.5, 3.25", s)
	}
	if s := statsOf([]float64{4}, true); s.q1 != 4 || s.q3 != 4 || s.low != 4 || s.high != 4 {
		t.Errorf("stats of one value %+v, want all 4", s)
	}
}

func TestRender_BoxWhisker(t *testing.T) {
	ink := typeset.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}
	c := &Chart{
		Axes: plainAxes(),
		Groups: []*Group{{Kind: BoxWhisker, GapWidth: 50, XAxis: 0, YAxis: 1, Series: []*Series{{
			Categories: Data{Values: strs("a", "b", "a", "a", "b", "a", "b"), Cached: true},
			Values:     Data{Values: nums(1, 10, 2, 3, 12, 4, 14), Cached: true},
			Fill:       &Paint{Color: red},
			Box:        &Box{Exclusive: true, Mean: true, Line: &Stroke{Color: ink, Width: 0.75}},
		}}}},
	}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	boxes := paths(frags, red)
	if len(boxes) != 2 {
		t.Fatalf("paints %d boxes, want one a category", len(boxes))
	}
	w := bounds(boxes[0]).Width
	if catW := chartRect.Width * 0.96 / 2; math.Abs(w-catW/1.5) > 0.5 {
		t.Errorf("box %v wide, want two thirds of a category, %v", w, catW/1.5)
	}
	var caps, crosses int
	walk(frags, 0, func(fr typeset.Fragment, _ int) {
		p, ok := fr.(*typeset.Path)
		if !ok || p.Stroke == nil || !near(p.Stroke.Color, ink) {
			return
		}
		b := bounds(p)
		switch {
		case b.Height == 0 && math.Abs(b.Width-boxCapShare*w) < 1e-6:
			caps++
		case b.Width > 0 && b.Height > 0:
			crosses++
		}
	})
	if caps != 4 || crosses != 4 {
		t.Errorf("draws %d caps and %d cross strokes, want 4 and 4", caps, crosses)
	}
	if c.Groups[0].Series[0].samples != nil {
		t.Errorf("Render changed the chart's series")
	}
}
