package chart

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestOfPieSecond_Splits(t *testing.T) {
	vals := Data{Values: nums(9, 8, 3, 2, 1), Cached: true}
	tests := []struct {
		name string
		o    OfPie
		want []bool
	}{
		{"last two", OfPie{Split: SplitPosition, SplitAt: 2}, []bool{false, false, false, true, true}},
		{"below 3", OfPie{Split: SplitValue, SplitAt: 3}, []bool{false, false, false, true, true}},
		{"below 10 percent", OfPie{Split: SplitPercent, SplitAt: 10}, []bool{false, false, false, true, true}},
		{"custom", OfPie{Split: SplitCustom, Custom: []int{0, 4}}, []bool{true, false, false, false, true}},
		{"none", OfPie{Split: SplitValue, SplitAt: 0}, nil},
		{"all", OfPie{Split: SplitPosition, SplitAt: 1e300}, nil},
	}
	rd := &renderer{c: &Chart{}}
	for _, tt := range tests {
		got := rd.ofPieSecond(&tt.o, vals, vals.Len())
		if len(got) != len(tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
				break
			}
		}
	}
}

// barOfPie returns a bar of pie of four points, the last two split off,
// each point its own colour.
func barOfPie() *Chart {
	cols := []typeset.Color{red, blue, {G: 1, A: 1}, {R: 1, G: 1, A: 1}}
	s := &Series{Values: Data{Values: nums(6, 3, 2, 1), Cached: true}, Fill: &Paint{Color: typeset.Color{R: 0.5, A: 1}}}
	for i, c := range cols {
		s.Points = append(s.Points, Point{Index: i, Fill: &Paint{Color: c}})
	}
	return &Chart{Groups: []*Group{{Kind: Pie, VaryColors: true, XAxis: -1, YAxis: -1, Series: []*Series{s},
		OfPie: &OfPie{Bar: true, Split: SplitPosition, SplitAt: 2, SecondSize: 75, Lines: &Stroke{Color: typeset.Black, Width: 0.5}}}}}
}

func TestRender_BarOfPie(t *testing.T) {
	frags := Render(context.Background(), ahem(t), barOfPie(), chartRect)
	top, bottom := paths(frags, typeset.Color{G: 1, A: 1}), paths(frags, typeset.Color{R: 1, G: 1, A: 1})
	other := paths(frags, typeset.Color{R: 0.5, A: 1})
	first := paths(frags, red)
	if len(top) != 1 || len(bottom) != 1 || len(other) != 1 || len(first) != 1 {
		t.Fatalf("got %d, %d bar segments, %d split sectors and %d first sectors, want one each", len(top), len(bottom), len(other), len(first))
	}
	a, b := bounds(top[0]), bounds(bottom[0])
	// The bar stacks the split points from the top, 2 over 1.
	if math.Abs(a.Height-2*b.Height) > 1e-6 || math.Abs(a.Y+a.Height-b.Y) > 1e-6 || a.Width != b.Width {
		t.Errorf("segments %+v and %+v: want 2 over 1, one width", a, b)
	}
	// The pie is d across; the bar 0.75 d high, half as wide, a quarter of
	// d right of the pie, centred on it.
	bar := typeset.Rect{X: a.X, Y: a.Y, Width: a.Width, Height: a.Height + b.Height}
	pie := bounds(first[0])
	o := bounds(other[0])
	cy := o.Y + o.Height/2
	d := 4 * bar.Width / 1.5
	if math.Abs(bar.Height-0.75*d) > 1e-6 || math.Abs(bar.Y+bar.Height/2-cy) > 1e-6 {
		t.Errorf("bar %+v around %v, want it centred on the pie", bar, cy)
	}
	// The split sector, 3 of 12, faces the bar: 90 degrees centred at three
	// o'clock, its chord as high as the arc's ends, its left edge the
	// pie's centre.
	if math.Abs(o.Height-math.Sqrt2*d/2) > 0.05 || math.Abs(bar.X-o.X-0.75*d) > 1e-6 {
		t.Errorf("split sector %+v, bar at %v, pie %v across", o, bar.X, d)
	}
	if pie.X > o.X {
		t.Errorf("the first point %+v starts right of the split sector %+v", pie, o)
	}
	var lines int
	walk(frags, 0, func(fr typeset.Fragment, _ int) {
		if p, ok := fr.(*typeset.Path); ok && p.Stroke != nil && near(p.Stroke.Color, typeset.Black) && len(p.Ops) == 4 {
			lines++
		}
	})
	if lines != 1 {
		t.Errorf("got %d series line paths, want 1", lines)
	}
}

func TestRender_PieOfPieTouchesTheSecondPie(t *testing.T) {
	c := barOfPie()
	c.Groups[0].OfPie.Bar = false
	frags := Render(context.Background(), ahem(t), c, chartRect)
	top, bottom := paths(frags, typeset.Color{G: 1, A: 1}), paths(frags, typeset.Color{R: 1, G: 1, A: 1})
	if len(top) != 1 || len(bottom) != 1 {
		t.Fatalf("got %d and %d second-pie sectors", len(top), len(bottom))
	}
	tp := tangentCheck(t, typeset.Point{X: 0, Y: -10}, 3)
	if math.Abs(tp.X*tp.X+tp.Y*tp.Y-9) > 1e-9 || math.Abs(tp.X*tp.X+(tp.Y+10)*tp.Y) > 1e-9 {
		t.Errorf("tangent point %+v not on the circle at a right angle", tp)
	}
}

// tangentCheck returns the upper tangent point from e to the circle of
// radius r around the origin, failing when there is none.
func tangentCheck(t *testing.T, e typeset.Point, r float64) typeset.Point {
	t.Helper()
	p, ok := tangent(e, 0, 0, r, true)
	if !ok {
		t.Fatalf("no tangent from %+v", e)
	}
	return p
}

func TestRender_OfPieSplittingNothingDrawsAPie(t *testing.T) {
	c := barOfPie()
	c.Groups[0].OfPie.SplitAt = 0
	frags := Render(context.Background(), ahem(t), c, chartRect)
	if n := len(paths(frags, typeset.Color{R: 0.5, A: 1})); n != 0 {
		t.Errorf("got %d split sectors, want none", n)
	}
	if n := len(paths(frags, typeset.Color{G: 1, A: 1})); n != 1 {
		t.Errorf("got %d sectors of the third point, want 1", n)
	}
}

func TestOfPieSecond_ManyCustomPointsStayLinear(t *testing.T) {
	vals := Data{Values: make([]Value, MaxPoints), Cached: true}
	o := &OfPie{Split: SplitCustom}
	for i := range MaxPoints {
		vals.Values[i] = Num(1)
		if i%2 == 0 {
			o.Custom = append(o.Custom, i)
		}
	}
	start := time.Now()
	second := (&renderer{c: &Chart{}}).ofPieSecond(o, vals, MaxPoints)
	if d := time.Since(start); d > time.Second {
		t.Errorf("splitting took %v", d)
	}
	if len(second) != MaxPoints || !second[0] || second[1] {
		t.Errorf("custom split of every other point: %v", second[:2])
	}
}
