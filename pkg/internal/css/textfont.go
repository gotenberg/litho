package css

// TextJustify is the text-justify property (CSS Text 3, 7.4).
type TextJustify uint8

// The text-justify values.
const (
	// JustifyAuto justifies as Chromium does: at word separators, and
	// between the characters of the Chinese, Japanese and Korean scripts
	// that set no space between words.
	JustifyAuto TextJustify = iota
	// JustifyNone does not justify.
	JustifyNone
	// JustifyInterWord justifies at word separators alone.
	JustifyInterWord
	// JustifyInterCharacter justifies between every two typographic
	// character units (distribute computes to it).
	JustifyInterCharacter
)

// TextAutospace is the text-autospace property as Chromium ships it (CSS
// Text 4, 8.2): the bits of the boundaries that take the extra space.
// Zero, no-autospace, is Chromium's initial value.
type TextAutospace uint8

// The text-autospace bits.
const (
	// AutospaceIdeographAlpha spaces ideographs from letters of the
	// non-ideographic scripts.
	AutospaceIdeographAlpha TextAutospace = 1 << iota
	// AutospaceIdeographNumeric spaces ideographs from digits.
	AutospaceIdeographNumeric
)

// textFontProps returns the properties of CSS Text 3 and 4 and CSS Fonts
// 4 this file adds: text-justify, text-autospace, text-spacing-trim,
// font-stretch, font-kerning, writing-mode and the font-synthesis
// longhands.
func textFontProps() map[propID]propDef {
	return map[propID]propDef{
		// font-kerning (CSS Fonts 4, 6.3) is inherited; auto, its initial
		// value, kerns as normal does.
		pFontKerning: enumProp("font-kerning", true, func(s *Style) *FontKerning { return &s.FontKerning }, map[string]FontKerning{
			"auto": KerningAuto, "normal": KerningNormal, "none": KerningNone,
		}),
		pWritingMode: enumProp("writing-mode", true, func(s *Style) *WritingMode { return &s.WritingMode }, map[string]WritingMode{
			"horizontal-tb": HorizontalTB, "vertical-rl": VerticalRL, "vertical-lr": VerticalLR,
			"sideways-rl": SidewaysRL, "sideways-lr": SidewaysLR,
			// The SVG 1.1 and Internet Explorer values (CSS Writing Modes 4,
			// 3.1.1).
			"lr": HorizontalTB, "lr-tb": HorizontalTB, "rl": HorizontalTB, "rl-tb": HorizontalTB,
			"tb": VerticalRL, "tb-rl": VerticalRL,
		}),
		pTextJustify: enumProp("text-justify", true, func(s *Style) *TextJustify { return &s.TextJustify }, map[string]TextJustify{
			"auto": JustifyAuto, "none": JustifyNone, "inter-word": JustifyInterWord,
			"inter-character": JustifyInterCharacter, "distribute": JustifyInterCharacter,
		}),
		pTextAutospace: {
			name: "text-autospace", inherited: true,
			parse: parseAutospace,
			compute: func(_ *styler, st *Style, v any) {
				if a, ok := v.(TextAutospace); ok {
					st.TextAutospace = a
				}
			},
			copyFrom: field(func(s *Style) *TextAutospace { return &s.TextAutospace }),
		},
		pFontStretch: {
			name: "font-stretch", inherited: true,
			parse: func(v []token, _ *declCtx) (any, bool) {
				t, ok := single(v)
				if !ok {
					return nil, false
				}
				return parseStretch(t)
			},
			compute: func(_ *styler, st *Style, v any) {
				if p, ok := v.(float64); ok {
					st.FontStretch = p
				}
			},
			copyFrom: field(func(s *Style) *float64 { return &s.FontStretch }),
		},
		// CSS Text 4, 8.3: space-all keeps every fullwidth punctuation
		// full width; the other values trim adjacent ones, as normal does.
		pTextSpacingTrim: {
			name: "text-spacing-trim", inherited: true,
			parse: func(v []token, _ *declCtx) (any, bool) {
				k, ok := keyword(v)
				if !ok {
					return nil, false
				}
				switch k {
				case "space-all":
					return true, true
				case "normal", "space-first", "trim-start", "trim-both", "trim-all", "auto":
					return false, true
				}
				return nil, false
			},
			compute: func(_ *styler, st *Style, v any) {
				if b, ok := v.(bool); ok {
					st.NoSpacingTrim = b
				}
			},
			copyFrom: field(func(s *Style) *bool { return &s.NoSpacingTrim }),
		},
		// text-autospace-size is Chromium's experimental amount of
		// text-autospace, a number of ems, 1/8 by default; litho lays it
		// out as the web-platform-tests check it.
		pTextAutospaceSize: {
			name: "text-autospace-size", inherited: true,
			parse: func(v []token, _ *declCtx) (any, bool) {
				t, ok := single(v)
				if !ok {
					return nil, false
				}
				n, ok := parseNumber(t)
				return n, ok && n >= -1 && n <= 1
			},
			compute: func(_ *styler, st *Style, v any) {
				if n, ok := v.(float64); ok {
					st.TextAutospaceSize = n
				}
			},
			copyFrom: field(func(s *Style) *float64 { return &s.TextAutospaceSize }),
		},
		pTextShadow:          {name: "text-shadow", inherited: true, parse: parseTextShadow, compute: computeTextShadows, copyFrom: copyTextShadows},
		pFontSynthesisWeight: synthesisProp("font-synthesis-weight", func(s *Style) *bool { return &s.NoSynthesizeWeight }),
		pFontSynthesisStyle:  synthesisProp("font-synthesis-style", func(s *Style) *bool { return &s.NoSynthesizeStyle }),
	}
}

// parseTextShadow parses text-shadow (CSS Text Decoration 3, 4): none, or
// a comma-separated list of shadows of two or three lengths and a colour,
// with no spread and no inset.
func parseTextShadow(v []token, c *declCtx) (any, bool) {
	pv, ok := parseBoxShadow(v, c)
	if !ok {
		return nil, false
	}
	specs, _ := pv.([]shadowSpec)
	for _, sp := range specs {
		if sp.inset || sp.n > 3 {
			return nil, false
		}
	}
	return specs, true
}

// computeTextShadows computes text-shadow as box-shadow is computed.
func computeTextShadows(s *styler, st *Style, v any) {
	saved := st.BoxShadow
	computeShadows(s, st, v)
	st.TextShadow, st.BoxShadow = st.BoxShadow, saved
}

// copyTextShadows inherits text-shadow, resolving a currentColor shadow
// against dst's color.
func copyTextShadows(dst, src *Style) {
	saved := dst.BoxShadow
	tmp := *src
	tmp.BoxShadow = src.TextShadow
	copyShadows(dst, &tmp)
	dst.TextShadow, dst.BoxShadow = dst.BoxShadow, saved
}

// maxFontStretch caps a font-stretch percentage: 1000%. Not
// spec-mandated, CSS Fonts 4, 2.3 takes any non-negative percentage: a
// larger value is rejected. Real faces span 50% to 200%.
const maxFontStretch = 1000

// parseStretch parses a font-stretch value (CSS Fonts 4, 2.3): a keyword
// or a non-negative percentage, as a percentage, normal being 100.
func parseStretch(t token) (float64, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tIdent:
		p, ok := stretchKeywords[lower(t.value)]
		return p, ok
	case tPercentage:
		return t.num, finite(t.num) && t.num >= 0 && t.num <= maxFontStretch
	}
	return 0, false
}

// synthesisProp defines a font-synthesis longhand: auto allows the
// synthesis, none forbids it, storing true at f (CSS Fonts 4, 5.5).
func synthesisProp(name string, f func(*Style) *bool) propDef {
	return propDef{
		name: name, inherited: true,
		parse: func(v []token, _ *declCtx) (any, bool) {
			switch k, _ := keyword(v); k {
			case "auto":
				return false, true
			case "none":
				return true, true
			}
			return nil, false
		},
		compute: func(_ *styler, st *Style, v any) {
			if b, ok := v.(bool); ok {
				*f(st) = b
			}
		},
		copyFrom: field(f),
	}
}

// parseAutospace parses text-autospace (CSS Text 4, 8.2): normal and auto
// space ideographs from letters and digits, no-autospace from nothing;
// ideograph-alpha and ideograph-numeric pick the boundaries, punctuation,
// insert and replace are accepted and draw nothing more.
func parseAutospace(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok {
		switch k {
		case "normal", "auto":
			return AutospaceIdeographAlpha | AutospaceIdeographNumeric, true
		case "no-autospace":
			return TextAutospace(0), true
		}
	}
	if len(v) == 0 || len(v) > 4 {
		return nil, false
	}
	var a TextAutospace
	seen := map[string]bool{}
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		k := lower(t.value)
		if seen[k] || (k == "insert" || k == "replace") && (seen["insert"] || seen["replace"]) {
			return nil, false
		}
		seen[k] = true
		switch k {
		case "ideograph-alpha":
			a |= AutospaceIdeographAlpha
		case "ideograph-numeric":
			a |= AutospaceIdeographNumeric
		case "punctuation", "insert", "replace":
		default:
			return nil, false
		}
	}
	return a, true
}

// textFontShorthands returns font-synthesis (CSS Fonts 4, 5.5): none, or
// any of weight, style and small-caps, each allowing its synthesis.
func textFontShorthands() map[string]shorthandDef {
	return map[string]shorthandDef{
		"font-synthesis": {
			longhands: []propID{pFontSynthesisWeight, pFontSynthesisStyle},
			expand: func(v []token, _ *declCtx) ([]entry, bool) {
				if k, ok := keyword(v); ok && k == "none" {
					return []entry{{pFontSynthesisWeight, true}, {pFontSynthesisStyle, true}}, true
				}
				weight, style := true, true
				seen := map[string]bool{}
				for _, t := range v {
					k := lower(t.value)
					if t.typ != tIdent || seen[k] {
						return nil, false
					}
					seen[k] = true
					switch k {
					case "weight":
						weight = false
					case "style":
						style = false
					case "small-caps", "position":
					default:
						return nil, false
					}
				}
				return []entry{{pFontSynthesisWeight, weight}, {pFontSynthesisStyle, style}}, true
			},
		},
	}
}
