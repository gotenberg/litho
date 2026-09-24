package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

func TestExtractGlyphNames_Type1(t *testing.T) {
	// eexec-encrypt a minimal Type1 private section (standard R=55665 cipher),
	// then confirm ExtractGlyphNames pulls the /CharStrings glyph names so the
	// CharSet-completeness check works for classic Type1 (FontFile) programs.
	plain := []byte("xxxx" + // 4-byte salt stripped on decrypt
		"/CharStrings 3 dict dup begin\n" +
		"/.notdef 4 RD AAAA ND\n" +
		"/a 5 RD HELLO ND\n" +
		"/k 4 RD WXYZ ND\n" +
		"end")
	enc := make([]byte, len(plain))
	r := uint32(55665)
	for i, p := range plain {
		c := p ^ byte(r>>8)
		enc[i] = c
		r = (uint32(c)+r)*52845 + 22719
	}
	font := append([]byte("%!PS-AdobeFont-1.0\ncurrentfile eexec\n"), enc...)

	got := ExtractGlyphNames(context.Background(), font)
	names := map[string]bool{}
	for _, n := range got {
		names[n] = true
	}
	for _, want := range []string{".notdef", "a", "k"} {
		if !names[want] {
			t.Errorf("ExtractGlyphNames missing %q; got %v", want, got)
		}
	}
}

func TestExtractType3GlyphWidth_D0(t *testing.T) {
	// A CharProc stream containing: "500 0 d0"
	stream := ast.NewStream(ast.PdfDict{}, []byte("500 0 d0"))
	w, err := extractType3GlyphWidth(context.Background(), &document.Document{Logger: document.NopLogger()}, stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 500 {
		t.Errorf("got width %v, want 500", w)
	}
}

func TestExtractType3GlyphWidth_D1(t *testing.T) {
	// A CharProc stream containing: "600 0 0 -200 600 800 d1"
	stream := ast.NewStream(ast.PdfDict{}, []byte("600 0 0 -200 600 800 d1"))
	w, err := extractType3GlyphWidth(context.Background(), &document.Document{Logger: document.NopLogger()}, stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 600 {
		t.Errorf("got width %v, want 600", w)
	}
}

func TestExtractType3GlyphWidth_NoOperator(t *testing.T) {
	stream := ast.NewStream(ast.PdfDict{}, []byte("100 200 m 300 400 l S"))
	_, err := extractType3GlyphWidth(context.Background(), &document.Document{Logger: document.NopLogger()}, stream)
	if err == nil {
		t.Error("expected error for missing d0/d1, got nil")
	}
}

func TestGlyphWidth_Type3(t *testing.T) {
	font := &Font{
		Subtype:      "Type3",
		Encoding:     map[byte]rune{'A': 'A'},
		MissingWidth: 1000,
		FontMatrix:   [6]float64{0.001, 0, 0, 0.001, 0, 0},
		Type3Widths:  map[string]float64{"A": 500},
	}

	// charCode for 'A' is 65
	w := font.GlyphWidth(65)
	// 500 * 0.001 * 1000 = 500
	if w != 500 {
		t.Errorf("got width %v, want 500", w)
	}
}

func TestGlyphWidth_Type3_CustomMatrix(t *testing.T) {
	font := &Font{
		Subtype:      "Type3",
		Encoding:     map[byte]rune{'B': 'B'},
		MissingWidth: 1000,
		FontMatrix:   [6]float64{0.01, 0, 0, 0.01, 0, 0}, // 10× larger than default
		Type3Widths:  map[string]float64{"B": 50},
	}

	w := font.GlyphWidth(66)
	// 50 * 0.01 * 1000 = 500
	if w != 500 {
		t.Errorf("got width %v, want 500", w)
	}
}

func TestGlyphWidth_Type3_FallbackToWidths(t *testing.T) {
	font := &Font{
		Subtype:      "Type3",
		Encoding:     map[byte]rune{'X': 'X'},
		MissingWidth: 1000,
		FontMatrix:   [6]float64{0.001, 0, 0, 0.001, 0, 0},
		Type3Widths:  map[string]float64{}, // No entry for 'X'
		FirstChar:    88,
		Widths:       []float64{750},
	}

	w := font.GlyphWidth(88)
	// Falls back to Widths array: 750
	if w != 750 {
		t.Errorf("got width %v, want 750", w)
	}
}

func TestGlyphWidth_Type3_MissingWidth(t *testing.T) {
	font := &Font{
		Subtype:      "Type3",
		Encoding:     map[byte]rune{},
		MissingWidth: 333,
		FontMatrix:   [6]float64{0.001, 0, 0, 0.001, 0, 0},
		Type3Widths:  map[string]float64{},
	}

	w := font.GlyphWidth(99)
	if w != 333 {
		t.Errorf("got width %v, want 333", w)
	}
}

func TestRuneToGlyph(t *testing.T) {
	// Known mapping
	if got := encodings.RuneToGlyph('A'); got != "A" {
		t.Errorf("RuneToGlyph('A') = %q, want %q", got, "A")
	}
	if got := encodings.RuneToGlyph(' '); got != "space" {
		t.Errorf("RuneToGlyph(' ') = %q, want %q", got, "space")
	}
}

// TestResolveFont_SymbolicFlagFallsBackToWinAnsi verifies that a Type 1
// font marked Symbolic via FontDescriptor /Flags bit 3 sticks to WinAnsi
// instead of the StandardEncoding default. PDF 32000-1 Table 123 marks
// such fonts as having bespoke glyph repertoires; producers that ship
// custom-named glyphs (Universal-NewswithCommPi, OCR symbols) lay them
// out at WinAnsi byte positions, so byte 0xA9 must surface as '©' not
// the StandardEncoding 'quotesingle'.
func TestResolveFont_SymbolicFlagFallsBackToWinAnsi(t *testing.T) {
	fontDict := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName("Universal-NewswithCommPi"),
		"FontDescriptor": ast.PdfDict{
			"Type":     ast.PdfName("FontDescriptor"),
			"FontName": ast.PdfName("Universal-NewswithCommPi"),
			"Flags":    ast.PdfInt(4), // bit 3: Symbolic
		},
	}
	font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()}, fontDict)
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	if got, want := font.Encoding[0xA9], rune(0x00A9); got != want {
		t.Errorf("byte 0xA9 = U+%04X, want U+%04X (©)", got, want)
	}
	if got, want := font.Encoding[0x91], rune(0x2018); got != want {
		t.Errorf("byte 0x91 = U+%04X, want U+%04X (WinAnsi quoteleft)", got, want)
	}
}

// TestResolveFont_NonStandard14NoEncoding pins the spec-compliant
// fallback for non-symbolic Type 1 fonts that omit /Encoding entirely
// (e.g. /Melior, /Minion, /Garamond). Per ISO 32000-1 §9.6.6.1 the
// implicit encoding is StandardEncoding. Litho previously fell back to
// WinAnsi here, so byte 0xB1 surfaced as '±' (Latin-1 plus-minus)
// instead of '–' (en-dash) in Federal-Register-style filings.
func TestResolveFont_NonStandard14NoEncoding(t *testing.T) {
	fontDict := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName("Melior"),
	}
	font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()}, fontDict)
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	cases := []struct {
		code byte
		want rune
	}{
		{0xB1, 0x2013}, // endash, the Federal-Register byte
		{0xD0, 0x2014}, // emdash
		{0x27, 0x2019}, // quoteright
	}
	for _, c := range cases {
		got, ok := font.Encoding[c.code]
		if !ok {
			t.Errorf("byte 0x%02X unmapped, want U+%04X", c.code, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("byte 0x%02X = U+%04X, want U+%04X", c.code, got, c.want)
		}
	}
}

// TestResolveFont_Standard14DifferencesNoBaseEncoding verifies that a
// standard 14 non-symbolic font with an /Encoding dict containing only
// /Differences (no /BaseEncoding) overlays StandardEncoding for unmapped
// slots, per ISO 32000-2 §9.6.6.4. The previous WinAnsi overlay turned
// byte 0xD0 (StandardEncoding emdash → U+2014) into 'Ð' on Mac-authored
// filings.
//
// # Corpus evidence
//
//   - govdocs: its Mac-authored filings showed the 'Ð' the previous WinAnsi
//     overlay produced for byte 0xD0.
func TestResolveFont_Standard14DifferencesNoBaseEncoding(t *testing.T) {
	fontDict := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName("Times-Roman"),
		"Encoding": ast.PdfDict{
			"Type":        ast.PdfName("Encoding"),
			"Differences": ast.PdfArray{ast.PdfInt(219), ast.PdfName("Zcaron")},
		},
	}
	font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()}, fontDict)
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	cases := []struct {
		code byte
		want rune
	}{
		{0xD0, 0x2014}, // emdash
		{0xB1, 0x2013}, // endash (canary that StandardEncoding survived)
		{0x27, 0x2019}, // quoteright (StandardEncoding diverges from WinAnsi here)
		{0xDB, 0x017D}, // /Zcaron from Differences
	}
	for _, c := range cases {
		got, ok := font.Encoding[c.code]
		if !ok {
			t.Errorf("byte 0x%02X unmapped, want U+%04X", c.code, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("byte 0x%02X = U+%04X, want U+%04X", c.code, got, c.want)
		}
	}
}

// TestResolveFont_SymbolDifferencesNoBaseEncoding verifies that a /Symbol
// font with an /Encoding dict containing only /Differences (no /BaseEncoding)
// keeps the Symbol built-in encoding for unmapped slots, per ISO 32000-2
// §9.6.6.4. A previous regression overlaid WinAnsi here, so byte 0xFC
// (Symbol bracerighttp → U+F8FC) leaked through as Latin-1 'ü' on legal
// caption braces in scanned filings.
func TestResolveFont_SymbolDifferencesNoBaseEncoding(t *testing.T) {
	fontDict := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName("Symbol"),
		"Encoding": ast.PdfDict{
			"Type":        ast.PdfName("Encoding"),
			"Differences": ast.PdfArray{ast.PdfInt(240), ast.PdfName("apple")},
		},
	}
	font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()}, fontDict)
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	cases := []struct {
		code byte
		want rune
	}{
		{0xFC, 0xF8FC}, // bracerighttp
		{0xFD, 0xF8FD}, // bracerightmid
		{0xFE, 0xF8FE}, // bracerightbt
		{0xA1, 0x03D2}, // Upsilon1 (canary that the Symbol base survived)
	}
	for _, c := range cases {
		got, ok := font.Encoding[c.code]
		if !ok {
			t.Errorf("byte 0x%02X unmapped, want U+%04X", c.code, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("byte 0x%02X = U+%04X, want U+%04X", c.code, got, c.want)
		}
	}
}
