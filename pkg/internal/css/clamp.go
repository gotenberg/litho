package css

import "strings"

// OverflowMark is what a line shows where its content is cut: nothing,
// an ellipsis, or a string (CSS Overflow 4, 5.1 and 6.4).
type OverflowMark struct {
	Kind MarkKind
	// Text is the string of a [MarkString].
	Text string
}

// MarkKind is the kind of an [OverflowMark].
type MarkKind uint8

// The mark kinds.
const (
	// MarkNone shows nothing: text-overflow: clip, block-ellipsis: none.
	MarkNone MarkKind = iota
	// MarkEllipsis shows U+2026 HORIZONTAL ELLIPSIS: text-overflow:
	// ellipsis, block-ellipsis: auto.
	MarkEllipsis
	// MarkString shows Text.
	MarkString
)

// String returns the characters the mark draws, "" for none.
func (m OverflowMark) String() string {
	switch m.Kind {
	case MarkEllipsis:
		return "…"
	case MarkString:
		return m.Text
	case MarkNone:
	}
	return ""
}

// Continue is the continue property (CSS Overflow 4, 6.3).
type Continue uint8

// The continue values.
const (
	// ContinueAuto lets the content continue in the next fragment.
	ContinueAuto Continue = iota
	// ContinueDiscard discards the content after the box's first
	// fragment, or after its max-lines line.
	ContinueDiscard
)

// LineClamp holds the line clamping longhands of CSS Overflow 4, 6 and
// the legacy -webkit-line-clamp of CSS Overflow 3 that Chromium honours on
// a display: -webkit-box container of vertical orientation.
type LineClamp struct {
	// MaxLines is the max-lines property: the lines kept before the
	// content is discarded, 0 for none (CSS Overflow 4, 6.2).
	MaxLines int
	// Auto is line-clamp: auto: the lines that end past the box's
	// maximum height are discarded (CSS Overflow 4, 6.1).
	Auto bool
	// Continue is the continue property (CSS Overflow 4, 6.3).
	Continue Continue
	// Ellipsis is the block-ellipsis property, inherited: the mark the
	// last line kept ends with (CSS Overflow 4, 6.4).
	Ellipsis OverflowMark
	// webkitBox records a display: -webkit-box or -webkit-inline-box
	// container, webkitVertical a -webkit-box-orient of vertical or
	// block-axis, webkitLines the -webkit-line-clamp, 0 for none.
	webkitBox, webkitVertical bool
	webkitLines               int
}

// Lines returns the number of lines a box of style s keeps before it
// discards its content, 0 for no limit: max-lines, or the legacy
// -webkit-line-clamp of a vertical -webkit-box, which Chromium honours.
func (s *Style) Lines() int {
	c := &s.LineClamp
	if c.MaxLines > 0 {
		return c.MaxLines
	}
	if c.webkitBox && c.webkitVertical && c.webkitLines > 0 {
		return c.webkitLines
	}
	return 0
}

// ClampMark returns the mark the last line a box of style s keeps ends
// with: block-ellipsis, which the legacy -webkit-line-clamp sets to auto.
func (s *Style) ClampMark() OverflowMark {
	c := &s.LineClamp
	if c.MaxLines == 0 && !c.Auto && c.Continue == ContinueAuto && s.Lines() > 0 {
		return OverflowMark{Kind: MarkEllipsis}
	}
	return c.Ellipsis
}

// Clamps reports whether a box of style s discards content past a line
// count, its maximum height or its first fragment (CSS Overflow 4, 6).
func (s *Style) Clamps() bool {
	return s.Lines() > 0 || s.LineClamp.Auto || s.LineClamp.Continue == ContinueDiscard
}

// maxClampLines caps max-lines and -webkit-line-clamp: 1,000,000. A
// larger count clamps nothing a page holds. Anti-DoS guard, not
// spec-mandated.
const maxClampLines = 1_000_000

// parseLineCount parses none or a positive integer, none as 0.
func parseLineCount(v []token) (int, bool) {
	t, ok := single(v)
	if !ok {
		return 0, false
	}
	if t.isIdent("none") {
		return 0, true
	}
	n, ok := parseInteger(t)
	if !ok || n < 1 {
		return 0, false
	}
	return min(n, maxClampLines), true
}

// parseMark parses one <string>, or one of the keywords, mapped to their
// mark.
func parseMark(t token, kws map[string]MarkKind) (OverflowMark, bool) {
	if t.typ == tString {
		// A segment break in the string draws as a space, as in the text
		// it stands for (CSS Text 3, 4.1.2), and as Chromium draws it.
		text := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\f", " ").Replace(t.value)
		return OverflowMark{Kind: MarkString, Text: text}, true
	}
	if t.typ != tIdent {
		return OverflowMark{}, false
	}
	k, ok := kws[lower(t.value)]
	return OverflowMark{Kind: k}, ok
}

// The keywords of text-overflow and block-ellipsis.
var (
	textOverflowKws  = map[string]MarkKind{"clip": MarkNone, "ellipsis": MarkEllipsis}
	blockEllipsisKws = map[string]MarkKind{"none": MarkNone, "auto": MarkEllipsis}
)

// lineClampAuto is the specified max-lines of line-clamp: auto.
type lineClampAuto struct{}

// textOverflowProps returns the properties of CSS Overflow 4 sections 5
// and 6 litho lays out.
func textOverflowProps() map[propID]propDef {
	return map[propID]propDef{
		// CSS Overflow 4, 5.1: clip | ellipsis | <string>, once for the
		// end edge or twice, line-left then line-right; the second of two
		// is the end edge of a left-to-right line, the one litho marks.
		pTextOverflow: {
			name: "text-overflow",
			parse: func(v []token, _ *declCtx) (any, bool) {
				if len(v) < 1 || len(v) > 2 {
					return nil, false
				}
				var m OverflowMark
				for _, t := range v {
					var ok bool
					if m, ok = parseMark(t, textOverflowKws); !ok {
						return nil, false
					}
				}
				return m, true
			},
			compute: func(_ *styler, st *Style, v any) {
				if m, ok := v.(OverflowMark); ok {
					st.TextOverflow = m
				}
			},
			copyFrom: field(func(s *Style) *OverflowMark { return &s.TextOverflow }),
		},
		pMaxLines: {
			name: "max-lines",
			parse: func(v []token, _ *declCtx) (any, bool) {
				n, ok := parseLineCount(v)
				return n, ok
			},
			compute: func(_ *styler, st *Style, v any) {
				switch n := v.(type) {
				case int:
					st.LineClamp.MaxLines, st.LineClamp.Auto = n, false
				case lineClampAuto:
					st.LineClamp.MaxLines, st.LineClamp.Auto = 0, true
				}
			},
			copyFrom: func(dst, src *Style) {
				dst.LineClamp.MaxLines, dst.LineClamp.Auto = src.LineClamp.MaxLines, src.LineClamp.Auto
			},
		},
		pBlockEllipsis: {
			name: "block-ellipsis", inherited: true,
			parse: func(v []token, _ *declCtx) (any, bool) {
				t, ok := single(v)
				if !ok {
					return nil, false
				}
				return parseMark(t, blockEllipsisKws)
			},
			compute: func(_ *styler, st *Style, v any) {
				if m, ok := v.(OverflowMark); ok {
					st.LineClamp.Ellipsis = m
				}
			},
			copyFrom: field(func(s *Style) *OverflowMark { return &s.LineClamp.Ellipsis }),
		},
		pContinue: enumProp("continue", false, func(s *Style) *Continue { return &s.LineClamp.Continue }, map[string]Continue{
			// -webkit-legacy is the continue of the legacy clamp, which
			// discards as discard does.
			"auto": ContinueAuto, "discard": ContinueDiscard, "-webkit-legacy": ContinueDiscard,
		}),
		pWebkitLineClamp: {
			name: "-webkit-line-clamp",
			parse: func(v []token, _ *declCtx) (any, bool) {
				n, ok := parseLineCount(v)
				return n, ok
			},
			compute: func(_ *styler, st *Style, v any) {
				if n, ok := v.(int); ok {
					st.LineClamp.webkitLines = n
				}
			},
			copyFrom: field(func(s *Style) *int { return &s.LineClamp.webkitLines }),
		},
		pWebkitBoxOrient: {
			name: "-webkit-box-orient",
			parse: func(v []token, _ *declCtx) (any, bool) {
				k, ok := keyword(v)
				if !ok {
					return nil, false
				}
				switch k {
				case "vertical", "block-axis":
					return true, true
				case "horizontal", "inline-axis":
					return false, true
				}
				return nil, false
			},
			compute: func(_ *styler, st *Style, v any) {
				if b, ok := v.(bool); ok {
					st.LineClamp.webkitVertical = b
				}
			},
			copyFrom: field(func(s *Style) *bool { return &s.LineClamp.webkitVertical }),
		},
	}
}

// textOverflowShorthands returns the line-clamp shorthand (CSS Overflow
// 4, 6.1): none | auto | <integer> || <'block-ellipsis'>, onto max-lines,
// block-ellipsis and continue. A count discards what follows its last
// line, ending it with block-ellipsis, auto by default; auto clamps at the
// box's maximum height; none clamps nothing.
func textOverflowShorthands() map[string]shorthandDef {
	return map[string]shorthandDef{
		"line-clamp": {
			longhands: []propID{pMaxLines, pBlockEllipsis, pContinue},
			expand: func(v []token, _ *declCtx) ([]entry, bool) {
				if k, ok := keyword(v); ok {
					switch k {
					case "none":
						return []entry{{pMaxLines, 0}, {pBlockEllipsis, OverflowMark{}}, {pContinue, ContinueAuto}}, true
					case "auto":
						return []entry{{pMaxLines, lineClampAuto{}}, {pBlockEllipsis, OverflowMark{Kind: MarkEllipsis}}, {pContinue, ContinueDiscard}}, true
					}
				}
				if len(v) < 1 || len(v) > 2 {
					return nil, false
				}
				n, mark := 0, OverflowMark{Kind: MarkEllipsis}
				seenN, seenMark := false, false
				for _, t := range v {
					if c, ok := parseLineCount([]token{t}); ok && c > 0 && !seenN {
						n, seenN = c, true
						continue
					}
					if m, ok := parseMark(t, blockEllipsisKws); ok && !seenMark {
						mark, seenMark = m, true
						continue
					}
					return nil, false
				}
				if !seenN {
					return nil, false
				}
				return []entry{{pMaxLines, n}, {pBlockEllipsis, mark}, {pContinue, ContinueDiscard}}, true
			},
		},
	}
}
