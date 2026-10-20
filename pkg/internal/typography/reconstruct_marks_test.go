package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// combiningSolidus is U+0338 COMBINING LONG SOLIDUS OVERLAY, a nonspacing
// mark.
const combiningSolidus = "\u0338"

// TestReconstructText_NonspacingMarks pins how a glyph whose character is a
// nonspacing mark reads: no word space before it, whatever the gap, no line
// break at it, and the glyph after it measured from the glyph before it,
// with the mark's own advance taken out.
func TestReconstructText_NonspacingMarks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		elements []models.TextElement
		want     string
	}{
		{
			name: "word gap before the mark",
			elements: []models.TextElement{
				{Text: "U", X: 100, Y: 500, Width: 7, FontSize: 10, Chunk: 1},
				{Text: combiningSolidus, X: 110, Y: 500, Width: 0, FontSize: 10, Chunk: 2},
				{Text: "=", X: 107.5, Y: 500, Width: 6, FontSize: 10, Chunk: 3},
			},
			want: "U" + combiningSolidus + "=",
		},
		{
			name: "mark kerned back over the glyph before it",
			elements: []models.TextElement{
				{Text: "d", X: 100, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: combiningSolidus, X: 101, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: "e", X: 110, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
			},
			want: "d" + combiningSolidus + "e",
		},
		{
			name: "mark raised above the line",
			elements: []models.TextElement{
				{Text: "a", X: 100, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: combiningSolidus, X: 102, Y: 503.5, Width: 0, FontSize: 10, Chunk: 2},
				{Text: "b", X: 105, Y: 500, Width: 5, FontSize: 10, Chunk: 3},
			},
			want: "a" + combiningSolidus + "b",
		},
		{
			name: "jump after a glyph a mark preceded",
			elements: []models.TextElement{
				{Text: "s", X: 100, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: combiningSolidus, X: 105, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: "t", X: 110, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: "|", X: 115.5, Y: 500, Width: 3, FontSize: 10, Chunk: 2},
			},
			want: "s" + combiningSolidus + "t |",
		},
		{
			name: "word gap after the mark",
			elements: []models.TextElement{
				{Text: "a", X: 100, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: combiningSolidus, X: 102, Y: 500, Width: 0, FontSize: 10, Chunk: 2},
				{Text: "b", X: 108, Y: 500, Width: 5, FontSize: 10, Chunk: 3},
			},
			want: "a" + combiningSolidus + " b",
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

// TestMarkAdvances_Drift pins how much farther apart litho reads two
// elements than PDF.js does: the advance of the marks before each in its run
// of one chunk, and 0 for an index out of range.
func TestMarkAdvances_Drift(t *testing.T) {
	t.Parallel()

	m := newMarkAdvances([]models.TextElement{
		{Text: "a", Width: 5, Chunk: 1},
		{Text: combiningSolidus, Width: 3, Chunk: 1},
		{Text: "b", Width: 5, Chunk: 1},
		{Text: "c", Width: 5, Chunk: 2},
		{Text: combiningSolidus, Width: 4, Chunk: 2},
		{Text: combiningSolidus, Width: 2, Chunk: 2},
		{Text: "d", Width: 5, Chunk: 2},
	}, nil)
	for _, tc := range []struct {
		a, b int
		want float64
	}{
		{0, 2, 3},
		{2, 3, -3},
		{0, 6, 6},
		{4, 6, 6},
		{5, 6, 2},
		{-1, 2, 3},
		{2, 9, -3},
	} {
		if got := m.drift(tc.a, tc.b); got != tc.want {
			t.Errorf("drift(%d, %d) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
