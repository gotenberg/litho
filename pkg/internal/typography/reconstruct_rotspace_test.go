package typography

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReconstructText_RotatedOverprintedSpace pins that a literal space the
// next glyph overprints marks no word break in text rotated by 90° or -90°,
// as in horizontal text: "b" starts 0.1 unit past the end of "a" along the
// advance, so the space shown between them is moved back over itself.
func TestReconstructText_RotatedOverprintedSpace(t *testing.T) {
	t.Parallel()

	// column shows "a", " " and "b" from y, advancing by step (+1 up, -1
	// down) with "b" 0.1 unit past the end of "a".
	column := func(y, step, angle float64) []models.TextElement {
		el := func(s string, at, w float64) models.TextElement {
			return models.TextElement{Text: s, X: 100, Y: y + step*at, Width: w, FontSize: 12, Angle: angle}
		}
		return []models.TextElement{el("a", 0, 6), el(" ", 6, 3), el("b", 6.1, 6)}
	}
	for _, tc := range []struct {
		name  string
		elems []models.TextElement
	}{
		{"rotated 90°", column(300, 1, math.Pi/2)},
		{"rotated -90°", column(300, -1, -math.Pi/2)},
	} {
		if got := ReconstructText(context.Background(), tc.elems, nil); got != "ab" {
			t.Errorf("%s: got %q, want %q", tc.name, got, "ab")
		}
	}
}

// TestReconstructText_SpaceAcrossDirectionsKept pins that the space between
// a horizontal glyph and a rotated one stays: the gap is read along the
// rotated glyph's advance, which says nothing about the horizontal one.
func TestReconstructText_SpaceAcrossDirectionsKept(t *testing.T) {
	t.Parallel()

	elems := []models.TextElement{
		{Text: "a", X: 100, Y: 300, Width: 6, FontSize: 12},
		{Text: " ", X: 106, Y: 300, Width: 3, FontSize: 12},
		{Text: "b", X: 106, Y: 300, Width: 6, FontSize: 12, Angle: math.Pi / 2},
	}
	if got := ReconstructText(context.Background(), elems, nil); got == "ab" {
		t.Errorf("got %q, want the space kept between directions", got)
	}
}
