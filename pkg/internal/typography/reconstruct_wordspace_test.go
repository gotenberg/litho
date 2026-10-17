package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReconstructText_WordSpaceFollowsTheGlyphSize pins that the gap a word
// space needs is a fraction of the size of the glyphs beside it, as PDF.js
// measures it, not of the line's average size. Every glyph is shown by a
// 1-point Tf scaled by its text matrix.
func TestReconstructText_WordSpaceFollowsTheGlyphSize(t *testing.T) {
	t.Parallel()
	f := &Font{}

	// glyphs lays out texts left to right at the given page sizes, each
	// glyph as wide as its size, with gaps[i] before glyph i+1.
	glyphs := func(texts []string, sizes, gaps []float64, mcids []int) ([]models.TextElement, []runFont) {
		var elems []models.TextElement
		var fonts []runFont
		x := 100.0
		for i, s := range texts {
			elems = append(elems, models.TextElement{Text: s, X: x, Y: 500, Width: sizes[i], FontSize: sizes[i], MCID: mcids[i]})
			fonts = append(fonts, runFont{name: "F1", font: f, size: 1})
			x += sizes[i]
			if i < len(gaps) {
				x += gaps[i]
			}
		}
		return elems, fonts
	}

	t.Run("a leader in a larger size keeps its tracking", func(t *testing.T) {
		t.Parallel()
		// Small capitals at 9 points, then a leader at 11, 1.0 apart: under
		// the 11-point glyph's 1.122 cutoff, though above the 0.969 the line
		// average gives.
		elems, fonts := glyphs([]string{"T", "H", "E", "M", "."}, []float64{9, 9, 9, 9, 11},
			[]float64{0, 0, 0, 1.0}, []int{0, 0, 0, 0, 0})
		if got := reconstructText(context.Background(), elems, fonts, nil); got != "THEM." {
			t.Errorf("got %q, want %q", got, "THEM.")
		}
	})

	t.Run("a marked-content boundary keeps the previous size", func(t *testing.T) {
		t.Parallel()
		// An underscore at 25.6 points, then a slash and a run of text at 8
		// in another marked-content sequence, 0.934 apart: PDF.js still
		// measures the gap against the underscore's size, where the slash's
		// size and the line average both fall below the gap.
		texts := []string{"_", "/"}
		sizes := []float64{25.6, 8}
		gaps := []float64{0.934}
		mcids := []int{1, 2}
		for range 20 {
			texts, sizes, gaps, mcids = append(texts, "x"), append(sizes, 8), append(gaps, 0), append(mcids, 2)
		}
		elems, fonts := glyphs(texts, sizes, gaps, mcids)
		want := "_/xxxxxxxxxxxxxxxxxxxx"
		if got := reconstructText(context.Background(), elems, fonts, nil); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("a word space between glyphs of one size", func(t *testing.T) {
		t.Parallel()
		elems, fonts := glyphs([]string{"a", "b", "c"}, []float64{10, 10, 10},
			[]float64{0, 1.1}, []int{0, 0, 0})
		if got := reconstructText(context.Background(), elems, fonts, nil); got != "ab c" {
			t.Errorf("got %q, want %q", got, "ab c")
		}
	})
}
