package css

import (
	"unicode"
	"unicode/utf8"
)

// EmphasisPosition is the text-emphasis-position property (CSS Text
// Decoration 3, 3.4): the side of the text its emphasis marks sit on in
// horizontal text. Its left and right keywords place marks in vertical
// text alone, which litho lays out horizontally, and compute to nothing.
type EmphasisPosition uint8

// The text-emphasis-position values.
const (
	// EmphasisOver puts the marks over the text, the initial value.
	EmphasisOver EmphasisPosition = iota
	// EmphasisUnder puts the marks under the text.
	EmphasisUnder
)

// emphasisShape is a parsed text-emphasis-style: none, a fill and a
// shape, either of which may be missing, or a string.
type emphasisShape struct {
	none, open  bool
	shape, str  string
	given, isSt bool
}

// emphasisMarks are the marks of the shapes of CSS Text Decoration 3,
// 3.1, filled then open.
var emphasisMarks = map[string][2]string{
	"dot":           {"•", "◦"},
	"circle":        {"●", "○"},
	"double-circle": {"◉", "◎"},
	"triangle":      {"▲", "△"},
	"sesame":        {"﹅", "﹆"},
}

// parseEmphasisStyle parses text-emphasis-style (CSS Text Decoration 3,
// 3.1): none, [filled | open] || <shape>, or a string.
func parseEmphasisStyle(v []token, _ *declCtx) (any, bool) {
	if len(v) == 1 && v[0].typ == tString {
		return emphasisShape{str: v[0].value, isSt: true}, true
	}
	if k, ok := keyword(v); ok && k == "none" {
		return emphasisShape{none: true}, true
	}
	if len(v) == 0 || len(v) > 2 {
		return nil, false
	}
	var out emphasisShape
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		switch k := lower(t.value); {
		case (k == "filled" || k == "open") && !out.given:
			out.given, out.open = true, k == "open"
		case emphasisMarks[k] != [2]string{} && out.shape == "":
			out.shape = k
		default:
			return nil, false
		}
	}
	return out, true
}

// mark returns the mark the style draws in the writing mode wm, "" for
// none: a fill alone takes dot in horizontal text, as Chromium computes
// it where CSS Text Decoration 3, 3.1 gives circle, and sesame in
// vertical text; a shape alone is filled, and a string draws its first
// grapheme cluster (3.1).
func (e emphasisShape) mark(wm WritingMode) string {
	switch {
	case e.none:
		return ""
	case e.isSt:
		return firstGrapheme(e.str)
	}
	shape := e.shape
	if shape == "" {
		shape = "dot"
		if wm.Vertical() {
			shape = "sesame"
		}
	}
	m := emphasisMarks[shape]
	if e.open {
		return m[1]
	}
	return m[0]
}

// firstGrapheme returns the first character of s with the marks,
// joiners and variation selectors that extend it: the first grapheme
// cluster of UAX #29 for the strings an emphasis mark takes.
func firstGrapheme(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return ""
	}
	joined := false
	for n < len(s) {
		c, w := utf8.DecodeRuneInString(s[n:])
		switch {
		case unicode.In(c, unicode.Mn, unicode.Me, unicode.Mc), unicode.Is(unicode.Variation_Selector, c):
		case c == '\u200d':
			joined = true
		case joined:
			joined = false
		default:
			return s[:n]
		}
		n += w
	}
	return s
}

// emphasisProps returns the properties of CSS Text Decoration 3, 3:
// text-emphasis-style, text-emphasis-color and text-emphasis-position, all
// inherited.
func emphasisProps() map[propID]propDef {
	colour := colorProp("text-emphasis-color", func(s *Style) *Color { return &s.TextEmphasisColor }, slotTextEmphasis, colorValue{current: true})
	colour.inherited = true
	return map[propID]propDef{
		pTextEmphasisStyle: {name: "text-emphasis-style", inherited: true, parse: parseEmphasisStyle, compute: func(_ *styler, st *Style, v any) {
			if e, ok := v.(emphasisShape); ok {
				st.TextEmphasisMark = e.mark(st.WritingMode)
			}
		}, copyFrom: field(func(s *Style) *string { return &s.TextEmphasisMark })},
		pTextEmphasisColor: colour,
		pTextEmphasisPosition: {name: "text-emphasis-position", inherited: true, parse: parseEmphasisPosition, compute: func(_ *styler, st *Style, v any) {
			if p, ok := v.(EmphasisPosition); ok {
				st.TextEmphasisPosition = p
			}
		}, copyFrom: field(func(s *Style) *EmphasisPosition { return &s.TextEmphasisPosition })},
	}
}

// parseEmphasisPosition parses text-emphasis-position (CSS Text
// Decoration 3, 3.4): over or under, with right or left in either order,
// as Chromium parses it, the side alone too.
func parseEmphasisPosition(v []token, _ *declCtx) (any, bool) {
	if len(v) == 0 || len(v) > 2 {
		return nil, false
	}
	pos, side := -1, false
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		switch k := lower(t.value); {
		case (k == "over" || k == "under") && pos < 0:
			pos = int(EmphasisOver)
			if k == "under" {
				pos = int(EmphasisUnder)
			}
		case (k == "right" || k == "left") && !side:
			side = true
		default:
			return nil, false
		}
	}
	if pos < 0 {
		return EmphasisOver, true
	}
	return EmphasisPosition(pos), true //nolint:gosec // fits uint8: 0 or 1
}

// expandTextEmphasis expands text-emphasis (CSS Text Decoration 3, 3.3):
// a style and a colour in either order, either left out.
func expandTextEmphasis(v []token, c *declCtx) ([]entry, bool) {
	if len(v) == 0 {
		return nil, false
	}
	style, colour := any(nil), any(nil)
	rest := v
	if cv, ok := props[pTextEmphasisColor].parse(v[len(v)-1:], c); ok {
		colour, rest = cv, v[:len(v)-1]
	} else if cv, ok := props[pTextEmphasisColor].parse(v[:1], c); ok {
		colour, rest = cv, v[1:]
	}
	if len(rest) > 0 {
		sv, ok := parseEmphasisStyle(rest, c)
		if !ok {
			return nil, false
		}
		style = sv
	}
	out := []entry{{pTextEmphasisStyle, wideInitial}, {pTextEmphasisColor, wideInitial}}
	if style != nil {
		out[0].v = style
	}
	if colour != nil {
		out[1].v = colour
	}
	return out, true
}
