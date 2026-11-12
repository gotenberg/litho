package css

import "testing"

// TestMedia_DiscreteFeatures checks the discrete media features against
// the values their clauses give a printed page and a screen (Media
// Queries 4, 5.4 to 5.6, 6.4, and 2.4.2 for the boolean context; Media
// Queries 5, 4.9, 6.5, 12.1 to 12.5).
//
// # Corpus evidence
//
//   - probe, html: one line per query, printed by Chromium through
//     gotenberg/gotenberg:9.4.1 with default options and with
//     emulatedMediaType=screen, gives every result below; Chromium
//     differs only on (scripting), true, where litho runs no script.
func TestMedia_DiscreteFeatures(t *testing.T) {
	t.Parallel()
	screen := Media{Type: "screen", Width: 595.28, Height: 841.89}
	for _, tc := range []struct {
		q             string
		print, screen bool
	}{
		{"(overflow-block: paged)", true, false},
		{"(overflow-block: scroll)", false, true},
		{"(overflow-block: none)", false, false},
		{"(overflow-block)", true, true},
		{"(overflow-inline: none)", true, false},
		{"(overflow-inline: scroll)", false, true},
		{"(overflow-inline)", false, true},
		{"(update)", false, true},
		{"(update: none)", true, false},
		{"(update: fast)", false, true},
		{"(forced-colors: none)", true, true},
		{"(forced-colors: active)", false, false},
		{"(forced-colors)", false, false},
		{"(prefers-color-scheme)", true, true},
		{"(prefers-color-scheme: light)", true, true},
		{"(prefers-color-scheme: dark)", false, false},
		{"(color-gamut)", true, true},
		{"(color-gamut: srgb)", true, true},
		{"(color-gamut: p3)", false, false},
		{"(dynamic-range)", true, true},
		{"(dynamic-range: standard)", true, true},
		{"(dynamic-range: high)", false, false},
		{"(display-mode)", true, true},
		{"(display-mode: browser)", true, true},
		{"(display-mode: standalone)", false, false},
		{"(prefers-reduced-motion)", false, false},
		{"(prefers-reduced-motion: no-preference)", true, true},
		{"(prefers-contrast)", false, false},
		{"(prefers-reduced-transparency)", false, false},
		{"(hover)", false, false},
		{"(hover: none)", true, true},
		{"(pointer)", false, false},
		{"(any-pointer)", false, false},
		{"(scripting)", false, false},
		{"(inverted-colors)", false, false},
		{"(color-index)", false, false},
		{"(monochrome)", false, false},
		{"(grid)", false, false},
	} {
		if got := MatchMedia(tc.q, a4); got != tc.print {
			t.Errorf("print %s = %v, want %v", tc.q, got, tc.print)
		}
		if got := MatchMedia(tc.q, screen); got != tc.screen {
			t.Errorf("screen %s = %v, want %v", tc.q, got, tc.screen)
		}
	}
}

// TestMedia_CallerFeatures checks that a caller's value of a discrete
// feature replaces the default in both contexts, and that a wider colour
// gamut or dynamic range also matches the narrower values it includes
// (Media Queries 4, 6.4; Media Queries 5, 6.5).
func TestMedia_CallerFeatures(t *testing.T) {
	t.Parallel()
	m := Media{Type: "print", Width: 595, Height: 842, Features: map[string]string{
		"forced-colors": "active", "color-gamut": "p3", "dynamic-range": "high", "prefers-contrast": "more",
	}}
	for _, tc := range []struct {
		q    string
		want bool
	}{
		{"(forced-colors)", true},
		{"(forced-colors: none)", false},
		{"(color-gamut: srgb)", true},
		{"(color-gamut: p3)", true},
		{"(color-gamut: rec2020)", false},
		{"(dynamic-range: standard)", true},
		{"(prefers-contrast)", true},
	} {
		if got := MatchMedia(tc.q, m); got != tc.want {
			t.Errorf("%s = %v, want %v", tc.q, got, tc.want)
		}
	}
}
