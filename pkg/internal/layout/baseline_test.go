package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestBaselineSource_PicksTheLine(t *testing.T) {
	// CSS Inline 3, 4.3: an inline-block aligns by its last line under
	// auto and last, by its first under first.
	two := func(src css.BaselineSource) *Node {
		return el(sty(display(css.InlineBlock), width(20), func(s *css.Style) { s.BaselineSource = src }),
			el(sty(height(10)), txt(sty(), "a")), el(sty(height(10)), txt(sty(), "b")))
	}
	for _, tt := range []struct {
		src  css.BaselineSource
		last bool
	}{{css.BaselineAuto, true}, {css.BaselineLast, true}, {css.BaselineFirst, false}} {
		rb := boxes(t, doc(el(sty(), txt(sty(), "x"), two(tt.src))), page(200, 200))
		a := kid(t, rb, 0, 0, 1)
		first, _ := firstBaseline(a)
		last, _ := lastBaseline(a)
		if near(first, last) {
			t.Fatalf("first and last baselines agree at %v", first)
		}
		want := first
		if tt.last {
			want = last
		}
		if !a.hasBaseline || !near(a.baseline+a.y, want) {
			t.Errorf("baseline-source %v: baseline %v, want %v", tt.src, a.baseline+a.y, want)
		}
	}
}
