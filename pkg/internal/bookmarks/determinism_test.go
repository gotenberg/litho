package bookmarks

import (
	"encoding/json"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// tiedSizePages holds two font sizes of equal weight, 12 and 48 points with
// six characters each, as pdfjs issue11713 does. Which of the two is the body
// size decides whether the 48-point line is a heading at all.
func tiedSizePages() []PageTextElements {
	return []PageTextElements{{
		PageNum: 1,
		Elements: []models.TextElement{
			{Text: "Theory", X: 50, Y: 800, FontSize: 48},
			{Text: "abcdef", X: 50, Y: 700, FontSize: 12},
		},
	}}
}

func TestDetectBodyFontSizeTieTakesTheSmallerSize(t *testing.T) {
	var elements []models.TextElement
	for _, p := range tiedSizePages() {
		elements = append(elements, p.Elements...)
	}
	for range 200 {
		if got := detectBodyFontSize(elements); got != 12 {
			t.Fatalf("body size: got %v, want 12", got)
		}
	}
}

// TestGenerateFromFontSizeIsDeterministic runs generation repeatedly on an
// input whose body size is a tie and requires the same outline every time.
// Two sizes in a Go map come out in either order, so 200 runs that agree
// without a deterministic tie break have a chance of 2^-199.
func TestGenerateFromFontSizeIsDeterministic(t *testing.T) {
	opts := Options{}
	opts.applyDefaults()
	first, err := json.Marshal(GenerateFromFontSize(t.Context(), tiedSizePages(), opts))
	if err != nil {
		t.Fatal(err)
	}
	for i := range 200 {
		got, err := json.Marshal(GenerateFromFontSize(t.Context(), tiedSizePages(), opts))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(first) {
			t.Fatalf("run %d: got %s, want %s", i+2, got, first)
		}
	}
}
