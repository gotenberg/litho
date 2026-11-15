package css

import "testing"

// TestWillChange_Parse locks what will-change makes of a box (CSS Will
// Change 1, 3): each named property's effects, legacy names and case
// folded, and the keywords no <custom-ident> may be.
func TestWillChange_Parse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		want  WillChange
	}{
		{"auto", 0},
		{"transform", WillStack | WillHoldAbsolute | WillHoldFixed},
		{"Transform", WillStack | WillHoldAbsolute | WillHoldFixed},
		{"-webkit-transform", WillStack | WillHoldAbsolute | WillHoldFixed},
		{"position", WillStack | WillHoldAbsolute},
		{"opacity, top", WillStack | WillBackdropRoot},
		{"scroll-position, contents, foo", 0},
		{"filter", WillStack | WillHoldAbsolute | WillHoldFixed | WillBackdropRoot},
		{"transform, all", 0},
		{"none", 0},
		{"auto, transform", 0},
	}
	for _, tt := range tests {
		st := compute(t, `<div id=d></div>`, "#d { will-change: "+tt.value+" }").of("d")
		if st.WillChange != tt.want {
			t.Errorf("will-change: %s = %b, want %b", tt.value, st.WillChange, tt.want)
		}
	}
}
