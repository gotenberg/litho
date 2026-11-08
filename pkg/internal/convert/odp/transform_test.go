package odp

import (
	"math"
	"testing"
)

func TestParseTransform(t *testing.T) {
	tests := []struct {
		in       string
		x, y     float64
		skew, ok bool
	}{
		// The point (1, 0) turned 90 degrees counterclockwise, then moved
		// by 1 cm right.
		{"rotate (1.5707963268) translate (1cm 0cm)", cm(1), -1, false, true},
		// In listed order: the translation is scaled too.
		{"translate (2pt) scale (3 2)", 9, 0, false, true},
		{"skewX (0.5)", 1, 0, true, true},
		{"matrix (1 0 0 1 1pt 2pt)", 2, 2, false, true},
		{"spin (1)", 0, 0, false, false},
		{"rotate (x)", 0, 0, false, false},
	}
	for _, tt := range tests {
		m, skew, ok := parseTransform(tt.in)
		if ok != tt.ok || skew != tt.skew {
			t.Errorf("parseTransform(%q): skew %v ok %v, want %v %v", tt.in, skew, ok, tt.skew, tt.ok)
			continue
		}
		if !ok {
			continue
		}
		if x, y := m.apply(1, 0); math.Abs(x-tt.x) > 1e-6 || math.Abs(y-tt.y) > 1e-6 {
			t.Errorf("parseTransform(%q) maps (1, 0) to (%v, %v), want (%v, %v)", tt.in, x, y, tt.x, tt.y)
		}
	}
}
