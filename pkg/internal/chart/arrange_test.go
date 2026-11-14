package chart

import (
	"math"
	"slices"
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
