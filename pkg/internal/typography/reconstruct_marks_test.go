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

// TestReconstructText_GlyphHoldingAMarkAdvances pins that a glyph whose
// characters hold a nonspacing mark beside a base character, a Devanagari
// conjunct or a letter with a nukta drawn as one glyph, advances by its
// width (ISO 32000-1, 9.4.4) like any glyph showing a base: the glyph a
// text-positioning operator sets after it where it ends abuts it, and no
// word gap is read there. A glyph whose characters are
// all nonspacing marks still advances nowhere ([isNonspacingMark]).
func TestReconstructText_GlyphHoldingAMarkAdvances(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		elements []models.TextElement
		want     string
	}{
		{
			name: "conjunct with a virama",
			elements: []models.TextElement{
				{Text: "स", X: 100, Y: 500, Width: 11.9, FontSize: 16, Chunk: 1},
				{Text: "ा", X: 111.9, Y: 500, Width: 4.7, FontSize: 16, Chunk: 1},
				{Text: "र्व", X: 116.6, Y: 500, Width: 9.68, FontSize: 16, Chunk: 1},
				{Text: "भ", X: 126.28, Y: 500, Width: 12.37, FontSize: 16, Chunk: 2},
				{Text: "ौ", X: 138.65, Y: 500, Width: 4.7, FontSize: 16, Chunk: 2},
				{Text: "म", X: 143.35, Y: 500, Width: 10.34, FontSize: 16, Chunk: 2},
			},
			want: "सार्वभौम",
		},
		{
			name: "letter with a nukta",
			elements: []models.TextElement{
				{Text: "क", X: 100, Y: 500, Width: 9.14, FontSize: 12, Chunk: 1},
				{Text: "फ़", X: 109.14, Y: 500, Width: 8, FontSize: 12, Chunk: 1},
				{Text: "र", X: 117.14, Y: 500, Width: 4.9, FontSize: 12, Chunk: 2},
			},
			want: "कफ़र",
		},
		{
			name: "mark alone, its width not advanced",
			elements: []models.TextElement{
				{Text: "a", X: 100, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: combiningSolidus, X: 105, Y: 500, Width: 5, FontSize: 10, Chunk: 1},
				{Text: "b", X: 108, Y: 500, Width: 5, FontSize: 10, Chunk: 2},
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
