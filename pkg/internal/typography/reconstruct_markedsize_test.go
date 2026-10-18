package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReconstructText_WordGapAcrossMarkedContent pins that the gap after a
// marked-content operator is measured at the size of the glyph before it,
// whether or not the sequences carry an MCID: a 0.77-unit gap from a 13-point
// glyph to a 7.4-point one is tracking at 13 points, though it passes the
// cutoff at 7.4. Inside one sequence the size of the glyph after the gap
// stands, and the same gap reads as a space.
func TestReconstructText_WordGapAcrossMarkedContent(t *testing.T) {
	t.Parallel()

	elements := []models.TextElement{
		{Text: "^", X: 403.38, Y: 77.21, Width: 9.07, FontSize: 12.96},
		{Text: "R", X: 413.22, Y: 77.69, Width: 5.37, FontSize: 7.44},
	}
	for _, tc := range []struct {
		name   string
		marked int
		want   string
	}{
		{"across a marked-content operator", 2, "^R"},
		{"inside one sequence", 1, "^ R"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fonts := []runFont{
				{name: "TT2", size: 1, height: 12.96, marked: 1},
				{name: "TT3", size: 1, height: 7.44, marked: tc.marked},
			}
			if got := reconstructText(context.Background(), elements, fonts, nil); got != tc.want {
				t.Fatalf("reconstructText = %q, want %q", got, tc.want)
			}
		})
	}
}
