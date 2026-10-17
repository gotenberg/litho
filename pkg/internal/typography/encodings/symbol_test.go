package encodings

import "testing"

// TestSymbolTablesAgreeWithAGL checks every Symbol and ZapfDingbats glyph name
// against the Adobe Glyph List. ISO 32000-1, Annex D.5 and D.6 name the glyphs
// of the two fonts, and the AGL (glyphlist.txt, zapfdingbats.txt) gives each
// name its Unicode value. A table entry that disagrees is a typo: copyrightsans
// read U+F6D8 (dieresisgrave) instead of U+F8E9, and a165 read U+2192, the
// value of a161, instead of U+2799.
//
// Delta, Omega and mu carry two AGL values, a symbol and a Greek letter, and
// the Symbol font draws the Greek letter, so the table keeps the second one.
func TestSymbolTablesAgreeWithAGL(t *testing.T) {
	greek := map[string]rune{"Delta": 0x0394, "Omega": 0x03A9, "mu": 0x03BC}
	for table, m := range map[string]map[string]rune{
		"SymbolGlyphsUnicode":   SymbolGlyphsUnicode,
		"DingbatsGlyphsUnicode": DingbatsGlyphsUnicode,
	} {
		for name, got := range m {
			if want, ok := greek[name]; ok && table == "SymbolGlyphsUnicode" {
				if got != want {
					t.Errorf("%s[%q] = U+%04X, want the Greek U+%04X", table, name, got, want)
				}
				continue
			}
			want, ok := extendedAGL[name]
			if !ok {
				want, ok = GlyphToRune[name]
			}
			if ok && got != want {
				t.Errorf("%s[%q] = U+%04X, the AGL gives U+%04X", table, name, got, want)
			}
		}
	}
	if got := BuildSymbolEncoding()[0xE3]; got != 0xF8E9 {
		t.Errorf("Symbol code 0xE3 (copyrightsans) = U+%04X, want U+F8E9", got)
	}
	if got := BuildZapfDingbatsEncoding()[0xD9]; got != 0x2799 {
		t.Errorf("ZapfDingbats code 0xD9 (a165) = U+%04X, want U+2799", got)
	}
}
