package typography

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReconstructText_LiteralSpaceOverprintedHalfTurn pins that text turned
// a half turn, by its text matrix or by a negative font size, drops a literal
// space the next glyph is set back over, as upright text does: the gap is
// measured leftward, along the advance, and against the size's magnitude.
// PDF.js negates both positions of a half-turned glyph before it compares
// them (src/core/evaluator.js, compareWithLastPosition).
//
// # Corpus evidence
//
//   - pdfjs issue14415.pdf: "agreement " at -12 Tf, then "on" set by a Tm
//     0.02 past the "t", which PDF.js reads "agreementon".
//   - govdocs thread_000_000150.pdf: words set by a Tm of -10.25 0 0 -10.25,
//     each after a space the next Tm overprints.
func TestReconstructText_LiteralSpaceOverprintedHalfTurn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		size float64
		gap  float64
		want string
	}{
		{"negative size, overprinted", -12, 0.02, "to"},
		{"turned matrix, overprinted", 12, 0.02, "to"},
		{"negative size, word gap", -12, 3, "t o"},
		{"turned matrix, word gap", 12, 3, "t o"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Glyphs advance leftward: each origin is the right end of its
			// glyph, and the next one sits its width to the left.
			elems := []models.TextElement{
				{Text: "t", X: 225, Y: 168, Width: 4, FontSize: tc.size, Angle: math.Pi},
				{Text: " ", X: 221, Y: 168, Width: 3.3, FontSize: tc.size, Angle: math.Pi},
				{Text: "o", X: 221 - tc.gap, Y: 168, Width: 6, FontSize: tc.size, Angle: math.Pi},
			}
			if got := ReconstructText(context.Background(), elems, nil); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
