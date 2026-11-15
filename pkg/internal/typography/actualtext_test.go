package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReorderActualText_OnlyClusters locks when extraction reads a
// sequence's ActualText: when its glyphs hold every character of it,
// decomposed, and do not spell it, and never when a character of it is
// not drawn. The text goes to the first glyph that advances, so a mark
// drawn before its base gives it to the base, also when the glyphs spell
// it.
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
		{"mark drawn before its base", []models.TextElement{el("ً", 2, 0), el("ر", 0, 6.6)}, "رً", []string{"", "رً"}, 2, 0},
		{"glyphs spelling the text, a mark first", []models.TextElement{el("ي", 2, 0), el("َّ", 0, 6.6)}, "يَّ", []string{"", "يَّ"}, 2, 0},
		{"glyphs spelling the text, a base first", []models.TextElement{el("ر", 0, 6.6), el("ً", 2, 0)}, "رً", []string{"ر", "ً"}, 0, 6.6},
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

// TestReconstructText_MarkBeforeItsBaseInActualText pins that an Arabic
// tanween drawn before its letter, lowered off the baseline, inside an
// ActualText span that a marked-content sequence opens, reads inside its
// word: the span's text goes to the letter ([reorderActualText]), and the
// mark, left with no character and no advance, starts no line
// ([takesNoPlace]).
func TestReconstructText_MarkBeforeItsBaseInActualText(t *testing.T) {
	el := func(text string, x, y, w float64, chunk int) models.TextElement {
		return models.TextElement{Text: text, X: x, Y: y, Width: w, FontSize: 22, Chunk: chunk, MCID: 27}
	}
	// "أحرارًا" in visual order, the tanween at 350.79 and 4.83 below the
	// baseline, then its letter.
	elems := []models.TextElement{
		el("ا", 342.66, 367.05, 6.09, 1),
		el("ً", 350.79, 362.22, 0, 2),
		el("ر", 348.77, 367.05, 10.60, 3),
		el("ا", 359.40, 367.05, 6.09, 3),
		el("ر", 365.51, 367.05, 12.12, 3),
		el("ح", 377.65, 367.05, 13.60, 3),
		el("أ", 391.25, 367.05, 6.09, 3),
	}
	reorderActualText(elems[1:3], "رً")
	f := runFont{size: 22, name: "F1"}
	fonts := []runFont{f, f, f, f, f, f, f}
	for i := 1; i < len(fonts); i++ {
		fonts[i].marked = min(i, 2)
	}
	if got, want := reconstructText(context.Background(), elems, fonts, nil), "أحرارًا"; got != want {
		t.Errorf("reconstructText = %q, want %q", got, want)
	}
}

// TestReconstructText_MarksLeftByActualTextAnchorNoGap pins that the marks
// whose text an ActualText span gave to their letters, glyphs with no
// character and no advance, anchor no word gap on a line read back from
// visual order ([takesNoPlace]): the gap from a mark set left of its letter
// to that letter is no gap between words.
func TestReconstructText_MarksLeftByActualTextAnchorNoGap(t *testing.T) {
	el := func(text string, x, y, w float64) models.TextElement {
		return models.TextElement{Text: text, X: x, Y: y, Width: w, FontSize: 22}
	}
	// "كَتَبَ" in visual order, each fatha drawn before its letter.
	elems := []models.TextElement{
		el("", 437.51, 364.90, 0),
		el("بَ", 433.34, 367.05, 21.58),
		el("", 452.66, 365.44, 0),
		el("تَ", 454.95, 367.05, 6.62),
		el("", 458.77, 370.81, 0),
		el("كَ", 461.59, 367.05, 10.47),
	}
	if got, want := ReconstructText(context.Background(), elems, nil), "كَتَبَ"; got != want {
		t.Errorf("ReconstructText = %q, want %q", got, want)
	}
}
