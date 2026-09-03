package writer

import (
	"strconv"
	"testing"
)

// TestFormatRealRoundTripsExactly pins that a value litho writes reads back as
// the value it was given.
//
// The formatter used a fixed six decimals, so it rounded. An object copied
// unchanged came out holding a different number: a Type 2 tint transform whose
// /C1 was .1000000015 was re-emitted as 0.1, and the copy then disagreed with
// the original the same file still contained. ISO 19005-2, 6.2.4.4 test 2
// compares the PDF objects rather than what they compute, so pdfjs
// issue21089.pdf failed it after remediation.
func TestFormatRealRoundTripsExactly(t *testing.T) {
	t.Parallel()

	for _, f := range []float64{
		0.1000000015,
		0.3333333333333333,
		1.0 / 3.0,
		0.7071067811865476,
		1e-7,
		-1e-7,
		1.00000005,
		612.34567891,
		-0.000123456789,
	} {
		s := formatReal(f)
		back, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Errorf("formatReal(%v) = %q, which does not parse: %v", f, s, err)
			continue
		}
		if back != f {
			t.Errorf("formatReal(%v) = %q, which reads back as %v", f, s, back)
		}
	}
}

// TestFormatRealKeepsOrdinaryValuesShort is the control: the common case must
// not grow. A coordinate or a whole number is written exactly as before.
func TestFormatRealKeepsOrdinaryValuesShort(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		value float64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{-42, "-42"},
		{1.5, "1.5"},
		{612, "612"},
		{792.5, "792.5"},
		{0.25, "0.25"},
		{-0.5, "-0.5"},
	} {
		if got := formatReal(tc.value); got != tc.want {
			t.Errorf("formatReal(%v) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

// TestFormatRealNeverUsesAnExponent pins the form ISO 32000-1, 7.3.3 requires:
// digits with an optional sign and period, never an exponent, whatever the
// magnitude.
func TestFormatRealNeverUsesAnExponent(t *testing.T) {
	t.Parallel()

	for _, f := range []float64{1e-38, -1e-38, 1e38, -1e38, 1e100, 3.4e38} {
		s := formatReal(f)
		for _, r := range s {
			if r == 'e' || r == 'E' {
				t.Errorf("formatReal(%v) = %q, which a PDF reader cannot read", f, s)
				break
			}
		}
	}
}
