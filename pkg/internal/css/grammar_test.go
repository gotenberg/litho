package css

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestGrammar_RefusesValuesOutsideIt checks values the grammars refuse,
// each with the invalid-value warning and without effect:
// page-break-inside takes auto and avoid alone (CSS Fragmentation 3,
// 3.4), caption-side top and bottom (CSS Tables 3, 3.5.3), quotes no
// match-parent (CSS Generated Content 3, 4.1), and word-space-transform
// no auto-phrase without space or ideographic-space (CSS Text 4, 4.3).
func TestGrammar_RefusesValuesOutsideIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		prop, before, value string
		unchanged           func(s *Style) bool
	}{
		{"page-break-inside", "break-inside: avoid", "avoid-page", func(s *Style) bool { return s.BreakInside == InsideAvoid }},
		{"page-break-inside", "break-inside: avoid", "avoid-column", func(s *Style) bool { return s.BreakInside == InsideAvoid }},
		{"page-break-inside", "break-inside: avoid", "avoid-region", func(s *Style) bool { return s.BreakInside == InsideAvoid }},
		{"page-break-inside", "break-inside: avoid", "left", func(s *Style) bool { return s.BreakInside == InsideAvoid }},
		{"caption-side", "caption-side: bottom", "block-start", func(s *Style) bool { return s.CaptionSide == CaptionBottom }},
		{"caption-side", "caption-side: top", "block-end", func(s *Style) bool { return s.CaptionSide == CaptionTop }},
		{"quotes", "quotes: none", "match-parent", func(s *Style) bool { return s.Quotes.None }},
		{"word-space-transform", "word-space-transform: space", "auto-phrase", func(s *Style) bool { return s.WordSpaceTransform == WordSpaceSpace }},
	} {
		c := compute(t, `<body><div id=t></div></body>`, `#t { `+tc.before+`; `+tc.prop+`: `+tc.value+` }`)
		if !tc.unchanged(c.of("t")) {
			t.Errorf("%s: %s applied", tc.prop, tc.value)
		}
		if !hasWarning(c.c.Warnings(), warn.CSSInvalid, `"`+tc.prop+`"`) {
			t.Errorf("%s: %s: no invalid-value warning", tc.prop, tc.value)
		}
	}
}

// TestGrammar_AcceptsTheValuesItAllows checks the values beside the
// refused ones that the same grammars allow.
func TestGrammar_AcceptsTheValuesItAllows(t *testing.T) {
	t.Parallel()
	s := styleOf(t, "page-break-inside: avoid; caption-side: bottom; quotes: '<' '>'; word-space-transform: ideographic-space auto-phrase")
	if s.BreakInside != InsideAvoid || s.CaptionSide != CaptionBottom || len(s.Quotes.Pairs) != 1 || s.WordSpaceTransform != WordSpaceIdeographic {
		t.Errorf("break-inside %v, caption-side %v, quotes %v, word-space-transform %v", s.BreakInside, s.CaptionSide, s.Quotes, s.WordSpaceTransform)
	}
	if s := styleOf(t, "break-inside: avoid; page-break-inside: auto"); s.BreakInside != InsideAuto {
		t.Errorf("page-break-inside: auto: break-inside %v", s.BreakInside)
	}
}
