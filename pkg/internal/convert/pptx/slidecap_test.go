package pptx

import (
	"bytes"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// TestLoad_FailsADeckItsPagesCannotPrint checks that a deck whose printed
// slides need more pages than the conversion allows fails with
// limits.ErrTooManyPages rather than losing its last slides, that hidden
// slides it does not print do not count, and that notes pages and
// handouts count as the pages they take.
func TestLoad_FailsADeckItsPagesCannotPrint(t *testing.T) {
	t.Parallel()
	shown := testSlide{tree: phShape("Title 1", "title", -1, 0, 0, 0, 0, para("Shown"))}
	hidden := testSlide{attrs: `show="0"`, tree: shown.tree}
	deck := zipFiles(t, deckFiles(testDeck{slides: []testSlide{shown, hidden, shown, hidden, shown}}))
	for _, tc := range []struct {
		name string
		opts office.Options
		fail bool
	}{
		{"three printed on three pages", office.Options{MaxPages: 3}, false},
		{"three printed on two pages", office.Options{MaxPages: 2}, true},
		{"hidden slides printed", office.Options{MaxPages: 4, HiddenSlides: true}, true},
		{"notes pages", office.Options{MaxPages: 5, NotesPages: true}, true},
		{"notes pages that fit", office.Options{MaxPages: 6, NotesPages: true}, false},
		{"handouts of two", office.Options{MaxPages: 2, Handouts: 2}, false},
	} {
		_, _, _, err := load(t.Context(), bytes.NewReader(deck), testFonts(t), tc.opts)
		if got := errors.Is(err, limits.ErrTooManyPages); got != tc.fail || !tc.fail && err != nil {
			t.Errorf("%s: %v, want failure %t", tc.name, err, tc.fail)
		}
	}
}
