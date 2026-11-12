package effect

import (
	"math"
	"testing"
)

// TestDetMath_MatchesMath compares the machine-independent functions with
// the math package's over their ranges: within 4 units in the last place
// for exp and log, 1e-14 relative for pow, and 4e-15 for the sine and
// cosine, which the reference loses to its argument in radians, rounded.
func TestDetMath_MatchesMath(t *testing.T) {
	close := func(got, want float64) bool {
		if got == want {
			return true
		}
		return math.Abs(got-want) <= 4*math.Abs(math.Nextafter(want, math.Inf(1))-want)
	}
	for x := -700.0; x <= 700; x += 0.731 {
		if got, want := detExp(x), math.Exp(x); !close(got, want) {
			t.Fatalf("detExp(%v) = %v, want %v", x, got, want)
		}
	}
	for x := 1e-300; x < 1e300; x *= 1.37 {
		if got, want := detLog(x), math.Log(x); !close(got, want) {
			t.Fatalf("detLog(%v) = %v, want %v", x, got, want)
		}
	}
	for x := 0.001; x <= 1; x += 0.0173 {
		for _, y := range []float64{0.3, 1, 2.2, 7} {
			got, want := detPow(x, y), math.Pow(x, y)
			if math.Abs(got-want) > 1e-14*want {
				t.Fatalf("detPow(%v, %v) = %v, want %v", x, y, got, want)
			}
		}
	}
	for d := -720.0; d <= 720; d += 0.913 {
		s, c := detSincos(d)
		ws, wc := math.Sincos(d * math.Pi / 180)
		if math.Abs(s-ws) > 4e-15 || math.Abs(c-wc) > 4e-15 {
			t.Fatalf("detSincos(%v) = %v, %v, want %v, %v", d, s, c, ws, wc)
		}
	}
	for _, tc := range []struct{ got, want float64 }{
		{detExp(math.Inf(-1)), 0}, {detExp(1000), math.Inf(1)}, {detLog(0), math.Inf(-1)},
		{detPow(0, 2), 0}, {detPow(0, -1), math.Inf(1)}, {detPow(5, 0), 1},
	} {
		if tc.got != tc.want {
			t.Errorf("edge case = %v, want %v", tc.got, tc.want)
		}
	}
	if !math.IsNaN(detLog(-1)) || !math.IsNaN(detPow(-1, 2)) || !math.IsNaN(detExp(math.NaN())) {
		t.Error("NaN cases do not return NaN")
	}
}
