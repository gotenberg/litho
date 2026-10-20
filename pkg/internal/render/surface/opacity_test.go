package surface

import (
	"math"
	"testing"
)

func TestOpacityZeroValueIsOpaque(t *testing.T) {
	var o Opacity
	if o.Alpha() != 1 || o.Transparent() || o != Opaque {
		t.Errorf("zero Opacity = %v, transparent %v; want opaque", o.Alpha(), o.Transparent())
	}
}

func TestNewOpacity(t *testing.T) {
	tests := []struct {
		in   float64
		want float32
	}{
		{1, 1}, {2, 1}, {0.25, 0.25}, {0, 0}, {-1, 0}, {math.NaN(), 0}, {math.Inf(1), 1},
	}
	for _, tt := range tests {
		got := NewOpacity(tt.in)
		if d := got.Alpha() - tt.want; d < -1e-6 || d > 1e-6 {
			t.Errorf("NewOpacity(%v).Alpha() = %v, want %v", tt.in, got.Alpha(), tt.want)
		}
		if got.Transparent() != (tt.want == 0) {
			t.Errorf("NewOpacity(%v).Transparent() = %v", tt.in, got.Transparent())
		}
	}
}
