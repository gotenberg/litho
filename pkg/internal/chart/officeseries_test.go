package chart

import (
	"context"
	"math"
	"testing"
)

// TestDims_OfficeDeletedAxisSharesTheFirstScale holds a group whose value
// axis is deleted to the first group's value axis under OfficeRules, and
// to a scale of its own under LibreOffice's.
func TestDims_OfficeDeletedAxisSharesTheFirstScale(t *testing.T) {
	for _, tc := range []struct {
		rules Rules
		want  int
	}{{OfficeRules, 1}, {LibreOfficeRules, 2}} {
		c := barChart(1, 2)
		c.Rules = tc.rules
		sec := &Axis{Kind: ValueAxis, Position: AxisLeft, Deleted: true, Min: math.NaN(), Max: math.NaN(), MajorUnit: math.NaN(), MinorUnit: math.NaN()}
		c.Axes = append(c.Axes, sec)
		c.Groups = append(c.Groups, &Group{Kind: Line, XAxis: 0, YAxis: 2, Series: []*Series{{Values: Data{Values: nums(4, 3), Cached: true}}}})
		rd := &renderer{ctx: context.Background(), fonts: ahem(t), c: c, page: chartRect}
		_, ys, yOf := rd.dims(c.Groups[0])
		if len(ys) != tc.want {
			t.Fatalf("rules %v: %d value dimensions, want %d", tc.rules, len(ys), tc.want)
		}
		if tc.want == 1 && (yOf[c.Groups[1]] != ys[0] || ys[0].hi != 4) {
			t.Errorf("rules %v: the line is not drawn on the first axis, its range %v to %v", tc.rules, ys[0].lo, ys[0].hi)
		}
	}
}
