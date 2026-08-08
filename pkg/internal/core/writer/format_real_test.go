package writer

import (
	"strings"
	"testing"
)

// TestFormatReal_NeverExponential pins that a PDF real is written in the
// decimal form ISO 32000-1, 7.3.3 requires.
//
// Go's %g switches to exponential form for small magnitudes, and a content
// stream written that way is invalid: an aggressive rewrite of
// pdfjs issue11678.pdf turned 5 violations of ISO 19005-2, 6.1.6 into 64.
func TestFormatReal_NeverExponential(t *testing.T) {
	values := []float64{
		0, 1, -1, 0.5, -0.5,
		0.00001, -0.00001, 1e-7, -1e-7, 1e-12,
		1e15, -1e15, 1e20, 123456789012345,
		595.276, 841.89, 0.000123456,
	}
	for _, v := range values {
		got := FormatReal(v)
		if strings.ContainsAny(got, "eE") {
			t.Errorf("FormatReal(%v) = %q, which carries an exponent", v, got)
		}
		for _, r := range got {
			if (r < '0' || r > '9') && r != '.' && r != '-' {
				t.Errorf("FormatReal(%v) = %q contains %q, not a digit, sign or period", v, got, r)
			}
		}
	}
}
