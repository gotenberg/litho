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

// TestMedia_NumericFeatures checks resolution (Media Queries 4, 6.1),
// the device pixel ratio and -webkit-transform-3d of the Compatibility
// Standard, color, color-index and monochrome with their
// min- and max- forms (6.2, 6.3, 6.5), scan (5.3), the viewport segments
// and device-posture (Media Queries 5, 4.2 and 4.3), and their range
// forms, on a printed page, 300 dots an inch, and on a screen, 1 dppx.
// "not" of each query whose result is false matches, so none is unknown.
//
// # Corpus evidence
//
//   - probe, html: one line per query, printed by Chromium through
//     gotenberg/gotenberg:9.4.1 with default options and with
//     emulatedMediaType=screen, gives every result below.
func TestMedia_NumericFeatures(t *testing.T) {
	t.Parallel()
	screen := Media{Type: "screen", Width: 595.28, Height: 841.89}
	for _, tc := range []struct {
		q             string
		print, screen bool
	}{
		{"(resolution)", true, true},
		{"(min-resolution: 300dpi)", true, false},
		{"(min-resolution: 400dpi)", false, false},
		{"(min-resolution: 3x)", true, false},
		{"(min-resolution: 3.5x)", false, false},
		{"(max-resolution: 2dppx)", false, true},
		{"(resolution: 3.125dppx)", true, false},
		{"(resolution: 1dppx)", false, true},
		{"(resolution >= 300dpi)", true, false},
		{"(2x < resolution < 4x)", true, false},
		{"(-webkit-device-pixel-ratio)", true, true},
		{"(-webkit-min-device-pixel-ratio: 2)", true, false},
		{"(-webkit-min-device-pixel-ratio: 4)", false, false},
		{"(-webkit-max-device-pixel-ratio: 1)", false, true},
		{"(-webkit-device-pixel-ratio: 3.125)", true, false},
		{"(-webkit-transform-3d)", true, true},
		{"(min-color: 8)", true, true},
		{"(min-color: 9)", false, false},
		{"(max-color: 8)", true, true},
		{"(color: 4)", false, false},
		{"(color > 4)", true, true},
		{"(min-color-index: 0)", true, true},
		{"(color-index: 1)", false, false},
		{"(max-monochrome: 0)", true, true},
		{"(device-aspect-ratio)", true, true},
		{"(scan)", false, false},
		{"(scan: progressive)", false, false},
		{"(horizontal-viewport-segments: 1)", true, true},
		{"(vertical-viewport-segments > 0)", true, true},
		{"(device-posture)", true, true},
		{"(device-posture: folded)", false, false},
	} {
		for _, side := range []struct {
			m    Media
			want bool
		}{{a4, tc.print}, {screen, tc.screen}} {
			if got := MatchMedia(tc.q, side.m); got != side.want {
				t.Errorf("%s %s = %v, want %v", side.m.Type, tc.q, got, side.want)
			}
			if !side.want && !MatchMedia("not "+tc.q, side.m) {
				t.Errorf("%s not %s does not match: the query is unknown", side.m.Type, tc.q)
			}
		}
	}
	for _, q := range []string{"(-webkit-min-device-pixel-ratio: 1x)", "(min-horizontal-viewport-segments: 1)", "(scan: foo)", "(resolution >= calc(sign(50%) * 1dpi))"} {
		if MatchMedia(q, a4) || MatchMedia("not "+q, a4) {
			t.Errorf("%s is not unknown", q)
		}
	}
}
