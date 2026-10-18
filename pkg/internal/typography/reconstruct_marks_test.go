package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// combiningSolidus is U+0338 COMBINING LONG SOLIDUS OVERLAY, a nonspacing
// mark.
const combiningSolidus = "̸"

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

// TestMarkAdvances_Between pins the advance [markAdvances] reads between two
// elements: the widths of the marks strictly between them that share the
// chunk of the second, and 0 for indices out of range.
func TestMarkAdvances_Between(t *testing.T) {
	t.Parallel()

	m := newMarkAdvances([]models.TextElement{
		{Text: "a", Width: 5, Chunk: 1},
		{Text: combiningSolidus, Width: 3, Chunk: 1},
		{Text: "b", Width: 5, Chunk: 2},
		{Text: combiningSolidus, Width: 4, Chunk: 2},
		{Text: combiningSolidus, Width: 2, Chunk: 2},
		{Text: "c", Width: 5, Chunk: 2},
	})
	for _, tc := range []struct {
		from, to int
		want     float64
	}{
		{0, 2, 0},
		{0, 5, 6},
		{2, 5, 6},
		{3, 5, 2},
		{-1, 5, 6},
		{5, 5, 0},
		{0, 9, 0},
		{-3, 2, 0},
	} {
		if got := m.between(tc.from, tc.to); got != tc.want {
			t.Errorf("between(%d, %d) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}
