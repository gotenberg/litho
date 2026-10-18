package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReconstructText_GapAfterZeroAdvanceGlyph pins that the glyph after one
// with no advance is measured from the last glyph that advanced: a zero-width
// glyph set over the next one after a word gap, as a negation slash over "="
// is, leaves that gap before both, so a space reads before each, as PDF.js
// reads them.
func TestReconstructText_GapAfterZeroAdvanceGlyph(t *testing.T) {
	t.Parallel()

	elements := []models.TextElement{
		{Text: "a", X: 100, Y: 500, Width: 5, FontSize: 10},
		{Text: ")", X: 105, Y: 500, Width: 4, FontSize: 10},
		{Text: "6", X: 112, Y: 500, Width: 0, FontSize: 10},
		{Text: "=", X: 112, Y: 500, Width: 8, FontSize: 10},
		{Text: "0", X: 123, Y: 500, Width: 5, FontSize: 10},
	}
	got := ReconstructText(context.Background(), elements, nil)
	if want := "a) 6 = 0"; got != want {
		t.Fatalf("ReconstructText = %q, want %q", got, want)
	}
}

// TestReconstructText_ShiftAfterZeroAdvanceGlyph pins that a move across the
// line is measured from the last glyph that advanced, not from a zero-width
// glyph between the two: a glyph 5.7 units above the last advancing glyph
// starts a new line although it sits within a quarter of the font size of the
// zero-width glyph before it.
func TestReconstructText_ShiftAfterZeroAdvanceGlyph(t *testing.T) {
	t.Parallel()

	elements := []models.TextElement{
		{Text: "A", X: 174.23, Y: 531.75, Width: 9.37, FontSize: 14.35},
		{Text: "b", X: 182.03, Y: 533.9, Width: 0, FontSize: 14.35},
		{Text: "c", X: 183.6, Y: 537.47, Width: 3.06, FontSize: 14.35},
	}
	got := ReconstructText(context.Background(), elements, nil)
	if want := "Ab\nc"; got != want {
		t.Fatalf("ReconstructText = %q, want %q", got, want)
	}
}

// TestReconstructText_ZeroAdvanceGlyphAtWordEnd is the control: a zero-width
// glyph at the end of the glyph before it, then a word gap, reads one space,
// whichever glyph the gap is measured from.
func TestReconstructText_ZeroAdvanceGlyphAtWordEnd(t *testing.T) {
	t.Parallel()

	elements := []models.TextElement{
		{Text: "f", X: 100, Y: 500, Width: 5, FontSize: 10},
		{Text: "x", X: 105, Y: 500, Width: 0, FontSize: 10},
		{Text: "S", X: 108, Y: 500, Width: 6, FontSize: 10},
	}
	got := ReconstructText(context.Background(), elements, nil)
	if want := "fx S"; got != want {
		t.Fatalf("ReconstructText = %q, want %q", got, want)
	}
}
