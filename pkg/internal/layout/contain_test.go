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
	container := func(c css.ContainerType) func(*css.Style) { return func(s *css.Style) { s.ContainerType = c } }
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
		// CSS Conditional 5, 5.1: container-type applies the containment
		// of its axes.
		{"inline-size container", sty(float(css.FloatLeft), container(css.ContainerInlineSize)), [2]float64{0, 10}},
		{"size container", sty(fit, container(css.ContainerSize)), [2]float64{0, 0}},
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

// TestContainer_IndependentFormattingContext holds a container for size
// queries to CSS Conditional 5, 5.1: it establishes an independent
// formatting context, so it holds its floats, and it is no containing
// block for its absolutely positioned descendants, which layout
// containment would make it.
func TestContainer_IndependentFormattingContext(t *testing.T) {
	c := sty(func(s *css.Style) { s.ContainerType = css.ContainerInlineSize }, margin(20))
	fl := sty(float(css.FloatLeft), width(30), height(50))
	rb := boxes(t, doc(el(c, el(fl))), page(400, 400))
	if got := rect(kid(t, rb, 0, 0)); got[3] != 50 {
		t.Errorf("container %v tall, want 50 holding its float", got[3])
	}
	abs := sty(func(s *css.Style) { s.Position = css.Absolute; s.Top, s.Left = css.Pts(0), css.Pts(0) }, width(10), height(10))
	rb = boxes(t, doc(el(c, el(abs))), page(400, 400))
	if got := rect(kid(t, rb, 0, 0, 0)); got[0] != 0 || got[1] != 0 {
		t.Errorf("absolute child at %v,%v, want the page's corner", got[0], got[1])
	}
}
