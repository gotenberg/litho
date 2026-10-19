package encodings

import "testing"

// TestGlyphToRune_AGLAliases pins the Adobe Glyph List names that share a
// code point with another listed name. Values per glyphlist.txt (AGL 2.0).
func TestGlyphToRune_AGLAliases(t *testing.T) {
	t.Parallel()
	want := map[string]rune{
		"nbspace":   0x00A0,
		"sfthyphen": 0x00AD,
		"mu1":       0x00B5,
		"hyphentwo": 0x2010,
	}
	for name, r := range want {
		if got, ok := GlyphToRuneFull(name); !ok || got != r {
			t.Errorf("GlyphToRuneFull(%q) = %U, %v; want %U, true", name, got, ok, r)
		}
		if got, ok := GlyphToRuneSimple(name); !ok || got != r {
			t.Errorf("GlyphToRuneSimple(%q) = %U, %v; want %U, true", name, got, ok, r)
		}
		if !InAdobeGlyphList(name) {
			t.Errorf("InAdobeGlyphList(%q) = false; want true", name)
		}
	}
}

// TestGlyphToRune_AGLAliasesLeaveReverseMap pins that the aliases stay out of
// [RuneToGlyph]: a name chosen for a code point is the primary one, never the
// alias a font program is less likely to carry.
func TestGlyphToRune_AGLAliasesLeaveReverseMap(t *testing.T) {
	t.Parallel()
	for r, alias := range map[rune]string{0x00A0: "nbspace", 0x00AD: "sfthyphen", 0x00B5: "mu1", 0x2010: "hyphentwo"} {
		if got := RuneToGlyph(r); got == alias {
			t.Errorf("RuneToGlyph(%U) = %q; want a name other than the alias", r, got)
		}
	}
}
