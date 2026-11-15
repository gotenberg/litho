package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// zeroWidthSpace is U+200B ZERO WIDTH SPACE and softHyphen U+00AD SOFT
// HYPHEN, two format characters (Unicode category Cf).
const (
	zeroWidthSpace = "\u200b"
	softHyphen     = "\u00ad"
)

// TestReconstructText_FormatCharacters pins how a glyph whose character is a
// format character reads: it is not written, and it passes a held space on
// to the next glyph. One with a width takes its place on the line, so the
// glyph a text-positioning operator sets where it ends abuts it ([advances]),
// except a soft hyphen, whose advance is passed over, so a gap is measured
// from the glyph before it ([skipsAdvance]).
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
			want: "a b",
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
			name: "glyphs after a format character keep their place",
			elements: []models.TextElement{
				{Text: "a", X: 100, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: zeroWidthSpace, X: 105, Y: 500, Width: 3, FontSize: 10, Chunk: 1},
				{Text: "b", X: 108, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: "c", X: 113.5, Y: 500, Width: 5, FontSize: 10, Chunk: 2},
			},
			want: "abc",
		},
		{
			name: "format character set back by its advance",
			elements: []models.TextElement{
				{Text: "ย", X: 100, Y: 500, Width: 7.13, FontSize: 12, Chunk: 1},
				{Text: zeroWidthSpace, X: 107.13, Y: 500, Width: 3.12, FontSize: 12, Chunk: 1},
				{Text: "ท", X: 107.13, Y: 500, Width: 7.31, FontSize: 12, Chunk: 1},
				{Text: "ั", X: 114.44, Y: 500, Width: 0, FontSize: 12, Chunk: 1},
				{Text: "ง", X: 114.44, Y: 500, Width: 6.44, FontSize: 12, Chunk: 2},
			},
			want: "ยทัง",
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

// TestReconstructText_Joiners pins that a glyph mapped to U+200C ZERO WIDTH
// NON-JOINER or U+200D ZERO WIDTH JOINER is written where it stands, with
// no word gap read before it, on a line in stream order and on a line read
// back from visual order, where every other format character stays
// unwritten ([isJoinerOnly]).
func TestReconstructText_Joiners(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		elements []models.TextElement
		want     string
	}{
		{
			name: "non-joiner breaking a conjunct",
			elements: []models.TextElement{
				{Text: "त", X: 100, Y: 500, Width: 11.4, FontSize: 20, Chunk: 1},
				{Text: "्", X: 111.4, Y: 500, Width: 0, FontSize: 20, Chunk: 1},
				{Text: "‌", X: 111.4, Y: 500, Width: 5.2, FontSize: 20, Chunk: 1},
				{Text: "रि", X: 111.4, Y: 500, Width: 13.36, FontSize: 20, Chunk: 1},
			},
			want: "त्‌रि",
		},
		{
			name: "joiner asking for a joining form",
			elements: []models.TextElement{
				{Text: "ع", X: 100, Y: 500, Width: 6.97, FontSize: 22, Chunk: 1},
				{Text: "‍", X: 100, Y: 500, Width: 13.11, FontSize: 22, Chunk: 1},
			},
			want: "ع‍",
		},
		{
			name: "visual order line",
			elements: []models.TextElement{
				{Text: "م", X: 100, Y: 500, Width: 6, FontSize: 12, Chunk: 1},
				{Text: "‏", X: 106, Y: 500, Width: 0, FontSize: 12, Chunk: 1},
				{Text: "‌", X: 106, Y: 500, Width: 0, FontSize: 12, Chunk: 1},
				{Text: "ی", X: 106, Y: 500, Width: 6, FontSize: 12, Chunk: 1},
			},
			want: "ی‌م",
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
