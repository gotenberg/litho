package encodings

import (
	"maps"
	"sort"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"
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
	// AGL spacing accent names reachable only through /Differences or /CharSet
	// (absent from the WinAnsi base 256, so not added with the Latin-1 range
	// above). Values per the Adobe Glyph List (glyphlist.txt).
	"breve":          0x02D8,
	"dotaccent":      0x02D9,
	"ring":           0x02DA,
	"ogonek":         0x02DB,
	"caron":          0x02C7,
	"hungarumlaut":   0x02DD,
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

	// Central/Eastern European, Turkish, and miscellaneous (Adobe Glyph List)
	"lslash":       0x0142, // ł
	"Lslash":       0x0141, // Ł
	"dotlessi":     0x0131, // ı (Turkish dotless i)
	"Idotaccent":   0x0130, // İ (Turkish capital I with dot above; canonical AGL name)
	"Dcroat":       0x0110, // Đ
	"dcroat":       0x0111, // đ
	"minus":        0x2212, // − (math minus sign)
	"fraction":     0x2044, // ⁄ (fraction slash)
	"Euro":         0x20AC, // €
	"Hbar":         0x0126, // Ħ
	"hbar":         0x0127, // ħ
	"Eng":          0x014A, // Ŋ
	"eng":          0x014B, // ŋ
	"Tbar":         0x0166, // Ŧ
	"tbar":         0x0167, // ŧ
	"IJ":           0x0132, // Ĳ
	"ij":           0x0133, // ĳ
	"napostrophe":  0x0149, // ŉ
	"kgreenlandic": 0x0138, // ĸ
	"Ohorn":        0x01A0, // Ơ
	"ohorn":        0x01A1, // ơ
	"Uhorn":        0x01AF, // Ư
	"uhorn":        0x01B0, // ư

	// Additional common names
	"dotlessj":     0x0237, // ȷ (dotless j)
	"Idot":         0x0130, // İ (Turkish dotted I)
	"circlecopyrt": 0x00A9, // © (copyright sign; TeX name)

	// Math symbols and Greek letters are NOT included here. They live
	// in the Symbol font table (SymbolGlyphsUnicode). Adding them
	// globally causes VeraPDF compliance regressions on test files that
	// verify Unicode mapping completeness (6-3-8, 6-2-11-7-2).
}

// runeToGlyph is the reverse mapping, built lazily and thread-safely.
var (
	runeToGlyph     map[rune]string
	runeToGlyphOnce sync.Once
)

// canonicalGlyphForRune pins the reverse mapping for the runes that more than
// one name in [GlyphToRune] shares, to the Adobe Glyph List primary name. Without
// it the reverse map's winner would depend on map iteration order (nondeterministic)
// and could resolve to a non-standard alias (e.g. "circlecopyrt") that an embedded
// font's charset does not contain, producing a spurious "glyph not present" report.
var canonicalGlyphForRune = map[rune]string{
	0x00A9: "copyright",  // not circlecopyrt (a TeX name)
	0x0130: "Idotaccent", // not Idot
	0x20AC: "Euro",       // not euro
}

// RuneToGlyph returns the Adobe standard glyph name for a given rune.
// If no mapping exists, it returns the Unicode name form "uniXXXX".
func RuneToGlyph(r rune) string {
	runeToGlyphOnce.Do(func() {
		runeToGlyph = make(map[rune]string, len(GlyphToRune))
		// Build deterministically: iterate names in sorted order and keep the
		// first winner per rune, so a rune shared by several names always
		// resolves to the same one regardless of map iteration order.
		names := make([]string, 0, len(GlyphToRune))
		for name := range GlyphToRune {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if _, exists := runeToGlyph[GlyphToRune[name]]; !exists {
				runeToGlyph[GlyphToRune[name]] = name
			}
		}
		// Pin the Adobe Glyph List primary name for the shared runes.
		maps.Copy(runeToGlyph, canonicalGlyphForRune)
	})
	if name, ok := runeToGlyph[r]; ok {
		return name
	}
	// Fallback: produce "uniXXXX" form.
	return "uni" + string(hexDigits[(r>>12)&0xF]) + string(hexDigits[(r>>8)&0xF]) + string(hexDigits[(r>>4)&0xF]) + string(hexDigits[r&0xF])
}

var hexDigits = [16]byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'A', 'B', 'C', 'D', 'E', 'F'}

// texMathGlyphs maps TeX-specific glyph names (not in the Adobe Glyph
// List) to the Unicode codepoints producers use when embedding TeX
// fonts. Names follow the conventions used by AMS fonts (MSBM, MSAM,
// CMSY, CMMI) and by pdfTeX's bitmapped math symbol fonts. Without
// these entries a /FontFile /Encoding vector that references, for
// example, /planckover2pi1 would fall through and leave the byte
// decoded as the WinAnsi default character at its slot.
var texMathGlyphs = map[string]rune{
	"planckover2pi1": 0x210F, // ℏ reduced Planck constant (ℏ)
	"planck":         0x210F,
	"hbar":           0x210F,
	"Reals":          0x211D, // ℝ
	"Integers":       0x2124, // ℤ
	"Naturals":       0x2115, // ℕ
	"Rationals":      0x211A, // ℚ
	"Complexes":      0x2102, // ℂ
	"lessequal":      0x2264,
	"greaterequal":   0x2265,

	// Standard AGL math-symbol names not in [extendedAGL] (the generator
	// dropped Greek/math entries to keep the compliance tables clean).
	// TeX CMSY/CMMI/MSAM/MSBM fonts rely on them via the /Encoding vector
	// in their /FontFile preambles. /negationslash is intentionally
	// absent: the AGL carries no entry for it, and falling back to the
	// byte-code ASCII character is the better behaviour for TeX fonts
	// that rely on it to compose negated relations.
	"nabla":           0x2207, // ∇
	"openbullet":      0x25E6, // ◦
	"arrowdblleft":    0x21D0, // ⇐
	"arrowdblright":   0x21D2, // ⇒
	"arrowdblboth":    0x21D4, // ⇔
	"arrowdblup":      0x21D1, // ⇑
	"arrowdbldown":    0x21D3, // ⇓
	"angbracketleft":  0x3008, // 〈
	"angbracketright": 0x3009, // 〉
	// AGL distinguishes /Delta → U+2206 INCREMENT from /Deltagreek →
	// U+0394. CMMI/CMEX/MSAM math fonts use /Delta for the increment
	// symbol; Symbol-font resolution still wins for explicitly Symbol-
	// keyed fonts because it checks [SymbolGlyphsUnicode] first.
	"Delta": 0x2206, // ∆
}

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
	if r, ok := texMathGlyphs[name]; ok {
		return r, true
	}
	// "uniXXXX": 4 hex digits after "uni".
	if len(name) == 7 && name[:3] == "uni" {
		if r, err := strconv.ParseUint(name[3:], 16, 32); err == nil {
			return rune(r), true //nolint:gosec // value constrained by 32-bit parse
		}
	}
	// "uXXXXXXXX": variable hex after "u" (Unicode scalar).
	if len(name) >= 5 && len(name) <= 9 && name[0] == 'u' && name[1] != 'n' {
		if r, err := strconv.ParseUint(name[1:], 16, 32); err == nil {
			return rune(r), true //nolint:gosec // value constrained by 32-bit parse
		}
	}
	// Non-standard glyph name patterns used by some PDF generators:
	// "Gxx": 2 hex digits (e.g., "G4F" = U+004F).
	if len(name) == 3 && name[0] == 'G' {
		if r, err := strconv.ParseUint(name[1:], 16, 32); err == nil && r > 0 {
			return rune(r), true //nolint:gosec // value constrained by 32-bit parse
		}
	}
	// "g00xx": 4 hex digits after "g" (e.g., "g0053" = U+0053).
	if len(name) == 5 && name[0] == 'g' {
		if r, err := strconv.ParseUint(name[1:], 16, 32); err == nil && r > 0 {
			return rune(r), true //nolint:gosec // value constrained by 32-bit parse
		}
	}
	// "Cdd" / "Cddd": decimal charcode (e.g., "C141" = rune(141)).
	// Reject C0/C1 control ranges (1-31, 127-159) since those are never
	// valid glyph Unicode values and would override real base encodings.
	if len(name) >= 3 && len(name) <= 4 && (name[0] == 'C' || name[0] == 'c') {
		if r, err := strconv.Atoi(name[1:]); err == nil && r > 0 && r <= 0xFFFF {
			if r >= 0x20 && r != 0x7F && (r < 0x80 || r > 0x9F) {
				return rune(r), true
			}
		}
	}
	// "Cxx": hex digits after C/c, only when the suffix contains at least
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
	// "iSQPXXXX": 4 hex digits after "iSQP" prefix (e.g., "iSQP0041" = U+0041 = 'A').
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
	// Try decomposing "<base><modifier>" names like "ccaron" → c + caron → č.
	// The base is a known glyph and the modifier is a combining diacritical.
	if r, ok := resolveBaseModifier(name); ok {
		return r, true
	}
	return 0, false
}

// diacriticalModifiers maps modifier suffixes to their Unicode combining
// diacritical marks. Used by resolveBaseModifier to decompose glyph names
// like "ccaron" → c + combining caron → č (after NFC normalization).
var diacriticalModifiers = map[string]rune{
	"acute":        0x0301, // combining acute accent
	"breve":        0x0306, // combining breve
	"caron":        0x030C, // combining caron (háček)
	"cedilla":      0x0327, // combining cedilla
	"circumflex":   0x0302, // combining circumflex
	"dieresis":     0x0308, // combining diaeresis (umlaut)
	"dotaccent":    0x0307, // combining dot above
	"grave":        0x0300, // combining grave accent
	"macron":       0x0304, // combining macron
	"ogonek":       0x0328, // combining ogonek
	"ring":         0x030A, // combining ring above
	"tilde":        0x0303, // combining tilde
	"hungarumlaut": 0x030B, // combining double acute accent
	"commaaccent":  0x0326, // combining comma below
	"stroke":       0x0335, // combining short stroke overlay
}

// resolveBaseModifier tries to decompose a glyph name into a base letter
// plus a diacritical modifier. For example, "ccaron" → 'c' + combining caron
// → NFC normalized to 'č' (U+010D).
func resolveBaseModifier(name string) (rune, bool) {
	// Try each known modifier suffix, longest first isn't needed since
	// modifiers don't overlap in practice.
	for suffix, combiner := range diacriticalModifiers {
		if !strings.HasSuffix(strings.ToLower(name), suffix) {
			continue
		}
		baseName := name[:len(name)-len(suffix)]
		if len(baseName) == 0 {
			continue
		}
		// Resolve the base letter.
		var baseRune rune
		if len(baseName) == 1 {
			baseRune = rune(baseName[0])
		} else {
			r, ok := GlyphToRuneSimple(baseName)
			if !ok {
				continue
			}
			baseRune = r
		}
		// Compose base + combining diacritical and normalize to NFC.
		composed := norm.NFC.String(string(baseRune) + string(combiner))
		runes := []rune(composed)
		if len(runes) == 1 {
			return runes[0], true
		}
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
