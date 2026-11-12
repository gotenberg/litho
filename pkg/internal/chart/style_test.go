package chart

import (
	"context"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

func TestPoint_FirstOverrideOfAnIndexWins(t *testing.T) {
	first, second := &Paint{Color: red}, &Paint{Color: compose.Black}
	s := &Series{Points: []Point{{Index: 2, Fill: first}, {Index: 2, Fill: second}}}
	rd := &renderer{c: &Chart{}}
	if p := rd.point(s, 2); p != &s.Points[0] {
		t.Errorf("point 2 is %+v, want the first override", p)
	}
	if p := rd.point(s, 1); p != nil {
		t.Errorf("point 1 is %+v, want none", p)
	}
	if p := rd.point(s, -1); p != nil {
		t.Errorf("point -1 is %+v, want none", p)
	}
}

func TestRender_EveryPointOverriddenStaysLinear(t *testing.T) {
	// Each plotted point looks its override up several times: a scan of
	// the overrides per lookup takes about 1.5e9 comparisons here, seconds
	// where the indexed lookups take milliseconds.
	vals := make([]float64, MaxPoints)
	pts := make([]Point, MaxPoints)
	for i := range vals {
		vals[i] = float64(i % 7)
		pts[i] = Point{Index: i, HasMarker: true, Marker: Marker{Symbol: MarkerSquare, Size: 3, Fill: &Paint{Color: red}}}
	}
	c := &Chart{
		Axes: plainAxes(),
		Groups: []*Group{{Kind: Line, XAxis: 0, YAxis: 1, Series: []*Series{
			{Name: "S", Values: Data{Values: nums(vals...), Cached: true}, Points: pts},
		}}},
	}
	fonts := ahem(t)
	done := make(chan []compose.Fragment, 1)
	go func() { done <- Render(context.Background(), fonts, c, chartRect) }()
	select {
	case frags := <-done:
		if n := len(paths(frags, red)); n != MaxPoints {
			t.Errorf("%d markers take their point's fill, want %d", n, MaxPoints)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Render does not return within 3 seconds")
	}
}
