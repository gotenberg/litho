package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/models"
)

// TestDecodeNextChar_UnmappedWhitespaceCodeReadsAsSpace pins that a
// single-byte code no table maps reads as a word space when the code is
// itself a whitespace character, as PDF.js shows an unmapped glyph by its
// code and reads a whitespace code as a space glyph. Any other unmapped code
// still reads as nothing.
func TestDecodeNextChar_UnmappedWhitespaceCodeReadsAsSpace(t *testing.T) {
	t.Parallel()

	font := &Font{
		Subtype:    "Type1",
		Encoding:   map[byte]rune{'A': 'A'},
		UnitsPerEm: 1000,
	}
	for _, tc := range []struct {
		code byte
		want string
	}{
		{0x20, " "},
		{0x09, " "},
		{0x0C, " "},
		{0x0D, " "},
		{0xA0, " "},
		{0x01, ""},
		{0x0E, ""},
		{'A', "A"},
	} {
		got, n, skip := decodeNextChar(font, []byte{tc.code})
		if got != tc.want || n != 1 || skip {
			t.Errorf("code %#02x: got (%q, %d, %v), want (%q, 1, false)", tc.code, got, n, skip, tc.want)
		}
	}
}

// TestProcessText_UnmappedWhitespaceCodeSpacesWords pins the word space an
// unmapped whitespace code shows: TeX's text fonts set the "fi" ligature at
// code 0x0C, and a font that names no glyph there shows "brie" 0x0C "y",
// which reads "brie y", where it read "briey" while the code read as nothing.
func TestProcessText_UnmappedWhitespaceCodeSpacesWords(t *testing.T) {
	t.Parallel()

	enc := map[byte]rune{}
	for _, r := range "briey" {
		enc[byte(r)] = r
	}
	font := &Font{
		Subtype:      "Type1",
		Encoding:     enc,
		FirstChar:    0,
		LastChar:     255,
		Widths:       make([]float64, 256),
		MissingWidth: 500,
		UnitsPerEm:   1000,
	}
	for i := range font.Widths {
		font.Widths[i] = 500
	}
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	machine.Current().Text.Tfs = 10
	var elements []models.TextElement
	if err := processText(context.Background(), ast.PdfString("brie\x0Cy"), font, machine, &elements); err != nil {
		t.Fatalf("processText: %v", err)
	}
	if got, want := ReconstructText(context.Background(), elements, nil), "brie y"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
