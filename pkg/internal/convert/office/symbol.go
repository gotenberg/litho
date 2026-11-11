package office

import (
	"strings"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/symbolfont"
)

// SymbolText returns the characters text stands for when it is written in
// family, a symbol font LibreOffice recodes (Symbol, Wingdings, Webdings
// and their kin, [symbolfont.Recode]): each code from 0x20 to 0xFF, or
// that code in the private use area at U+F000, as the character
// [SymbolChar] names, else the character LibreOffice's recoding table
// draws it as, U+FFFD when that one is private-use too. Other characters stay. It returns "" when family is no such
// font or text holds no symbol code.
//
// A run's glyphs keep the codes the source wrote, which a reader cannot
// name when they are private-use: the converters give the run this text
// as its ActualText (ISO 19005-2, 6.2.11.7.3; ISO 14289-2, 8.4.3).
func SymbolText(text, family string) string {
	if !symbolfont.Recodes(family) {
		return ""
	}
	fams := []string{family}
	var b strings.Builder
	changed := false
	for _, r := range text {
		code := r
		if code >= 0xf020 && code <= 0xf0ff {
			code -= 0xf000
		}
		if code < 0x20 || code > 0xff {
			b.WriteRune(r)
			continue
		}
		if u, ok := SymbolChar(code, family); ok {
			changed = true
			b.WriteRune(u)
			continue
		}
		draw, _, _ := symbolfont.Recode(string(r), fams)
		if draw == string(r) && !unicode.Is(unicode.Co, r) {
			b.WriteRune(r)
			continue
		}
		changed = true
		for _, d := range draw {
			if unicode.Is(unicode.Co, d) {
				d = unicode.ReplacementChar
			}
			b.WriteRune(d)
		}
	}
	if !changed {
		return ""
	}
	return b.String()
}

// symbolChars maps codes of the Symbol font, the font's byte, to the
// Unicode character they stand for (Adobe's Symbol encoding), for the
// characters list labels and bullets use.
var symbolChars = map[rune]rune{
	0xB7: '•', 0xA7: '♣', 0xA8: '♦', 0xA9: '♥', 0xAA: '♠', 0xD8: '¬', 0xAE: '→', 0xDE: '⇒', 0xB0: '°', 0x2D: '−', 0x2A: '∗',
}

// wingdingsChars maps Wingdings characters list labels use to Unicode.
var wingdingsChars = map[rune]rune{
	0xA7: '▪', 0xA8: '◻', 0x6E: '■', 0x6F: '□', 0x71: '❑', 0x75: '◆', 0x76: '❖', 0xD8: '➢', 0xFC: '✔', 0xFB: '✘',
	0x9F: '•', 0xA1: '○', 0x77: '⬥', 0xE0: '➔', 0xE8: '➔', 0x4A: '☺', 0x4C: '☹', 0xF0: '⇨',
}

// SymbolChar returns the Unicode character the code, a byte from 0x20 to
// 0xFF, stands for in the symbol font family (Symbol or Wingdings, 2 and
// 3), for the codes bullets and list labels use; false for another code
// or font.
func SymbolChar(code rune, family string) (rune, bool) {
	var table map[rune]rune
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "symbol":
		table = symbolChars
	case "wingdings", "wingdings 2", "wingdings 3":
		table = wingdingsChars
	}
	u, ok := table[code]
	return u, ok
}
