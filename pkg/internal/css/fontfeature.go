package css

import (
	"slices"
	"strings"
)

// FeatureSetting is one entry of font-feature-settings (CSS Fonts 4, 6.12):
// an OpenType feature tag and its value, 1 for on and 0 for off.
type FeatureSetting struct {
	// Tag is the four-character feature tag, each character from U+20 to
	// U+7E.
	Tag string
	// Value is the feature's value, 0 to maxFeatureValue.
	Value int
}

// maxFeatureValue caps the value of a feature setting. Overflow guard, not
// spec-mandated: CSS Fonts 4, 6.12 allows any non-negative integer, the
// shaper reads it as 32 bits, and alternates are numbered far below it.
const maxFeatureValue = 1 << 30

// maxFeatureSettings caps the entries of one font-feature-settings value
// and of the lists of font-variant-alternates. Anti-DoS guard, not
// spec-mandated: an OpenType font registers a few dozen features, and a
// value of more than 256 entries names the same ones again.
const maxFeatureSettings = 256

// Ligatures is font-variant-ligatures (CSS Fonts 4, 6.4) as the bits that
// change the shaper's defaults: zero is normal.
type Ligatures uint8

// The font-variant-ligatures bits.
const (
	// LigaturesCommonOff turns off the common ligatures (liga, clig).
	LigaturesCommonOff Ligatures = 1 << iota
	// LigaturesDiscretionary turns on dlig.
	LigaturesDiscretionary
	// LigaturesHistorical turns on hlig.
	LigaturesHistorical
	// LigaturesContextualOff turns off calt.
	LigaturesContextualOff
)

// NumericVariant is font-variant-numeric (CSS Fonts 4, 6.7) as bits, zero
// for normal.
type NumericVariant uint8

// The font-variant-numeric bits.
const (
	NumericLining NumericVariant = 1 << iota
	NumericOldstyle
	NumericProportional
	NumericTabular
	NumericDiagonalFractions
	NumericStackedFractions
	NumericOrdinal
	NumericSlashedZero
)

// EastAsianVariant is font-variant-east-asian (CSS Fonts 4, 6.10): a form
// in the low bits, then the width and ruby bits; zero is normal.
type EastAsianVariant uint8

// The font-variant-east-asian forms, widths and ruby.
const (
	EastAsianJIS78 EastAsianVariant = iota + 1
	EastAsianJIS83
	EastAsianJIS90
	EastAsianJIS04
	EastAsianSimplified
	EastAsianTraditional
	// EastAsianFormMask selects the form of a value.
	EastAsianFormMask EastAsianVariant = 7

	EastAsianFullWidth         EastAsianVariant = 8
	EastAsianProportionalWidth EastAsianVariant = 16
	EastAsianRuby              EastAsianVariant = 32
)

// VariantPosition is font-variant-position (CSS Fonts 4, 6.5).
type VariantPosition uint8

// The font-variant-position values.
const (
	PositionNormal VariantPosition = iota
	PositionSub
	PositionSuper
)

// VariantEmoji is font-variant-emoji (CSS Fonts 4, 6.13).
type VariantEmoji uint8

// The font-variant-emoji values.
const (
	EmojiNormal VariantEmoji = iota
	EmojiText
	EmojiEmoji
	EmojiUnicode
)

// Alternates is font-variant-alternates (CSS Fonts 4, 6.8): the feature
// value names its functions give, which @font-feature-values resolves
// against the family of the font in use, and historical-forms. The zero
// value is normal.
type Alternates struct {
	Stylistic, Swash, Ornaments, Annotation string
	Styleset, CharacterVariant              []string
	HistoricalForms                         bool
}

// IsNormal reports whether a is normal.
func (a *Alternates) IsNormal() bool {
	return !a.HistoricalForms && a.Stylistic == "" && a.Swash == "" && a.Ornaments == "" &&
		a.Annotation == "" && len(a.Styleset) == 0 && len(a.CharacterVariant) == 0
}

// parseFeatureSettings parses font-feature-settings (CSS Fonts 4, 6.12):
// normal, or a comma list of a four-character string tag optionally
// followed by a non-negative integer, on or off.
func parseFeatureSettings(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "normal" {
		return []FeatureSetting(nil), true
	}
	parts := splitCommas(v)
	if len(parts) > maxFeatureSettings {
		return nil, false
	}
	out := make([]FeatureSetting, 0, len(parts))
	for _, part := range parts {
		f, ok := parseFeatureSetting(significant(part))
		if !ok {
			return nil, false
		}
		out = append(out, f)
	}
	return out, len(out) > 0
}

// parseFeatureSetting parses one <feature-tag-value>.
func parseFeatureSetting(p []token) (FeatureSetting, bool) {
	if len(p) == 0 || len(p) > 2 || p[0].typ != tString || !validFeatureTag(p[0].value) {
		return FeatureSetting{}, false
	}
	f := FeatureSetting{Tag: p[0].value, Value: 1}
	if len(p) == 2 {
		switch {
		case p[1].isIdent("on"):
		case p[1].isIdent("off"):
			f.Value = 0
		default:
			n, ok := parseInteger(p[1])
			if !ok || n < 0 {
				return FeatureSetting{}, false
			}
			f.Value = min(n, maxFeatureValue)
		}
	}
	return f, true
}

// validFeatureTag reports a string of four characters from U+20 to U+7E,
// the <opentype-tag> of CSS Fonts 4, 6.12.
func validFeatureTag(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := range len(s) {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// computeFeatureSettings sets font-feature-settings.
func computeFeatureSettings(_ *styler, st *Style, v any) {
	if l, ok := v.([]FeatureSetting); ok {
		st.FontFeatureSettings = l
	}
}

// ligatureKeywords are the font-variant-ligatures keywords (CSS Fonts 4,
// 6.4): the group of each, the || combinator taking one of a group, and
// the bits it sets.
var ligatureKeywords = map[string]struct {
	group int
	bits  Ligatures
}{
	"common-ligatures": {0, 0}, "no-common-ligatures": {0, LigaturesCommonOff},
	"discretionary-ligatures": {1, LigaturesDiscretionary}, "no-discretionary-ligatures": {1, 0},
	"historical-ligatures": {2, LigaturesHistorical}, "no-historical-ligatures": {2, 0},
	"contextual": {3, 0}, "no-contextual": {3, LigaturesContextualOff},
}

// parseLigatures parses font-variant-ligatures: normal, none, or one
// keyword of each group at most.
func parseLigatures(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok {
		switch k {
		case "normal":
			return Ligatures(0), true
		case "none":
			return LigaturesCommonOff | LigaturesContextualOff, true
		}
	}
	var out Ligatures
	var seen [4]bool
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		kw, ok := ligatureKeywords[lower(t.value)]
		if !ok || seen[kw.group] {
			return nil, false
		}
		seen[kw.group] = true
		out |= kw.bits
	}
	return out, len(v) > 0
}

// numericKeywords are the font-variant-numeric keywords (CSS Fonts 4,
// 6.7) with their group and bit.
var numericKeywords = map[string]struct {
	group int
	bit   NumericVariant
}{
	"lining-nums": {0, NumericLining}, "oldstyle-nums": {0, NumericOldstyle},
	"proportional-nums": {1, NumericProportional}, "tabular-nums": {1, NumericTabular},
	"diagonal-fractions": {2, NumericDiagonalFractions}, "stacked-fractions": {2, NumericStackedFractions},
	"ordinal": {3, NumericOrdinal}, "slashed-zero": {4, NumericSlashedZero},
}

// parseNumeric parses font-variant-numeric: normal or one keyword of each
// group at most.
func parseNumeric(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "normal" {
		return NumericVariant(0), true
	}
	var out NumericVariant
	var seen [5]bool
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		kw, ok := numericKeywords[lower(t.value)]
		if !ok || seen[kw.group] {
			return nil, false
		}
		seen[kw.group] = true
		out |= kw.bit
	}
	return out, len(v) > 0
}

// eastAsianKeywords are the font-variant-east-asian keywords (CSS Fonts 4,
// 6.10) with their group and value.
var eastAsianKeywords = map[string]struct {
	group int
	val   EastAsianVariant
}{
	"jis78": {0, EastAsianJIS78}, "jis83": {0, EastAsianJIS83}, "jis90": {0, EastAsianJIS90},
	"jis04": {0, EastAsianJIS04}, "simplified": {0, EastAsianSimplified}, "traditional": {0, EastAsianTraditional},
	"full-width": {1, EastAsianFullWidth}, "proportional-width": {1, EastAsianProportionalWidth},
	"ruby": {2, EastAsianRuby},
}

// parseEastAsian parses font-variant-east-asian: normal or one keyword of
// each group at most.
func parseEastAsian(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "normal" {
		return EastAsianVariant(0), true
	}
	var out EastAsianVariant
	var seen [3]bool
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		kw, ok := eastAsianKeywords[lower(t.value)]
		if !ok || seen[kw.group] {
			return nil, false
		}
		seen[kw.group] = true
		out |= kw.val
	}
	return out, len(v) > 0
}

// alternateFunctions are the functional values of font-variant-alternates
// (CSS Fonts 4, 6.8), and whether each takes a list.
var alternateFunctions = map[string]bool{
	"stylistic": false, "styleset": true, "character-variant": true,
	"swash": false, "ornaments": false, "annotation": false,
}

// parseAlternates parses font-variant-alternates: normal, or
// historical-forms and the functions, each at most once.
func parseAlternates(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "normal" {
		return Alternates{}, true
	}
	var a Alternates
	seen := map[string]bool{}
	for _, t := range v {
		name := lower(t.value)
		if seen[name] {
			return nil, false
		}
		seen[name] = true
		if !alternateComponent(&a, t) {
			return nil, false
		}
	}
	return a, len(v) > 0
}

// alternateComponent adds one component of font-variant-alternates to a,
// false when t is not one.
func alternateComponent(a *Alternates, t token) bool {
	if t.typ == tIdent {
		if !t.isIdent("historical-forms") {
			return false
		}
		a.HistoricalForms = true
		return true
	}
	list, ok := alternateFunctions[lower(t.value)]
	if t.typ != tFunction || !ok || !featureValueNames(t.children, list) {
		return false
	}
	var names []string
	for _, p := range splitCommas(trimWS(t.children)) {
		names = append(names, significant(p)[0].value)
	}
	if len(names) > maxFeatureSettings {
		return false
	}
	switch lower(t.value) {
	case "stylistic":
		a.Stylistic = names[0]
	case "swash":
		a.Swash = names[0]
	case "ornaments":
		a.Ornaments = names[0]
	case "annotation":
		a.Annotation = names[0]
	case "styleset":
		a.Styleset = names
	case "character-variant":
		a.CharacterVariant = names
	}
	return true
}

// parseLanguageOverride parses font-language-override (CSS Fonts 4, 6.14):
// normal or a string, an OpenType language system tag.
func parseLanguageOverride(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "normal" {
		return "", true
	}
	t, ok := single(v)
	if !ok || t.typ != tString {
		return nil, false
	}
	return t.value, true
}

// FontSizeAdjust is font-size-adjust (CSS Fonts 5, 2.6): the value a
// metric of the font keeps as a fraction of the font size. The zero value
// is none.
type FontSizeAdjust struct {
	// Set is false for none.
	Set bool
	// Metric is the metric held: ex-height, cap-height, ch-width,
	// ic-width or ic-height, in that order from 0.
	Metric uint8
	// Value is the fraction, from 0 to maxFontSizeAdjust; with FromFont
	// it is the first available font's own.
	Value    float64
	FromFont bool
}

// fontSizeAdjustMetrics are the metric keywords of font-size-adjust.
var fontSizeAdjustMetrics = map[string]uint8{
	"ex-height": 0, "cap-height": 1, "ch-width": 2, "ic-width": 3, "ic-height": 4,
}

// maxFontSizeAdjust caps the value of font-size-adjust. Overflow guard,
// not spec-mandated: CSS Fonts 5, 2.6 takes any non-negative number, and
// a metric of ten ems draws nothing a reader can see whole.
const maxFontSizeAdjust = 10

// parseFontSizeAdjust parses font-size-adjust: none, or an optional metric
// keyword then from-font or a non-negative number.
func parseFontSizeAdjust(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "none" {
		return FontSizeAdjust{}, true
	}
	a := FontSizeAdjust{Set: true}
	if len(v) == 2 {
		m, ok := fontSizeAdjustMetrics[lower(v[0].value)]
		if v[0].typ != tIdent || !ok {
			return nil, false
		}
		a.Metric, v = m, v[1:]
	}
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	if t.isIdent("from-font") {
		a.FromFont = true
		return a, true
	}
	n, ok := parseNumber(t)
	if !ok || n < 0 {
		return nil, false
	}
	a.Value = min(n, maxFontSizeAdjust)
	return a, true
}

// fontFeatureProps returns the longhands of CSS Fonts 4, 6 this file adds,
// every one inherited: font-feature-settings, font-variant-ligatures,
// -numeric, -east-asian, -position, -alternates and -emoji,
// font-language-override, font-size-adjust and font-synthesis-small-caps.
func fontFeatureProps() map[propID]propDef {
	return map[propID]propDef{
		pFontFeatureSettings: {
			name: "font-feature-settings", inherited: true, parse: parseFeatureSettings, compute: computeFeatureSettings,
			copyFrom: func(dst, src *Style) { dst.FontFeatureSettings = slices.Clone(src.FontFeatureSettings) },
		},
		pFontVariantLigatures: {
			name: "font-variant-ligatures", inherited: true, parse: parseLigatures,
			compute: func(_ *styler, st *Style, v any) {
				if l, ok := v.(Ligatures); ok {
					st.FontVariantLigatures = l
				}
			},
			copyFrom: field(func(s *Style) *Ligatures { return &s.FontVariantLigatures }),
		},
		pFontVariantNumeric: {
			name: "font-variant-numeric", inherited: true, parse: parseNumeric,
			compute: func(_ *styler, st *Style, v any) {
				if n, ok := v.(NumericVariant); ok {
					st.FontVariantNumeric = n
				}
			},
			copyFrom: field(func(s *Style) *NumericVariant { return &s.FontVariantNumeric }),
		},
		pFontVariantEastAsian: {
			name: "font-variant-east-asian", inherited: true, parse: parseEastAsian,
			compute: func(_ *styler, st *Style, v any) {
				if e, ok := v.(EastAsianVariant); ok {
					st.FontVariantEastAsian = e
				}
			},
			copyFrom: field(func(s *Style) *EastAsianVariant { return &s.FontVariantEastAsian }),
		},
		pFontVariantPosition: enumProp("font-variant-position", true, func(s *Style) *VariantPosition { return &s.FontVariantPosition }, map[string]VariantPosition{
			"normal": PositionNormal, "sub": PositionSub, "super": PositionSuper,
		}),
		pFontVariantEmoji: enumProp("font-variant-emoji", true, func(s *Style) *VariantEmoji { return &s.FontVariantEmoji }, map[string]VariantEmoji{
			"normal": EmojiNormal, "text": EmojiText, "emoji": EmojiEmoji, "unicode": EmojiUnicode,
		}),
		pFontVariantAlternates: {
			name: "font-variant-alternates", inherited: true, parse: parseAlternates,
			compute: func(_ *styler, st *Style, v any) {
				if a, ok := v.(Alternates); ok {
					st.FontVariantAlternates = a
				}
			},
			copyFrom: func(dst, src *Style) {
				dst.FontVariantAlternates = src.FontVariantAlternates
				dst.FontVariantAlternates.Styleset = slices.Clone(src.FontVariantAlternates.Styleset)
				dst.FontVariantAlternates.CharacterVariant = slices.Clone(src.FontVariantAlternates.CharacterVariant)
			},
		},
		pFontLanguageOverride: {
			name: "font-language-override", inherited: true, parse: parseLanguageOverride,
			compute: func(_ *styler, st *Style, v any) {
				if s, ok := v.(string); ok {
					st.FontLanguageOverride = s
				}
			},
			copyFrom: field(func(s *Style) *string { return &s.FontLanguageOverride }),
		},
		pFontSizeAdjust: {
			name: "font-size-adjust", inherited: true, parse: parseFontSizeAdjust,
			compute: func(_ *styler, st *Style, v any) {
				if a, ok := v.(FontSizeAdjust); ok {
					st.FontSizeAdjust = a
				}
			},
			copyFrom: field(func(s *Style) *FontSizeAdjust { return &s.FontSizeAdjust }),
		},
		pFontSynthesisSmallCaps: synthesisProp("font-synthesis-small-caps", func(s *Style) *bool { return &s.NoSynthesizeSmallCaps }),
	}
}

// capsKeywords are the font-variant-caps values (CSS Fonts 4, 6.6).
var capsKeywords = map[string]FontVariant{
	"normal": VariantNormal, "small-caps": SmallCaps, "all-small-caps": AllSmallCaps,
	"petite-caps": PetiteCaps, "all-petite-caps": AllPetiteCaps, "unicase": Unicase,
	"titling-caps": TitlingCaps,
}

// variantLonghands are the longhands of the font-variant shorthand (CSS
// Fonts 4, 6.11) in the order of its value.
var variantLonghands = []propID{
	pFontVariantLigatures, pFontVariant, pFontVariantAlternates, pFontVariantNumeric,
	pFontVariantEastAsian, pFontVariantPosition, pFontVariantEmoji,
}

// variantComponent returns the font-variant longhand a component of the
// shorthand belongs to, false for none.
func variantComponent(t token) (propID, bool) {
	k := lower(t.value)
	switch t.typ { //nolint:exhaustive // any other token is invalid here
	case tIdent:
		switch {
		case k == "historical-forms":
			return pFontVariantAlternates, true
		case capsKeywords[k] != VariantNormal:
			return pFontVariant, true
		}
		if _, ok := ligatureKeywords[k]; ok {
			return pFontVariantLigatures, true
		}
		if _, ok := numericKeywords[k]; ok {
			return pFontVariantNumeric, true
		}
		if _, ok := eastAsianKeywords[k]; ok {
			return pFontVariantEastAsian, true
		}
		switch k {
		case "sub", "super":
			return pFontVariantPosition, true
		case "text", "emoji", "unicode":
			return pFontVariantEmoji, true
		}
	case tFunction:
		if _, ok := alternateFunctions[k]; ok {
			return pFontVariantAlternates, true
		}
	}
	return 0, false
}

// expandFontVariant expands the font-variant shorthand (CSS Fonts 4,
// 6.11): normal resets every longhand, none sets font-variant-ligatures
// to none and resets the others, and any other value hands each longhand
// its components, in any order, those it leaves out reset.
func expandFontVariant(v []token, c *declCtx) ([]entry, bool) {
	out := make([]entry, 0, len(variantLonghands))
	if k, ok := keyword(v); ok && (k == "normal" || k == "none") {
		for _, id := range variantLonghands {
			val := any(wideInitial)
			if id == pFontVariantLigatures && k == "none" {
				val = LigaturesCommonOff | LigaturesContextualOff
			}
			out = append(out, entry{id, val})
		}
		return out, true
	}
	parts := map[propID][]token{}
	for _, t := range v {
		id, ok := variantComponent(t)
		if !ok {
			return nil, false
		}
		parts[id] = append(parts[id], t)
	}
	for _, id := range variantLonghands {
		p, ok := parts[id]
		if !ok {
			out = append(out, entry{id, wideInitial})
			continue
		}
		val, ok := props[id].parse(p, c)
		if !ok {
			return nil, false
		}
		out = append(out, entry{id, val})
	}
	return out, true
}

// featureValueNames reports whether the arguments of a font-variant
// function are <font-feature-value-name>s, identifiers (CSS Fonts 4,
// 6.8): one, or a comma-separated list when list is set.
func featureValueNames(args []token, list bool) bool {
	parts := splitCommas(trimWS(args))
	if len(parts) == 0 || (!list && len(parts) > 1) {
		return false
	}
	for _, p := range parts {
		p = significant(p)
		if len(p) != 1 || p[0].typ != tIdent {
			return false
		}
	}
	return true
}

// FontFeatureValues is the content of the @font-feature-values rules of
// one family (CSS Fonts 4, 6.9): for each feature type, the values each
// name stands for.
type FontFeatureValues struct {
	// Family is the family name the rules name.
	Family string
	// Values maps a feature type (stylistic, styleset, character-variant,
	// swash, ornaments, annotation) to its names and their values.
	Values map[string]map[string][]int
}

// Resolve returns the OpenType features a, as computed, turns on for the
// family the values belong to, in Chromium's order (FontVariantAlternates
// ::Resolve): swash, ornaments, annotation, stylistic, the style sets,
// the character variants, then historical-forms. A nil v resolves no
// name. A name the values do not define, or define with a value count or
// range the feature type does not take, is ignored (CSS Fonts 4, 6.9.1).
func (a *Alternates) Resolve(v *FontFeatureValues) []FeatureSetting {
	lookup := func(kind, name string) []int {
		if v == nil || name == "" {
			return nil
		}
		return v.Values[kind][name]
	}
	var out []FeatureSetting
	one := func(kind, name string, tags ...string) {
		if n := lookup(kind, name); len(n) == 1 {
			for _, t := range tags {
				out = append(out, FeatureSetting{Tag: t, Value: n[0]})
			}
		}
	}
	one("swash", a.Swash, "swsh", "cswh")
	one("ornaments", a.Ornaments, "ornm")
	one("annotation", a.Annotation, "nalt")
	one("stylistic", a.Stylistic, "salt")
	for _, name := range a.Styleset {
		for _, n := range lookup("styleset", name) {
			if n >= 1 && n <= 99 {
				out = append(out, FeatureSetting{Tag: numberedTag("ss", n), Value: 1})
			}
		}
	}
	for _, name := range a.CharacterVariant {
		n := lookup("character-variant", name)
		if len(n) == 0 || len(n) > 2 || n[0] < 1 || n[0] > 99 {
			continue
		}
		val := 1
		if len(n) == 2 {
			val = n[1]
		}
		out = append(out, FeatureSetting{Tag: numberedTag("cv", n[0]), Value: val})
	}
	if a.HistoricalForms {
		out = append(out, FeatureSetting{Tag: "hist", Value: 1})
	}
	return out
}

// numberedTag returns the tag of prefix and the two digits of n, from 1
// to 99: ss01 to ss99, cv01 to cv99.
func numberedTag(prefix string, n int) string {
	return prefix + string([]byte{byte('0' + n/10), byte('0' + n%10)}) //nolint:gosec // n is from 1 to 99, checked by the callers
}

// featureValueBlocks are the feature type blocks of @font-feature-values
// (CSS Fonts 4, 6.9.1) and the most values a name of each takes, 0 for
// any number.
var featureValueBlocks = map[string]int{
	"stylistic": 1, "styleset": 0, "character-variant": 2,
	"swash": 1, "ornaments": 1, "annotation": 1,
}

// maxFeatureValueNames caps the names one @font-feature-values rule
// defines. Anti-DoS guard, not spec-mandated: a font has at most 99 style
// sets and 99 character variants, and a rule names each a few times.
const maxFeatureValueNames = 1024

// parseFontFeatureValues parses an @font-feature-values rule (CSS Fonts
// 4, 6.9): a comma list of family names, then a block of feature type
// at-rules, each a list of declarations of a name and non-negative
// integers. Declarations that do not parse are dropped. It returns one
// entry per family named.
func parseFontFeatureValues(prelude, block []token) []FontFeatureValues {
	var families []string
	for _, part := range splitCommas(trimWS(prelude)) {
		p := significant(part)
		var words []string
		for _, t := range p {
			if t.typ != tIdent && (t.typ != tString || len(p) != 1) {
				return nil
			}
			words = append(words, t.value)
		}
		if len(words) == 0 {
			return nil
		}
		families = append(families, strings.Join(words, " "))
	}
	if len(families) == 0 {
		return nil
	}
	values := map[string]map[string][]int{}
	names := 0
	newListParser(block).declarations(func(it item) bool {
		if it.kind != itemAtRule || !it.hasBlock {
			return true
		}
		kind := lower(it.name)
		maxValues, ok := featureValueBlocks[kind]
		if !ok {
			return true
		}
		newListParser(it.block).declarations(func(d item) bool {
			if d.kind != itemDeclaration || names >= maxFeatureValueNames {
				return true
			}
			n, ok := featureValueList(significant(d.value), maxValues)
			if !ok {
				return true
			}
			if values[kind] == nil {
				values[kind] = map[string][]int{}
			}
			values[kind][d.name] = n
			names++
			return true
		})
		return true
	})
	out := make([]FontFeatureValues, 0, len(families))
	for _, f := range families {
		out = append(out, FontFeatureValues{Family: f, Values: values})
	}
	return out
}

// featureValueList parses the value of a feature value declaration: one
// or more non-negative integers, at most maxValues of them unless it is 0.
func featureValueList(v []token, maxValues int) ([]int, bool) {
	if len(v) == 0 || (maxValues > 0 && len(v) > maxValues) || len(v) > maxFeatureSettings {
		return nil, false
	}
	out := make([]int, 0, len(v))
	for _, t := range v {
		n, ok := parseInteger(t)
		if !ok || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
