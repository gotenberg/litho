package remediation

import "testing"

// TestPDFDateToXMPRejectsMalformedFields pins that a PDF date whose fixed-width
// fields are not fixed width yields no XMP date at all.
//
// ISO 32000-1, 7.9.4 defines the value as D:YYYYMMDDHHmmSSOHH'mm, every field
// two digits. A producer writing a shorter field leaves each slice straddling
// the next separator, and copying the slices through yields well-formed XMP
// holding an invalid Date, which ISO 19005-1 6.7.9 test 3 reports. Writing a
// wrong date is worse than leaving the property absent, and the caller skips
// the sync on an empty return.
//
// # Corpus evidence
//
//   - pdfjs issue18122.pdf: carries "D:202403083:4:5 ", whose hour slice is
//     "3:", and litho injected xmp:CreateDate "2024-03-08T3::4::5 Z".
func TestPDFDateToXMPRejectsMalformedFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"a full date converts", "D:20240308123456Z", "2024-03-08T12:34:56Z"},
		{"a date with no time defaults", "D:20240308", "2024-03-08T00:00:00Z"},
		{"single-digit fields are rejected", "D:202403083:4:5 ", ""},
		{"a non-numeric month is rejected", "D:2024xx08120000Z", ""},
		{"an impossible month is rejected", "D:20241308120000Z", ""},
		{"an impossible day is rejected", "D:20240332120000Z", ""},
		{"an impossible hour is rejected", "D:20240308250000Z", ""},
		{"an impossible minute is rejected", "D:20240308126000Z", ""},
		{"a leap second is accepted", "D:20240308235960Z", "2024-03-08T23:59:60Z"},
		{"a short string is rejected", "D:20", ""},
		{"a non-numeric year is rejected", "D:20x4", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := pdfDateToXMP(tc.in); got != tc.want {
				t.Errorf("pdfDateToXMP(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestPDFDateToXMPKeepsOffsets pins that a numeric UTC offset still survives,
// so the stricter field check did not cost the timezone handling.
func TestPDFDateToXMPKeepsOffsets(t *testing.T) {
	t.Parallel()

	cases := []struct{ in, want string }{
		{"D:20240308120000+02'00", "2024-03-08T12:00:00+02:00"},
		{"D:20240308120000-05'30", "2024-03-08T12:00:00-05:30"},
		{"D:20240308120000Z", "2024-03-08T12:00:00Z"},
	}
	for _, tc := range cases {
		if got := pdfDateToXMP(tc.in); got != tc.want {
			t.Errorf("pdfDateToXMP(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
