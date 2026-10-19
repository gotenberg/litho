package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReconstructText_UnmappedGlyphKeepsWordGaps pins that a glyph with no
// character keeps the word gaps around it: "(" and ")" here map to nothing,
// as a symbol font's parentheses with unknown glyph names do, and the gap
// before "(" and after ")" still reads as a space.
func TestReconstructText_UnmappedGlyphKeepsWordGaps(t *testing.T) {
	t.Parallel()
	el := func(s string, x, w float64) models.TextElement {
		return models.TextElement{Text: s, X: x, Y: 500, Width: w, FontSize: 10}
	}
	in := []models.TextElement{
		el("n", 100, 5),
		el("", 107.5, 3),
		el("F", 110.5, 6),
		el("D", 116.5, 6),
		el("", 122.5, 3),
		el("p", 128, 5),
	}
	if got, want := ReconstructText(context.Background(), in, nil), "n FD p"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestReconstructText_WordGapBetweenUnmappedGlyphs pins that a word gap
// between two glyphs with no character reads as a space: "(11b)" set with
// parentheses no table maps, then "Ex" a word gap past the ")", read "11b Ex".
// The gap was read only after a glyph with a character, so it vanished
// ("11bEx").
func TestReconstructText_WordGapBetweenUnmappedGlyphs(t *testing.T) {
	t.Parallel()
	el := func(s string, x, w float64) models.TextElement {
		return models.TextElement{Text: s, X: x, Y: 500, Width: w, FontSize: 10}
	}
	in := []models.TextElement{
		el("b", 100, 5),
		el("", 105, 3),
		el("", 112, 3),
		el("E", 115, 7),
		el("x", 122, 5),
	}
	if got, want := ReconstructText(context.Background(), in, nil), "b Ex"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
