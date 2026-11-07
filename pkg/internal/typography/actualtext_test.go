package typography

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

func TestReorderActualText_OnlyReorderings(t *testing.T) {
	el := func(text string, x, w float64) models.TextElement {
		return models.TextElement{Text: text, X: x, Width: w, Y: 10, FontSize: 10}
	}
	tests := []struct {
		name   string
		elems  []models.TextElement
		actual string
		want   []string
		x, w   float64
	}{
		{"vowel sign first", []models.TextElement{el("ि", 0, 3), el("न", 3, 6)}, "नि", []string{"नि", ""}, 0, 9},
		{"already in order", []models.TextElement{el("a", 0, 5), el("b", 5, 5)}, "ab", []string{"a", "b"}, 0, 5},
		{"small caps", []models.TextElement{el("V", 0, 5), el("X", 5, 5)}, "vx", []string{"V", "X"}, 0, 5},
		{"other characters", []models.TextElement{el("b", 0, 5), el("c", 5, 5)}, "ab", []string{"b", "c"}, 0, 5},
		{"missing glyph text", []models.TextElement{el("b", 0, 5), el("", 5, 5)}, "ab", []string{"b", ""}, 0, 5},
		{"one element", []models.TextElement{el("ba", 0, 5)}, "ab", []string{"ba"}, 0, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reorderActualText(tt.elems, tt.actual)
			for i, e := range tt.elems {
				if e.Text != tt.want[i] {
					t.Errorf("element %d = %q, want %q", i, e.Text, tt.want[i])
				}
			}
			if tt.elems[0].X != tt.x || tt.elems[0].Width != tt.w {
				t.Errorf("first element at %v wide %v, want %v wide %v", tt.elems[0].X, tt.elems[0].Width, tt.x, tt.w)
			}
		})
	}
}
