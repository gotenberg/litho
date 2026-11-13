package typography

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReorderActualText_OnlyClusters locks when extraction reads a
// sequence's ActualText: when its glyphs hold every character of it,
// decomposed, and do not spell it, and never when a character of it is
// not drawn.
//
// # Corpus evidence
//
//   - litho/html th-line-break and the Thai fixtures of litho/odt, litho/ods,
//     litho/odp, litho/docx, litho/pptx and litho/xlsx: SARA AM is drawn as
//     NIKHAHIT and SARA AA inside an ActualText span; read glyph by glyph
//     it came out as U+0E4D U+0E32.
//   - probe, html: Noto Sans Tamil and Khmer draw a dotted circle under a
//     mark, Noto Sans Khmer U+17BE as two glyphs around the consonant.
func TestReorderActualText_OnlyClusters(t *testing.T) {
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
		{"character drawn in parts", []models.TextElement{el("ก", 0, 5), el("ํ", 5, 0), el("า", 5, 4)}, "กำ", []string{"กำ", "", ""}, 0, 9},
		{"dotted circle under a mark", []models.TextElement{el("◌", 0, 5), el("்", 0, 0)}, "்", []string{"்", ""}, 0, 5},
		{"split vowel around a consonant", []models.TextElement{el("ើ", 0, 3), el("ក", 3, 5), el("ី", 6, 0)}, "កើ", []string{"កើ", "", ""}, 0, 8},
		{"extra glyph, a character missing", []models.TextElement{el("b", 0, 5), el("c", 5, 5), el("d", 10, 5)}, "ab", []string{"b", "c", "d"}, 0, 5},
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
