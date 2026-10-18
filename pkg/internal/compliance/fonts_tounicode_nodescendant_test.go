package compliance

import "testing"

// TestToUnicode_Type0WithoutDescendant pins that a Type 0 font whose
// /DescendantFonts holds no CIDFont dictionary still answers the ToUnicode
// presence rule (ISO 19005-2, 6.2.11.7.2 t1; ISO 14289-1, 7.21.7 t1) for the
// glyphs it shows; see [type0ToUnicodeWithoutDescendant].
//
// veraPDF 1.30.2 builds such a font over an empty dictionary, which names no
// character collection, and states each glyph. The font is shown on two pages,
// so the judgement of a later page reaches it too, and states the glyph once.
func TestToUnicode_Type0WithoutDescendant(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, descendants string
	}{
		{"an array holding null", "/DescendantFonts [null]"},
		{"no entry", ""},
		{"an empty array", "/DescendantFonts []"},
		{"a dictionary, no array", "/DescendantFonts << /Type /Font /Subtype /CIDFontType2 /BaseFont /LithoNoArray >>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			font := []string{"<< /Type /Font /Subtype /Type0 /BaseFont /LithoBare /Encoding /Identity-H " + tc.descendants + " >>"}
			objs := glyphPagesObjs(font, glyphText("<0001> Tj"), glyphText("<0001> Tj"))
			if got := validateCount(t, objs, "PDF/A-2u", Rule_19005_2_6_2_11_7_2_t01); got != 1 {
				t.Errorf("PDF/A-2u: 6.2.11.7.2 test 1 fired %d times, want 1", got)
			}
			if got := validateCount(t, objs, "PDF/UA-1", Rule_14289_1_7_21_7_t01); got != 1 {
				t.Errorf("PDF/UA-1: 7.21.7 test 1 fired %d times, want 1", got)
			}
		})
	}
}
