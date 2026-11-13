package css

import "testing"

// TestContainIntrinsic_Parse holds contain-intrinsic-width, -height and
// the contain-intrinsic-size shorthand to CSS Sizing 4, 5.1: auto? [ none
// | <length [0,∞]> ], auto naming a size a printed document never has; the
// shorthand gives one value to both or the width then the height, and the
// logical longhands map by writing mode.
func TestContainIntrinsic_Parse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl string
		w, h float64
	}{
		{"contain-intrinsic-size: none", -1, -1},
		{"contain-intrinsic-size: 10px", 10, 10},
		{"contain-intrinsic-size: 10px 20px", 10, 20},
		{"contain-intrinsic-size: auto 10px auto 20px", 10, 20},
		{"contain-intrinsic-size: auto 10px 20px", 10, 20},
		{"contain-intrinsic-size: none 20px", -1, 20},
		{"contain-intrinsic-width: 30px", 30, -1},
		{"contain-intrinsic-height: auto none", -1, -1},
		{"contain-intrinsic-inline-size: 5px", 5, -1},
		{"contain-intrinsic-block-size: 5px; writing-mode: vertical-rl", 5, -1},
		{"contain-intrinsic-size: -1px", -1, -1},
		{"contain-intrinsic-size: 10%", -1, -1},
		{"contain-intrinsic-size: 1px 2px 3px", -1, -1},
		{"contain-intrinsic-size: auto", -1, -1},
	}
	for _, tt := range tests {
		s := styleOf(t, tt.decl)
		w, h := -1.0, -1.0
		if c := s.ContainIntrinsic; c != nil {
			w, h = c.Width, c.Height
		}
		if w >= 0 {
			w /= Pt
		}
		if h >= 0 {
			h /= Pt
		}
		if !near(w, tt.w) || !near(h, tt.h) {
			t.Errorf("%s = %v by %v, want %v by %v", tt.decl, w, h, tt.w, tt.h)
		}
	}
}
