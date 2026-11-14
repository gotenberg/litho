package omml

// variant is a style of the Mathematical Alphanumeric Symbols of Unicode
// (the Unicode Standard, 22.2, block U+1D400 to U+1D7FF): the forms a
// math run's sty and scr select (ECMA-376 Part 1, 22.1.2.111 and
// 22.1.2.94), as MathML's mathvariant names them.
type variant uint8

const (
	normal variant = iota
	bold
	italic
	boldItalic
	script
	boldScript
	fraktur
	boldFraktur
	doubleStruck
	sansSerif
	sansBold
	sansItalic
	sansBoldItalic
	monospace
)

// latinStart is the code point of the capital A of each variant's Latin
// letters, 26 capitals then 26 small letters; 0 for none.
var latinStart = [...]rune{
	bold: 0x1D400, italic: 0x1D434, boldItalic: 0x1D468, script: 0x1D49C, boldScript: 0x1D4D0,
	fraktur: 0x1D504, boldFraktur: 0x1D56C, doubleStruck: 0x1D538, sansSerif: 0x1D5A0,
	sansBold: 0x1D5D4, sansItalic: 0x1D608, sansBoldItalic: 0x1D63C, monospace: 0x1D670,
}

// greekStart is the code point of the capital Alpha of each variant's
// Greek letters, in the order [greekIndex] gives; 0 for none.
var greekStart = [...]rune{
	bold: 0x1D6A8, italic: 0x1D6E2, boldItalic: 0x1D71C, sansBold: 0x1D756, sansBoldItalic: 0x1D790,
	boldScript: 0x1D6A8, boldFraktur: 0x1D6A8,
}

// digitStart is the code point of the digit zero of each variant's
// digits; 0 for none.
var digitStart = [...]rune{
	bold: 0x1D7CE, boldItalic: 0x1D7CE, boldScript: 0x1D7CE, boldFraktur: 0x1D7CE,
	doubleStruck: 0x1D7D8, sansSerif: 0x1D7E2, sansItalic: 0x1D7E2, sansBold: 0x1D7EC,
	sansBoldItalic: 0x1D7EC, monospace: 0x1D7F6,
}

// letterlike holds the letters of the Mathematical Alphanumeric Symbols
// block that Unicode encodes in Letterlike Symbols, and leaves reserved
// in the block (the Unicode Standard, 22.2, "Mathematical Alphanumeric
// Symbols"): by variant and letter.
var letterlike = map[variant]map[rune]rune{
	italic: {'h': 0x210E},
	script: {
		'B': 0x212C, 'E': 0x2130, 'F': 0x2131, 'H': 0x210B, 'I': 0x2110, 'L': 0x2112, 'M': 0x2133,
		'R': 0x211B, 'e': 0x212F, 'g': 0x210A, 'o': 0x2134,
	},
	fraktur:      {'C': 0x212D, 'H': 0x210C, 'I': 0x2111, 'R': 0x211C, 'Z': 0x2128},
	doubleStruck: {'C': 0x2102, 'H': 0x210D, 'N': 0x2115, 'P': 0x2119, 'Q': 0x211A, 'R': 0x211D, 'Z': 0x2124},
}

// greekIndex returns the position of r among the 58 Greek letters and
// symbols each Greek variant encodes in order: the capitals Alpha to
// Omega with capital theta symbol in the place of U+03A2, nabla, the
// small letters alpha to omega, then the partial differential and the
// lunate epsilon, theta, kappa, phi, rho and pi symbols.
func greekIndex(r rune) (rune, bool) {
	switch {
	case r >= 0x391 && r <= 0x3A9 && r != 0x3A2:
		return r - 0x391, true
	case r == 0x3F4:
		return 17, true
	case r == 0x2207:
		return 25, true
	case r >= 0x3B1 && r <= 0x3C9:
		return 26 + (r - 0x3B1), true
	}
	for i, s := range [...]rune{0x2202, 0x3F5, 0x3D1, 0x3F0, 0x3D5, 0x3F1, 0x3D6} {
		if r == s {
			return 51 + rune(i), true // fits rune: i is at most 6
		}
	}
	return 0, false
}

// styled returns r in variant v: its mathematical alphanumeric form, or
// r itself when v has none for it.
func styled(r rune, v variant) rune {
	switch {
	case v == normal:
		return r
	case r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z':
		if c, ok := letterlike[v][r]; ok {
			return c
		}
		if latinStart[v] == 0 {
			return r
		}
		if r >= 'a' {
			return latinStart[v] + 26 + (r - 'a')
		}
		return latinStart[v] + (r - 'A')
	case r >= '0' && r <= '9':
		if digitStart[v] == 0 {
			return r
		}
		return digitStart[v] + (r - '0')
	case v == italic && r == 0x131:
		return 0x1D6A4
	case v == italic && r == 0x237:
		return 0x1D6A5
	}
	if i, ok := greekIndex(r); ok && greekStart[v] != 0 {
		return greekStart[v] + i
	}
	return r
}

// variantOf returns the variant of a run whose sty is sty and scr is scr
// (ECMA-376 Part 1, 22.1.2.111 and 22.1.2.94), and whether letters take
// the italic form by default: sty absent, a roman, sans-serif or
// monospace run sets its letters as math-auto does, italic, and its
// digits upright.
func variantOf(sty, scr string) variant {
	b := sty == "b" || sty == "bi"
	i := sty == "i" || sty == "bi" || sty == ""
	switch scr {
	case "script":
		if b {
			return boldScript
		}
		return script
	case "fraktur":
		if b {
			return boldFraktur
		}
		return fraktur
	case "double-struck":
		return doubleStruck
	case "sans-serif":
		switch {
		case b && i:
			return sansBoldItalic
		case b:
			return sansBold
		case i:
			return sansItalic
		}
		return sansSerif
	case "monospace":
		return monospace
	}
	switch {
	case b && i:
		return boldItalic
	case b:
		return bold
	case i:
		return italic
	}
	return normal
}
