package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestContain_SizeContainment holds a box under size containment to CSS
// Contain 2, 3.2 and 3.3 and CSS Sizing 4, 5.1: it sizes as if empty, at
// its contain-intrinsic-width and -height when set; inline-size
// containment does so in the inline axis alone; without containment the
// intrinsic sizes are not read.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) draws each case
//     at the size the test wants.
func TestContain_SizeContainment(t *testing.T) {
	fit := func(s *css.Style) { s.Width = css.Dimension{Kind: css.FitContent} }
	contain := func(c css.Containment) func(*css.Style) { return func(s *css.Style) { s.Contain = c } }
	cis := func(w, h float64) func(*css.Style) {
		return func(s *css.Style) { s.ContainIntrinsic = &css.IntrinsicSize{Width: px(w), Height: px(h)} }
	}
	tests := []struct {
		name string
		st   *css.Style
		want [2]float64
	}{
		{"size containment", sty(fit, contain(css.ContainSize), cis(50, 30)), [2]float64{50, 30}},
		{"no containment", sty(fit, cis(50, 30)), [2]float64{40, 10}},
		{"no intrinsic size", sty(fit, contain(css.ContainSize)), [2]float64{0, 0}},
		{"inline-size containment", sty(float(css.FloatLeft), contain(css.ContainInlineSize), cis(60, -1)), [2]float64{60, 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := boxes(t, doc(el(tt.st, txt(sty(), "aaaa"))), page(400, 400))
			if got := rect(kid(t, rb, 0, 0)); got[2] != tt.want[0] || got[3] != tt.want[1] {
				t.Errorf("box %v by %v, want %v by %v", got[2], got[3], tt.want[0], tt.want[1])
			}
		})
	}
}
