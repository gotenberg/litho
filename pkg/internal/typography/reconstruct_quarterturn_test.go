package typography

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// oneStreamFonts returns n run fonts in one font at one size, shown in the
// page's own stream, so no font change or stream edge starts a run.
func oneStreamFonts(n int) []runFont {
	streams := make([]int, n)
	segments := make([]int, n)
	for i := range n {
		streams[i], segments[i] = 1, 2
	}
	return streamFonts(1, streams, segments)
}

// TestReconstructText_QuarterTurnSubscript pins that a line turned a quarter
// round keeps a subscript on its line the way an upright line does: PDF.js
// swaps the axes of such a line, so a glyph shifted across it by less than a
// quarter of the size of the glyph that began the run continues the run,
// however many units that is. "2" sits 7.98 units across the line from "O",
// under a quarter of the 31.98-unit size of "S".
func TestReconstructText_QuarterTurnSubscript(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		angle float64
		// sign is the direction of the column along the page Y axis.
		sign float64
	}{
		{"rotated 90°", math.Pi / 2, 1},
		{"rotated -90°", -math.Pi / 2, -1},
	} {
		el := func(s string, x, along, w, size float64) models.TextElement {
			return models.TextElement{Text: s, X: x, Y: 300 + tc.sign*along, Width: w, FontSize: size, Angle: tc.angle}
		}
		in := []models.TextElement{
			el("S", 300.48, 0, 17.78, 31.98),
			el("O", 300.48, 17.76, 23.09, 31.98),
			el("2", 300.48+tc.sign*7.98, 40.86, 10.5, 21),
		}
		if got := reconstructText(context.Background(), in, oneStreamFonts(len(in)), nil); got != "SO2" {
			t.Errorf("%s: got %q, want %q", tc.name, got, "SO2")
		}
	}
}

// TestReconstructText_ShiftAgainstRunStart pins that a baseline shift is
// measured against the size of the glyph that began the run, the height
// PDF.js gives a text item when it begins it (ensureTextContentItem), not
// against the glyph before the shift. "c" drops 4.5 units: past a quarter
// of the 16-unit "a" that began the run, so it starts a run of its own and
// a line, but within a quarter of the 20-unit "b" before it.
func TestReconstructText_ShiftAgainstRunStart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		startSize float64
		want      string
	}{
		{"run begun in a smaller size", 16, "ab\nc"},
		{"run begun in the same size", 20, "abc"},
	} {
		in := []models.TextElement{
			{Text: "a", X: 100, Y: 500, Width: 8, FontSize: tc.startSize},
			{Text: "b", X: 108, Y: 500, Width: 10, FontSize: 20},
			{Text: "c", X: 118, Y: 495.5, Width: 6, FontSize: 13},
		}
		if got := reconstructText(context.Background(), in, oneStreamFonts(len(in)), nil); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
