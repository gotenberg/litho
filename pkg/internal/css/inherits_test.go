package css

import "testing"

// TestInheritsAlike checks that two styles differing in a property that
// does not inherit inherit alike, and two differing in color do not.
func TestInheritsAlike(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=d><span id=a style="display: contents">x</span><span id=b style="display: contents; color: blue">y</span></div>`, ``)
	if !InheritsAlike(c.of("a"), c.of("d")) {
		t.Error("a span of display: contents inherits as its div does")
	}
	if InheritsAlike(c.of("b"), c.of("d")) {
		t.Error("a span of colour blue inherits otherwise")
	}
}
