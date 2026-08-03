package compliance

import (
	"fmt"
	"testing"
)

// subtypeFontPDF builds a one-page PDF whose only font carries the given
// /Subtype line, with no /FirstChar, /LastChar or /Widths.
func subtypeFontPDF(subtypeEntry string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length 44 >>\nstream\nBT /F1 12 Tf 50 700 Td (subtype) Tj ET\nendstream",
		fmt.Sprintf("<< /Type /Font %s /BaseFont /NotStandard /Encoding /WinAnsiEncoding >>",
			subtypeEntry),
	)
}

// TestSimpleFontKeysNeedASimpleSubtype covers which font dictionaries answer to
// the simple-font required-key rules. ISO 19005-1 6.3.2 t4, t5 and t6 carry
// veraPDF's PDSimpleFont as their object, and its font factory builds one only
// for a known simple subtype. A dictionary whose /Subtype is absent, or is not
// a font type at all, is not a simple font: litho used to report three missing
// keys against it on top of the invalid subtype t2 already reports.
func TestSimpleFontKeysNeedASimpleSubtype(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		subtype string
		// wantKeys is whether the /FirstChar, /LastChar and /Widths rules fire.
		wantKeys bool
		// wantBadSubtype is whether the invalid-subtype rule fires.
		wantBadSubtype bool
	}{
		{
			name:           "a subtype that is not a font type answers only for itself",
			subtype:        "/Subtype /Type42",
			wantKeys:       false,
			wantBadSubtype: true,
		},
		{
			name:           "no subtype at all answers only for the missing one",
			subtype:        "",
			wantKeys:       false,
			wantBadSubtype: true,
		},
		{
			name:           "a Type1 font answers for its missing keys",
			subtype:        "/Subtype /Type1",
			wantKeys:       true,
			wantBadSubtype: false,
		},
		{
			name:           "a TrueType font answers for its missing keys",
			subtype:        "/Subtype /TrueType",
			wantKeys:       true,
			wantBadSubtype: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := subtypeFontPDF(tc.subtype)
			for _, test := range []int{4, 5, 6} {
				if got := firesRule(t, data, "6.3.2", test); got != tc.wantKeys {
					t.Errorf("6.3.2 t%d fired = %v, want %v", test, got, tc.wantKeys)
				}
			}
			if got := firesRule(t, data, "6.3.2", 2); got != tc.wantBadSubtype {
				t.Errorf("6.3.2 t2 fired = %v, want %v", got, tc.wantBadSubtype)
			}
		})
	}
}
