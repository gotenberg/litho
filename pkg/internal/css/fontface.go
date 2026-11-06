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
	// 400 when the rule leaves font-weight out.
	WeightMin, WeightMax int
	// Style is the font-style the face covers.
	Style FontStyle
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
	// Format is the lowercased format() hint, "" when absent.
	Format string
}

// parseFontFace parses an @font-face block, false when it lacks a family
// or a usable src.
func parseFontFace(block []token, c *declCtx) (FontFace, bool) {
	f := FontFace{WeightMin: 400, WeightMax: 400}
	newListParser(block).declarations(func(it item) bool {
		if it.kind != itemDeclaration {
			return true
		}
		v := significant(it.value)
		switch lower(it.name) {
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
			if s, ok := parseFontStyle(v[:min(len(v), 1)], c); ok {
				if fs, isStyle := s.(FontStyle); isStyle {
					f.Style = fs
				}
			}
		case "unicode-range":
			f.UnicodeRange = parseUnicodeRanges(it.value)
		}
		return true
	})
	return f, f.Family != "" && len(f.Sources) > 0
}

// parseFontSources parses the src descriptor (CSS Fonts 4, 4.3), keeping
// the entries that parse.
func parseFontSources(v []token, base string) []FontSource {
	var out []FontSource
	for _, part := range splitCommas(v) {
		if len(part) == 0 {
			continue
		}
		var s FontSource
		if part[0].typ == tFunction && eqFold(part[0].value, "local") {
			args := significant(part[0].children)
			var words []string
			for _, a := range args {
				if a.typ == tString || a.typ == tIdent {
					words = append(words, a.value)
				}
			}
			if len(words) == 0 {
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
		for _, hint := range part[1:] {
			if hint.typ == tFunction && eqFold(hint.value, "format") {
				for _, a := range significant(hint.children) {
					if a.typ == tString || a.typ == tIdent {
						s.Format = lower(a.value)
						break
					}
				}
			}
		}
		out = append(out, s)
	}
	return out
}

// parseFaceWeight parses the @font-face font-weight descriptor (CSS Fonts
// 4, 4.5): auto, normal, bold, or one or two numbers.
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
	return 400, 400
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
