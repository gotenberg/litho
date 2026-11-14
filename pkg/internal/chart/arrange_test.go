package chart

import (
	"context"
	"math"
	"slices"
	"strings"
	"testing"
)

func TestArrange(t *testing.T) {
	cats := Data{Values: strs("a", "b", "a", "c"), Cached: true}
	vals := Data{Values: nums(1, 2, 3, 6), Cached: true}
	values := func(d Data) []float64 {
		var out []float64
		for _, v := range d.Values {
			out = append(out, v.Number)
		}
		return out
	}
	labels := func(d Data) []string {
		var out []string
		for _, v := range d.Values {
			out = append(out, v.Text)
		}
		return out
	}
	for _, tc := range []struct {
		a    Arrangement
		cats []string
		vals []float64
	}{
		{Arrangement{Aggregate: true}, []string{"a", "b", "c"}, []float64{4, 2, 6}},
		{Arrangement{Aggregate: true, Descending: true}, []string{"c", "a", "b"}, []float64{6, 4, 2}},
		{Arrangement{Descending: true}, []string{"c", "a", "b", "a"}, []float64{6, 3, 2, 1}},
		{Arrangement{Aggregate: true, Descending: true, Cumulative: true}, []string{"c", "a", "b"}, []float64{0.5, 10.0 / 12, 1}},
	} {
		c, v := arrange(cats, vals, tc.a)
		got := values(v)
		ok := len(got) == len(tc.vals)
		for i := range got {
			ok = ok && math.Abs(got[i]-tc.vals[i]) < 1e-12
		}
		if !ok || !slices.Equal(labels(c), tc.cats) {
			t.Errorf("%+v: %v %v, want %v %v", tc.a, labels(c), got, tc.cats, tc.vals)
		}
	}
}

func TestRender_ParetoLine(t *testing.T) {
	c := barChart(1, 3, 2)
	c.Groups[0].Series[0].Arrange = Arrangement{Descending: true}
	c.Groups[0].Series[0].Categories = Data{Values: strs("x", "y", "z"), Cached: true}
	c.Axes[0].Labels = LabelNextTo
	frags := Render(context.Background(), ahem(t), c, chartRect)
	bars := paths(frags, red)
	if len(bars) != 3 {
		t.Fatalf("got %d bars, want 3", len(bars))
	}
	h0, h1, h2 := bounds(bars[0]).Height, bounds(bars[1]).Height, bounds(bars[2]).Height
	if !(h0 > h1 && h1 > h2) {
		t.Errorf("bar heights %v %v %v, want them descending", h0, h1, h2)
	}
	if got := strings.Join(texts(frags), " "); got != "y z x" {
		t.Errorf("categories %q, want them ordered by value", got)
	}
	if c.Groups[0].Series[0].Values.Values[0].Number != 1 {
		t.Errorf("Render changed the chart's series")
	}
}
