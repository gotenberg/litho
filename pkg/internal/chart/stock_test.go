package chart

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// stockChart returns an open-high-low-close line chart of two categories
// whose series draw no line, with high-low lines in grey and up-down bars
// green up and red down.
func stockChart() *Chart {
	none := &Stroke{None: true}
	ser := func(vs ...float64) *Series {
		return &Series{Values: Data{Values: nums(vs...), Cached: true}, Border: none, Marker: Marker{Symbol: MarkerNone}}
	}
	return &Chart{
		Axes: plainAxes(),
		Groups: []*Group{{
			Kind: Line, XAxis: 0, YAxis: 1, Stock: true,
			Series:  []*Series{ser(2, 8), ser(9, 9), ser(1, 3), ser(6, 4)},
			HighLow: &Stroke{Color: grey, Width: 0.75},
			UpDown: &UpDownBars{
				GapWidth: 150,
				Up:       Frame{Fill: &Paint{Color: green}},
				Down:     Frame{Fill: &Paint{Color: red}},
			},
		}},
	}
}

var (
	grey  = typeset.Color{R: 0.25, G: 0.25, B: 0.25, A: 1}
	green = typeset.Color{G: 0.5, A: 1}
)

func TestRender_StockHighLowAndUpDown(t *testing.T) {
	frags := Render(context.Background(), ahem(t), stockChart(), chartRect)
	var hl []*typeset.Path
	walk(frags, 0, func(fr typeset.Fragment, _ int) {
		if p, ok := fr.(*typeset.Path); ok && p.Stroke != nil && near(p.Stroke.Color, grey) {
			hl = append(hl, p)
		}
	})
	if len(hl) != 1 || len(hl[0].Ops) != 4 {
		t.Fatalf("got %d high-low paths, want one of two lines", len(hl))
	}
	up, down := paths(frags, green), paths(frags, red)
	if len(up) != 1 || len(down) != 1 {
		t.Fatalf("got %d up and %d down bars, want 1 each", len(up), len(down))
	}
	u, d := bounds(up[0]), bounds(down[0])
	// Each bar is a category's width over 1 + 150%, centred on the
	// category, from the first series' value to the last's.
	catW := u.Width * 2.5
	if math.Abs(d.Width-u.Width) > 1e-9 || math.Abs((d.X-u.X)-catW) > 1e-6 {
		t.Errorf("bars %+v and %+v: want equal widths a category of %v apart", u, d, catW)
	}
	l0, l1 := bounds(hl[0]), hl[0].Ops
	if math.Abs(l1[0].P[0].X-(u.X+u.Width/2)) > 1e-6 {
		t.Errorf("high-low line at x %v, want the up bar's middle %v", l1[0].P[0].X, u.X+u.Width/2)
	}
	// Up from 2 to 6 spans 4 units; the high-low line from 1 to 9 spans 8.
	if math.Abs(l0.Height-2*u.Height) > 1e-6 {
		t.Errorf("high-low height %v, up bar %v: want twice", l0.Height, u.Height)
	}
	// Down from 8 to 4: the same 4 units.
	if math.Abs(d.Height-u.Height) > 1e-6 {
		t.Errorf("down bar height %v, want %v", d.Height, u.Height)
	}
}

func TestRender_StockWithoutUpDownDrawsNoBars(t *testing.T) {
	c := stockChart()
	c.Groups[0].UpDown = nil
	c.Groups[0].HighLow = nil
	frags := Render(context.Background(), ahem(t), c, chartRect)
	if n := len(paths(frags, green)) + len(paths(frags, red)); n != 0 {
		t.Errorf("got %d bars, want none", n)
	}
}
