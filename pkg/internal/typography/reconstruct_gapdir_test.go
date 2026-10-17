package typography

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReconstructText_GapAlongTheAdvance pins that word gaps are measured in
// the direction the text advances: down the page for vertical writing and for
// text rotated by -90°, up it for text rotated by 90°. Two glyphs that abut
// stay one word and a gap of more than the tracking cutoff reads as a space,
// whichever way the column runs.
func TestReconstructText_GapAlongTheAdvance(t *testing.T) {
	t.Parallel()

	// column places "a" and "b" abutting and "c" 8 units past "b", each 6
	// units long, starting at y and advancing by step (+1 up, -1 down).
	column := func(y, step float64, el models.TextElement) []models.TextElement {
		var out []models.TextElement
		for i, s := range []string{"a", "b", "c"} {
			e := el
			e.Text, e.X, e.Width, e.FontSize = s, 100, 6, 12
			e.Y = y + step*float64(6*i)
			if i == 2 {
				e.Y += step * 8
			}
			out = append(out, e)
		}
		return out
	}
	for _, tc := range []struct {
		name  string
		elems []models.TextElement
	}{
		{"rotated -90°", column(300, -1, models.TextElement{Angle: -math.Pi / 2})},
		{"rotated 90°", column(300, 1, models.TextElement{Angle: math.Pi / 2})},
		{"vertical writing", column(300, -1, models.TextElement{Vertical: true})},
	} {
		if got := ReconstructText(context.Background(), tc.elems, nil); got != "ab c" {
			t.Errorf("%s: got %q, want %q", tc.name, got, "ab c")
		}
	}
}
