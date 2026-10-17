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

// TestReconstructText_RunningHeadResetDropsTrailingSpace pins the
// running-head reset behaviour: a large backward X jump on the same
// line (a producer rendering a right-aligned page number then a Tm
// reset to the left margin for the title text) drops the trailing
// space the previous run baked in, so 'p56213 Federal Register' no
// longer surfaces an extra space where the raw byte sequence is
// 'p56213Federal Register'.
func TestReconstructText_RunningHeadResetDropsTrailingSpace(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "p", X: 530, Y: 750, Width: 6, FontSize: 11},
		{Text: " ", X: 536, Y: 750, Width: 3, FontSize: 11},
		{Text: "F", X: 96, Y: 750, Width: 7, FontSize: 11},
		{Text: "e", X: 103, Y: 750, Width: 6, FontSize: 11},
	}
	got := ReconstructText(context.Background(), in, nil)
	if strings.Contains(got, "p Fe") || strings.Contains(got, "p F") {
		t.Errorf("trailing space leaked across backward jump: got %q", got)
	}
	if !strings.Contains(got, "pFe") {
		t.Errorf("expected 'pFe' (running-head reset trims the space): got %q", got)
	}
}

// TestReconstructText_RTLBackwardFlowKeepsSpace guards the running-head
// drop's RTL gate: Hebrew/Arabic glyphs naturally decrease in X as
// the run progresses, so the literal space between two RTL words must
// survive even though gap is negative.
func TestReconstructText_RTLBackwardFlowKeepsSpace(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "ם", X: 416, Y: 758, Width: 7, FontSize: 12},
		{Text: " ", X: 472, Y: 758, Width: 3, FontSize: 12},
		{Text: "ב", X: 476, Y: 758, Width: 6, FontSize: 12},
	}
	got := ReconstructText(context.Background(), in, nil)
	if !strings.Contains(got, " ") {
		t.Errorf("RTL backward flow should preserve literal space: got %q", got)
	}
}

// TestReconstructText_TrackedSameChunk merges glyphs whose inter-glyph
// gap (Tc + TJ shift expressed in user space) is below the chunk-kerning
// floor, the way letter-spaced display text should collapse into a
// single token. The synthetic case mirrors a TJ run with Tc + TJ shift
// ≈ 0.23pt at FontSize 7.29 against a chunk floor of 0.102 × 7.29 ≈
// 0.74pt, so the residual gap stays in the tracking band on every
// glyph boundary.
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

// TestReconstructText_LiteralSpaceOverlappedDropped pins that a literal
// space whose following glyph starts before the end of the glyph ahead of
// it reads as no word break: the space advanced the text matrix and the
// producer moved it back.
func TestReconstructText_LiteralSpaceOverlappedDropped(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "者", X: 100, Y: 500, Width: 12, FontSize: 12},
		{Text: " ", X: 112, Y: 500, Width: 6, FontSize: 12},
		{Text: "，", X: 111.4, Y: 500, Width: 12, FontSize: 12},
	}
	if got := ReconstructText(context.Background(), in, nil); got != "者，" {
		t.Errorf("got %q, want %q", got, "者，")
	}
}

// TestReconstructText_LiteralSpaceWithGapKept pins that a literal space
// followed by a real gap stays a single space.
func TestReconstructText_LiteralSpaceWithGapKept(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "全", X: 100, Y: 500, Width: 12, FontSize: 12},
		{Text: " ", X: 112, Y: 500, Width: 12, FontSize: 12},
		{Text: "世", X: 116, Y: 500, Width: 12, FontSize: 12},
	}
	if got := ReconstructText(context.Background(), in, nil); got != "全 世" {
		t.Errorf("got %q, want %q", got, "全 世")
	}
}

// TestReconstructText_LiteralSpaceExactAbutmentKept pins that a literal
// space stays when the next glyph starts exactly at the end of the
// previous one: the producer placed the word itself and the space is the
// only break it signals.
func TestReconstructText_LiteralSpaceExactAbutmentKept(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "9", X: 100, Y: 500, Width: 4.5, FontSize: 8},
		{Text: " ", X: 104.5, Y: 500, Width: 2.25, FontSize: 8},
		{Text: "a", X: 104.5, Y: 500, Width: 4.5, FontSize: 8},
	}
	if got := ReconstructText(context.Background(), in, nil); got != "9 a" {
		t.Errorf("got %q, want %q", got, "9 a")
	}
}

// TestReconstructText_LiteralSpaceKeptOnMarkedLine pins that a line
// carrying a nonspacing mark keeps its literal spaces whatever the gap,
// since shaped text places glyphs by positions the advance does not give.
func TestReconstructText_LiteralSpaceKeptOnMarkedLine(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "ก", X: 100, Y: 500, Width: 6, FontSize: 12},
		{Text: "ิ", X: 106, Y: 500, Width: 4, FontSize: 12},
		{Text: " ", X: 110, Y: 500, Width: 3, FontSize: 12},
		{Text: "ม", X: 105, Y: 500, Width: 6, FontSize: 12},
	}
	if got := ReconstructText(context.Background(), in, nil); got != "กิ ม" {
		t.Errorf("got %q, want %q", got, "กิ ม")
	}
}

// TestReconstructText_LiteralSpaceSmallerFontKept pins that the cutoff
// follows the smaller of the two font sizes, so a drop cap does not
// swallow the space before the body text that follows it.
func TestReconstructText_LiteralSpaceSmallerFontKept(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "M", X: 100, Y: 500, Width: 30, FontSize: 37.5},
		{Text: " ", X: 130, Y: 500, Width: 9, FontSize: 37.5},
		{Text: "a", X: 131, Y: 500, Width: 5, FontSize: 11},
	}
	if got := ReconstructText(context.Background(), in, nil); got != "M a" {
		t.Errorf("got %q, want %q", got, "M a")
	}
}

// TestReconstructText_IdeographicSpace pins that U+3000 reads as a word
// break: it folds to U+0020 and two in a row read as one.
func TestReconstructText_IdeographicSpace(t *testing.T) {
	t.Parallel()
	var in []models.TextElement
	for i, s := range []string{"目", "　", "　", "录"} {
		in = append(in, models.TextElement{Text: s, X: 100 + float64(i)*15.78, Y: 500, Width: 15.78, FontSize: 15.78})
	}
	if got := ReconstructText(context.Background(), in, nil); got != "目 录" {
		t.Errorf("got %q, want %q", got, "目 录")
	}
}
