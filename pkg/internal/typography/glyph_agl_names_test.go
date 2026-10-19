package typography

import "testing"

// TestResolveGlyphRune_NBSpace pins the Adobe Glyph List's "nbspace", which
// a /Differences array or a built-in encoding names for U+00A0.
func TestResolveGlyphRune_NBSpace(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"nbspace", "nonbreakingspace"} {
		if r, ok := resolveGlyphRune(name, false, false); !ok || r != 0x00A0 {
			t.Errorf("resolveGlyphRune(%q) = %U, %v; want U+00A0, true", name, r, ok)
		}
	}
	if r, ok := builtinGlyphRune("nbspace", 0xCA, false); !ok || r != 0x00A0 {
		t.Errorf("builtinGlyphRune(nbspace) = %U, %v; want U+00A0, true", r, ok)
	}
}
