package typography

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReconstructText_TurnedLabelsBreakLine pins that two labels set in two
// directions, the second away from the end of the first, read as two lines,
// as in govdocs thread_100_100945.pdf ("RiverRiver") and
// thread_000_000024.pdf ("OCEANPACIFIC").
func TestReconstructText_TurnedLabelsBreakLine(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		elems []models.TextElement
		want  string
	}{
		{
			name: "two rivers turned apart",
			elems: []models.TextElement{
				{Text: "River", X: 380.255, Y: 585.887, Width: 16.483, FontSize: 6.98, Angle: 1.164},
				{Text: "River", X: 378.814, Y: 537.356, Width: 16.482, FontSize: 6.98, Angle: 1.777},
			},
			want: "River\nRiver",
		},
		{
			name: "upside-down states",
			elems: []models.TextElement{
				{Text: "MONTANA", X: 118.164, Y: 64.585, Width: 40.122, FontSize: 7.99, Angle: 3.109},
				{Text: "IDAHO", X: 156.470, Y: 63.001, Width: 25.432, FontSize: 7.99, Angle: 3.117},
			},
			want: "MONTANA\nIDAHO",
		},
		{
			name: "ocean label set back along its line",
			elems: []models.TextElement{
				{Text: "OCEAN", X: 109.101, Y: 374.922, Width: 39.335, FontSize: 12, Angle: -1.403},
				{Text: "PACIFIC", X: 83.248, Y: 522.218, Width: 46.002, FontSize: 12, Angle: -1.399},
			},
			want: "OCEAN\nPACIFIC",
		},
	} {
		if got := ReconstructText(context.Background(), tc.elems, nil); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestReconstructText_CurvedLabelStaysOneLine pins that the letters of a
// label set along a curve, each turned a little and each starting where the
// one before it ends, read as one line with their word gap, as "Big Hog" in
// govdocs thread_000_000146.pdf.
func TestReconstructText_CurvedLabelStaysOneLine(t *testing.T) {
	t.Parallel()

	const size, width = 7.0, 4.0
	var elems []models.TextElement
	x, y, angle := 200.0, 300.0, 0.3
	for _, r := range "Big Hog" {
		if r == ' ' {
			// The word gap: the next letter starts half the size further on.
			x += 0.5 * size * math.Cos(angle)
			y += 0.5 * size * math.Sin(angle)
			continue
		}
		elems = append(elems, models.TextElement{Text: string(r), X: x, Y: y, Width: width, FontSize: size, Angle: angle})
		x += width * math.Cos(angle)
		y += width * math.Sin(angle)
		angle -= 0.05
	}
	if got, want := ReconstructText(context.Background(), elems, nil), "Big Hog"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
