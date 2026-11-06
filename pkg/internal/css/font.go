package css

import (
	"math"
	"strings"
)

// fontSizeKeywords are the absolute-size keywords as multiples of medium
// (CSS Fonts 4, 2.5, the table of CSS Fonts 3, 3.5), medium being 16px.
var fontSizeKeywords = map[string]float64{
	"xx-small": 3.0 / 5, "x-small": 3.0 / 4, "small": 8.0 / 9, "medium": 1,
	"large": 6.0 / 5, "x-large": 3.0 / 2, "xx-large": 2, "xxx-large": 3,
}

// mediumFontSize is the medium font size in points: 16px.
const mediumFontSize = 16 * Pt

// fontSizeScale is the ratio larger and smaller apply (CSS 2.1, 15.7).
const fontSizeScale = 1.2

// fontSizeSpec is a specified font-size: an absolute keyword's factor, a
// relative keyword's step, or a length whose percentages and em units
// resolve against the parent's font size.
type fontSizeSpec struct {
	factor float64
	step   int
	l      length
	isLen  bool
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
		f, ok := fontSizeKeywords[k]
		return fontSizeSpec{factor: f}, ok
	}
	l, ok := parseLength(t, allowPct|nonNeg|allowMixed)
	return fontSizeSpec{l: l, isLen: true}, ok
}

// computeFontSize computes font-size (CSS Fonts 4, 2.5): em and
// percentages against the parent's size, rem against the root's.
func computeFontSize(s *styler, st *Style, v any) {
	f, ok := v.(fontSizeSpec)
	if !ok {
		return
	}
	parent := s.parent.FontSize
	var size float64
	switch {
	case f.isLen:
		lc := s.lc
		lc.fontSize = parent
		size = f.l.points(lc) + f.l.pct/100*parent
	case f.step > 0:
		size = parent * fontSizeScale
	case f.step < 0:
		size = parent / fontSizeScale
	default:
		size = f.factor * mediumFontSize
	}
	st.FontSize = math.Max(0, math.Min(size, maxFontSize))
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
