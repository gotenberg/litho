package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReconstructText_SpaceSetBackOverTheGlyphBefore pins that a space glyph
// set back over the glyph before it marks no word break when the next glyph
// starts exactly where that glyph ends: the space paints inside the
// ligature's advance and leaves no gap on the page. A space set at the end
// of the glyph before it, with the next glyph at the same point, still reads
// as the break its producer signals, and so does a space set back over a
// single letter, which negative character spacing sets (govdocs
// thread_200_200744.pdf).
func TestReconstructText_SpaceSetBackOverTheGlyphBefore(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		glyph  string
		spaceX float64
		want   string
	}{
		{"space set back over the ligature", "fi", 387.831, "fficu"},
		{"space at the end of the ligature", "fi", 390.702, "ffi cu"},
		{"space set back over a letter, as tracking sets it", "y", 387.831, "fy cu"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			elements := []models.TextElement{
				{Text: "f", X: 381.77, Y: 538.493, Width: 3.19, FontSize: 11, Chunk: 1},
				{Text: tc.glyph, X: 384.96, Y: 538.493, Width: 5.742, FontSize: 11, Chunk: 1},
				{Text: " ", X: tc.spaceX, Y: 538.493, Width: 2.75, FontSize: 11, Chunk: 2},
				{Text: "c", X: 390.702, Y: 538.493, Width: 4.4, FontSize: 11, Chunk: 2},
				{Text: "u", X: 395.102, Y: 538.493, Width: 5.632, FontSize: 11, Chunk: 2},
			}
			if got := reconstructText(context.Background(), elements, nil, nil); got != tc.want {
				t.Fatalf("reconstructText = %q, want %q", got, tc.want)
			}
		})
	}
}
