package writer

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// TestFormatRealKeepsThePointBeyondInt64 pins that a real too large for a
// 64-bit integer keeps its decimal point. Trimming it emitted an integer
// literal no reader could hold, and litho's own parser answered the enclosing
// dictionary with no keys at all, so a remediated file could lose its catalog.
//
// # Corpus evidence
//
//   - litho limits_2b.pdf: lost its catalog, and its /Pages with it, so
//     neither veraPDF nor pikepdf could open the remediated file.
func TestFormatRealKeepsThePointBeyondInt64(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value float64
		want  string
	}{
		{"whole small", 1, "1"},
		{"whole negative", -42, "-42"},
		{"fractional", 1.5, "1.5"},
	}
	for _, tc := range cases {
		if got := formatReal(tc.value); got != tc.want {
			t.Errorf("formatReal(%g) = %q, want %q", tc.value, got, tc.want)
		}
	}

	// Every whole real past the 64-bit range keeps a point, whatever its size,
	// and the digits still name the same value.
	for _, f := range []float64{math.MaxInt64, -math.MaxInt64, 1e30, 1e43, -1e43, 1e100} {
		s := formatReal(f)
		if !strings.ContainsRune(s, '.') {
			t.Errorf("formatReal(%g) = %q, which reads back as an integer", f, s)
			continue
		}
		back, err := strconv.ParseFloat(s, 64)
		if err != nil || back != f {
			t.Errorf("formatReal(%g) = %q, which reads back as %g (%v)", f, s, back, err)
		}
	}
}
