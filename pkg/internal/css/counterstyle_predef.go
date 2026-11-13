package css

//go:generate go run ../../../cmd/gen-counter-styles -out counterstyles_gen.go

import (
	"context"
	"strings"
	"sync"
	"unicode"
)

// predefinedCounterStyles returns the predefined counter styles by name:
// the rules of CSS Counter Styles 3, sections 6 and 7, the longhand
// Chinese and Korean styles and ethiopic-numeric by their algorithms,
// and the legacy aliases. Built once; immutable.
func predefinedCounterStyles() map[string]*counterStyleRule {
	predefinedOnce.Do(buildPredefined)
	return predefined
}

// predefined holds the predefined styles, built once by buildPredefined.
var (
	predefinedOnce sync.Once
	predefined     map[string]*counterStyleRule
)

// algorithmicStyles are the predefined styles litho spells by an
// algorithm rather than a rule of the generated style sheet, aliases
// included.
var algorithmicStyles = []string{
	"simp-chinese-informal", "simp-chinese-formal", "trad-chinese-informal", "trad-chinese-formal",
	"ethiopic-numeric", "cjk-ideographic",
}

// predefinedNames returns the names of the predefined styles, read from
// the generated style sheet without parsing it, for [counterStyleName].
var predefinedNames = sync.OnceValue(func() map[string]bool {
	out := map[string]bool{}
	for _, n := range algorithmicStyles {
		out[n] = true
	}
	for part := range strings.SplitSeq(predefinedCounterStyleSheet, "@counter-style ") {
		if name, _, ok := strings.Cut(part, " "); ok {
			out[name] = true
		}
	}
	return out
})

// buildPredefined fills predefined.
func buildPredefined() {
	out := map[string]*counterStyleRule{}
	sheet, _ := Parse(context.Background(), []byte(predefinedCounterStyleSheet), ParseOptions{}) //nolint:errcheck // a constant sheet parses whole; TestPredefinedCounterStyles reads every style
	if sheet != nil {
		for _, r := range sheet.counterStyles {
			out[r.name] = r
		}
	}
	for _, name := range []string{"simp-chinese-informal", "simp-chinese-formal", "trad-chinese-informal", "trad-chinese-formal"} {
		out[name] = &counterStyleRule{
			name: name, system: systemChinese, hasSystem: true, digits: cjkCharacters[name],
			suffix: "、", hasSuffix: true, fallback: "cjk-decimal", hasFall: true,
		}
	}
	for _, name := range []string{"korean-hangul-formal", "korean-hanja-informal", "korean-hanja-formal"} {
		r := out[name]
		if r == nil {
			continue
		}
		k := *r
		k.system, k.digits, k.additive, k.ranges, k.hasRange = systemKorean, cjkCharacters[name], nil, nil, false
		out[name] = &k
	}
	out["ethiopic-numeric"] = &counterStyleRule{name: "ethiopic-numeric", system: systemEthiopic, hasSystem: true, suffix: "/ ", hasSuffix: true}
	out["cjk-ideographic"] = out["trad-chinese-informal"]
	for _, name := range []string{"armenian", "lower-armenian"} {
		if r := out[name]; r != nil {
			out[name] = armenianExtended(r)
		}
	}
	if r := out["hebrew"]; r != nil {
		out["hebrew"] = hebrewZero(r)
	}
	predefined = out
}

// hebrewZero returns the hebrew style r with 0 in its range, spelled אפס,
// as Chromium spells it. CSS Counter Styles 3, 6.1 defines the style from
// 1 to 10999, and litho follows Chromium below it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 spells 0 in
//     hebrew "אפס".
func hebrewZero(r *counterStyleRule) *counterStyleRule {
	x := *r
	x.additive = append(append([]additiveSymbol(nil), r.additive...), additiveSymbol{weight: 0, symbol: "אפס"})
	x.ranges, x.hasRange = []counterRange{{lo: 0, hi: 10999}}, true
	return &x
}

// armenianExtended returns the armenian style r over the range Chromium
// gives it, 1 to 99999999: a symbol followed by U+0302 COMBINING
// CIRCUMFLEX ACCENT counts ten thousand times its weight. CSS Counter
// Styles 3, 6.1 defines the style from 1 to 9999, and litho follows
// Chromium past it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 spells 10000 in
//     armenian "Ա̂" and 78010092 "Ւ̂Պ̂Ա̂ՂԲ", and in
//     lower-armenian alike.
func armenianExtended(r *counterStyleRule) *counterStyleRule {
	x := *r
	x.additive = make([]additiveSymbol, 0, 2*len(r.additive))
	for _, t := range r.additive {
		x.additive = append(x.additive, additiveSymbol{weight: t.weight * 10000, symbol: t.symbol + "̂"})
	}
	x.additive = append(x.additive, r.additive...)
	x.ranges, x.hasRange = []counterRange{{lo: 1, hi: 99999999}}, true
	return &x
}

// isCombining reports a combining mark, which joins the grapheme cluster
// of the character before it.
func isCombining(r rune) bool { return unicode.Is(unicode.M, r) }

// cjkGroups splits v > 0 into groups of four decimal digits, ones group
// first, each group's digits ones first.
func cjkGroups(v int) [][4]int {
	var groups [][4]int
	for v > 0 {
		var g [4]int
		for i := range g {
			g[i] = v % 10
			v /= 10
		}
		groups = append(groups, g)
	}
	return groups
}

// chineseText spells v in a longhand Chinese style whose characters are
// chars (CSS Counter Styles 3, 7.1.5): the decimal number in groups of
// four digits with their group markers, a digit marker after each
// non-zero digit, the tens digit of a group from ten to nineteen dropped
// in the informal styles, the zeros trailing a group dropped and each
// run of the others, across groups, collapsed into one.
func chineseText(chars []string, v int) string {
	if v == 0 {
		return chars[0]
	}
	neg := v < 0
	if neg {
		v = -v
	}
	informal := chars[1] == "一"
	groups := cjkGroups(v)
	// parts holds the number's characters, "" standing for a zero digit.
	var parts []string
	for gi := len(groups) - 1; gi >= 0; gi-- {
		g := groups[gi]
		val := g[0] + 10*g[1] + 100*g[2] + 1000*g[3]
		if val == 0 {
			parts = append(parts, "")
			continue
		}
		top := 3
		if gi == len(groups)-1 {
			for g[top] == 0 {
				top--
			}
		}
		last := 0
		for g[last] == 0 {
			last++
		}
		for d := top; d >= last; d-- {
			switch {
			case g[d] == 0:
				parts = append(parts, "")
			case informal && d == 1 && val >= 10 && val <= 19:
				parts = append(parts, chars[10])
			case d > 0:
				parts = append(parts, chars[g[d]]+chars[9+d])
			default:
				parts = append(parts, chars[g[d]])
			}
		}
		if last > 0 {
			parts = append(parts, "")
		}
		if gi > 0 {
			parts = append(parts, chars[12+gi])
		}
	}
	var b strings.Builder
	if neg {
		b.WriteString(chars[16])
	}
	zero := false
	for i, p := range parts {
		if p == "" {
			zero = true
			continue
		}
		if zero && i > 0 && !isGroupMarker(chars, p) {
			b.WriteString(chars[0])
		}
		zero = false
		b.WriteString(p)
	}
	return b.String()
}

// isGroupMarker reports whether p is a group marker of chars.
func isGroupMarker(chars []string, p string) bool {
	return p == chars[13] || p == chars[14] || p == chars[15]
}

// koreanText spells v in a longhand Korean style whose characters are
// chars (CSS Counter Styles 3, 7.1.5): groups of four digits with their
// group markers, separated by spaces, a digit marker after each non-zero
// digit and every zero dropped. Two steps follow Chromium instead of
// 7.1.5: the informal style drops a one before any digit or group marker,
// where 7.1.5 keeps the one that leads its group, and a space follows the
// negative sign, as the limited range's negative descriptor writes it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 spells 78010092
//     "칠천팔백일만 구십이" in korean-hangul-formal and -3 "마이너스 삼", and
//     12345 "萬 二千三百四十五" and 100100000 "億 十萬" in
//     korean-hanja-informal.
func koreanText(chars []string, v int) string {
	if v == 0 {
		return chars[0]
	}
	neg := v < 0
	if neg {
		v = -v
	}
	informal := chars[1] == "一"
	groups := cjkGroups(v)
	var b strings.Builder
	if neg {
		b.WriteString(chars[16] + " ")
	}
	sep := false
	for gi := len(groups) - 1; gi >= 0; gi-- {
		g := groups[gi]
		if g == [4]int{} {
			continue
		}
		if sep {
			b.WriteString(" ")
		}
		sep = true
		for d := 3; d >= 0; d-- {
			if g[d] == 0 {
				continue
			}
			if !informal || g[d] != 1 || d == 0 && gi == 0 {
				b.WriteString(chars[g[d]])
			}
			if d > 0 {
				b.WriteString(chars[9+d])
			}
		}
		if gi > 0 {
			b.WriteString(chars[12+gi])
		}
	}
	return b.String()
}

// ethiopicUnits and ethiopicTens are the digits of ethiopic-numeric (CSS
// Counter Styles 3, 7.2), index 0 unused.
var (
	ethiopicUnits = []string{"", "፩", "፪", "፫", "፬", "፭", "፮", "፯", "፰", "፱"}
	ethiopicTens  = []string{"", "፲", "፳", "፴", "፵", "፶", "፷", "፸", "፹", "፺"}
)

// ethiopicText spells v >= 1 in ethiopic-numeric (CSS Counter Styles 3,
// 7.2): groups of two digits, ones group first, a group of 0, or of 1
// when it is the most significant or has an odd index, losing its digits,
// then ፻ after each odd group that is not 0 and ፼ after each even one but
// the first.
func ethiopicText(v int) string {
	if v == 1 {
		return "፩"
	}
	var groups []int
	for v > 0 {
		groups = append(groups, v%100)
		v /= 100
	}
	var b strings.Builder
	for i := len(groups) - 1; i >= 0; i-- {
		g := groups[i]
		if g != 0 && (g != 1 || i != len(groups)-1 && i%2 == 0) {
			b.WriteString(ethiopicTens[g/10])
			b.WriteString(ethiopicUnits[g%10])
		}
		switch {
		case i%2 == 1:
			if g != 0 {
				b.WriteString("፻")
			}
		case i > 0:
			b.WriteString("፼")
		}
	}
	return b.String()
}
