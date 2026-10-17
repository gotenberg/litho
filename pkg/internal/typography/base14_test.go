package typography

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

func TestNormalizeBase14Name(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Helvetica", "Helvetica"},
		{"ABCDEF+Helvetica", "Helvetica"},
		{"GHIJKL+Times-Roman", "Times-Roman"},
		{"Helvetica,Bold", "Helvetica-Bold"},
		{"ABCXYZ+Courier,BoldOblique", "Courier-BoldOblique"},
		{"NoPrefix", "NoPrefix"},
	}
	for _, tc := range tests {
		got := normalizeBase14Name(tc.input)
		if got != tc.want {
			t.Errorf("normalizeBase14Name(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestIsStandard14(t *testing.T) {
	standard := []string{
		"Courier", "Courier-Bold", "Courier-Oblique", "Courier-BoldOblique",
		"Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Helvetica-BoldOblique",
		"Times-Roman", "Times-Bold", "Times-Italic", "Times-BoldItalic",
		"Symbol", "ZapfDingbats",
	}
	for _, name := range standard {
		if !isStandard14(name) {
			t.Errorf("isStandard14(%q) = false, want true", name)
		}
	}

	nonStandard := []string{"Arial", "ComicSans", "CustomFont"}
	for _, name := range nonStandard {
		if isStandard14(name) {
			t.Errorf("isStandard14(%q) = true, want false", name)
		}
	}
}

func TestInjectStandard14Widths_Helvetica(t *testing.T) {
	font := &Font{
		Encoding:     copyEncoding(encodings.WinAnsiEncoding),
		MissingWidth: 1000,
	}

	injectStandard14Widths(font, "Helvetica")

	if len(font.Widths) != 256 {
		t.Fatalf("expected 256 widths, got %d", len(font.Widths))
	}
	if font.FirstChar != 0 || font.LastChar != 255 {
		t.Errorf("FirstChar=%d LastChar=%d, want 0/255", font.FirstChar, font.LastChar)
	}

	// Verify known character widths.
	// 'A' (char code 0x41=65) should be 667.
	if w := font.GlyphWidth(65); w != 667 {
		t.Errorf("Helvetica 'A' width: got %g, want 667", w)
	}
	// 'i' (char code 0x69=105) should be 222.
	if w := font.GlyphWidth(105); w != 222 {
		t.Errorf("Helvetica 'i' width: got %g, want 222", w)
	}
	// space (char code 0x20=32) should be 278.
	if w := font.GlyphWidth(32); w != 278 {
		t.Errorf("Helvetica space width: got %g, want 278", w)
	}
}

func TestInjectStandard14Widths_Courier(t *testing.T) {
	font := &Font{
		Encoding:     copyEncoding(encodings.WinAnsiEncoding),
		MissingWidth: 1000,
	}

	injectStandard14Widths(font, "Courier")

	if len(font.Widths) != 256 {
		t.Fatalf("expected 256 widths, got %d", len(font.Widths))
	}

	// All Courier widths should be 600.
	for code := range 256 {
		if w := font.Widths[code]; w != 600 {
			t.Errorf("Courier char %d: got %g, want 600", code, w)
			break
		}
	}
}

func TestInjectStandard14Widths_CourierBold(t *testing.T) {
	font := &Font{
		Encoding:     copyEncoding(encodings.WinAnsiEncoding),
		MissingWidth: 1000,
	}

	injectStandard14Widths(font, "Courier-Bold")

	// All Courier-Bold widths should be 600 (monospaced).
	if w := font.GlyphWidth(65); w != 600 {
		t.Errorf("Courier-Bold 'A' width: got %g, want 600", w)
	}
}

func TestInjectStandard14Widths_SubsetPrefix(t *testing.T) {
	font := &Font{
		Encoding:     copyEncoding(encodings.WinAnsiEncoding),
		MissingWidth: 1000,
	}

	injectStandard14Widths(font, "ABCDEF+Helvetica-Bold")

	if len(font.Widths) != 256 {
		t.Fatalf("expected 256 widths, got %d", len(font.Widths))
	}

	// Helvetica-Bold 'A' (code 65) should be 722.
	if w := font.GlyphWidth(65); w != 722 {
		t.Errorf("Helvetica-Bold 'A' width: got %g, want 722", w)
	}
}

func TestInjectStandard14Widths_NonStandard(t *testing.T) {
	font := &Font{
		Encoding:     copyEncoding(encodings.WinAnsiEncoding),
		MissingWidth: 1000,
	}

	injectStandard14Widths(font, "CustomFont")

	// Non-standard fonts now get Helvetica fallback widths.
	if len(font.Widths) != 256 {
		t.Errorf("expected Helvetica fallback widths (256), got %d", len(font.Widths))
	}
}

func TestInjectStandard14Widths_TimesRoman(t *testing.T) {
	font := &Font{
		Encoding:     copyEncoding(encodings.WinAnsiEncoding),
		MissingWidth: 1000,
	}

	injectStandard14Widths(font, "Times-Roman")

	// Times-Roman 'A' (code 65) should be 722.
	if w := font.GlyphWidth(65); w != 722 {
		t.Errorf("Times-Roman 'A' width: got %g, want 722", w)
	}
	// Times-Roman 'm' (code 109) should be 778.
	if w := font.GlyphWidth(109); w != 778 {
		t.Errorf("Times-Roman 'm' width: got %g, want 778", w)
	}
}

func TestInjectStandard14Widths_HelveticaOblique(t *testing.T) {
	font := &Font{
		Encoding:     copyEncoding(encodings.WinAnsiEncoding),
		MissingWidth: 1000,
	}

	injectStandard14Widths(font, "Helvetica-Oblique")

	// Helvetica-Oblique shares widths with Helvetica.
	// 'A' should be 667.
	if w := font.GlyphWidth(65); w != 667 {
		t.Errorf("Helvetica-Oblique 'A' width: got %g, want 667", w)
	}
}

func TestInjectStandard14Widths_CommaVariant(t *testing.T) {
	font := &Font{
		Encoding:     copyEncoding(encodings.WinAnsiEncoding),
		MissingWidth: 1000,
	}

	injectStandard14Widths(font, "Helvetica,Bold")

	if len(font.Widths) != 256 {
		t.Fatalf("expected 256 widths for comma variant, got %d", len(font.Widths))
	}
}

func TestInjectStandard14Widths_ExtendedChars(t *testing.T) {
	font := &Font{
		Encoding:     copyEncoding(encodings.WinAnsiEncoding),
		MissingWidth: 1000,
	}

	injectStandard14Widths(font, "Helvetica")

	// WinAnsiEncoding 0xC4 (196) = Ä (Adieresis) → should be 667.
	if w := font.GlyphWidth(0xC4); w != 667 {
		t.Errorf("Helvetica Adieresis width: got %g, want 667", w)
	}
	// WinAnsiEncoding 0xE9 (233) = é (eacute) → should be 556.
	if w := font.GlyphWidth(0xE9); w != 556 {
		t.Errorf("Helvetica eacute width: got %g, want 556", w)
	}
}

func TestStandard14GlyphWidths_Completeness(t *testing.T) {
	// Verify Latin Standard 14 fonts have metrics for basic ASCII glyphs.
	// Symbol and ZapfDingbats use different glyph name sets.
	basicGlyphs := []string{"space", "A", "a", "zero", "period"}
	latinFonts := []string{
		"Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Helvetica-BoldOblique",
		"Times-Roman", "Times-Bold", "Times-Italic", "Times-BoldItalic",
	}

	for _, fontName := range latinFonts {
		widths := standard14GlyphWidths[fontName]
		for _, glyph := range basicGlyphs {
			if _, ok := widths[glyph]; !ok {
				t.Errorf("%s missing glyph %q", fontName, glyph)
			}
		}
	}

	// Symbol must have its Greek letters.
	if _, ok := symbolWidths["Alpha"]; !ok {
		t.Error("Symbol missing glyph Alpha")
	}
	// ZapfDingbats must have its dingbat entries.
	if _, ok := zapfDingbatsWidths["a1"]; !ok {
		t.Error("ZapfDingbats missing glyph a1")
	}
}

// TestInjectStandard14Widths_SymbolBuiltinNames pins that Symbol glyphs
// whose names lie outside the Adobe Glyph List take their AFM widths
// through the built-in encoding: 0xA2 is minute (247), 0xB2 second (411).
func TestInjectStandard14Widths_SymbolBuiltinNames(t *testing.T) {
	font := &Font{Encoding: encodings.BuildSymbolEncoding()}
	injectStandard14Widths(font, "Symbol")
	if w := font.GlyphWidth(0xA2); w != 247 {
		t.Errorf("Symbol minute width: got %g, want 247", w)
	}
	if w := font.GlyphWidth(0xB2); w != 411 {
		t.Errorf("Symbol second width: got %g, want 411", w)
	}
}

// TestInjectStandard14Widths_SymbolDifferences pins that a Symbol code a
// /Differences entry moves to another glyph takes that glyph's width:
// code 4 named phi is 521 units, theta at code 8 is 521.
func TestInjectStandard14Widths_SymbolDifferences(t *testing.T) {
	font := &Font{Encoding: map[byte]rune{4: 0x03C6, 8: 0x03B8}}
	injectStandard14Widths(font, "Symbol")
	if w := font.GlyphWidth(4); w != 521 {
		t.Errorf("Symbol phi width: got %g, want 521", w)
	}
	if w := font.GlyphWidth(8); w != 521 {
		t.Errorf("Symbol theta width: got %g, want 521", w)
	}
}
