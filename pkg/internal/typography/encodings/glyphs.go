package encodings

import (
	"strconv"
	"strings"
	"sync"
)

// GlyphToRune maps Adobe standard glyph names to UTF-8 runes.
// This is a subset containing common Latin characters.
var GlyphToRune = map[string]rune{
	"space":          ' ',
	"exclam":         '!',
	"quotedbl":       '"',
	"numbersign":     '#',
	"dollar":         '$',
	"percent":        '%',
	"ampersand":      '&',
	"quotesingle":    '\'',
	"parenleft":      '(',
	"parenright":     ')',
	"asterisk":       '*',
	"plus":           '+',
	"comma":          ',',
	"hyphen":         '-',
	"period":         '.',
	"slash":          '/',
	"zero":           '0',
	"one":            '1',
	"two":            '2',
	"three":          '3',
	"four":           '4',
	"five":           '5',
	"six":            '6',
	"seven":          '7',
	"eight":          '8',
	"nine":           '9',
	"colon":          ':',
	"semicolon":      ';',
	"less":           '<',
	"equal":          '=',
	"greater":        '>',
	"question":       '?',
	"at":             '@',
	"A":              'A',
	"B":              'B',
	"C":              'C',
	"D":              'D',
	"E":              'E',
	"F":              'F',
	"G":              'G',
	"H":              'H',
	"I":              'I',
	"J":              'J',
	"K":              'K',
	"L":              'L',
	"M":              'M',
	"N":              'N',
	"O":              'O',
	"P":              'P',
	"Q":              'Q',
	"R":              'R',
	"S":              'S',
	"T":              'T',
	"U":              'U',
	"V":              'V',
	"W":              'W',
	"X":              'X',
	"Y":              'Y',
	"Z":              'Z',
	"bracketleft":    '[',
	"backslash":      '\\',
	"bracketright":   ']',
	"asciicircum":    '^',
	"underscore":     '_',
	"grave":          '`',
	"a":              'a',
	"b":              'b',
	"c":              'c',
	"d":              'd',
	"e":              'e',
	"f":              'f',
	"g":              'g',
	"h":              'h',
	"i":              'i',
	"j":              'j',
	"k":              'k',
	"l":              'l',
	"m":              'm',
	"n":              'n',
	"o":              'o',
	"p":              'p',
	"q":              'q',
	"r":              'r',
	"s":              's',
	"t":              't',
	"u":              'u',
	"v":              'v',
	"w":              'w',
	"x":              'x',
	"y":              'y',
	"z":              'z',
	"braceleft":      '{',
	"bar":            '|',
	"braceright":     '}',
	"asciitilde":     '~',
	"euro":           0x20AC,
	"quotesinglbase": 0x201A,
	"florin":         0x0192,
	"quotedblbase":   0x201E,
	"ellipsis":       0x2026,
	"dagger":         0x2020,
	"daggerdbl":      0x2021,
	"circumflex":     0x02C6,
	"perthousand":    0x2030,
	"Scaron":         0x0160,
	"guilsinglleft":  0x2039,
	"OE":             0x0152,
	"Zcaron":         0x017D,
	"quoteleft":      0x2018,
	"quoteright":     0x2019,
	"quotedblleft":   0x201C,
	"quotedblright":  0x201D,
	"bullet":         0x2022,
	"endash":         0x2013,
	"emdash":         0x2014,
	"tilde":          0x02DC,
	"trademark":      0x2122,
	"scaron":         0x0161,
	"guilsinglright": 0x203A,
	"oe":             0x0153,
	"zcaron":         0x017E,
	"Ydieresis":      0x0178,
	"exclamdown":     0x00A1,
	"cent":           0x00A2,
	"sterling":       0x00A3,
	"currency":       0x00A4,
	"yen":            0x00A5,
	"brokenbar":      0x00A6,
	"section":        0x00A7,
	"dieresis":       0x00A8,
	"copyright":      0x00A9,
	"ordfeminine":    0x00AA,
	"guillemotleft":  0x00AB,
	"logicalnot":     0x00AC,
	"hyphenation":    0x00AD,
	"registered":     0x00AE,
	"macron":         0x00AF,
	"degree":         0x00B0,
	"plusminus":      0x00B1,
	"twosuperior":    0x00B2,
	"threesuperior":  0x00B3,
	"acute":          0x00B4,
	"mu":             0x00B5,
	"paragraph":      0x00B6,
	"periodcentered": 0x00B7,
	"cedilla":        0x00B8,
	"onesuperior":    0x00B9,
	"ordmasculine":   0x00BA,
	"guillemotright": 0x00BB,
	"onequarter":     0x00BC,
	"onehalf":        0x00BD,
	"threequarters":  0x00BE,
	"questiondown":   0x00BF,
	"Agrave":         0x00C0,
	"Aacute":         0x00C1,
	"Acircumflex":    0x00C2,
	"Atilde":         0x00C3,
	"Adieresis":      0x00C4,
	"Aring":          0x00C5,
	"AE":             0x00C6,
	"Ccedilla":       0x00C7,
	"Egrave":         0x00C8,
	"Eacute":         0x00C9,
	"Ecircumflex":    0x00CA,
	"Edieresis":      0x00CB,
	"Igrave":         0x00CC,
	"Iacute":         0x00CD,
	"Icircumflex":    0x00CE,
	"Idieresis":      0x00CF,
	"Eth":            0x00D0,
	"Ntilde":         0x00D1,
	"Ograve":         0x00D2,
	"Oacute":         0x00D3,
	"Ocircumflex":    0x00D4,
	"Otilde":         0x00D5,
	"Odieresis":      0x00D6,
	"multiply":       0x00D7,
	"Oslash":         0x00D8,
	"Ugrave":         0x00D9,
	"Uacute":         0x00DA,
	"Ucircumflex":    0x00DB,
	"Udieresis":      0x00DC,
	"Yacute":         0x00DD,
	"Thorn":          0x00DE,
	"germandbls":     0x00DF,
	"agrave":         0x00E0,
	"aacute":         0x00E1,
	"acircumflex":    0x00E2,
	"atilde":         0x00E3,
	"adieresis":      0x00E4,
	"aring":          0x00E5,
	"ae":             0x00E6,
	"ccedilla":       0x00E7,
	"egrave":         0x00E8,
	"eacute":         0x00E9,
	"ecircumflex":    0x00EA,
	"edieresis":      0x00EB,
	"igrave":         0x00EC,
	"iacute":         0x00ED,
	"icircumflex":    0x00EE,
	"idieresis":      0x00EF,
	"eth":            0x00F0,
	"ntilde":         0x00F1,
	"ograve":         0x00F2,
	"oacute":         0x00F3,
	"ocircumflex":    0x00F4,
	"otilde":         0x00F5,
	"odieresis":      0x00F6,
	"divide":         0x00F7,
	"oslash":         0x00F8,
	"ugrave":         0x00F9,
	"uacute":         0x00FA,
	"ucircumflex":    0x00FB,
	"udieresis":      0x00FC,
	"yacute":         0x00FD,
	"thorn":          0x00FE,
	"ydieresis":      0x00FF,

	// Standard ligatures (Adobe Glyph List)
	"fi":  0xFB01,
	"fl":  0xFB02,
	"ff":  0xFB00,
	"ffi": 0xFB03,
	"ffl": 0xFB04,
}

// runeToGlyph is the reverse mapping, built lazily and thread-safely.
var (
	runeToGlyph     map[rune]string
	runeToGlyphOnce sync.Once
)

// RuneToGlyph returns the Adobe standard glyph name for a given rune.
// If no mapping exists, it returns the Unicode name form "uniXXXX".
func RuneToGlyph(r rune) string {
	runeToGlyphOnce.Do(func() {
		runeToGlyph = make(map[rune]string, len(GlyphToRune))
		for name, ru := range GlyphToRune {
			runeToGlyph[ru] = name
		}
	})
	if name, ok := runeToGlyph[r]; ok {
		return name
	}
	// Fallback: produce "uniXXXX" form.
	return "uni" + string(hexDigits[(r>>12)&0xF]) + string(hexDigits[(r>>8)&0xF]) + string(hexDigits[(r>>4)&0xF]) + string(hexDigits[r&0xF])
}

var hexDigits = [16]byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'A', 'B', 'C', 'D', 'E', 'F'}

// GlyphToRuneFull resolves a glyph name to a Unicode rune using the core
// GlyphToRune map, the extended AGL, and the "uniXXXX" / "uXXXXXXXX" naming
// convention. Returns (rune, true) on success, (0, false) if unknown.
func GlyphToRuneFull(name string) (rune, bool) {
	if r, ok := GlyphToRune[name]; ok {
		return r, true
	}
	if r, ok := extendedAGL[name]; ok {
		return r, true
	}
	// "uniXXXX" — 4 hex digits after "uni"
	if len(name) == 7 && name[:3] == "uni" {
		if r, err := strconv.ParseUint(name[3:], 16, 32); err == nil {
			return rune(r), true //nolint:gosec // value constrained by 32-bit parse
		}
	}
	// "uXXXXXXXX" — variable hex after "u" (Unicode scalar)
	if len(name) >= 5 && len(name) <= 9 && name[0] == 'u' && name[1] != 'n' {
		if r, err := strconv.ParseUint(name[1:], 16, 32); err == nil {
			return rune(r), true //nolint:gosec // value constrained by 32-bit parse
		}
	}
	// Non-standard glyph name patterns used by some PDF generators:
	// "Gxx" — 2 hex digits (e.g., "G4F" → U+004F)
	if len(name) == 3 && name[0] == 'G' {
		if r, err := strconv.ParseUint(name[1:], 16, 32); err == nil && r > 0 {
			return rune(r), true //nolint:gosec // value constrained by 32-bit parse
		}
	}
	// "g00xx" — 4 hex digits after "g" (e.g., "g0053" → U+0053)
	if len(name) == 5 && name[0] == 'g' {
		if r, err := strconv.ParseUint(name[1:], 16, 32); err == nil && r > 0 {
			return rune(r), true //nolint:gosec // value constrained by 32-bit parse
		}
	}
	// "Cdd" / "Cddd" — decimal charcode (e.g., "C141" → rune(141)).
	// Reject C0/C1 control ranges (1-31, 127-159) since those are never
	// valid glyph Unicode values and would override real base encodings.
	if len(name) >= 3 && len(name) <= 4 && (name[0] == 'C' || name[0] == 'c') {
		if r, err := strconv.Atoi(name[1:]); err == nil && r > 0 && r <= 0xFFFF {
			if r >= 0x20 && r != 0x7F && (r < 0x80 || r > 0x9F) {
				return rune(r), true
			}
		}
	}
	// "Cxx" — hex digits after C/c, only when the suffix contains at least
	// one hex-only letter (a-f/A-F). Handles names like "C2c" (0x2C = ',')
	// and "C4b" (0x4B = 'K'). Pure-decimal names like "C128" are handled
	// by the decimal pattern above; hex fallback would misinterpret them.
	if len(name) >= 3 && len(name) <= 5 && (name[0] == 'C' || name[0] == 'c') && hasHexLetter(name[1:]) {
		if r, err := strconv.ParseUint(name[1:], 16, 32); err == nil && r >= 0x20 && r != 0x7F && r <= 0xFFFF {
			if r < 0x80 || r > 0x9F {
				return rune(r), true //nolint:gosec // value constrained by 32-bit parse
			}
		}
	}
	// "iSQPXXXX" — 4 hex digits after "iSQP" prefix (e.g., "iSQP0041" → U+0041 = 'A').
	// Used by some PDF generators as font-internal Unicode references.
	if len(name) == 8 && name[:4] == "iSQP" {
		if r, err := strconv.ParseUint(name[4:], 16, 32); err == nil && r > 0 {
			return rune(r), true //nolint:gosec // value constrained by 32-bit parse
		}
	}
	// Try RecoverGlyphName (strip suffixes like ".001", ".alt", etc.)
	recovered := RecoverGlyphName(name)
	if recovered != name {
		return GlyphToRuneSimple(recovered)
	}
	return 0, false
}

// hasHexLetter reports whether s contains at least one hex-only letter (a-f/A-F).
func hasHexLetter(s string) bool {
	for _, c := range s {
		if (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			return true
		}
	}
	return false
}

// GlyphToRuneSimple resolves a glyph name without RecoverGlyphName fallback.
func GlyphToRuneSimple(name string) (rune, bool) {
	if r, ok := GlyphToRune[name]; ok {
		return r, true
	}
	if r, ok := extendedAGL[name]; ok {
		return r, true
	}
	return 0, false
}

// RecoverGlyphName strips common suffixes from corrupted glyph names.
// For example, "A.001" → "A", "space.alt" → "space".
func RecoverGlyphName(name string) string {
	if idx := strings.IndexByte(name, '.'); idx > 0 {
		return name[:idx]
	}
	return name
}
