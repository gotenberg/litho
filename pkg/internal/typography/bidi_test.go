package typography

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/models"
)

func TestReorderBidiLine_LTROnly(t *testing.T) {
	input := []string{"H", "e", "l", "l", "o"}
	got := reorderBidiLine(input)
	want := "Hello"
	if join(got) != want {
		t.Errorf("LTR only: got %q, want %q", join(got), want)
	}
}

func TestReorderBidiLine_RTLOnly(t *testing.T) {
	// Hebrew: שלום (Shin, Lamed, Vav, Mem)
	// In PDF visual order they are stored left-to-right as drawn,
	// but logical order is right-to-left, so we must reverse.
	input := []string{"ם", "ו", "ל", "ש"}
	got := reorderBidiLine(input)
	want := "שלום"
	if join(got) != want {
		t.Errorf("RTL only: got %q, want %q", join(got), want)
	}
}

func TestReorderBidiLine_Mixed(t *testing.T) {
	// "Hello שלום World" in visual PDF order:
	// H e l l o [space] ם ו ל ש [space] W o r l d
	input := []string{"H", "e", "l", "l", "o", " ", "ם", "ו", "ל", "ש", " ", "W", "o", "r", "l", "d"}
	got := reorderBidiLine(input)
	result := join(got)
	// The RTL run should be reversed to שלום
	want := "Hello שלום World"
	if result != want {
		t.Errorf("Mixed: got %q, want %q", result, want)
	}
}

func TestReorderBidiLine_Arabic(t *testing.T) {
	// Arabic: مرحبا (Meem, Ra, Ha, Ba, Alef)
	// Visual order (as drawn left-to-right): ا ب ح ر م
	input := []string{"ا", "ب", "ح", "ر", "م"}
	got := reorderBidiLine(input)
	want := "مرحبا"
	if join(got) != want {
		t.Errorf("Arabic: got %q, want %q", join(got), want)
	}
}

func TestReorderBidiLine_Empty(t *testing.T) {
	got := reorderBidiLine(nil)
	if len(got) != 0 {
		t.Errorf("empty: got %v, want nil/empty", got)
	}
}

func TestStripSoftHyphens(t *testing.T) {
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
			name:  "standalone soft hyphens removed",
			input: "soft\u00ADhyphen",
			want:  "softhyphen",
		},
		{
			name:  "no hyphens unchanged",
			input: "hello\nworld",
			want:  "hello\nworld",
		},
		{
			name:  "multiple soft hyphens",
			input: "un\u00AD\nbreak\u00AD\nable",
			want:  "unbreakable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripSoftHyphens(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReconstructText_BidiLine(t *testing.T) {
	// Simulate a line with Hebrew characters in visual (PDF) order.
	// They should be reversed to logical order in the output.
	// X spacing: each char at prev.X + 0.5*fontSize to avoid gap detection.
	elements := []models.TextElement{
		{Text: "ם", X: 10, Y: 100, FontSize: 12},
		{Text: "ו", X: 16, Y: 100, FontSize: 12},
		{Text: "ל", X: 22, Y: 100, FontSize: 12},
		{Text: "ש", X: 28, Y: 100, FontSize: 12},
	}

	got := ReconstructText(elements, nil)
	want := "שלום"
	if got != want {
		t.Errorf("ReconstructText bidi: got %q, want %q", got, want)
	}
}

func TestReconstructText_SoftHyphenAcrossLines(t *testing.T) {
	// "docu-" on line 1, "ment" on line 2, with a soft hyphen.
	// X spacing: 6pt apart with fontSize 12 → gap = 6-6=0, no space inserted.
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

	got := ReconstructText(elements, nil)
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
