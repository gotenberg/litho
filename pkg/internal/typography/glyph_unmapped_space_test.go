package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/models"
)

// TestDecodeNextChar_UnmappedCode32ReadsAsSpace pins that the single-byte
// code 32 no table maps reads as a word space, the code ISO 32000-1, 9.3.3
// applies word spacing to, and that no other unmapped code does, whitespace
// valued or not: nothing in the font says the glyph there is blank.
func TestDecodeNextChar_UnmappedCode32ReadsAsSpace(t *testing.T) {
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
		{0x09, ""},
		{0x0B, ""},
		{0x0C, ""},
		{0x0D, ""},
		{0xA0, ""},
		{0x01, ""},
		{'A', "A"},
	} {
		got, n, skip := decodeNextChar(font, []byte{tc.code})
		if got != tc.want || n != 1 || skip {
			t.Errorf("code %#02x: got (%q, %d, %v), want (%q, 1, false)", tc.code, got, n, skip, tc.want)
		}
	}
}

// unmappedLigatureFont returns a simple font whose encoding maps the letters
// of word and nothing else, every code 500 units wide, as a TeX text font
// whose ligatures carry no glyph name litho maps.
func unmappedLigatureFont(word string) *Font {
	enc := map[byte]rune{}
	for _, r := range word {
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
	return font
}

// showText runs shown through [processText] in font at 10 points.
func showText(t *testing.T, font *Font, shown string) []models.TextElement {
	t.Helper()
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	machine.Current().Text.Tfs = 10
	var elements []models.TextElement
	if err := processText(context.Background(), ast.PdfString(shown), font, machine, &elements, nil); err != nil {
		t.Fatalf("processText: %v", err)
	}
	return elements
}

// TestProcessText_UnmappedLigatureCodeKeepsWordWhole pins that a TeX "fi"
// ligature at code 0x0C with no glyph name litho maps puts no space inside
// the word: "Veri" 0x0C "cation" reads "Verication", where PDF.js, which
// shows the code and reads a form feed as whitespace, reads "Veri cation".
// The glyph also reaches no element as a space.
func TestProcessText_UnmappedLigatureCodeKeepsWordWhole(t *testing.T) {
	t.Parallel()

	elements := showText(t, unmappedLigatureFont("Vericaton"), "Veri\x0Ccation")
	for _, el := range elements {
		if isSpaceGlyph(el.Text) {
			t.Errorf("the ligature reached an element as %q", el.Text)
		}
	}
	if got, want := ReconstructText(context.Background(), elements, nil), "Verication"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestProcessText_UnmappedCode32SpacesWords pins the word space an unmapped
// code 32 shows: "brie" 32 "y" reads "brie y".
func TestProcessText_UnmappedCode32SpacesWords(t *testing.T) {
	t.Parallel()

	elements := showText(t, unmappedLigatureFont("briey"), "brie y")
	if got, want := ReconstructText(context.Background(), elements, nil), "brie y"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
