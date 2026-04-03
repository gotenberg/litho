package typography

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/models"
)

// reorderTexts is a test helper that applies reorderBidiIndices and returns
// the reordered text strings for easy assertion.
func reorderTexts(input []string) []string {
	indices := make([]int, len(input))
	for i := range indices {
		indices[i] = i
	}
	reordered := reorderBidiIndices(input, indices)
	result := make([]string, len(reordered))
	for i, idx := range reordered {
		result[i] = input[idx]
	}
	return result
}

func TestReorderBidiIndices_LTROnly(t *testing.T) {
	input := []string{"H", "e", "l", "l", "o"}
	got := reorderTexts(input)
	want := "Hello"
	if join(got) != want {
		t.Errorf("LTR only: got %q, want %q", join(got), want)
	}
}

func TestReorderBidiIndices_RTLOnly(t *testing.T) {
	input := []string{"ם", "ו", "ל", "ש"}
	got := reorderTexts(input)
	want := "שלום"
	if join(got) != want {
		t.Errorf("RTL only: got %q, want %q", join(got), want)
	}
}

func TestReorderBidiIndices_Mixed(t *testing.T) {
	input := []string{"H", "e", "l", "l", "o", " ", "ם", "ו", "ל", "ש", " ", "W", "o", "r", "l", "d"}
	got := reorderTexts(input)
	result := join(got)
	want := "Hello שלום World"
	if result != want {
		t.Errorf("Mixed: got %q, want %q", result, want)
	}
}

func TestReorderBidiIndices_Arabic(t *testing.T) {
	input := []string{"ا", "ب", "ح", "ر", "م"}
	got := reorderTexts(input)
	want := "مرحبا"
	if join(got) != want {
		t.Errorf("Arabic: got %q, want %q", join(got), want)
	}
}

func TestReorderBidiIndices_Empty(t *testing.T) {
	got := reorderBidiIndices(nil, nil)
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
			name:  "discretionary hyphen at line break",
			input: "docu\u2010\nment",
			want:  "document",
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

func join(ss []string) string {
	var result strings.Builder
	for _, s := range ss {
		result.WriteString(s)
	}
	return result.String()
}
