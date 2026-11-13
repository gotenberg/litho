package html

import "testing"

// TestConvert_AnimationsAtEnd checks that the end-state switch reaches the
// cascade: a fade from hidden with a forwards fill prints hidden at time
// zero, the default, and revealed at its end; one without a fill prints
// the element's own style at its end.
func TestConvert_AnimationsAtEnd(t *testing.T) {
	t.Parallel()
	const src = `<style>
@keyframes fade { from { opacity: 0 } to { opacity: 1 } }
#f { animation: fade 1s forwards }
#n { opacity: .5; animation: fade 1s }
</style><p id=f>a</p><p id=n>b</p>`
	start := prep(t, src, Options{})
	end := prep(t, src, Options{AnimationsAtEnd: true})
	for _, tc := range []struct {
		id         string
		start, end float64
	}{{"f", 0, 1}, {"n", 0, 0.5}} {
		if got := byID(t, start.root, tc.id).Style.Opacity; got != tc.start {
			t.Errorf("#%s at time zero: opacity %v, want %v", tc.id, got, tc.start)
		}
		if got := byID(t, end.root, tc.id).Style.Opacity; got != tc.end {
			t.Errorf("#%s at the end: opacity %v, want %v", tc.id, got, tc.end)
		}
	}
	for _, w := range start.warnings {
		t.Errorf("warning %v", w)
	}
}
