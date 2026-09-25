package compliance

import (
	"fmt"
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

// TestInlineImagesInARowJudgedOnce pins that an inline image whose BI follows
// the EI before it, with only white space and comments between, is not judged
// (see [checkInlineImageFilters]). Every case was measured with veraPDF 1.30 on
// a one-page file of this content at PDF/A-1b and PDF/A-2b: an EI followed by a
// keyword that names no operator is part of the data, and one followed by an
// operator or a number ends it.
func TestInlineImagesInARowJudgedOnce(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		content string
		want    int
	}{
		{"two in a row", "BI /F /LZW ID xx EI\nBI /F /LZW ID yy EI", 1},
		{"two in a row with a comment between", "BI /F /LZW ID xx EI\n% c\nBI /F /LZW ID yy EI", 1},
		{"three in a row", "BI /F /LZW ID xx EI\nBI /F /LZW ID yy EI\nBI /F /LZW ID zz EI", 1},
		{"an operator between", "BI /F /LZW ID xx EI\nq Q\nBI /F /LZW ID yy EI", 2},
		{"a plain image first", "BI /W 2 /H 1 /BPC 8 /CS /G ID xx EI\nBI /F /LZW ID yy EI", 0},
		{"an EI before a keyword that is no operator", "BI /F /LZW ID xEI zz EI\nBI /F /LZW ID yy EI", 1},
		{"an EI before an operator", "BI /F /LZW ID xEI q EI\nBI /F /LZW ID yy EI", 2},
		{"an EI before a number", "BI /F /LZW ID xEI 12 EI\nBI /F /LZW ID yy EI", 2},
	} {
		for _, p := range []struct {
			part int
			rule Rule
		}{{1, Rule_19005_1_6_1_10_t02}, {2, Rule_19005_2_6_1_10_t01}} {
			t.Run(fmt.Sprintf("%s, part %d", tc.name, p.part), func(t *testing.T) {
				t.Parallel()
				got := checkInlineImageFilters([]byte(tc.content), p.part, 1)
				if len(got) != tc.want {
					t.Fatalf("got %d violations, want %d: %v", len(got), tc.want, got)
				}
				for _, v := range got {
					if v.Rule != p.rule {
						t.Errorf("got rule %s test %d", v.Rule.Clause, v.Rule.TestNumber)
					}
				}
			})
		}
	}
}

// TestInlineImageFilterEnds pins where [inlineImageEnd] ends the data of an
// inline image and what [nextKeyword] reads after an EI.
func TestInlineImageFilterEnds(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		data string
		want int
	}{
		{" xx EI", 6},
		{" xx EI\n", 6},
		{" xxEI q", 5},
		{" xEI zz EI ", 10},
		{" xEI d0 EI", 4},
		{" xx", -1},
		{" xx EIQ", -1},
		{"", -1},
	} {
		if got := inlineImageEnd([]byte(tc.data), 0); got != tc.want {
			t.Errorf("inlineImageEnd(%q) = %d, want %d", tc.data, got, tc.want)
		}
	}
	for _, tc := range []struct{ data, want string }{
		{"  q Q", "q"},
		{"% c\nBI /W", ""},
		{" \nBI /W", "BI"},
		{"q1", "q"},
		{"d0 1 2", "d0"},
		{"12 w", ""},
		{"/Name", ""},
		{"", ""},
	} {
		if got := nextKeyword([]byte(tc.data)); got != tc.want {
			t.Errorf("nextKeyword(%q) = %q, want %q", tc.data, got, tc.want)
		}
	}
}
