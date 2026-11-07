package chart

import (
	"math"
	"testing"
)

func TestAutoScale_LibreOfficeRanges(t *testing.T) {
	nan := math.NaN()
	tests := []struct {
		name            string
		lo, hi          float64
		zero, border    bool
		count           int
		ax              *Axis
		min, max, major float64
	}{
		{"negative and positive", -2, 3, true, true, 10, nil, -3, 4, 1},
		{"wide positive goes to zero", 1, 4, true, true, 10, nil, 0, 4.5, 0.5},
		{"narrow positive halves toward zero", 100, 110, true, true, 10, nil, 94, 112, 2},
		{"all negative mirrors", -4, -1, true, true, 10, nil, -4.5, 0, 0.5},
		{"empty range", 0, 0, true, true, 10, nil, 0, 1, 0.1},
		{"no zero rule", 0.5, 3.5, false, true, 10, nil, 0, 4, 0.5},
		{"fewer intervals", 0, 100, true, true, 4, nil, 0, 150, 50},
		{"fixed bounds", 1, 4, true, true, 10, &Axis{Min: 0, Max: 10, MajorUnit: nan, MinorUnit: nan}, 0, 10, 1},
		{"fixed unit", 1, 4, true, true, 10, &Axis{Min: nan, Max: nan, MajorUnit: 2, MinorUnit: nan}, 0, 6, 2},
		{"percent", 0, 1, true, false, 10, nil, 0, 1, 0.1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := autoScale(tt.ax, tt.lo, tt.hi, scaleOptions{zero: tt.zero, border: tt.border, maxCount: tt.count})
			if math.Abs(s.min-tt.min) > 1e-9 || math.Abs(s.max-tt.max) > 1e-9 || math.Abs(s.major-tt.major) > 1e-9 {
				t.Errorf("scale = [%v, %v] by %v, want [%v, %v] by %v", s.min, s.max, s.major, tt.min, tt.max, tt.major)
			}
		})
	}
}

func TestAutoScale_Logarithmic(t *testing.T) {
	nan := math.NaN()
	s := autoScale(&Axis{Log: 10, Min: nan, Max: nan, MajorUnit: nan, MinorUnit: nan}, 3, 4500, scaleOptions{zero: true, border: true, maxCount: 10})
	if s.min != 1 || s.max != 10000 {
		t.Fatalf("log scale = [%v, %v], want [1, 10000]", s.min, s.max)
	}
	if got := s.ticks(); len(got) != 5 || got[2] != 100 {
		t.Errorf("ticks = %v", got)
	}
	if f := s.frac(100); math.Abs(f-0.5) > 1e-9 {
		t.Errorf("frac(100) = %v, want 0.5", f)
	}
}

func TestScale_TicksAndReverse(t *testing.T) {
	s := scale{min: -1, max: 1, major: 0.5, reverse: true}
	got := s.ticks()
	want := []float64{-1, -0.5, 0, 0.5, 1}
	if len(got) != len(want) {
		t.Fatalf("ticks = %v", got)
	}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-12 {
			t.Fatalf("ticks = %v, want %v", got, want)
		}
	}
	if f := s.frac(1); f != 0 {
		t.Errorf("reversed frac(max) = %v, want 0", f)
	}
	if n := len(s.minorTicks()); n != 16 {
		t.Errorf("minor ticks = %d, want 16", n)
	}
}

func TestSplineSlopes_LinearDataStaysStraight(t *testing.T) {
	d := splineSlopes([]float64{1, 3, 5, 7})
	for i, v := range d {
		if math.Abs(v-2) > 1e-9 {
			t.Fatalf("slope %d = %v, want 2", i, v)
		}
	}
}
