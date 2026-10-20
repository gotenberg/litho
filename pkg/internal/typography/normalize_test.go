package typography

import "testing"

// TestNormalizeExtractionUnicode_LongSTLigature pins that U+FB05 LATIN
// SMALL LIGATURE LONG S T reads as its Unicode decomposition, U+017F
// LATIN SMALL LETTER LONG S and "t", the long s litho keeps wherever a font
// shows it alone, and that the other Latin ligatures keep their NFKC fold.
func TestNormalizeExtractionUnicode_LongSTLigature(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"ﬅ":      "ſt",
		"Chriﬅi": "Chriſti",
		"ﬅﬅ":     "ſtſt",
		"eﬀﬁﬅﬆ":  "efffiſtst",
		"ſ":      "ſ",
	} {
		if got := normalizeExtractionUnicode(in); got != want {
			t.Errorf("normalizeExtractionUnicode(%q) = %q, want %q", in, got, want)
		}
	}
}
