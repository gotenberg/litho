package typography

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/models"
)

// TestReconstructText_Empty returns an empty string when there are no
// text elements. The contract: empty input → empty output, no panic.
func TestReconstructText_Empty(t *testing.T) {
	t.Parallel()
	got := ReconstructText(context.Background(), nil, nil)
	if got != "" {
		t.Errorf("empty input: got %q, want empty", got)
	}
	got = ReconstructText(context.Background(), []models.TextElement{}, nil)
	if got != "" {
		t.Errorf("empty slice: got %q, want empty", got)
	}
}

// TestReconstructText_SingleElement returns the element's text when
// there is exactly one input.
func TestReconstructText_SingleElement(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "Hello", X: 100, Y: 500, Width: 30, FontSize: 12},
	}
	got := ReconstructText(context.Background(), in, nil)
	if got != "Hello" {
		t.Errorf("single element: got %q, want %q", got, "Hello")
	}
}

// TestReconstructText_SameLine concatenates elements on the same Y row
// with either no separator or an inferred space. The exact joining
// behavior is a heuristic; the contract is "the text of both elements
// appears in the output".
func TestReconstructText_SameLine(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "Hello", X: 100, Y: 500, Width: 30, FontSize: 12, SpaceWidth: 4},
		{Text: "world", X: 140, Y: 500, Width: 30, FontSize: 12, SpaceWidth: 4},
	}
	got := ReconstructText(context.Background(), in, nil)
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "world") {
		t.Errorf("same line: got %q, want both words present", got)
	}
	// Both words on the same line → no newline between them.
	if strings.Contains(got, "Hello\nworld") {
		t.Errorf("unexpected newline inside line: %q", got)
	}
}

// TestReconstructText_TrackedSameChunk merges glyphs whose inter-glyph
// gap (Tc + TJ shift expressed in user space) is below the chunk-kerning
// floor, the way PDF.js merges letter-spaced display text. The synthetic
// case mirrors a TJ run with Tc + TJ shift ≈ 0.23pt at FontSize 7.29
// against a chunk floor of 0.102 × 7.29 ≈ 0.74pt, so the residual gap
// stays in the tracking band on every glyph boundary.
func TestReconstructText_TrackedSameChunk(t *testing.T) {
	t.Parallel()
	const fs = 7.29
	chars := []string{"V", "o", "l", "u", "m", "e"}
	widths := []float64{6.52, 5.32, 2.00, 3.23, 4.45, 2.17}
	xs := make([]float64, len(chars))
	xs[0] = 0
	for i := 1; i < len(chars); i++ {
		xs[i] = xs[i-1] + widths[i-1] + 0.23 // residual gap below chunk floor
	}
	xs[1] = xs[0] + widths[0] - 0.16 // 'Vo' overlap as observed on real fixtures
	in := make([]models.TextElement, len(chars))
	for i, c := range chars {
		in[i] = models.TextElement{
			Text:       c,
			X:          xs[i],
			Y:          500,
			Width:      widths[i],
			FontSize:   fs,
			SpaceWidth: fs * 0.25,
			Chunk:      4,
		}
	}
	got := ReconstructText(context.Background(), in, nil)
	if !strings.Contains(got, "Volume") {
		t.Errorf("tracked same-chunk text should merge: got %q, want substring %q", got, "Volume")
	}
}

// TestReconstructText_DifferentLines emits a newline between elements
// whose Y coordinates differ by more than the line threshold.
func TestReconstructText_DifferentLines(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "Top", X: 100, Y: 500, Width: 30, FontSize: 12, SpaceWidth: 4},
		{Text: "Bottom", X: 100, Y: 400, Width: 30, FontSize: 12, SpaceWidth: 4},
	}
	got := ReconstructText(context.Background(), in, nil)
	if !strings.Contains(got, "Top") || !strings.Contains(got, "Bottom") {
		t.Errorf("different lines: got %q, want both words present", got)
	}
	if !strings.Contains(got, "\n") {
		t.Errorf("different Y → expected newline, got %q", got)
	}
}

// TestReconstructText_ContextCancellation honors a cancelled context
// and returns promptly rather than iterating over every element.
func TestReconstructText_ContextCancellation(t *testing.T) {
	t.Parallel()
	// Build 1000 elements so the cancellation check (every 500) fires.
	in := make([]models.TextElement, 1000)
	for i := range in {
		in[i] = models.TextElement{
			Text: "x", X: 100, Y: float64(500 - i), FontSize: 12,
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Either returns empty (early bail) or a partial string, both are
	// acceptable. The contract is "no panic, no block".
	_ = ReconstructText(ctx, in, nil)
}

// TestReconstructText_WithBeads exercises the article-thread beads
// path: when beads are present, elements are sorted by bead membership
// first, then spatially. At minimum the function must not panic on any
// combination of elements and beads.
func TestReconstructText_WithBeads(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "A", X: 100, Y: 500, Width: 10, FontSize: 12},
		{Text: "B", X: 100, Y: 480, Width: 10, FontSize: 12},
	}
	beads := []semantics.Bead{
		{Rect: [4]float64{50, 450, 200, 550}},
	}
	got := ReconstructText(context.Background(), in, beads)
	// Both texts must still appear regardless of ordering strategy.
	if !strings.Contains(got, "A") || !strings.Contains(got, "B") {
		t.Errorf("with beads: got %q, want both tokens present", got)
	}
}
