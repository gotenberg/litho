package css

import (
	"strings"
	"testing"
)

// TestBackgroundPosition_Axes holds background-position to CSS
// Backgrounds 3, 3.6 and CSS Backgrounds 4, 2.4: an offset from the right
// or bottom edge is 100% less the offset, center takes the axis the other
// keyword leaves, background-position is the shorthand of
// background-position-x and -y, and a later declaration wins axis by
// axis.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) answers
//     CSS.supports true for right 10px bottom 20px, right 10px top, center
//     right 10px, background-position-x: right 10px, center and left,
//     right, and false for x-start, x-end 5px, top and center 10px there.
func TestBackgroundPosition_Axes(t *testing.T) {
	t.Parallel()
	pct := func(v float64) Dimension { return Dimension{Kind: Percent, Value: v} }
	px := func(v float64) Dimension { return Pts(v * Pt) }
	far := func(v float64) Dimension { return Dimension{Kind: Calc, Value: -v * Pt, Fraction: 1} }
	tests := []struct {
		decl string
		want [2]Dimension
	}{
		{"background-position: right 10px bottom 20px", [2]Dimension{far(10), far(20)}},
		{"background-position: bottom 20px right 10px", [2]Dimension{far(10), far(20)}},
		{"background-position: right 10px top", [2]Dimension{far(10), pct(0)}},
		{"background-position: center right 10px", [2]Dimension{far(10), pct(0.5)}},
		{"background-position: left 10px center", [2]Dimension{px(10), pct(0.5)}},
		{"background-position: center center 10px", [2]Dimension{pct(0), pct(0)}},
		{"background-position-x: right 10px", [2]Dimension{far(10), pct(0)}},
		{"background-position-x: center", [2]Dimension{pct(0.5), pct(0)}},
		{"background-position-x: 5px", [2]Dimension{px(5), pct(0)}},
		{"background-position-y: bottom", [2]Dimension{pct(0), pct(1)}},
		{"background-position-x: top", [2]Dimension{pct(0), pct(0)}},
		{"background-position-x: center 10px", [2]Dimension{pct(0), pct(0)}},
		{"background-position: 1px 2px; background-position-x: center", [2]Dimension{pct(0.5), px(2)}},
		{"background-position-x: center; background-position: 1px 2px", [2]Dimension{px(1), px(2)}},
		{"background: url(a.png) right 5px bottom / 10px", [2]Dimension{far(5), pct(1)}},
	}
	for _, tt := range tests {
		got := styleOf(t, tt.decl).BackgroundPosition
		for i := range got {
			if got[i].Kind != tt.want[i].Kind || !near(got[i].Value, tt.want[i].Value) || !near(got[i].Fraction, tt.want[i].Fraction) {
				t.Errorf("%s = %+v, want %+v", tt.decl, got, tt.want)
				break
			}
		}
	}
}

// TestPosition_ThreeValues holds that a <position> (CSS Values 4, 9.1)
// takes one, two or four values, and a <bg-position> (CSS Backgrounds 3,
// 3.6) three as well: mask-position and object-position refuse the
// three-value forms background-position takes.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) answers
//     CSS.supports false for mask-position, object-position, a radial
//     gradient's and a circle()'s position and transform-origin of right
//     10px top, and for mask-position: center right 10px and left 10px
//     top; true for background-position: right 10px top and for every
//     four-value form.
func TestPosition_ThreeValues(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		decl string
		ok   bool
	}{
		{"mask-position: right 10px bottom 20px", true},
		{"mask-position: right 10px top", false},
		{"mask-position: center right 10px", false},
		{"mask-position: left 10px top", false},
		{"object-position: right 10px top", false},
		{"object-position: right 10px bottom 20px", true},
	} {
		s := styleOf(t, "mask-image: linear-gradient(red, blue); "+tt.decl)
		var got [2]Dimension
		if strings.HasPrefix(tt.decl, "mask") {
			if l := s.MaskLayers(); len(l) > 0 {
				got = l[0].Position
			}
		} else {
			got = s.ObjectPosition
		}
		if set := got[0].Kind == Calc; set != tt.ok {
			t.Errorf("%s: parsed %v, want %v (%+v)", tt.decl, set, tt.ok, got)
		}
	}
}
