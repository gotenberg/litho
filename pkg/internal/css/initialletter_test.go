package css

import "testing"

// TestInitialLetter_Parse checks the values of initial-letter Chromium
// parses (CSS Inline 3, 4.3) and the ones it refuses, which leave the
// initial value.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 supports
//     initial-letter 3, 3 2, 3 drop, raise 3 and 2.5, refuses 0.5 and
//     3 0, and parses neither initial-letter-align nor
//     initial-letter-wrap.
func TestInitialLetter_Parse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl string
		want InitialLetter
	}{
		{"initial-letter: 3", InitialLetter{3, 3}},
		{"initial-letter: 3 2", InitialLetter{3, 2}},
		{"initial-letter: 3 drop", InitialLetter{3, 3}},
		{"initial-letter: drop 2.5", InitialLetter{2.5, 2}},
		{"initial-letter: raise 3", InitialLetter{3, 1}},
		{"initial-letter: 3 raise", InitialLetter{3, 1}},
		{"initial-letter: 2.5", InitialLetter{2.5, 2}},
		{"initial-letter: normal", InitialLetter{}},
		{"initial-letter: 0.5", InitialLetter{}},
		{"initial-letter: 3 0", InitialLetter{}},
		{"initial-letter: 3 2.5", InitialLetter{}},
		{"initial-letter: 2 3 drop", InitialLetter{}},
		{"initial-letter: drop raise", InitialLetter{}},
		{"initial-letter: 2 drop raise", InitialLetter{}},
	}
	for _, tt := range tests {
		if got := styleOf(t, tt.decl).InitialLetter; got != tt.want {
			t.Errorf("%s = %+v, want %+v", tt.decl, got, tt.want)
		}
	}
	c := compute(t, `<p id=p><span id=s>x</span></p>`, `p { initial-letter: 3 }`)
	if got := c.of("s").InitialLetter; got != (InitialLetter{}) {
		t.Errorf("initial-letter inherited: %+v", got)
	}
}
