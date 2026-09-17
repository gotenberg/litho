package compliance

import "testing"

// TestXMPNormalizeISO8601ShortOffset pins that an offset written without
// minutes is expanded to the full form. ISO 19005-1 6.7.3 asks whether the Info
// entry and its XMP analogue are EQUIVALENT, and the instant comparison parses
// RFC 3339, which rejects "+03". Leaving it short made the caller fall back to
// comparing spellings, so a date naming the same instant was reported as a
// mismatch: verapdf/PDF_UA-1__7.1-t06-fail-a.pdf reported 6.7.3 t1 where
// veraPDF 1.30.2 reports none.
//
// veraPDF parses the short form rather than passing vacuously, measured on a
// fixture whose short-offset date names a DIFFERENT instant: it reports the
// violation there.
func TestXMPNormalizeISO8601ShortOffset(t *testing.T) {
	t.Parallel()

	cases := []struct{ in, want string }{
		{"2019-12-24T15:49:34+03", "2019-12-24T15:49:34+03:00"},
		{"2019-12-24T15:49:34.000+03", "2019-12-24T15:49:34.000+03:00"},
		{"2019-12-24T15:49:34-05", "2019-12-24T15:49:34-05:00"},
		// +00 expands, then normalizes to Z like any other UTC spelling.
		{"2019-12-24T15:49:34+00", "2019-12-24T15:49:34Z"},
		// Already full: untouched.
		{"2019-12-24T15:49:34+03:00", "2019-12-24T15:49:34+03:00"},
		{"2019-12-24T15:49:34Z", "2019-12-24T15:49:34Z"},
		// No timezone at all: an absent zone means "unknown" and must not
		// acquire one. The trailing digits are seconds, not an offset.
		{"2019-12-24T15:49:34", "2019-12-24T15:49:34"},
		// Minute precision only: padded to seconds, still no zone invented.
		{"2019-12-24T15:49", "2019-12-24T15:49:00"},
		// No time part at all: returned as-is.
		{"2019-12-24", "2019-12-24"},
	}
	for _, c := range cases {
		if got := xmpNormalizeISO8601(c.in); got != c.want {
			t.Errorf("xmpNormalizeISO8601(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestXMPDateInstantSubSecond pins that the canonical instant keeps sub-second
// precision. Formatting with RFC 3339 alone dropped it, so two dates 729 ms
// apart compared equal and litho stayed silent where veraPDF 1.30.2 reports
// ISO 19005-1 6.7.3. Trailing zeros still collapse, so ".000" and a value
// carrying no fraction remain the same instant.
func TestXMPDateInstantSubSecond(t *testing.T) {
	t.Parallel()

	instant := func(s string) string {
		got, ok := xmpDateInstant(s)
		if !ok {
			t.Fatalf("xmpDateInstant(%q) failed to parse", s)
		}
		return got
	}

	if a, b := instant("2022-11-18T07:42:46.729Z"), instant("2022-11-18T07:42:46Z"); a == b {
		t.Errorf("dates 729ms apart must not share an instant: both %q", a)
	}
	if a, b := instant("2022-11-18T07:42:46.000Z"), instant("2022-11-18T07:42:46Z"); a != b {
		t.Errorf("a zero fraction is the same instant: %q vs %q", a, b)
	}
	// Sub-second equality still holds across zones.
	if a, b := instant("2022-11-18T10:42:46.729+03:00"), instant("2022-11-18T07:42:46.729Z"); a != b {
		t.Errorf("same instant in two zones: %q vs %q", a, b)
	}
}
