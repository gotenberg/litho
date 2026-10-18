package compliance

import (
	"slices"
	"testing"
)

// TestEmptyCharStringsDefineNoGlyph pins the verdicts on a CFF program whose
// CharStrings INDEX holds no charstring. The program still reads, so the font
// is embedded, but it defines no glyph: every code shown fails ISO 19005-2,
// 6.2.11.4.1 t2 but one its encoding names .notdef, which 6.2.11.8 t1 reports
// instead. The width rule still weighs each glyph, at the defaultWidthX of the
// Private DICT for a name-keyed program and, for a subset or a CID-keyed
// program, at the width the font dictionary gives a missing glyph. The litho
// fixture fontemptycs_2b.pdf plants each shape, and veraPDF 1.30.2 reports
// exactly these glyphs.
func TestEmptyCharStringsDefineNoGlyph(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		rule Rule
		want []string
	}{
		{"glyph presence", Rule_19005_2_6_2_11_4_1_t02, []string{
			"C1", "C1", "C2", "C2", "E1", "E1", "E2", "E2", "E3", "E3", "E4", "E4", "E5", "K1", "K1", "K2", "K2",
		}},
		{"glyph width", Rule_19005_2_6_2_11_5_t01, []string{"C2", "C2", "E2", "E2", "E3", "E3", "K2", "K2"}},
		{".notdef", Rule_19005_2_6_2_11_8_t01, []string{"C1", "C1", "C2", "C2", "E5", "K1", "K1", "K2", "K2"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fixtureFontsFailing(t, "fontemptycs_2b.pdf", "PDF/A-2b", tt.rule); !slices.Equal(got, tt.want) {
				t.Errorf("fonts failing = %v, want %v", got, tt.want)
			}
		})
	}
}
