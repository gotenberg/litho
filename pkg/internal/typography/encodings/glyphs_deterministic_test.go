package encodings

import "testing"

// TestRuneToGlyphCanonical pins the reverse mapping for the runes that several
// glyph names share to the Adobe Glyph List primary name, guarding against the
// map-iteration nondeterminism that previously let an alias win.
func TestRuneToGlyphCanonical(t *testing.T) {
	cases := map[rune]string{
		0x00A9: "copyright",
		0x0130: "Idotaccent",
		0x20AC: "Euro",
	}
	for r, want := range cases {
		if got := RuneToGlyph(r); got != want {
			t.Errorf("RuneToGlyph(U+%04X) = %q, want %q", r, got, want)
		}
	}
}

// TestRuneToGlyphStable calls the reverse lookup repeatedly to confirm a stable
// result within a process (the sync.Once map is built deterministically).
func TestRuneToGlyphStable(t *testing.T) {
	first := RuneToGlyph(0x00A9)
	for range 1000 {
		if got := RuneToGlyph(0x00A9); got != first {
			t.Fatalf("RuneToGlyph(U+00A9) unstable: %q vs %q", got, first)
		}
	}
}
