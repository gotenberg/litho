package symbolfont

import "strings"

// Recodes reports whether family is a symbol font whose codes [Recode]
// recodes through LibreOffice's tables (fontcvt.cxx), as opposed to
// OpenSymbol, StarSymbol or a text font, whose characters stay.
func Recodes(family string) bool {
	_, ok := symbolFonts[strings.ReplaceAll(strings.ToLower(strings.TrimSpace(family)), " ", "")]
	return ok
}

// Lookup returns the OpenSymbol code point LibreOffice's recoding table
// gives code in the symbol font family (fontcvt.cxx): code is a code from
// 0x20 to 0xFF, or that code in the private use area at U+F000. It is
// false for a family that is no symbol font [Recodes] knows, a code out of
// that range, or a code the table leaves out.
func Lookup(family string, code rune) (rune, bool) {
	table, ok := symbolFonts[strings.ReplaceAll(strings.ToLower(strings.TrimSpace(family)), " ", "")]
	if !ok {
		return 0, false
	}
	if code >= 0xf020 && code <= 0xf0ff {
		code -= 0xf000
	}
	if code < 0x20 || code > 0xff || table[code-0x20] == 0 {
		return 0, false
	}
	return table[code-0x20], true
}
