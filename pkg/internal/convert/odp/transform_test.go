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
		{"skewY (0.5)", 1, -math.Tan(0.5), true, true},
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

func TestMatrix_PlaceSkew(t *testing.T) {
	t.Parallel()
	deg := 0.5 * 180 / math.Pi
	tests := []struct {
		name           string
		transform      string
		rotation, skew float64
		centre         [2]float64
	}{
		{"none", "translate (0pt 0pt)", 0, 0, [2]float64{20, 10}},
		// The 40 by 20 box sheared about its corner: its centre moves left
		// by tan 0.5 times half its height.
		{"skew", "skewX (0.5)", 0, deg, [2]float64{20 - 10*math.Tan(0.5), 10}},
		{"skew then rotate", "skewX (0.5) rotate (1.5707963268)", 270, deg, [2]float64{10, -20 + 10*math.Tan(0.5)}},
		{"negative skew", "skewX (-0.5)", 0, -deg, [2]float64{20 + 10*math.Tan(0.5), 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _, ok := parseTransform(tt.transform)
			if !ok {
				t.Fatal("does not parse")
			}
			p := m.place(40, 20)
			cx, cy := p.box.X+p.box.Width/2, p.box.Y+p.box.Height/2
			if math.Abs(p.rotation-tt.rotation) > 1e-6 || math.Abs(p.skew-tt.skew) > 1e-6 || math.Abs(cx-tt.centre[0]) > 1e-6 || math.Abs(cy-tt.centre[1]) > 1e-6 ||
				math.Abs(p.box.Width-40) > 1e-6 || math.Abs(p.box.Height-20) > 1e-6 || p.flipV {
				t.Errorf("placement %+v rotation %v skew %v centre (%v, %v), want %v %v %v", p.box, p.rotation, p.skew, cx, cy, tt.rotation, tt.skew, tt.centre)
			}
		})
	}
}
