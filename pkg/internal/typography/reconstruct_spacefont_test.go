package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReconstructText_SpaceGlyphInAnotherFont pins that a space glyph shown in
// another font than the glyph before it fixes the scale the gap after it is
// measured at: the 0.29-unit gap from an 8.9-point "O" to "f", past a space
// shown at 12 points in another font, is within 0.03 of 12 points, so the
// space is dropped. Shown in the font of "O", the space ends no item, the
// scale of "f" stands, and the space stays.
func TestReconstructText_SpaceGlyphInAnotherFont(t *testing.T) {
	t.Parallel()

	letters, spaces := &Font{}, &Font{}
	elements := []models.TextElement{
		{Text: "O", X: 222.12, Y: 408.66, Width: 6.91, FontSize: 8.88, Chunk: 1},
		{Text: " ", X: 229.32, Y: 408.66, Width: 3, FontSize: 12, Chunk: 2},
		{Text: "f", X: 229.32, Y: 408.66, Width: 2.96, FontSize: 8.88, Chunk: 3},
	}
	for _, tc := range []struct {
		name  string
		space runFont
		want  string
	}{
		{"space in another font", runFont{name: "TT1", font: spaces, size: 1, height: 12}, "Of"},
		{"space in the same font", runFont{name: "TT2", font: letters, size: 1, height: 12}, "O f"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fonts := []runFont{
				{name: "TT2", font: letters, size: 1, height: 8.88},
				tc.space,
				{name: "TT2", font: letters, size: 1, height: 8.88},
			}
			if got := reconstructText(context.Background(), elements, fonts, nil); got != tc.want {
				t.Fatalf("reconstructText = %q, want %q", got, tc.want)
			}
		})
	}
}
