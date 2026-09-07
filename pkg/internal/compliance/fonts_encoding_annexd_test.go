package compliance

import (
	"fmt"
	"testing"
)

// TestPredefinedEncodingNames_AnnexDGlyphNames pins the codes whose ISO 32000-1,
// Annex D.2 glyph name is not the Adobe Glyph List name for the Unicode value
// the same table assigns them. Deriving the name from the rune yields a
// synthetic uniXXXX no charset holds, so the code resolves to no glyph: govdocs
// thread_000_000152.pdf shows WinAnsi code 173, whose Annex D name "hyphen"
// selects a 800-unit glyph against a declared 650, a §6.2.11.5 mismatch
// veraPDF reports and litho read as U+00AD instead.
func TestPredefinedEncodingNames_AnnexDGlyphNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		encoding string
		code     byte
		want     string
	}{
		{"WinAnsiEncoding", 0o240, "space"},
		{"WinAnsiEncoding", 0o255, "hyphen"},
		{"MacRomanEncoding", 0o255, "notequal"},
		{"MacRomanEncoding", 0o271, "pi"},
		{"MacRomanEncoding", 0o306, "Delta"},
		{"MacRomanEncoding", 0o312, "space"},
		{"MacRomanEncoding", 0o333, "currency"},
		{"MacRomanEncoding", 0o360, "apple"},
		// Unchanged neighbours: the rune round-trip is right for every other code.
		{"WinAnsiEncoding", 0o055, "hyphen"},
		{"WinAnsiEncoding", 0o222, "quoteright"},
		{"MacRomanEncoding", 0o331, "Ydieresis"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s code %d", tc.encoding, tc.code), func(t *testing.T) {
			t.Parallel()
			if got := predefinedEncodingNames(tc.encoding)[tc.code]; got != tc.want {
				t.Errorf("%s[%d] = %q, want %q", tc.encoding, tc.code, got, tc.want)
			}
		})
	}
}
