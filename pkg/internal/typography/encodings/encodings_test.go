package encodings

import "testing"

func TestStandardEncoding_ASCIIRange(t *testing.T) {
	// Standard ASCII letters and digits should match.
	for b := byte(0x41); b <= 0x5A; b++ {
		if r, ok := StandardEncoding[b]; !ok || r != rune(b) {
			t.Errorf("StandardEncoding[0x%02X] = %U, want %U", b, r, rune(b))
		}
	}
	for b := byte(0x61); b <= 0x7A; b++ {
		if r, ok := StandardEncoding[b]; !ok || r != rune(b) {
			t.Errorf("StandardEncoding[0x%02X] = %U, want %U", b, r, rune(b))
		}
	}
}

func TestStandardEncoding_DiffersFromWinAnsi(t *testing.T) {
	// 0x27 is quoteright (U+2019) in StandardEncoding, apostrophe (U+0027) in WinAnsi.
	if r := StandardEncoding[0x27]; r != 0x2019 {
		t.Errorf("StandardEncoding[0x27] = %U, want U+2019 (quoteright)", r)
	}
	if r := WinAnsiEncoding[0x27]; r != '\'' {
		t.Errorf("WinAnsiEncoding[0x27] = %U, want U+0027 (apostrophe)", r)
	}

	// 0x60 is quoteleft (U+2018) in StandardEncoding, grave accent (U+0060) in WinAnsi.
	if r := StandardEncoding[0x60]; r != 0x2018 {
		t.Errorf("StandardEncoding[0x60] = %U, want U+2018 (quoteleft)", r)
	}
	if r := WinAnsiEncoding[0x60]; r != '`' {
		t.Errorf("WinAnsiEncoding[0x60] = %U, want U+0060 (grave)", r)
	}
}

func TestStandardEncoding_UpperRange(t *testing.T) {
	tests := []struct {
		code byte
		want rune
		name string
	}{
		{0xA1, 0x00A1, "exclamdown"},
		{0xA4, 0x2044, "fraction"},
		{0xAE, 0xFB01, "fi"},
		{0xAF, 0xFB02, "fl"},
		{0xB1, 0x2013, "endash"},
		{0xD0, 0x2014, "emdash"},
		{0xE1, 0x00C6, "AE"},
		{0xF1, 0x00E6, "ae"},
		{0xFB, 0x00DF, "germandbls"},
	}
	for _, tc := range tests {
		r, ok := StandardEncoding[tc.code]
		if !ok {
			t.Errorf("StandardEncoding[0x%02X] (%s) missing", tc.code, tc.name)
		} else if r != tc.want {
			t.Errorf("StandardEncoding[0x%02X] (%s) = %U, want %U", tc.code, tc.name, r, tc.want)
		}
	}
}

func TestMacExpertEncoding_OldStyleFigures(t *testing.T) {
	// Old-style figures 0-9 should map to PUA range 0xF730-0xF739.
	for i := byte(0); i <= 9; i++ {
		code := 0x30 + i
		want := rune(0xF730 + int(i))
		if r, ok := MacExpertEncoding[code]; !ok || r != want {
			t.Errorf("MacExpertEncoding[0x%02X] = %U, want %U", code, r, want)
		}
	}
}

func TestMacExpertEncoding_SmallCaps(t *testing.T) {
	// Small caps A-Z should be at 0x61-0x7A, mapping to PUA 0xF761-0xF77A.
	for i := range byte(26) {
		code := 0x61 + i
		want := rune(0xF761 + int(i))
		if r, ok := MacExpertEncoding[code]; !ok || r != want {
			t.Errorf("MacExpertEncoding[0x%02X] = %U, want %U", code, r, want)
		}
	}
}

func TestMacExpertEncoding_Ligatures(t *testing.T) {
	tests := []struct {
		code byte
		want rune
		name string
	}{
		{0x56, 0xFB00, "ff"},
		{0x57, 0xFB01, "fi"},
		{0x58, 0xFB02, "fl"},
		{0x59, 0xFB03, "ffi"},
		{0x5A, 0xFB04, "ffl"},
	}
	for _, tc := range tests {
		if r, ok := MacExpertEncoding[tc.code]; !ok || r != tc.want {
			t.Errorf("MacExpertEncoding[0x%02X] (%s) = %U, want %U", tc.code, tc.name, r, tc.want)
		}
	}
}

// TestGlyphToRuneFull_SpacingAccents locks resolution of the AGL spacing
// accent names that appear in Type1 /Differences and /CharSet on real
// documents. They are absent from the WinAnsi base 256, so an incomplete
// table caused ToUnicode-exemption false positives. Values per glyphlist.txt.
func TestGlyphToRuneFull_SpacingAccents(t *testing.T) {
	want := map[string]rune{
		"breve":        0x02D8,
		"caron":        0x02C7,
		"dotaccent":    0x02D9,
		"hungarumlaut": 0x02DD,
		"ogonek":       0x02DB,
		"ring":         0x02DA,
	}
	for name, r := range want {
		got, ok := GlyphToRuneFull(name)
		if !ok || got != r {
			t.Errorf("GlyphToRuneFull(%q) = %U, %v; want %U, true", name, got, ok, r)
		}
	}
}

// TestGlyphToRuneFull_TeXMathNames locks the cmmi and cmsy glyph names that
// only TeX Live's texglyphlist.txt defines, at the values it gives them, and
// keeps them out of the Adobe Glyph List the compliance rules read.
func TestGlyphToRuneFull_TeXMathNames(t *testing.T) {
	want := map[string]rune{
		"lscript":     0x2113,
		"epsilon1":    0x03F5,
		"pi1":         0x03D6,
		"rho1":        0x03F1,
		"star":        0x22C6,
		"Rfractur":    0x211C,
		"floorleft":   0x230A,
		"ceilingleft": 0x2308,
		"coproduct":   0x2A3F,
	}
	for name, r := range want {
		got, ok := GlyphToRuneFull(name)
		if !ok || got != r {
			t.Errorf("GlyphToRuneFull(%q) = %U, %v; want %U, true", name, got, ok, r)
		}
		if InAdobeGlyphList(name) {
			t.Errorf("InAdobeGlyphList(%q) = true, want false", name)
		}
	}
	for _, name := range []string{"vector", "negationslash", "tie", "mapsto"} {
		if r, ok := GlyphToRuneFull(name); ok {
			t.Errorf("GlyphToRuneFull(%q) = %U, true; want no value", name, r)
		}
	}
}
