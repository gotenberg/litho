package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// zeroWidthSpace is U+200B ZERO WIDTH SPACE and softHyphen U+00AD SOFT
// HYPHEN, two format characters (Unicode category Cf).
const (
	zeroWidthSpace = "​"
	softHyphen     = "­"
)

// TestReconstructText_FormatCharacters pins how a glyph whose character is a
// format character reads: written where it stands, it passes a held space on
// to the next glyph, and the glyphs after it in its run sit its advance
// earlier, so a gap is measured from the glyph before it.
func TestReconstructText_FormatCharacters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		elements []models.TextElement
		want     string
	}{
		{
			name: "held space passed over a zero width space",
			elements: []models.TextElement{
				{Text: "a", X: 100, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: zeroWidthSpace, X: 105, Y: 500, Width: 0, FontSize: 10, Chunk: 1},
				{Text: " ", X: 105, Y: 500, Width: 3, FontSize: 10, Chunk: 1},
				{Text: zeroWidthSpace, X: 108, Y: 500, Width: 0, FontSize: 10, Chunk: 1},
				{Text: "b", X: 108, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
			},
			want: "a" + zeroWidthSpace + " " + zeroWidthSpace + "b",
		},
		{
			name: "gap measured past a soft hyphen",
			elements: []models.TextElement{
				{Text: "s", X: 100, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: softHyphen, X: 105, Y: 500, Width: 4, FontSize: 10, Chunk: 1},
				{Text: "r", X: 109.5, Y: 500, Width: 4, FontSize: 10, Chunk: 2},
			},
			want: "s r",
		},
		{
			name: "glyphs after a format character sit its advance earlier",
			elements: []models.TextElement{
				{Text: "a", X: 100, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: zeroWidthSpace, X: 105, Y: 500, Width: 3, FontSize: 10, Chunk: 1},
				{Text: "b", X: 108, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: "c", X: 113.5, Y: 500, Width: 5, FontSize: 10, Chunk: 2},
			},
			want: "a" + zeroWidthSpace + "b c",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ReconstructText(context.Background(), tc.elements, nil); got != tc.want {
				t.Fatalf("ReconstructText = %q, want %q", got, tc.want)
			}
		})
	}
}
