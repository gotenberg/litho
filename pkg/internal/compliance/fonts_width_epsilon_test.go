package compliance

import "testing"

// TestWidthsInconsistent_ToleranceBoundary pins that the rounding a width
// computation carries does not turn an allowed difference into a violation.
//
// ISO 19005-2, 6.2.11.5 and ISO 14289-2, 8.4.5.6 allow the declared width and
// the program advance to differ by one unit, and veraPDF writes its test as
// abs(widthFromFontProgram - widthFromDictionary) <= 1. litho reaches the
// program advance through a scale read from the font, and a Type 1 /FontMatrix
// of 0.001 parses to 0.0010000000000000000208: an advance of 334 against a
// declared 333 then computes as 1.0000000000000191, which a strict comparison
// against 1.0 reports. pdfjs bug921760.pdf, issue13915.pdf and issue6127.pdf
// all failed that way and veraPDF passes every one.
func TestWidthsInconsistent_ToleranceBoundary(t *testing.T) {
	t.Parallel()

	// The value litho actually computes for bug921760's char 40: the glyph
	// advances 334 and the /FontMatrix scale carries the error.
	const scaled334 = 334 * 0.0010000000000000000208 * 1000.0

	cases := []struct {
		name     string
		declared float64
		program  float64
		want     bool
	}{
		{"a difference of exactly one is allowed", 333, 334, false},
		{"the same difference carrying float error is too", 333, scaled334, false},
		{"an identical width is allowed", 500, 500, false},
		{"a difference below one is allowed", 333, 333.5, false},
		{"a difference of two is a mismatch", 333, 335, true},
		{"a large difference is a mismatch", 285.5, 500, true},
		{"the sign of the difference does not matter", 335, 333, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := widthsInconsistent(tc.declared, tc.program); got != tc.want {
				t.Errorf("widthsInconsistent(%v, %v) = %v, want %v",
					tc.declared, tc.program, got, tc.want)
			}
		})
	}
}
