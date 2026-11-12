package numeric

import (
	"math"
	"testing"
)

func TestTests(t *testing.T) {
	t.Parallel()

	nan, inf := math.NaN(), math.Inf(1)
	cases := []struct {
		v                                             float64
		finite, positive, positiveFinite, nonNegative bool
	}{
		{1, true, true, true, true},
		{0, true, false, false, true},
		{math.Copysign(0, -1), true, false, false, true},
		{-1, true, false, false, false},
		{math.SmallestNonzeroFloat64, true, true, true, true},
		{inf, false, true, false, true},
		{-inf, false, false, false, false},
		{nan, false, false, false, false},
	}
	for _, c := range cases {
		if got := Finite(c.v); got != c.finite {
			t.Errorf("Finite(%v) = %t", c.v, got)
		}
		if got := Positive(c.v); got != c.positive {
			t.Errorf("Positive(%v) = %t", c.v, got)
		}
		if got := PositiveFinite(c.v); got != c.positiveFinite {
			t.Errorf("PositiveFinite(%v) = %t", c.v, got)
		}
		if got := NonNegative(c.v); got != c.nonNegative {
			t.Errorf("NonNegative(%v) = %t", c.v, got)
		}
	}
}
