package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestColorScope_ImageMaskColorSpaceIgnored pins that a /ColorSpace written on
// an image mask selects no colour: ISO 32000-1, 8.9.5, Table 89 does not allow
// the entry on a mask, whose unmasked areas take the current fill colour
// (8.9.6.2). pdfjs issue11518.pdf gives its masks /ColorSpace /DeviceGray while
// filling them in an ICCBased space, and veraPDF reports no device colour. With
// no fill space selected the initial DeviceGray still counts. Every case was
// measured against veraPDF.
func TestColorScope_ImageMaskColorSpaceIgnored(t *testing.T) {
	t.Parallel()

	mask := func(cs string) string {
		return "<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ImageMask true" + cs +
			" /BitsPerComponent 1 /Length 8 >>\nstream\n\x00\x00\x00\x00\x00\x00\x00\x00\nendstream"
	}
	const filled = "q /CS0 cs 0 0 0 sc /Im0 Do Q"

	cases := []struct {
		name    string
		content string
		cs      string
		clause  string
		test    int
		want    bool
	}{
		{"a DeviceGray entry under a selected fill space", filled, " /ColorSpace /DeviceGray", "6.2.4.3", 4, false},
		{"a DeviceRGB entry under a selected fill space", filled, " /ColorSpace /DeviceRGB", "6.2.4.3", 2, false},
		{"a DeviceGray entry with the initial fill colour", "q /Im0 Do Q", " /ColorSpace /DeviceGray", "6.2.4.3", 4, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := colorScopePDF(
				"<< /XObject << /Im0 5 0 R >> /ColorSpace << /CS0 [/CalRGB << /WhitePoint [0.95 1 1.09] >>] >> >>",
				tc.content,
				mask(tc.cs),
			)
			if got := firesRuleProfile(t, data, models.PDFA2b, tc.clause, tc.test); got != tc.want {
				t.Errorf("%s t%d violation = %v, want %v", tc.clause, tc.test, got, tc.want)
			}
		})
	}
}
