package typography

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/models"
)

func TestReorderBidiIndices_LTROnly(t *testing.T) {
	input := []string{"H", "e", "l", "l", "o"}
	xPos := []float64{10, 16, 22, 28, 34}
	indices := make([]int, len(input))
	for i := range indices {
		indices[i] = i
	}
	reordered := reorderBidiIndices(input, indices, xPos)
	got := joinByIndices(input, reordered)
	want := "Hello"
	if got != want {
		t.Errorf("LTR only: got %q, want %q", got, want)
	}
}

func TestReorderBidiIndices_RTLVisualOrder(t *testing.T) {
	// Visual order: leftmost glyph first in stream (X increasing for RTL
	// = last logical character first). Should reverse.
	input := []string{"ם", "ו", "ל", "ש"}
	xPos := []float64{10, 16, 22, 28} // X increasing = visual order for RTL
	indices := make([]int, len(input))
	for i := range indices {
		indices[i] = i
	}
	reordered := reorderBidiIndices(input, indices, xPos)
	got := joinByIndices(input, reordered)
	want := "שלום"
	if got != want {
		t.Errorf("RTL visual order: got %q, want %q", got, want)
	}
}

func TestReorderBidiIndices_RTLLogicalOrder(t *testing.T) {
	// Logical order: first logical character first (X decreasing for RTL
	// = rightmost character first in reading order). Should NOT reverse.
	input := []string{"ש", "ל", "ו", "ם"}
	xPos := []float64{28, 22, 16, 10} // X decreasing = logical order for RTL
	indices := make([]int, len(input))
	for i := range indices {
		indices[i] = i
	}
	reordered := reorderBidiIndices(input, indices, xPos)
	got := joinByIndices(input, reordered)
	want := "שלום" // Already correct, not reversed
	if got != want {
		t.Errorf("RTL logical order: got %q, want %q", got, want)
	}
}

func TestReorderBidiIndices_Mixed(t *testing.T) {
	// Visual order RTL embedded in LTR: Hebrew glyphs have increasing X.
	input := []string{"H", "e", "l", "l", "o", " ", "ם", "ו", "ל", "ש", " ", "W", "o", "r", "l", "d"}
	xPos := []float64{10, 16, 22, 28, 34, 40, 52, 58, 64, 70, 76, 82, 88, 94, 100, 106}
	indices := make([]int, len(input))
	for i := range indices {
		indices[i] = i
	}
	reordered := reorderBidiIndices(input, indices, xPos)
	got := joinByIndices(input, reordered)
	want := "Hello שלום World"
	if got != want {
		t.Errorf("Mixed: got %q, want %q", got, want)
	}
}

func TestReorderBidiIndices_ArabicVisualOrder(t *testing.T) {
	// Visual order: leftmost glyph first (X increasing).
	input := []string{"ا", "ب", "ح", "ر", "م"}
	xPos := []float64{10, 16, 22, 28, 34} // X increasing = visual order for RTL
	indices := make([]int, len(input))
	for i := range indices {
		indices[i] = i
	}
	reordered := reorderBidiIndices(input, indices, xPos)
	got := joinByIndices(input, reordered)
	want := "مرحبا"
	if got != want {
		t.Errorf("Arabic visual: got %q, want %q", got, want)
	}
}

func TestReorderBidiIndices_Empty(t *testing.T) {
	got := reorderBidiIndices(nil, nil, nil)
	if len(got) != 0 {
		t.Errorf("empty: got %v, want nil/empty", got)
	}
}

func TestNormalizeSoftHyphens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "soft hyphen at line break",
			input: "docu\u00AD\nment",
			want:  "document",
		},
		{
			name:  "unicode hyphen preserved at line break",
			input: "docu\u2010\nment",
			want:  "docu\u2010\nment",
		},
		{
			name:  "standalone soft hyphens become regular hyphens",
			input: "soft\u00ADhyphen",
			want:  "soft-hyphen",
		},
		{
			name:  "no hyphens unchanged",
			input: "hello\nworld",
			want:  "hello\nworld",
		},
		{
			name:  "multiple soft hyphens at line breaks",
			input: "un\u00AD\nbreak\u00AD\nable",
			want:  "unbreakable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeSoftHyphens(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReconstructText_BidiLine(t *testing.T) {
	// Visual order: Hebrew glyphs with X increasing (leftmost first in stream).
	elements := []models.TextElement{
		{Text: "ם", X: 10, Y: 100, FontSize: 12},
		{Text: "ו", X: 16, Y: 100, FontSize: 12},
		{Text: "ל", X: 22, Y: 100, FontSize: 12},
		{Text: "ש", X: 28, Y: 100, FontSize: 12},
	}

	got := ReconstructText(context.Background(), elements, nil)
	want := "שלום"
	if got != want {
		t.Errorf("ReconstructText bidi: got %q, want %q", got, want)
	}
}

func TestReconstructText_SoftHyphenAcrossLines(t *testing.T) {
	elements := []models.TextElement{
		{Text: "d", X: 10, Y: 100, FontSize: 12},
		{Text: "o", X: 16, Y: 100, FontSize: 12},
		{Text: "c", X: 22, Y: 100, FontSize: 12},
		{Text: "u", X: 28, Y: 100, FontSize: 12},
		{Text: "\u00AD", X: 34, Y: 100, FontSize: 12},
		{Text: "m", X: 10, Y: 80, FontSize: 12},
		{Text: "e", X: 16, Y: 80, FontSize: 12},
		{Text: "n", X: 22, Y: 80, FontSize: 12},
		{Text: "t", X: 28, Y: 80, FontSize: 12},
	}

	got := ReconstructText(context.Background(), elements, nil)
	want := "document"
	if got != want {
		t.Errorf("ReconstructText soft hyphen: got %q, want %q", got, want)
	}
}

// Ensure the beads parameter is unused here but compiles.
var _ = []semantics.Bead{}

func joinByIndices(texts []string, indices []int) string {
	var sb strings.Builder
	for _, idx := range indices {
		sb.WriteString(texts[idx])
	}
	return sb.String()
}
