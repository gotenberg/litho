package css

import (
	"strconv"
	"strings"
)

// FontFace is an @font-face rule (CSS Fonts 4, 4): the descriptors that
// select and locate a font. Loading it is the caller's business.
type FontFace struct {
	// Family is the family name the rule defines.
	Family string
	// Sources lists the src descriptor's entries in order of preference.
	Sources []FontSource
	// WeightMin and WeightMax bound the weights the face covers, 400 and
	// 400 when the rule leaves font-weight out or sets it to auto (CSS
	// Fonts 4, 4.5).
	WeightMin, WeightMax int
	// Style is the font-style the face covers.
	Style FontStyle
	// ObliqueMin and ObliqueMax bound the slant angles, in degrees, an
	// oblique face covers: 14 and 14 for a bare oblique (CSS Fonts 4,
	// 4.4). Both are 0 for a normal or italic face.
	ObliqueMin, ObliqueMax float64
	// StretchMin and StretchMax bound the widths the face covers, as
	// percentages of the normal width: 100 and 100 when the rule leaves
	// font-stretch out or sets it to auto (CSS Fonts 4, 4.6).
	StretchMin, StretchMax float64
	// UnicodeRange lists the inclusive code point ranges the face covers,
	// nil for all of them.
	UnicodeRange [][2]rune
}

// FontSource is one entry of an @font-face src descriptor (CSS Fonts 4,
// 4.3).
type FontSource struct {
	// URL is the resolved URL of a url() entry, "" for local().
	URL string
	// Local is the font name of a local() entry.
	Local string
	// Format is the first lowercased format() hint, "" when absent.
	Format string
	// Formats lists every lowercased format() hint: the older syntax
	// allows several, any of which the user agent may support.
	Formats []string
	// Tech lists the lowercased tech() keywords, the font technologies the
	// file needs (CSS Fonts 4, 4.3.1).
	Tech []string
}

// Defaults of the @font-face descriptors (CSS Fonts 4, 4.4 to 4.6).
const (
	defaultFaceWeight  = 400
	defaultFaceStretch = 100
	// defaultObliqueAngle is the angle a bare oblique stands for.
	defaultObliqueAngle = 14
	// maxObliqueAngle bounds an oblique angle: CSS Fonts 4, 4.4 makes
	// angles beyond 90 degrees either way invalid.
	maxObliqueAngle = 90
)

// ignoredFaceDescriptors are @font-face descriptors litho parses and does
// not apply: the font feature and variation settings, the metric overrides
// (CSS Fonts 4, 4.8 to 4.10; CSS Fonts 5, 4.10 and 4.11) and the named
// instance. A rule that sets one still loads, without it, and gets a
// warning.
var ignoredFaceDescriptors = map[string]bool{
	"font-feature-settings":   true,
	"font-variation-settings": true,
	"font-named-instance":     true,
	"font-language-override":  true,
	"ascent-override":         true,
	"descent-override":        true,
	"line-gap-override":       true,
	"size-adjust":             true,
}

// parseFontFace parses an @font-face block, false when it lacks a family
// or a usable src. A descriptor whose value does not parse keeps its
// initial value (CSS Fonts 4, 4.1).
func parseFontFace(block []token, c *declCtx) (FontFace, bool) {
	f := FontFace{
		WeightMin: defaultFaceWeight, WeightMax: defaultFaceWeight,
		StretchMin: defaultFaceStretch, StretchMax: defaultFaceStretch,
	}
	newListParser(block).declarations(func(it item) bool {
		if it.kind != itemDeclaration {
			return true
		}
		v := significant(it.value)
		name := lower(it.name)
		switch name {
		case "font-family":
			fam, ok := parseFontFamily(v, c)
			if l, isList := fam.([]string); ok && isList && len(l) == 1 && !genericFamilies[l[0]] {
				f.Family = l[0]
			}
		case "src":
			f.Sources = parseFontSources(v, c.baseURL)
		case "font-weight":
			f.WeightMin, f.WeightMax = parseFaceWeight(v)
		case "font-style":
			if st, lo, hi, ok := parseFaceStyle(v); ok {
				f.Style, f.ObliqueMin, f.ObliqueMax = st, lo, hi
			}
		case "font-stretch":
			if lo, hi, ok := parseFaceStretch(v); ok {
				f.StretchMin, f.StretchMax = lo, hi
			}
		case "unicode-range":
			f.UnicodeRange = parseUnicodeRanges(it.value)
		case "font-display":
			// Loading is synchronous: every face is there before layout,
			// so the display policy has nothing to choose (CSS Fonts 4,
			// 4.9).
		default:
			if ignoredFaceDescriptors[name] {
				c.warn("unsupported-css-property", "the @font-face descriptor %q is not supported and was ignored", name)
			}
		}
		return true
	})
	return f, f.Family != "" && len(f.Sources) > 0
}

// parseFontSources parses the src descriptor (CSS Fonts 4, 4.3), keeping
// the entries that parse: local(<family-name>), or <url> followed by an
// optional format() and an optional tech(). An entry that does not parse is
// dropped and the others kept.
func parseFontSources(v []token, base string) []FontSource {
	var out []FontSource
	for _, part := range splitCommas(v) {
		if len(part) == 0 {
			continue
		}
		var s FontSource
		if part[0].typ == tFunction && eqFold(part[0].value, "local") {
			if len(part) != 1 {
				continue
			}
			args := significant(part[0].children)
			var words []string
			for _, a := range args {
				if a.typ != tString && a.typ != tIdent {
					words = nil
					break
				}
				words = append(words, a.value)
			}
			if len(words) == 0 || (len(words) > 1 && args[0].typ == tString) {
				continue
			}
			s.Local = strings.Join(words, " ")
			out = append(out, s)
			continue
		}
		u, ok := parseURL(part[0], base)
		if !ok {
			continue
		}
		s.URL = u
		if !parseSourceHints(part[1:], &s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// parseSourceHints reads the format() and tech() functions after a url()
// entry, in that order, each at most once, false when anything else
// follows.
func parseSourceHints(hints []token, s *FontSource) bool {
	keywords := func(t token) ([]string, bool) {
		var out []string
		for _, part := range splitCommas(significant(t.children)) {
			if len(part) != 1 || (part[0].typ != tString && part[0].typ != tIdent) {
				return nil, false
			}
			out = append(out, lower(part[0].value))
		}
		return out, len(out) > 0
	}
	seenFormat, seenTech := false, false
	for _, h := range hints {
		if h.typ != tFunction {
			return false
		}
		switch {
		case eqFold(h.value, "format") && !seenFormat && !seenTech:
			seenFormat = true
			f, ok := keywords(h)
			if !ok {
				return false
			}
			s.Formats, s.Format = f, f[0]
		case eqFold(h.value, "tech") && !seenTech:
			seenTech = true
			t, ok := keywords(h)
			if !ok {
				return false
			}
			s.Tech = t
		default:
			return false
		}
	}
	return true
}

// parseFaceWeight parses the @font-face font-weight descriptor (CSS Fonts
// 4, 4.5): auto, normal, bold, or one or two numbers from 1 to 1000; a
// decreasing range is swapped.
func parseFaceWeight(v []token) (lo, hi int) {
	one := func(t token) (int, bool) {
		switch {
		case t.isIdent("normal"):
			return 400, true
		case t.isIdent("bold"):
			return 700, true
		case t.typ == tNumber && t.num >= 1 && t.num <= 1000:
			return int(t.num), true
		}
		return 0, false
	}
	switch len(v) {
	case 1:
		if w, ok := one(v[0]); ok {
			return w, w
		}
	case 2:
		a, ok1 := one(v[0])
		b, ok2 := one(v[1])
		if ok1 && ok2 {
			return min(a, b), max(a, b)
		}
	}
	return defaultFaceWeight, defaultFaceWeight
}

// parseFaceStyle parses the @font-face font-style descriptor (CSS Fonts 4,
// 4.4): auto or normal, italic, or oblique with up to two angles between
// -90 and 90 degrees, 14 degrees by default; a decreasing range is swapped.
func parseFaceStyle(v []token) (FontStyle, float64, float64, bool) {
	if len(v) == 0 {
		return FontNormal, 0, 0, false
	}
	switch {
	case len(v) == 1 && (v[0].isIdent("normal") || v[0].isIdent("auto")):
		return FontNormal, 0, 0, true
	case len(v) == 1 && v[0].isIdent("italic"):
		return FontItalic, 0, 0, true
	case !v[0].isIdent("oblique") || len(v) > 3:
		return FontNormal, 0, 0, false
	}
	if len(v) == 1 {
		return FontOblique, defaultObliqueAngle, defaultObliqueAngle, true
	}
	var angles []float64
	for _, t := range v[1:] {
		if t.typ != tDimension {
			return FontNormal, 0, 0, false
		}
		a, ok := parseAngle(t)
		if !ok || a < -maxObliqueAngle || a > maxObliqueAngle {
			return FontNormal, 0, 0, false
		}
		angles = append(angles, a)
	}
	lo, hi := angles[0], angles[len(angles)-1]
	return FontOblique, min(lo, hi), max(lo, hi), true
}

// stretchKeywords are the font-stretch keywords as percentages (CSS Fonts
// 4, 2.3, Table 2).
var stretchKeywords = map[string]float64{
	"ultra-condensed": 50, "extra-condensed": 62.5, "condensed": 75,
	"semi-condensed": 87.5, "normal": 100, "semi-expanded": 112.5,
	"expanded": 125, "extra-expanded": 150, "ultra-expanded": 200,
}

// parseFaceStretch parses the @font-face font-stretch descriptor (CSS
// Fonts 4, 4.6): auto, or one or two keywords or non-negative percentages;
// a decreasing range is swapped.
func parseFaceStretch(v []token) (float64, float64, bool) {
	one := func(t token) (float64, bool) {
		if t.typ == tIdent {
			p, ok := stretchKeywords[lower(t.value)]
			return p, ok
		}
		if t.typ == tPercentage && finite(t.num) && t.num >= 0 {
			return t.num, true
		}
		return 0, false
	}
	switch {
	case len(v) == 1 && v[0].isIdent("auto"):
		return defaultFaceStretch, defaultFaceStretch, true
	case len(v) == 1:
		if p, ok := one(v[0]); ok {
			return p, p, true
		}
	case len(v) == 2:
		a, ok1 := one(v[0])
		b, ok2 := one(v[1])
		if ok1 && ok2 {
			return min(a, b), max(a, b), true
		}
	}
	return 0, 0, false
}

// tokenRepr serializes a token as the <urange> algorithm reads it (CSS
// Syntax 3, 7.1).
func tokenRepr(t token) (string, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tIdent:
		return t.value, true
	case tDelim:
		return string(t.delim), true
	case tNumber:
		return t.repr, true
	case tDimension:
		return t.repr + t.value, true
	}
	return "", false
}

// parseUnicodeRanges parses the unicode-range descriptor: a comma list of
// <urange> (CSS Fonts 4, 4.7 and CSS Syntax 3, 7.1). nil means the value
// failed and the face covers everything.
func parseUnicodeRanges(value []token) [][2]rune {
	var out [][2]rune
	for _, part := range splitCommas(trimWS(value)) {
		part = trimWS(part)
		if len(part) < 2 || !part[0].isIdent("u") {
			return nil
		}
		var b strings.Builder
		for _, t := range part[1:] {
			s, ok := tokenRepr(t)
			if !ok {
				return nil
			}
			b.WriteString(s)
		}
		r, ok := parseURange(b.String())
		if !ok {
			return nil
		}
		out = append(out, r)
	}
	return out
}

// parseURange parses the text after "u" of a <urange>: "+" then up to six
// hex digits or trailing question marks, or two hex runs joined by "-".
func parseURange(s string) ([2]rune, bool) {
	if !strings.HasPrefix(s, "+") {
		return [2]rune{}, false
	}
	s = s[1:]
	i := 0
	for i < len(s) && i < 6 && isHexDigit(rune(s[i])) {
		i++
	}
	q := i
	for q < len(s) && q < 6 && s[q] == '?' {
		q++
	}
	if q == 0 {
		return [2]rune{}, false
	}
	if q > i {
		if q != len(s) {
			return [2]rune{}, false
		}
		lo, _ := strconv.ParseUint(s[:i]+strings.Repeat("0", q-i), 16, 32) //nolint:errcheck // hex digits checked above
		hi, _ := strconv.ParseUint(s[:i]+strings.Repeat("F", q-i), 16, 32) //nolint:errcheck // hex digits checked above
		return checkRange(lo, hi)
	}
	lo, _ := strconv.ParseUint(s[:i], 16, 32) //nolint:errcheck // hex digits checked above
	if i == len(s) {
		return checkRange(lo, lo)
	}
	if s[i] != '-' {
		return [2]rune{}, false
	}
	rest := s[i+1:]
	if len(rest) == 0 || len(rest) > 6 {
		return [2]rune{}, false
	}
	for j := 0; j < len(rest); j++ {
		if !isHexDigit(rune(rest[j])) {
			return [2]rune{}, false
		}
	}
	hi, _ := strconv.ParseUint(rest, 16, 32) //nolint:errcheck // hex digits checked above
	return checkRange(lo, hi)
}

func checkRange(lo, hi uint64) ([2]rune, bool) {
	if hi > 0x10FFFF || lo > hi {
		return [2]rune{}, false
	}
	return [2]rune{rune(lo), rune(hi)}, true //nolint:gosec // both at most 0x10FFFF, checked above
}
