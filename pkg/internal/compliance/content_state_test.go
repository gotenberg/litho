package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// inlineFilterViolations parses stream as compliance reads a content stream
// and states the inline image filter rule of the part over its operations.
func inlineFilterViolations(t *testing.T, stream string, part int) []Violation {
	t.Helper()
	ops, err := content.ParseStreamData(context.Background(), []byte(stream))
	if err != nil {
		t.Fatalf("parsing %q: %v", stream, err)
	}
	return checkInlineImageFilters(ops, part, 1)
}

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
			got := inlineFilterViolations(t, tc.content, 1)
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

// TestInlineImageCodecFiltersForbidden pins that parts 2 to 4 forbid
// JPXDecode and JBIG2Decode in an inline image, and part 1 does not. Every case
// was measured with veraPDF 1.30.2 on the litho fixture inlinejpx_2b.pdf, whose
// five such images fail ISO 19005-2/3, 6.1.10 t1 and ISO 19005-4, 6.1.9 t1 five
// times and ISO 19005-1, 6.1.10 t2 never, while its CCF and Fl images pass.
func TestInlineImageCodecFiltersForbidden(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		content string
		want    map[int]int
	}{
		{"JPXDecode", "BI /W 8 /H 8 /CS /RGB /BPC 8 /F /JPXDecode ID xx EI", map[int]int{1: 0, 2: 1, 3: 1, 4: 1}},
		{"JBIG2Decode", "BI /W 8 /H 8 /CS /G /BPC 1 /F /JBIG2Decode ID xx EI", map[int]int{1: 0, 2: 1, 3: 1, 4: 1}},
		{"JBIG2Decode in a chain", "BI /W 8 /H 8 /IM true /F [/AHx /JBIG2Decode] ID xx EI", map[int]int{1: 0, 2: 1, 3: 1, 4: 1}},
		{"full key", "BI /W 8 /H 8 /Filter /JPXDecode ID xx EI", map[int]int{1: 0, 2: 1, 3: 1, 4: 1}},
		{"CCF is permitted", "BI /W 8 /H 8 /CS /G /BPC 1 /F /CCF ID xx EI", map[int]int{1: 0, 2: 0, 3: 0, 4: 0}},
		{"DCT is permitted", "BI /W 8 /H 8 /CS /G /BPC 8 /F /DCTDecode ID xx EI", map[int]int{1: 0, 2: 0, 3: 0, 4: 0}},
	} {
		for part, want := range tc.want {
			t.Run(fmt.Sprintf("%s, part %d", tc.name, part), func(t *testing.T) {
				t.Parallel()
				if got := inlineFilterViolations(t, tc.content, part); len(got) != want {
					t.Fatalf("got %d violations, want %d: %v", len(got), want, got)
				}
			})
		}
	}
}

// TestInlineImagesInARowJudgedOnce pins that an inline image whose BI follows
// the EI before it, with only white space and comments between, is not judged:
// the parse reads it as part of the check of that EI (see
// [checkInlineImageFilters]). Every case was measured with veraPDF 1.30 on a
// one-page file of this content at PDF/A-1b and PDF/A-2b: an EI followed by a
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
				got := inlineFilterViolations(t, tc.content, p.part)
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
