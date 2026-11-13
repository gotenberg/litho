package css

import "github.com/gotenberg/litho/pkg/internal/warn"

// Hyphens is the hyphens property (CSS Text 3, 5.4).
type Hyphens uint8

// The hyphens values.
const (
	// HyphensManual breaks words at their soft hyphens alone, the initial
	// value.
	HyphensManual Hyphens = iota
	// HyphensNone breaks no word at a soft hyphen.
	HyphensNone
	// HyphensAuto lets the user agent hyphenate by a dictionary as well:
	// litho has none, and breaks words at their soft hyphens alone.
	HyphensAuto
)

// textProps returns the properties of CSS Text 3 and 4 this file adds:
// hyphens and hyphenate-character.
func textProps() map[propID]propDef {
	return map[propID]propDef{
		pHyphens: {name: "hyphens", inherited: true, parse: parseHyphens, compute: func(_ *styler, st *Style, v any) {
			if h, ok := v.(Hyphens); ok {
				st.Hyphens = h
			}
		}, copyFrom: field(func(s *Style) *Hyphens { return &s.Hyphens })},
		pHyphenateCharacter: {name: "hyphenate-character", inherited: true, parse: parseHyphenateCharacter, compute: func(_ *styler, st *Style, v any) {
			if h, ok := v.(hyphenateCharacter); ok {
				st.HyphenateCharacter = HyphenateCharacter(h)
			}
		}, copyFrom: field(func(s *Style) *HyphenateCharacter { return &s.HyphenateCharacter })},
	}
}

// parseHyphens parses hyphens (CSS Text 3, 5.4): auto, which needs a
// hyphenation dictionary, breaks at soft hyphens alone, with a warning.
func parseHyphens(v []token, c *declCtx) (any, bool) {
	k, ok := keyword(v)
	if !ok {
		return nil, false
	}
	switch k {
	case "none":
		return HyphensNone, true
	case "manual":
		return HyphensManual, true
	case "auto":
		c.warn(warn.CSSUnsupported, warn.Approximated, "hyphens: auto hyphenates at soft hyphens alone")
		return HyphensAuto, true
	}
	return nil, false
}

// HyphenateCharacter is the hyphenate-character property (CSS Text 4,
// 5.5): the string Text a hyphenated line ends with when Given, auto, the
// hyphen the font suits, when not.
type HyphenateCharacter struct {
	Given bool
	Text  string
}

// hyphenateCharacter is a parsed hyphenate-character value.
type hyphenateCharacter HyphenateCharacter

// parseHyphenateCharacter parses hyphenate-character: auto | <string>.
func parseHyphenateCharacter(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "auto" {
		return hyphenateCharacter{}, true
	}
	t, ok := single(v)
	if !ok || t.typ != tString {
		return nil, false
	}
	return hyphenateCharacter{Given: true, Text: t.value}, true
}
