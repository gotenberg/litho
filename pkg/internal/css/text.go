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
// hyphens, hyphenate-character, text-wrap-mode and text-wrap-style.
func textProps() map[propID]propDef {
	return map[propID]propDef{
		pTextWrapMode: {name: "text-wrap-mode", inherited: true, parse: func(v []token, _ *declCtx) (any, bool) {
			k, ok := keyword(v)
			switch {
			case ok && k == "wrap":
				return textWrapMode(true), true
			case ok && k == "nowrap":
				return textWrapMode(false), true
			}
			return nil, false
		}, compute: func(_ *styler, st *Style, v any) {
			if w, ok := v.(textWrapMode); ok {
				st.WhiteSpace = whiteSpaceOf(collapseOf(st.WhiteSpace), bool(w))
			}
		}, copyFrom: func(dst, src *Style) {
			dst.WhiteSpace = whiteSpaceOf(collapseOf(dst.WhiteSpace), wrapsOf(src.WhiteSpace))
		}},
		pTextWrapStyle: enumProp("text-wrap-style", true, func(s *Style) *TextWrapStyle { return &s.TextWrapStyle }, textWrapStyles),
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

// whiteSpaceCollapse is a value of white-space-collapse (CSS Text 4, 3.1).
type whiteSpaceCollapse uint8

// The white-space-collapse values Chromium parses: discard and
// preserve-spaces are left out.
const (
	collapseSpaces whiteSpaceCollapse = iota
	preserveSpaces
	preserveBreaks
	breakSpaces
)

var whiteSpaceCollapses = map[string]whiteSpaceCollapse{
	"collapse": collapseSpaces, "preserve": preserveSpaces, "preserve-breaks": preserveBreaks, "break-spaces": breakSpaces,
}

// textWrapMode is a value of text-wrap-mode (CSS Text 4, 6.1), true for
// wrap.
type textWrapMode bool

var textWrapStyles = map[string]TextWrapStyle{
	"auto": WrapStyleAuto, "balance": WrapStyleBalance, "stable": WrapStyleStable, "pretty": WrapStylePretty,
}

// whiteSpaceOf returns the [WhiteSpace] of white-space-collapse c and
// text-wrap-mode wrap, as the white-space shorthand names their pairs
// (CSS Text 4, 3.4).
func whiteSpaceOf(c whiteSpaceCollapse, wrap bool) WhiteSpace {
	switch {
	case c == preserveSpaces && wrap:
		return PreWrap
	case c == preserveSpaces:
		return Pre
	case c == preserveBreaks && wrap:
		return PreLine
	case c == preserveBreaks:
		return PreLineNowrap
	case c == breakSpaces && wrap:
		return BreakSpaces
	case c == breakSpaces:
		return BreakSpacesNowrap
	case wrap:
		return WhiteSpaceNormal
	}
	return Nowrap
}

// collapseOf returns the white-space-collapse part of ws.
func collapseOf(ws WhiteSpace) whiteSpaceCollapse {
	switch ws {
	case Pre, PreWrap:
		return preserveSpaces
	case PreLine, PreLineNowrap:
		return preserveBreaks
	case BreakSpaces, BreakSpacesNowrap:
		return breakSpaces
	case WhiteSpaceNormal, Nowrap:
	}
	return collapseSpaces
}

// wrapsOf returns the text-wrap-mode part of ws, true for wrap.
func wrapsOf(ws WhiteSpace) bool {
	switch ws {
	case Pre, Nowrap, PreLineNowrap, BreakSpacesNowrap:
		return false
	case WhiteSpaceNormal, PreWrap, PreLine, BreakSpaces:
	}
	return true
}

// whiteSpaceCollapseProp is white-space-collapse, the half of
// [Style.WhiteSpace] that says which white space is kept.
func whiteSpaceCollapseProp() propDef {
	return propDef{name: "white-space-collapse", inherited: true, parse: func(v []token, _ *declCtx) (any, bool) {
		k, ok := keyword(v)
		if !ok {
			return nil, false
		}
		c, ok := whiteSpaceCollapses[k]
		return c, ok
	}, compute: func(_ *styler, st *Style, v any) {
		if c, ok := v.(whiteSpaceCollapse); ok {
			st.WhiteSpace = whiteSpaceOf(c, wrapsOf(st.WhiteSpace))
		}
	}, copyFrom: func(dst, src *Style) {
		dst.WhiteSpace = whiteSpaceOf(collapseOf(src.WhiteSpace), wrapsOf(dst.WhiteSpace))
	}}
}

// whiteSpaceKeywords are the white-space values that set both longhands
// (CSS Text 4, 3.4), the legacy vendor spellings of pre-wrap among them.
var whiteSpaceKeywords = map[string][2]any{
	"normal":        {collapseSpaces, textWrapMode(true)},
	"pre":           {preserveSpaces, textWrapMode(false)},
	"pre-wrap":      {preserveSpaces, textWrapMode(true)},
	"pre-line":      {preserveBreaks, textWrapMode(true)},
	"-moz-pre-wrap": {preserveSpaces, textWrapMode(true)},
	"-o-pre-wrap":   {preserveSpaces, textWrapMode(true)},
}

// expandWhiteSpace expands the white-space shorthand (CSS Text 4, 3.4):
// normal | pre | pre-wrap | pre-line | <white-space-collapse> ||
// <text-wrap-mode>. white-space-trim, which Chromium does not parse, is
// left out.
func expandWhiteSpace(v []token, c *declCtx) ([]entry, bool) {
	if k, ok := keyword(v); ok {
		if p, ok := whiteSpaceKeywords[k]; ok {
			return []entry{{pWhiteSpace, p[0]}, {pTextWrapMode, p[1]}}, true
		}
	}
	return expandAnyOrder(v, c, pWhiteSpace, pTextWrapMode)
}

// expandTextWrap expands the text-wrap shorthand (CSS Text 4, 6.3):
// <text-wrap-mode> || <text-wrap-style>.
func expandTextWrap(v []token, c *declCtx) ([]entry, bool) {
	return expandAnyOrder(v, c, pTextWrapMode, pTextWrapStyle)
}

// expandAnyOrder expands a shorthand of single keyword longhands given in
// any order, each at most once.
func expandAnyOrder(v []token, c *declCtx, longhands ...propID) ([]entry, bool) {
	if len(v) == 0 || len(v) > len(longhands) {
		return nil, false
	}
	out := make([]entry, 0, len(v))
	seen := make([]bool, len(longhands))
	for _, t := range v {
		found := false
		for i, id := range longhands {
			if seen[i] {
				continue
			}
			if pv, ok := props[id].parse([]token{t}, c); ok {
				out = append(out, entry{id, pv})
				seen[i], found = true, true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return out, true
}
