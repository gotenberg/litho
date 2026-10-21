package blend

import "testing"

func TestEditionFor(t *testing.T) {
	for version, want := range map[string]Edition{
		"1.4": ISO32000v1, "1.7": ISO32000v1, "2.0": ISO32000v2, "2.1": ISO32000v2, "": ISO32000v1, "x": ISO32000v1,
	} {
		if got := EditionFor(version); got != want {
			t.Errorf("EditionFor(%q) = %v, want %v", version, got, want)
		}
	}
}

// TestEditionEdges checks the two cells of Table 136 where ISO 32000-1 and
// ISO 32000-2 part, and that elsewhere the editions blend alike.
func TestEditionEdges(t *testing.T) {
	cases := []struct {
		mode       Mode
		cb, cs     float32
		one, two   float32
		difference string
	}{
		{ColorDodge, 0, 1, 1, 0, "black backdrop, white source"},
		{ColorBurn, 1, 0, 0, 1, "white backdrop, black source"},
		{ColorDodge, 0.3, 0.5, 0.6, 0.6, "inside"},
		{ColorDodge, 0.4, 1, 1, 1, "white source"},
		{ColorBurn, 0.8, 0.5, 0.6, 0.6, "inside"},
		{ColorBurn, 0.2, 0, 0, 0, "black source"},
	}
	for _, tc := range cases {
		for e, want := range map[Edition]float32{ISO32000v1: tc.one, ISO32000v2: tc.two} {
			m := tc.mode.In(e)
			got := m.Blend([3]float32{tc.cb, tc.cb, tc.cb}, [3]float32{tc.cs, tc.cs, tc.cs})
			if !near32(got[0], want) {
				t.Errorf("%v in edition %d, %s: B(%g, %g) = %g, want %g", tc.mode, e, tc.difference, tc.cb, tc.cs, got[0], want)
			}
			if m.String() != tc.mode.String() || !m.Separable() {
				t.Errorf("%v in edition %d reads as %v, separable %v", tc.mode, e, m, m.Separable())
			}
		}
	}
	// Modes the editions blend alike keep their value, so Normal stays the
	// zero value a group tests for.
	if Normal.In(ISO32000v2) != Normal || Multiply.In(ISO32000v2) != Multiply || ColorDodge.In(ISO32000v2).In(ISO32000v1) != ColorDodge {
		t.Error("In changed a mode both editions define alike, or did not restore ISO 32000-1")
	}
}
