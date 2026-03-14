package typography

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

func TestExtractType3GlyphWidth_D0(t *testing.T) {
	// A CharProc stream containing: "500 0 d0"
	stream := ast.PdfStream{
		Dict: ast.PdfDict{},
		Data: []byte("500 0 d0"),
	}
	w, err := extractType3GlyphWidth(stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 500 {
		t.Errorf("got width %v, want 500", w)
	}
}

func TestExtractType3GlyphWidth_D1(t *testing.T) {
	// A CharProc stream containing: "600 0 0 -200 600 800 d1"
	stream := ast.PdfStream{
		Dict: ast.PdfDict{},
		Data: []byte("600 0 0 -200 600 800 d1"),
	}
	w, err := extractType3GlyphWidth(stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 600 {
		t.Errorf("got width %v, want 600", w)
	}
}

func TestExtractType3GlyphWidth_NoOperator(t *testing.T) {
	stream := ast.PdfStream{
		Dict: ast.PdfDict{},
		Data: []byte("100 200 m 300 400 l S"),
	}
	_, err := extractType3GlyphWidth(stream)
	if err == nil {
		t.Error("expected error for missing d0/d1, got nil")
	}
}

func TestGetGlyphWidth_Type3(t *testing.T) {
	font := &Font{
		Subtype:      "Type3",
		Encoding:     map[byte]rune{'A': 'A'},
		MissingWidth: 1000,
		FontMatrix:   [6]float64{0.001, 0, 0, 0.001, 0, 0},
		Type3Widths:  map[string]float64{"A": 500},
	}

	// charCode for 'A' is 65
	w := font.GetGlyphWidth(65)
	// 500 * 0.001 * 1000 = 500
	if w != 500 {
		t.Errorf("got width %v, want 500", w)
	}
}

func TestGetGlyphWidth_Type3_CustomMatrix(t *testing.T) {
	font := &Font{
		Subtype:      "Type3",
		Encoding:     map[byte]rune{'B': 'B'},
		MissingWidth: 1000,
		FontMatrix:   [6]float64{0.01, 0, 0, 0.01, 0, 0}, // 10× larger than default
		Type3Widths:  map[string]float64{"B": 50},
	}

	w := font.GetGlyphWidth(66)
	// 50 * 0.01 * 1000 = 500
	if w != 500 {
		t.Errorf("got width %v, want 500", w)
	}
}

func TestGetGlyphWidth_Type3_FallbackToWidths(t *testing.T) {
	font := &Font{
		Subtype:      "Type3",
		Encoding:     map[byte]rune{'X': 'X'},
		MissingWidth: 1000,
		FontMatrix:   [6]float64{0.001, 0, 0, 0.001, 0, 0},
		Type3Widths:  map[string]float64{}, // No entry for 'X'
		FirstChar:    88,
		Widths:       []float64{750},
	}

	w := font.GetGlyphWidth(88)
	// Falls back to Widths array: 750
	if w != 750 {
		t.Errorf("got width %v, want 750", w)
	}
}

func TestGetGlyphWidth_Type3_MissingWidth(t *testing.T) {
	font := &Font{
		Subtype:      "Type3",
		Encoding:     map[byte]rune{},
		MissingWidth: 333,
		FontMatrix:   [6]float64{0.001, 0, 0, 0.001, 0, 0},
		Type3Widths:  map[string]float64{},
	}

	w := font.GetGlyphWidth(99)
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
