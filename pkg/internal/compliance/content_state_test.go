package compliance

import (
	"testing"
)

// TestInlineImageLZWStatedOnce covers the inline-image LZW prohibition,
// ISO 19005-1, 6.1.10 t2. It replaces a test of a second, redundant LZW reader
// that stated the same rule again for the same inline image: veraPDF keys the
// rule on CosIIFilter, and reported one check where litho reported two.
//
// The cases are the ones that reader covered: the abbreviated and the full
// filter name, under both the abbreviated and the full key, alone and inside a
// filter-chain array.
func TestInlineImageLZWStatedOnce(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		content string
		want    int
	}{
		{"abbreviated /F /LZW", "BI /W 2 /H 2 /BPC 8 /CS /G /F /LZW ID xx EI", 1},
		{"full /Filter /LZWDecode", "BI /W 2 /H 2 /BPC 8 /CS /G /Filter /LZWDecode ID xx EI", 1},
		{"array /F [/LZW]", "BI /W 2 /H 2 /BPC 8 /CS /G /F [/LZW] ID xx EI", 1},
		{"array with LZW among others", "BI /W 2 /H 2 /BPC 8 /CS /G /F [/AHx /LZWDecode] ID xx EI", 1},
		{"flate is permitted", "BI /W 2 /H 2 /BPC 8 /CS /G /F /Fl ID xx EI", 0},
		{"no filter at all", "BI /W 2 /H 2 /BPC 8 /CS /G ID xx EI", 0},
		{"two LZW images answer twice", "BI /F /LZW ID xx EI\nBI /F /LZW ID yy EI", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkInlineImageFilters([]byte(tc.content), 1, 1)
			if len(got) != tc.want {
				t.Fatalf("got %d violations, want %d: %v", len(got), tc.want, got)
			}
			for _, v := range got {
				if v.Rule != Rule_19005_1_6_1_10_t02 {
					t.Errorf("got rule %s test %d, want 6.1.10 test 2", v.Rule.Clause, v.Rule.TestNumber)
				}
			}
		})
	}
}
