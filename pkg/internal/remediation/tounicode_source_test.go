package remediation

import "testing"

// TestIsValidUnicodeMapping_Rejects pins that a generated /ToUnicode entry
// never claims a glyph means a control character, a surrogate half, or a
// noncharacter.
//
// A symbolic subset font numbers its glyphs from 1, so inverting its (1,0)
// Macintosh cmap yields glyph 1 = U+0001. Written into /ToUnicode that claim
// wins over the font's own encoding, and readable text extracts as control
// bytes: "La prévention du suicide" came back as "\x01\x02\x03\x04...".
func TestIsValidUnicodeMapping_Rejects(t *testing.T) {
	rejected := []struct {
		r    rune
		what string
	}{
		{0x0000, "NUL"},
		{0x0001, "C0 control"},
		{0x001F, "last C0 control"},
		{0x007F, "DEL"},
		{0x009F, "last C1 control"},
		{0xD800, "high surrogate"},
		{0xDFFF, "low surrogate"},
		{0xFDD0, "noncharacter"},
		{0xFFFE, "noncharacter"},
		{0xFFFF, "noncharacter"},
		{0x1FFFE, "plane 1 noncharacter"},
		{0xFEFF, "BOM"},
		{0xE000, "private use"},
	}
	for _, c := range rejected {
		if isValidUnicodeMapping(c.r) {
			t.Errorf("isValidUnicodeMapping(U+%04X) = true, want false (%s)", c.r, c.what)
		}
	}

	accepted := []rune{0x0020, 0x0041, 0x00E9, 0x0400, 0x4E2D, 0x10000, 0x1F600}
	for _, r := range accepted {
		if !isValidUnicodeMapping(r) {
			t.Errorf("isValidUnicodeMapping(U+%04X) = false, want true", r)
		}
	}
}

// TestGenerateToUnicodeCMap_EmptyWritesNothing pins that a mapping left empty
// by the validity filter produces no CMap at all. Writing no /ToUnicode leaves
// the font's own encoding in place, which is what extraction was already using
// correctly; writing an empty or bogus one replaces a working path.
func TestGenerateToUnicodeCMap_EmptyWritesNothing(t *testing.T) {
	if got := generateToUnicodeCMap(map[uint16]rune{}, 1); got != nil {
		t.Errorf("empty mapping produced a CMap of %d bytes, want none", len(got))
	}
	// The shape a symbolic subset produces: every code maps to itself, and
	// every destination is a control character. Filtered the way the fixer
	// filters, nothing survives, so nothing is written.
	identityGarbage := map[uint16]rune{}
	for code := uint16(1); code < 0x20; code++ {
		if isValidUnicodeMapping(rune(code)) {
			identityGarbage[code] = rune(code)
		}
	}
	if got := generateToUnicodeCMap(identityGarbage, 1); got != nil {
		t.Errorf("identity control-character mapping produced a CMap of %d bytes, want none", len(got))
	}
}
