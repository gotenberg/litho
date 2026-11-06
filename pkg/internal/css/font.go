package css

import (
	"math"
	"strings"
)

// fontSizeKeywords numbers the absolute-size keywords (CSS Fonts 4, 2.5),
// xx-small to xxx-large, from 1, the columns of [keywordSizes].
var fontSizeKeywords = map[string]int8{
	"xx-small": 1, "x-small": 2, "small": 3, "medium": mediumKeyword,
	"large": 5, "x-large": 6, "xx-large": 7, "xxx-large": 8,
}

// mediumKeyword is the number of medium in [fontSizeKeywords].
const mediumKeyword = 4

// keywordSizes are the absolute-size keywords in pixels, xx-small to
// xxx-large, for a proportional font (medium 16px) and for the generic
// monospace family alone (medium 13px). CSS Fonts 4, 2.5 leaves the table
// to the user agent: these are Chromium's, the standards-mode table of
// third_party/blink/renderer/core/css/font_size_functions.cc
// (kStrictFontSizeTable) at its default font sizes, 16px and 13px.
var keywordSizes = [2][8]float64{
	{9, 10, 13, 16, 18, 24, 32, 48},
	{9, 10, 12, 13, 16, 20, 26, 39},
}

// mediumFontSize is the medium font size in points: 16px.
const mediumFontSize = 16 * Pt

// fixedFontScale is the ratio of the default monospace font size to the
// default font size, 13px to 16px, the sizes Chromium's settings default to
// (Settings::GetDefaultFixedFontSize and GetDefaultFontSize).
const fixedFontScale = 13.0 / 16

// fontSizeScale is the ratio larger and smaller apply (CSS 2.1, 15.7).
const fontSizeScale = 1.2

// keywordSize returns the size in points of the absolute-size keyword k of
// [fontSizeKeywords], for the generic monospace family alone when mono.
func keywordSize(k int8, mono bool) float64 {
	row := 0
	if mono {
		row = 1
	}
	return keywordSizes[row][max(1, min(k, 8))-1] * Pt
}

// fontSizeSpec is a specified font-size: an absolute keyword's number, a
// relative keyword's step, or a length whose percentages and em units
// resolve against the parent's font size. relative marks a length that
// keeps the parent's absoluteness: a percentage, or an em, ex or ch length
// outside calc().
type fontSizeSpec struct {
	keyword  int8
	step     int
	l        length
	isLen    bool
	relative bool
}

func parseFontSize(v []token, _ *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	if t.typ == tIdent {
		k := lower(t.value)
		switch k {
		case "larger":
			return fontSizeSpec{step: 1}, true
		case "smaller":
			return fontSizeSpec{step: -1}, true
		}
		n, ok := fontSizeKeywords[k]
		return fontSizeSpec{keyword: n}, ok
	}
	l, ok := parseLength(t, allowPct|nonNeg|allowMixed)
	relative := t.typ == tPercentage
	if t.typ == tDimension {
		switch lower(t.value) {
		case "em", "ex", "ch":
			relative = true
		}
	}
	return fontSizeSpec{l: l, isLen: true, relative: relative}, ok
}

// computeFontSize computes font-size (CSS Fonts 4, 2.5): em and
// percentages against the parent's size, rem against the root's, an
// absolute-size keyword from [keywordSizes]. A keyword is sized for a
// proportional font here; [fixMonospaceSize] resizes it once the family is
// known.
func computeFontSize(s *styler, st *Style, v any) {
	f, ok := v.(fontSizeSpec)
	if !ok {
		return
	}
	parent := s.parent.FontSize
	var size float64
	st.fontSizeKeyword, st.fontSizeAbsolute = 0, s.parent.fontSizeAbsolute
	switch {
	case f.isLen:
		lc := s.lc
		lc.fontSize = parent
		size = f.l.points(lc) + f.l.pct/100*parent
		st.fontSizeAbsolute = st.fontSizeAbsolute || !f.relative
	case f.step > 0:
		size = parent * fontSizeScale
	case f.step < 0:
		size = parent / fontSizeScale
	default:
		size = keywordSize(f.keyword, false)
		st.fontSizeKeyword, st.fontSizeAbsolute = f.keyword, false
	}
	st.FontSize = math.Max(0, math.Min(size, maxFontSize))
}

// copyFontSize copies the font size and what it was derived from.
func copyFontSize(dst, src *Style) {
	dst.FontSize, dst.fontSizeKeyword, dst.fontSizeAbsolute = src.FontSize, src.fontSizeKeyword, src.fontSizeAbsolute
}

// isMonospace reports whether families is the generic monospace family
// alone, the family list Chromium sizes as monospace
// (FontDescription::IsMonospace).
func isMonospace(families []string) bool {
	return len(families) == 1 && families[0] == "monospace"
}

// fixMonospaceSize applies the monospace size rule browsers follow and no
// specification states: the generic monospace family alone is sized against
// a medium of 13px rather than 16px. An absolute-size keyword takes the size
// of its row of [keywordSizes] for the element's family. A size no
// absolute length set (a keyword, a percentage or an em length up the
// chain) is scaled by [fixedFontScale] where the element turns to that
// family from another, and back where it turns away from it. A size an
// absolute length set (px, pt, rem, a viewport unit or calc()) stays. This
// is Chromium's FontBuilder::CheckForGenericFamilyChange and
// UpdateSpecifiedSize (third_party/blink/renderer/core/css/resolver
// /font_builder.cc), with StyleBuilderConverterBase::ConvertFontSize
// deciding what is absolute.
func fixMonospaceSize(st, parent *Style) {
	mono := isMonospace(st.FontFamilies)
	switch {
	case st.fontSizeKeyword > 0:
		st.FontSize = keywordSize(st.fontSizeKeyword, mono)
	case st.fontSizeAbsolute || mono == isMonospace(parent.FontFamilies):
	case mono:
		st.FontSize *= fixedFontScale
	default:
		st.FontSize /= fixedFontScale
	}
	st.FontSize = math.Max(0, math.Min(st.FontSize, maxFontSize))
}

// genericFamilies are the generic family keywords (CSS Fonts 4, 2.1.1).
var genericFamilies = map[string]bool{
	"serif": true, "sans-serif": true, "monospace": true, "cursive": true, "fantasy": true,
	"system-ui": true, "ui-serif": true, "ui-sans-serif": true, "ui-monospace": true,
	"ui-rounded": true, "math": true, "emoji": true, "fangsong": true,
}

// parseFontFamily parses a <family-name> list (CSS Fonts 4, 2.1): strings
// and sequences of idents joined by single spaces. A generic family is
// kept lowercased; a CSS-wide or default keyword as a lone ident is
// invalid.
func parseFontFamily(v []token, _ *declCtx) (any, bool) {
	var out []string
	for _, part := range splitCommas(v) {
		if len(part) == 0 {
			return nil, false
		}
		if len(part) == 1 && part[0].typ == tString {
			out = append(out, part[0].value)
			continue
		}
		words := make([]string, 0, len(part))
		for _, t := range part {
			if t.typ != tIdent {
				return nil, false
			}
			words = append(words, t.value)
		}
		if len(words) == 1 {
			k := lower(words[0])
			if genericFamilies[k] {
				out = append(out, k)
				continue
			}
			switch k {
			case "inherit", "initial", "unset", "default", "revert", "revert-layer":
				return nil, false
			}
		}
		out = append(out, strings.Join(words, " "))
	}
	return out, len(out) > 0
}

// weightSpec is a specified font-weight: a number, or a relative step.
type weightSpec struct {
	n    int
	step int
}

func parseFontWeight(v []token, _ *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	if t.typ == tIdent {
		switch lower(t.value) {
		case "normal":
			return weightSpec{n: 400}, true
		case "bold":
			return weightSpec{n: 700}, true
		case "bolder":
			return weightSpec{step: 1}, true
		case "lighter":
			return weightSpec{step: -1}, true
		}
		return nil, false
	}
	n, ok := parseNumber(t)
	if !ok || n < 1 || n > 1000 {
		return nil, false
	}
	return weightSpec{n: int(math.Round(n))}, true
}

// computeFontWeight computes font-weight (CSS Fonts 4, 2.2): bolder and
// lighter follow the table of relative weights; the result is clamped to
// 100 to 900, the range [Style.FontWeight] promises.
func computeFontWeight(s *styler, st *Style, v any) {
	w, ok := v.(weightSpec)
	if !ok {
		return
	}
	n := w.n
	p := s.parent.FontWeight
	switch {
	case w.step > 0:
		switch {
		case p < 350:
			n = 400
		case p < 550:
			n = 700
		default:
			n = 900
		}
	case w.step < 0:
		switch {
		case p < 550:
			n = 100
		case p < 750:
			n = 400
		default:
			n = 700
		}
	}
	st.FontWeight = max(100, min(900, n))
}

// parseFontStyle parses font-style (CSS Fonts 4, 2.4); an oblique angle is
// accepted and ignored.
func parseFontStyle(v []token, _ *declCtx) (any, bool) {
	if len(v) == 0 || v[0].typ != tIdent {
		return nil, false
	}
	switch lower(v[0].value) {
	case "normal":
		return FontNormal, len(v) == 1
	case "italic":
		return FontItalic, len(v) == 1
	case "oblique":
		if len(v) == 2 {
			if _, ok := parseAngle(v[1]); !ok || v[1].typ != tDimension {
				return nil, false
			}
		}
		return FontOblique, len(v) <= 2
	}
	return nil, false
}

func parseLineHeight(v []token, _ *declCtx) (any, bool) {
	d, ok := parseDim(v, allowPct|nonNeg|allowMixed, kwNormal, true)
	return d, ok
}

// computeLineHeight computes line-height (CSS 2.1, 10.8.1): a length or a
// percentage of the element's font size becomes points, a number stays a
// factor.
func computeLineHeight(s *styler, st *Style, v any) {
	d, ok := v.(dimSpec)
	if !ok {
		return
	}
	switch d.kind { //nolint:exhaustive // the other kinds take the default
	case Points:
		st.LineHeight = Pts(math.Max(0, d.l.points(s.lc)+d.l.pct/100*st.FontSize))
	case Number:
		st.LineHeight = Dimension{Kind: Number, Value: math.Min(d.num, maxLength)}
	default:
		st.LineHeight = Dimension{Kind: Normal}
	}
}
