package pptx

import (
	"strings"
	"unicode/utf8"
)

// symbolFace is the face a bullet of a symbol font draws in once its
// character is mapped to Unicode: LibreOffice's OpenSymbol, which Impress
// draws Wingdings and Symbol bullets with.
const symbolFace = "OpenSymbol"

// symbolBullets maps the characters of the symbol fonts bullets use to
// their Unicode equivalents, by font: the code points of the font's own
// encoding (0x20 to 0xFF, also written in the private use area at
// U+F020 to U+F0FF), as LibreOffice's symbol font conversion maps them.
var symbolBullets = map[string]map[rune]rune{
	"wingdings": {
		0x6C: 0x25CF, 0x6E: 0x25A0, 0x6F: 0x25A1, 0x70: 0x25FB, 0x71: 0x2751, 0x72: 0x2752,
		0x73: 0x2B27, 0x74: 0x29EB, 0x75: 0x25C6, 0x76: 0x2756, 0x77: 0x2B25, 0x9F: 0x2022,
		0xA1: 0x25CB, 0xA7: 0x25AA, 0xA8: 0x25FB, 0xD8: 0x27A2, 0xDF: 0x2190, 0xE0: 0x2192,
		0xE8: 0x2794, 0xF0: 0x21E8, 0xFB: 0x2717, 0xFC: 0x2714, 0xFE: 0x2611, 0x4A: 0x263A,
		0xAB: 0x2605, 0x46: 0x261E,
	},
	"symbol": {
		0xB7: 0x2022, 0xA8: 0x2666, 0xA7: 0x2663, 0xA9: 0x2665, 0xAA: 0x2660, 0xAE: 0x2192,
		0xDE: 0x21D2, 0x2D: 0x2212, 0xB0: 0x00B0, 0xD7: 0x22C5, 0xE0: 0x25CA,
	},
}

// symbolBullet maps a bullet character of a symbol font face to Unicode
// and the face it then draws in, false when face is no symbol font or the
// character none the table maps.
func symbolBullet(char, face string) (string, string, bool) {
	r, size := utf8.DecodeRuneInString(char)
	if size == 0 || size != len(char) {
		return "", "", false
	}
	if r >= 0xF020 && r <= 0xF0FF {
		r -= 0xF000
	}
	key := strings.ToLower(strings.TrimSpace(face))
	if key == "wingdings 2" || key == "wingdings 3" || key == "webdings" {
		return "", "", false
	}
	table, ok := symbolBullets[key]
	if !ok {
		return "", "", false
	}
	u, ok := table[r]
	if !ok {
		return "", "", false
	}
	return string(u), symbolFace, true
}
