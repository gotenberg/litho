package typography

import (
	"testing"

	"golang.org/x/text/unicode/norm"
)

// TestResolveGlyphRune_OmegaReadsGreekCapital pins the reading of "Omega" as
// U+03A9, the canonical decomposition of the Adobe Glyph List's U+2126 OHM
// SIGN, in a plain and a Symbol font and through a built-in encoding.
func TestResolveGlyphRune_OmegaReadsGreekCapital(t *testing.T) {
	t.Parallel()
	for _, isSymbol := range []bool{false, true} {
		if r, ok := resolveGlyphRune("Omega", false, isSymbol); !ok || r != 0x03A9 {
			t.Errorf("resolveGlyphRune(Omega, symbol=%v) = %U, %v; want U+03A9, true", isSymbol, r, ok)
		}
	}
	if r, ok := builtinGlyphRune("Omega", 0x0A, false); !ok || r != 0x03A9 {
		t.Errorf("builtinGlyphRune(Omega) = %U, %v; want U+03A9, true", r, ok)
	}
	if got := norm.NFC.String("Ω"); got != "Ω" {
		t.Errorf("NFC(U+2126) = %q; want U+03A9, the reading's ground", got)
	}
	if got := normalizeExtractionUnicode("Ω"); got != "Ω" {
		t.Errorf("normalizeExtractionUnicode(U+2126) = %q; want U+03A9", got)
	}
}

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
