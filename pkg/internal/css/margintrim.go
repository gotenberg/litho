package css

// MarginTrim is margin-trim (CSS Box 4, 3.3): the edges of a block
// container or a multi-column container at which the margins of its
// in-flow children, and the margins collapsed with them, are truncated to
// zero. The zero value is none.
type MarginTrim uint8

// The margin-trim edges.
const (
	TrimBlockStart MarginTrim = 1 << iota
	TrimBlockEnd
)

// parseMarginTrim parses margin-trim: none, block, or block-start and
// block-end in either order, each at most once. The inline keywords of
// earlier drafts are refused, as the grammar of CSS Box 4, 3.3 refuses
// them and Chromium does not parse them.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156's CSS.supports takes none, block,
//     block-start, block-end and both of the last in either order (computed
//     block); it refuses inline, inline-start, inline-end, block inline,
//     block block-start and block-start block-start.
func parseMarginTrim(v []token, _ *declCtx) (any, bool) {
	if len(v) == 1 && v[0].isIdent("none") {
		return MarginTrim(0), true
	}
	if len(v) == 1 && v[0].isIdent("block") {
		return TrimBlockStart | TrimBlockEnd, true
	}
	if len(v) == 0 || len(v) > 2 {
		return nil, false
	}
	var m MarginTrim
	for _, t := range v {
		var k MarginTrim
		switch {
		case t.isIdent("block-start"):
			k = TrimBlockStart
		case t.isIdent("block-end"):
			k = TrimBlockEnd
		default:
			return nil, false
		}
		if m&k != 0 {
			return nil, false
		}
		m |= k
	}
	return m, true
}

// marginTrimProp returns margin-trim, not inherited.
func marginTrimProp() propDef {
	return propDef{name: "margin-trim", parse: parseMarginTrim, compute: func(_ *styler, st *Style, v any) {
		if m, ok := v.(MarginTrim); ok {
			st.MarginTrim = m
		}
	}, copyFrom: field(func(s *Style) *MarginTrim { return &s.MarginTrim })}
}
