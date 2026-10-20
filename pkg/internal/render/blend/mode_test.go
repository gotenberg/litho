package blend

import (
	"math"
	"testing"
)

const eps = 1e-5

func near32(a, b float32) bool { return math.Abs(float64(a-b)) <= eps }

func nearRGB(a, b [3]float32) bool {
	return near32(a[0], b[0]) && near32(a[1], b[1]) && near32(a[2], b[2])
}

// TestSeparableModes checks each mode of ISO 32000-1, Table 136 at sample
// points whose expected values were worked by hand from the table's
// formulas, both branches of every piecewise mode included.
func TestSeparableModes(t *testing.T) {
	cases := []struct {
		mode     Mode
		cb, cs   float32
		want     float32
		branches string
	}{
		{Normal, 0.3, 0.8, 0.8, ""},
		{Multiply, 0.5, 0.4, 0.2, ""},
		{Screen, 0.5, 0.4, 0.7, ""},
		{Overlay, 0.3, 0.8, 0.48, "cb <= 0.5: multiply"},
		{Overlay, 0.75, 0.5, 0.75, "cb > 0.5: screen"},
		{Darken, 0.3, 0.8, 0.3, ""},
		{Lighten, 0.3, 0.8, 0.8, ""},
		{ColorDodge, 0.3, 0.5, 0.6, "cs < 1"},
		{ColorDodge, 0.6, 0.5, 1, "cs < 1, clamped"},
		{ColorDodge, 0, 1, 1, "cs = 1"},
		{ColorBurn, 0.8, 0.5, 0.6, "cs > 0"},
		{ColorBurn, 0.2, 0.5, 0, "cs > 0, clamped"},
		{ColorBurn, 1, 0, 0, "cs = 0"},
		{HardLight, 0.5, 0.25, 0.25, "cs <= 0.5: multiply"},
		{HardLight, 0.5, 0.75, 0.75, "cs > 0.5: screen"},
		{SoftLight, 0.5, 0.25, 0.375, "cs <= 0.5"},
		{SoftLight, 0.16, 0.75, 0.279168, "cs > 0.5, D polynomial"},
		{SoftLight, 0.64, 1, 0.8, "cs > 0.5, D square root"},
		{Difference, 0.3, 0.8, 0.5, ""},
		{Difference, 0.8, 0.3, 0.5, ""},
		{Exclusion, 0.2, 0.6, 0.56, ""},
	}
	for _, tc := range cases {
		got := tc.mode.Blend([3]float32{tc.cb, tc.cb, tc.cb}, [3]float32{tc.cs, tc.cs, tc.cs})
		for k := range 3 {
			if !near32(got[k], tc.want) {
				t.Errorf("%v(%g, %g) %s = %g, want %g", tc.mode, tc.cb, tc.cs, tc.branches, got[k], tc.want)
			}
		}
		if !tc.mode.Separable() {
			t.Errorf("%v is separable", tc.mode)
		}
	}
}

// TestNonSeparableModes checks Table 137 against values worked by hand
// through Lum, SetLum, ClipColor, Sat and SetSat, each exercising a clip.
func TestNonSeparableModes(t *testing.T) {
	cases := []struct {
		mode   Mode
		cb, cs [3]float32
		want   [3]float32
	}{
		// SetLum((1, 0, 0), 0.5) = (1.2, 0.2, 0.2), clipped above.
		{Luminosity, [3]float32{1, 0, 0}, [3]float32{0.5, 0.5, 0.5}, [3]float32{1, 0.2857143, 0.2857143}},
		// A grey backdrop has no saturation to give the source's hue.
		{Hue, [3]float32{0.5, 0.5, 0.5}, [3]float32{1, 0, 0}, [3]float32{0.5, 0.5, 0.5}},
		// SetLum((0, 0, 1), 0.5) = (0.39, 0.39, 1.39), clipped above.
		{Color, [3]float32{0.5, 0.5, 0.5}, [3]float32{0, 0, 1}, [3]float32{0.4382022, 0.4382022, 1}},
		// SetSat((0.2, 0.4, 0.6), 1) = (0, 0.5, 1); SetLum to 0.362 goes
		// below zero and is clipped from beneath.
		{Saturation, [3]float32{0.2, 0.4, 0.6}, [3]float32{0, 0, 1}, [3]float32{0, 0.4469136, 0.8938272}},
	}
	for _, tc := range cases {
		if got := tc.mode.Blend(tc.cb, tc.cs); !nearRGB(got, tc.want) {
			t.Errorf("%v(%v, %v) = %v, want %v", tc.mode, tc.cb, tc.cs, got, tc.want)
		}
		if tc.mode.Separable() {
			t.Errorf("%v is not separable", tc.mode)
		}
	}
}

// TestNonSeparableKeepsLuminosity checks the invariant Table 137 builds the
// modes on: Hue, Saturation and Color keep the backdrop's luminosity and
// Luminosity takes the source's, for colours whose result stays in gamut.
func TestNonSeparableKeepsLuminosity(t *testing.T) {
	cb := [3]float32{0.4, 0.5, 0.3}
	cs := [3]float32{0.45, 0.35, 0.5}
	for _, m := range []Mode{Hue, Saturation, Color} {
		if got := lum(m.Blend(cb, cs)); !near32(got, lum(cb)) {
			t.Errorf("Lum(%v) = %g, want the backdrop's %g", m, got, lum(cb))
		}
	}
	if got := lum(Luminosity.Blend(cb, cs)); !near32(got, lum(cs)) {
		t.Errorf("Lum(Luminosity) = %g, want the source's %g", got, lum(cs))
	}
}

func TestParseMode(t *testing.T) {
	for m := Normal; m <= Luminosity; m++ {
		got, ok := ParseMode(m.String())
		if !ok || got != m {
			t.Errorf("ParseMode(%q) = %v, %v", m.String(), got, ok)
		}
	}
	if got, ok := ParseMode("Compatible"); !ok || got != Normal {
		t.Errorf("ParseMode(Compatible) = %v, %v, want Normal", got, ok)
	}
	if _, ok := ParseMode("Dissolve"); ok {
		t.Error("ParseMode(Dissolve) recognised a non-standard mode")
	}
}
