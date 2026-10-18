package compliance

import (
	"fmt"
	"slices"
	"testing"
)

// TestProgramNotdefValuesTheGlyph pins that a code an embedded Type 1 font's
// encoding leaves unnamed takes the name its program gives it, as veraPDF's
// PDSimpleFont.toUnicode reads it ([programNotdefCodes]). The litho fixture
// type1notdef_2u.pdf shows such a code through N1 to N6: the programs of N1,
// N3, N4, N5 and N6 name it .notdef, valued U+0000, and the one of N2 names it
// nothing. veraPDF 1.30.2 reports the value rule for the first five and the
// presence rule for N2 alone, and at PDF/A-1a, where .notdef passes, N2 alone.
func TestProgramNotdefValuesTheGlyph(t *testing.T) {
	t.Parallel()
	notdef := []string{"N1", "N3", "N4", "N5", "N6"}
	for _, tt := range []struct {
		profile string
		rule    Rule
		want    []string
	}{
		{"PDF/A-1a", Rule_19005_1_6_3_8_t01, []string{"N2"}},
		{"PDF/A-2u", Rule_19005_2_6_2_11_7_2_t01, []string{"N2"}},
		{"PDF/A-2u", Rule_19005_2_6_2_11_7_2_t02, notdef},
		{"PDF/A-3a", Rule_19005_3_6_2_11_7_2_t02, notdef},
		{"PDF/A-4", Rule_19005_4_6_2_10_7_t01, notdef},
		{"PDF/UA-1", Rule_14289_1_7_21_7_t01, []string{"N2"}},
		{"PDF/UA-1", Rule_14289_1_7_21_7_t02, notdef},
		{"PDF/UA-2", Rule_14289_2_8_4_5_8_t01, []string{"N2"}},
		{"PDF/UA-2", Rule_14289_2_8_4_5_8_t02, notdef},
	} {
		t.Run(fmt.Sprintf("%s %s t%d", tt.profile, tt.rule.Clause, tt.rule.TestNumber), func(t *testing.T) {
			t.Parallel()
			if got := fixtureFontsFailing(t, "type1notdef_2u.pdf", tt.profile, tt.rule); !slices.Equal(got, tt.want) {
				t.Errorf("%s t%d fired for %v, want %v", tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}
