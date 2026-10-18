package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReconstructText_WhitespaceGlyphs pins that a glyph whose character is
// any whitespace, a line feed or U+00A0 as much as U+0020, is read as a space
// glyph: the glyph after it, moved back onto the end of the glyph before it,
// cancels it, and one at a word gap reads as one U+0020.
func TestReconstructText_WhitespaceGlyphs(t *testing.T) {
	t.Parallel()

	const lineFeed, noBreakSpace = "\x0a", "\xc2\xa0"
	for _, tc := range []struct {
		name  string
		space string
		x     float64
		want  string
	}{
		{"line feed cancelled", lineFeed, 108.004, "3-"},
		{"no-break space cancelled", noBreakSpace, 107.9, "3-"},
		{"line feed at a word gap", lineFeed, 112, "3 -"},
		{"no-break space at a word gap", noBreakSpace, 112, "3 -"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			elements := []models.TextElement{
				{Text: "3", X: 100, Y: 500, Width: 8, FontSize: 12},
				{Text: tc.space, X: 108, Y: 500, Width: 3, FontSize: 12},
				{Text: "-", X: tc.x, Y: 500, Width: 4, FontSize: 12},
			}
			if got := ReconstructText(context.Background(), elements, nil); got != tc.want {
				t.Fatalf("ReconstructText = %q, want %q", got, tc.want)
			}
		})
	}
}
