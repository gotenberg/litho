package chart

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestOfficeTimeUnit_Bounds holds Office's automatic unit of a date axis
// to the slot widths that bound each step: the unit is a function of the
// points a day takes.
func TestOfficeTimeUnit_Bounds(t *testing.T) {
	for _, tc := range []struct {
		slot         float64
		days, months int
	}{
		{14.74, 1, 0}, {10.79, 2, 0},
		{7.70, 2, 0}, {7.05, 7, 0},
		{3.82, 7, 0}, {3.68, 0, 1},
		{0.62, 0, 1}, {0.41, 0, 2},
		{0.40, 0, 2}, {0.29, 0, 12},
	} {
		if d, m := officeTimeUnit(tc.slot); d != tc.days || m != tc.months {
			t.Errorf("officeTimeUnit(%v) = %d days, %d months, want %d, %d", tc.slot, d, m, tc.days, tc.months)
		}
	}
}

// timeChart returns a line chart under OfficeRules over the serial days
// dates on a date axis counted in days, its line blue.
func timeChart(dates ...float64) *Chart {
	nan := math.NaN()
	vals := make([]float64, len(dates))
	for i := range vals {
		vals[i] = float64(i + 1)
	}
	return &Chart{
		Rules: OfficeRules,
		Axes: []*Axis{
			{Kind: DateAxis, BaseTimeUnit: TimeDays, BetweenCategories: true, Labels: LabelNextTo, NoBreak: true, Min: nan, Max: nan, MajorUnit: nan, MinorUnit: nan, Style: TextStyle{Size: 10, Color: typeset.Black}},
			{Kind: ValueAxis, Position: AxisLeft, Labels: LabelNextTo, Min: nan, Max: nan, MajorUnit: nan, MinorUnit: nan},
		},
		Groups: []*Group{{Kind: Line, XAxis: 0, YAxis: 1, Series: []*Series{{
			Name:       "S",
			Values:     Data{Values: nums(vals...), Cached: true},
			Categories: Data{Values: nums(dates...), Format: "m/d/yyyy", Cached: true},
			Border:     &Stroke{Color: blue, Width: 1},
		}}}},
	}
}

// TestRender_OfficeTimeAxis holds a date axis under OfficeRules to a time
// axis: one slot a day from the first date to the last, each point at
// its date's slot centre, a label every major unit from the first date,
// the unit officeTimeUnit's for the slot.
func TestRender_OfficeTimeAxis(t *testing.T) {
	for _, tc := range []struct {
		width float64
		want  []string
	}{
		{600, []string{"1/1/2024", "1/2/2024", "1/3/2024"}},
		{290, []string{"1/1/2024", "1/3/2024", "1/5/2024"}},
		{200, []string{"1/1/2024", "1/8/2024", "1/15/2024"}},
	} {
		// 1/1/2024, 1/3/2024 and 1/21/2024: 21 slots.
		c := timeChart(45292, 45294, 45312)
		frags := Render(context.Background(), ahem(t), c, typeset.Rect{Width: tc.width, Height: 200})
		var line *typeset.Path
		walk(frags, 0, func(fr typeset.Fragment, _ int) {
			if p, ok := fr.(*typeset.Path); ok && p.Stroke != nil && near(p.Stroke.Color, blue) && line == nil {
				line = p
			}
		})
		if line == nil || len(line.Ops) < 3 {
			t.Fatalf("width %v: no series line", tc.width)
		}
		x0, x1, x2 := line.Ops[0].P[0].X, line.Ops[1].P[0].X, line.Ops[2].P[0].X
		slot := (x2 - x0) / 20
		if math.Abs((x1-x0)-2*slot) > 1e-6 {
			t.Errorf("width %v: second point %v pt after the first, want two slots of %v", tc.width, x1-x0, slot)
		}
		var labels []string
		for _, s := range texts(frags) {
			if strings.Count(s, "/") == 2 {
				labels = append(labels, s)
			}
		}
		if len(labels) < 3 || strings.Join(labels[:3], " ") != strings.Join(tc.want, " ") {
			t.Errorf("width %v (slots of %.2f pt): labels %q, want them to start %q", tc.width, slot, labels, tc.want)
		}
	}
}

// TestRender_OfficeTimeReserve holds a date axis whose labels Office turns
// by 45 degrees to the room it keeps right of its last slot: a turned
// line's width before officeRightRoom and officePad.
func TestRender_OfficeTimeReserve(t *testing.T) {
	// 1/1/2024 to 4/30/2024 on a chart 600 pt wide: one-day slots of
	// about 4.5 pt, half of which is less than the room kept, and 10 pt
	// Ahem labels 80 pt wide a week apart that turn by 45 degrees.
	c := timeChart(45292, 45294, 45412)
	const width = 600.0
	frags := Render(context.Background(), ahem(t), c, typeset.Rect{Width: width, Height: 300})
	var line *typeset.Path
	walk(frags, 0, func(fr typeset.Fragment, _ int) {
		if p, ok := fr.(*typeset.Path); ok && p.Stroke != nil && near(p.Stroke.Color, blue) && line == nil {
			line = p
		}
	})
	if line == nil || len(line.Ops) < 3 {
		t.Fatal("no series line")
	}
	turned := false
	walk(frags, 0, func(fr typeset.Fragment, _ int) {
		if g, ok := fr.(*typeset.Group); ok && g.Transform.B != 0 {
			turned = true
		}
	})
	if !turned {
		t.Fatal("the labels are not turned")
	}
	last := line.Ops[2].P[0].X
	if want := width - officePad - officeRightRoom - 10*math.Sin(math.Pi/4); math.Abs(last-want) > 0.01 {
		t.Errorf("last point at %v, want %v", last, want)
	}
}
