package css

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// Media is the medium a [Resolver] evaluates @media rules against.
type Media struct {
	// Type is "print" or "screen"; "" means print.
	Type string
	// Width and Height are the page box size in points. Width and height
	// media features compare against them and, unless ViewportWidth and
	// ViewportHeight are set, the vw, vh, vmin and vmax units resolve
	// against them. Zero makes every width and height feature false and
	// every viewport unit zero.
	Width, Height float64
	// ViewportWidth and ViewportHeight, when both are positive, are the
	// size in points the vw, vh, vmin and vmax units resolve against in
	// place of Width and Height (CSS Values 4, 6.1.2): a paginated medium
	// whose media features see another size than its initial containing
	// block, as Chromium's print evaluates media queries against the
	// paper's page area and sizes viewport units by the @page page area.
	ViewportWidth, ViewportHeight float64
	// Features are the discrete media features (Media Queries 5, 2.4) the
	// @media rules evaluate against, by name, such as
	// "prefers-color-scheme": "dark" or "hover": "hover". A feature absent
	// from the map keeps the value a printed page has, or a screen's where
	// the two differ and Type is "screen": paged and scroll overflow,
	// no and fast updates, an sRGB gamut, a standard dynamic range, the
	// browser display mode, no forced colours, a light colour scheme, no
	// preference, no pointer and no hover. Keys are lowercase; values are
	// compared case-insensitively.
	Features map[string]string
}

// mediaCond is the media query list of a conditional group or an
// @import, chained to the enclosing one: a rule applies when every list
// in its chain matches.
type mediaCond struct {
	list   []token
	parent *mediaCond
}

// matches evaluates every list of the chain.
func (c *mediaCond) matches(m Media, cache map[*mediaCond]bool) bool {
	if c == nil {
		return true
	}
	if v, ok := cache[c]; ok {
		return v
	}
	v := evalMediaList(c.list, m) && c.parent.matches(m, cache)
	cache[c] = v
	return v
}

// evalMediaList evaluates a <media-query-list> (Media Queries 4, 3): true
// when any query matches, an empty list matching all. A query that fails
// to parse is "not all" (Media Queries 4, 3.2).
func evalMediaList(list []token, m Media) bool {
	parts := splitCommas(trimWS(list))
	if len(parts) == 0 {
		return true
	}
	for _, q := range parts {
		if v, ok := evalMediaQuery(significant(q), m); ok && v == triTrue {
			return true
		}
	}
	return false
}

// evalMediaQuery evaluates one <media-query>: [not | only]? <media-type>
// [and <media-condition-without-or>]? or a <media-condition>, in the
// three-valued logic of Media Queries 4, 3.2: a query that is unknown
// does not match, and not unknown is unknown.
func evalMediaQuery(q []token, m Media) (tri, bool) {
	leaf := func(t token) (tri, bool) { return evalFeature(t, m) }
	if len(q) == 0 {
		return triFalse, false
	}
	if q[0].typ != tIdent || (q[0].isIdent("not") && len(q) > 1 && q[1].typ != tIdent) {
		return evalCondition(q, leaf)
	}
	neg := false
	i := 0
	switch {
	case q[0].isIdent("not"):
		neg = true
		i++
	case q[0].isIdent("only"):
		i++
	}
	if i >= len(q) || q[i].typ != tIdent {
		return triFalse, false
	}
	typ := lower(q[i].value)
	switch typ {
	case "and", "or", "not", "only", "layer":
		return triFalse, false
	}
	want := m.Type
	if want == "" {
		want = "print"
	}
	v := triOf(typ == "all" || typ == lower(want))
	i++
	if i < len(q) {
		if !q[i].isIdent("and") || i+1 >= len(q) {
			return triFalse, false
		}
		rest := q[i+1:]
		for _, t := range rest {
			if t.isIdent("or") {
				return triFalse, false
			}
		}
		c, ok := evalCondition(rest, leaf)
		if !ok {
			return triFalse, false
		}
		v = min(v, c)
	}
	if neg {
		v = v.not()
	}
	return v, true
}

// evalCondition evaluates a condition of the shape Media Queries 4, 3 and
// CSS Conditional 3, 6 share: not <in>, <in> [and <in>]*, <in> [or <in>]*,
// where <in> is a parenthesised condition or what leaf evaluates, in
// three-valued logic (Media Queries 4, 3.2): and is the least of its
// operands, or the greatest, and not unknown is unknown. It is false
// with false when the condition does not parse.
func evalCondition(l []token, leaf func(token) (tri, bool)) (tri, bool) {
	if len(l) == 0 {
		return triFalse, false
	}
	in := func(t token) (tri, bool) {
		if t.typ == tLParen {
			inner := significant(t.children)
			if len(inner) > 0 && (inner[0].typ == tLParen || inner[0].isIdent("not")) {
				if v, ok := evalCondition(inner, leaf); ok {
					return v, true
				}
				// A parenthesised value that is no condition is a
				// <general-enclosed> (Media Queries 4, 3; CSS
				// Conditional 3, 6), which leaf evaluates.
			}
		}
		return leaf(t)
	}
	if l[0].isIdent("not") {
		if len(l) != 2 {
			return triFalse, false
		}
		v, ok := in(l[1])
		if !ok {
			return triFalse, false
		}
		return v.not(), true
	}
	v, ok := in(l[0])
	if !ok {
		return triFalse, false
	}
	if len(l) == 1 {
		return v, true
	}
	op := ""
	for i := 1; i < len(l); i += 2 {
		if l[i].typ != tIdent || i+1 >= len(l) {
			return triFalse, false
		}
		o := lower(l[i].value)
		if (o != "and" && o != "or") || (op != "" && o != op) {
			return triFalse, false
		}
		op = o
		w, ok := in(l[i+1])
		if !ok {
			return triFalse, false
		}
		if op == "and" {
			v = min(v, w)
		} else {
			v = max(v, w)
		}
	}
	return v, true
}

// mediaLength evaluates a length in a media feature, in CSS pixels; em
// and rem are the initial font size, 16px (Media Queries 4, 1.3).
func mediaLength(t token) (float64, bool) {
	l, ok := parseLength(t, 0)
	if !ok || l.viewportRelative() {
		return 0, false
	}
	return l.points(lenCtx{fontSize: mediumFontSize, rootFontSize: mediumFontSize, lineHeight: normalLineHeight * mediumFontSize, rootLineHeight: normalLineHeight * mediumFontSize}) / Pt, true
}

// mediaRatio evaluates a <ratio>: a number, or two separated by a slash.
func mediaRatio(l []token) (float64, bool) {
	switch {
	case len(l) == 1 && l[0].typ == tNumber:
		return l[0].num, numeric.Finite(l[0].num)
	case len(l) == 3 && l[0].typ == tNumber && l[1].isDelim('/') && l[2].typ == tNumber && l[2].num != 0:
		return l[0].num / l[2].num, numeric.Finite(l[0].num / l[2].num)
	}
	return 0, false
}

// mediaColorBits is the bits per colour component the color media feature
// reports for a printed page (Media Queries 4, 6.1).
const mediaColorBits = 8

// evalFeature evaluates a <media-in-parens> holding a feature (Media
// Queries 4, 4): width, height, aspect-ratio and orientation against the
// page box, a handful of discrete features with the values of a static
// printed page, and the range syntax for width and height. A feature
// litho does not know, a value its feature does not take, and a
// <general-enclosed> are unknown (Media Queries 4, 3.2), as Chromium
// evaluates them: not (unknown) matches nothing either.
func evalFeature(t token, m Media) (tri, bool) {
	if t.typ != tLParen {
		return triUnknown, t.typ == tFunction
	}
	in := significant(t.children)
	if len(in) == 0 || in[0].typ != tIdent {
		if len(in) >= 3 {
			return evalRange(in, m), true
		}
		return triUnknown, true
	}
	name := lower(in[0].value)
	if len(in) == 1 {
		switch name {
		case "width", "height", "device-width", "device-height":
			return triOf(m.Width > 0 && m.Height > 0), true
		case "color", "orientation", "aspect-ratio":
			return triTrue, true
		case "monochrome", "grid":
			return triFalse, true
		}
		v, ok := discreteFeature(name, m)
		if !ok {
			return triUnknown, true
		}
		return triOf(discreteTrue(name, v)), true
	}
	if len(in) >= 3 && in[1].typ == tDelim {
		return evalRange(in, m), true
	}
	if in[1].typ != tColon || len(in) < 3 {
		return triUnknown, true
	}
	val := in[2:]
	w, h := m.Width/Pt, m.Height/Pt
	cmp := func(prefix string, got, want float64) bool {
		switch prefix {
		case "min-":
			return got >= want
		case "max-":
			return got <= want
		}
		return math.Abs(got-want) < 1e-9
	}
	for _, prefix := range []string{"min-", "max-", ""} {
		if len(name) < len(prefix) || name[:len(prefix)] != prefix {
			continue
		}
		base := name[len(prefix):]
		switch base {
		case "width", "height", "device-width", "device-height":
			if len(val) != 1 {
				return triUnknown, true
			}
			px, ok := mediaLength(val[0])
			if !ok {
				return triUnknown, true
			}
			if m.Width <= 0 || m.Height <= 0 {
				return triFalse, true
			}
			got := w
			if base == "height" || base == "device-height" {
				got = h
			}
			return triOf(cmp(prefix, got, px)), true
		case "aspect-ratio", "device-aspect-ratio":
			r, ok := mediaRatio(val)
			if !ok {
				return triUnknown, true
			}
			return triOf(h > 0 && cmp(prefix, w/h, r)), true
		}
	}
	if len(val) != 1 || val[0].typ != tIdent && val[0].typ != tNumber {
		return triUnknown, true
	}
	v := lower(val[0].value)
	if got, ok := discreteFeature(name, m); ok {
		if vals, known := discreteValues[name]; known && (val[0].typ != tIdent || !slices.Contains(vals, v)) {
			return triUnknown, true
		}
		return triOf(discreteMatches(name, v, got)), true
	}
	switch name {
	case "orientation":
		if v != "portrait" && v != "landscape" {
			return triUnknown, true
		}
		if h >= w {
			return triOf(v == "portrait"), true
		}
		return triOf(v == "landscape"), true
	case "color", "monochrome", "grid":
		if val[0].typ != tNumber || !val[0].integer || val[0].num < 0 {
			return triUnknown, true
		}
		if name == "color" {
			return triOf(val[0].num <= mediaColorBits), true
		}
		return triOf(val[0].num == 0), true
	}
	return triUnknown, true
}

// discreteValues are the values each discrete feature
// [discreteFeature] answers takes (Media Queries 4, 5 to 7; Media
// Queries 5, 4, 6 and 12): a query for any other value is unknown.
var discreteValues = map[string][]string{
	"overflow-block":               {"none", "scroll", "paged"},
	"overflow-inline":              {"none", "scroll"},
	"update":                       {"none", "slow", "fast"},
	"color-gamut":                  {"srgb", "p3", "rec2020"},
	"dynamic-range":                {"standard", "high"},
	"display-mode":                 {"fullscreen", "standalone", "minimal-ui", "browser", "picture-in-picture", "window-controls-overlay"},
	"prefers-color-scheme":         {"light", "dark"},
	"hover":                        {"none", "hover"},
	"any-hover":                    {"none", "hover"},
	"pointer":                      {"none", "coarse", "fine"},
	"any-pointer":                  {"none", "coarse", "fine"},
	"scripting":                    {"none", "initial-only", "enabled"},
	"forced-colors":                {"none", "active"},
	"prefers-reduced-motion":       {"no-preference", "reduce"},
	"prefers-contrast":             {"no-preference", "less", "more", "custom"},
	"prefers-reduced-transparency": {"no-preference", "reduce"},
}

// discreteFeature returns the value of a discrete media feature for m:
// the caller's, else the value a printed page has, else, when m is a
// screen, a screen's where the two differ. It is false for a feature
// litho does not evaluate, which no value matches.
//
//   - overflow-block is paged on a printer and scroll on a screen, and
//     overflow-inline none and scroll (Media Queries 4, 5.5 and 5.6).
//   - update is none for a document printed on paper and fast on a
//     computer screen (Media Queries 4, 5.4).
//   - color-gamut is srgb (Media Queries 4, 6.4): a converted page holds
//     sRGB colours.
//   - dynamic-range is standard, which matches on any visual device
//     (Media Queries 5, 6.5).
//   - display-mode is browser, which matches in any context that is not
//     an application (Media Queries 5, 4.9).
//   - forced-colors is none (Media Queries 5, 12.4): no palette is forced
//     on a converted document.
//   - prefers-color-scheme is light, the preferences are no-preference,
//     and hover and pointer are none: no user and no pointing device take
//     part in a conversion.
//   - scripting is none: litho runs no script.
func discreteFeature(name string, m Media) (string, bool) {
	if v, ok := m.Features[name]; ok {
		return lower(v), true
	}
	screen := lower(m.Type) == "screen"
	switch name {
	case "overflow-block":
		if screen {
			return "scroll", true
		}
		return "paged", true
	case "overflow-inline":
		if screen {
			return "scroll", true
		}
		return "none", true
	case "update":
		if screen {
			return "fast", true
		}
		return "none", true
	case "color-gamut":
		return "srgb", true
	case "dynamic-range":
		return "standard", true
	case "display-mode":
		return "browser", true
	case "prefers-color-scheme":
		return "light", true
	case "hover", "any-hover", "pointer", "any-pointer", "scripting", "forced-colors":
		return "none", true
	case "prefers-reduced-motion", "prefers-contrast", "prefers-reduced-transparency":
		return "no-preference", true
	}
	return "", false
}

// nestedFeatureValues are the discrete features whose values include the
// ones before them: a p3 device also matches srgb (Media Queries 4, 6.4),
// a high dynamic range device also standard (Media Queries 5, 6.5).
var nestedFeatureValues = map[string][]string{
	"color-gamut":   {"srgb", "p3", "rec2020"},
	"dynamic-range": {"standard", "high"},
}

// discreteMatches reports whether a query for the value want of a
// discrete feature matches the medium's value got.
func discreteMatches(name, want, got string) bool {
	if order, ok := nestedFeatureValues[name]; ok {
		w, g := slices.Index(order, want), slices.Index(order, got)
		return w >= 0 && g >= 0 && w <= g
	}
	return want == got
}

// discreteTrue evaluates a discrete feature of value v in a boolean
// context (Media Queries 4, 2.4.2): true unless the value is none, or
// no-preference, which the preference features define to evaluate as
// false there (Media Queries 5, 12.1 to 12.3).
func discreteTrue(name, v string) bool {
	switch name {
	case "prefers-reduced-motion", "prefers-contrast", "prefers-reduced-transparency":
		return v != "no-preference"
	}
	return v != "none"
}

// evalRange evaluates the range form of width and height (Media Queries
// 4, 2.4.3): "width >= 600px", "600px < width" and "400px <= width <=
// 800px".
func evalRange(in []token, m Media) tri {
	type op struct {
		lt, eq bool
		gt     bool
	}
	readOp := func(i int) (op, int, bool) {
		if i >= len(in) || in[i].typ != tDelim {
			return op{}, i, false
		}
		var o op
		switch in[i].delim {
		case '<':
			o.lt = true
		case '>':
			o.gt = true
		case '=':
			return op{eq: true}, i + 1, true
		default:
			return op{}, i, false
		}
		if i+1 < len(in) && in[i+1].isDelim('=') {
			o.eq = true
			return o, i + 2, true
		}
		return o, i + 1, true
	}
	test := func(a float64, o op, b float64) bool {
		switch {
		case o.lt:
			return a < b || (o.eq && a == b)
		case o.gt:
			return a > b || (o.eq && a == b)
		}
		return a == b
	}
	feature := func(t token) (float64, bool) {
		switch {
		case t.isIdent("width"):
			return m.Width / Pt, true
		case t.isIdent("height"):
			return m.Height / Pt, true
		}
		return 0, false
	}
	if f, ok := feature(in[0]); ok {
		o, j, ok := readOp(1)
		if !ok || j != len(in)-1 {
			return triUnknown
		}
		v, ok := mediaLength(in[j])
		if !ok {
			return triUnknown
		}
		return triOf(m.Width > 0 && m.Height > 0 && test(f, o, v))
	}
	a, ok := mediaLength(in[0])
	if !ok {
		return triUnknown
	}
	o1, j, ok := readOp(1)
	if !ok || j >= len(in) {
		return triUnknown
	}
	f, ok := feature(in[j])
	if !ok {
		return triUnknown
	}
	if j == len(in)-1 {
		return triOf(m.Width > 0 && m.Height > 0 && test(a, o1, f))
	}
	o2, k, ok := readOp(j + 1)
	if !ok || k != len(in)-1 {
		return triUnknown
	}
	b, ok := mediaLength(in[k])
	if !ok {
		return triUnknown
	}
	return triOf(m.Width > 0 && m.Height > 0 && test(a, o1, f) && test(f, o2, b))
}

// evalSupports evaluates an @supports condition (CSS Conditional 3, 6;
// CSS Conditional 4 and 5, 2): a declaration is supported when litho
// parses it as the style sheet's declarations parse, the quirky lengths
// and colours of a sheet of a document in quirks mode included when
// quirks is set (CSS Values 4, Appendix C), and the functions as
// [supportsFunction] says.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 applies the rules
//     of @supports (width: 10), (color: ff0000) and (margin: 10 20) in a
//     quirks mode document, and of @supports not (width: 10) in a
//     no-quirks one; its CSS.supports() is false for all three in both.
func evalSupports(l []token, ns *namespaces, quirks bool) bool {
	v, ok := evalCondition(significant(l), func(t token) (tri, bool) {
		v, ok := supportsLeaf(t, ns, quirks)
		return triOf(v), ok
	})
	return v == triTrue && ok
}

// supportsLeaf evaluates a <supports-in-parens> that is no nested
// condition: false with false when it does not parse.
func supportsLeaf(t token, ns *namespaces, quirks bool) (bool, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tLParen:
		it, ok := declaration(trimWS(t.children))
		if !ok {
			return false, true
		}
		if len(it.name) > 2 && it.name[:2] == "--" {
			return declarationValue(it.value), true
		}
		name := propertyName(lower(it.name))
		if !knownProperty(name) {
			return false, true
		}
		if containsVar(it.value) {
			return declarationValue(it.value), true
		}
		c := &declCtx{quiet: true, quirks: quirks}
		_, ok = c.expand(name, it.value)
		return ok, true
	case tFunction:
		return supportsFunction(t, ns), true
	}
	return false, false
}

// supportsFunction evaluates a supports function (CSS Conditional 4 and
// 5, 2), true when litho supports what it names; any other function is a
// <general-enclosed>, false.
//
//   - selector(<complex-selector>): one selector that parses, its
//     namespace prefixes declared by the style sheet's @namespace rules
//     (CSS Namespaces 3, 4), every
//     :is() and :where() in it holding only selectors that parse, since
//     their forgiving lists would drop the others unseen (CSS Conditional
//     4, 2: an unknown part is unsupported even there).
//   - font-format(<font-format>): a keyword whose files litho reads, as
//     an @font-face format() hint has them ([ReadsFontFormat]).
//   - font-tech(<font-tech>): one keyword litho supports, as an
//     @font-face tech() has them ([SupportsFontTech]).
//   - at-rule(<at-keyword-token>): an at-rule litho reads
//     ([readsAtRule]).
//
// # Corpus evidence
//
//   - probe, html: Chromium 156's CSS.supports is false for
//     selector(:is(:foo)), selector(:is(a, :foo)) and
//     selector(:where(:foo)), true for font-format() of collection,
//     opentype, truetype, woff and woff2 and false for embedded-opentype
//     and svg, true for at-rule() of @supports, @SUPPORTS, @top-left and
//     @stylistic and false for @charset, @foo, supports and "@supports;".
func supportsFunction(t token, ns *namespaces) bool {
	args := significant(t.children)
	switch lower(t.value) {
	case "selector":
		for _, a := range args {
			if a.typ == tComma {
				return false
			}
		}
		sels, err := parseSelectorList(trimWS(t.children), 0, false)
		return err == nil && strictForgivingLists(t.children) && resolveNamespaces(sels, ns, true)
	case "font-format":
		if len(args) != 1 || args[0].typ != tIdent {
			return false
		}
		f := lower(args[0].value)
		return slices.Contains(fontFormatKeywords, f) && ReadsFontFormat(f)
	case "font-tech":
		return len(args) == 1 && args[0].typ == tIdent && SupportsFontTech(lower(args[0].value))
	case "at-rule":
		return len(args) == 1 && args[0].typ == tAtKeyword && readsAtRule(lower(args[0].value))
	}
	return false
}

// strictForgivingLists reports whether every :is(), :where() and
// :-webkit-any() in a selector holds selectors that all parse.
func strictForgivingLists(l []token) bool {
	for i := range l {
		t := &l[i]
		if t.typ == tFunction && i > 0 && l[i-1].typ == tColon {
			switch lower(t.value) {
			case "is", "where", "-webkit-any":
				if _, err := parseSelectorList(trimWS(t.children), 0, false); err != nil {
					return false
				}
			}
		}
		if len(t.children) > 0 && !strictForgivingLists(t.children) {
			return false
		}
	}
	return true
}

// viewport returns the size the viewport units resolve against.
func (m Media) viewport() (float64, float64) {
	if m.ViewportWidth > 0 && m.ViewportHeight > 0 {
		return m.ViewportWidth, m.ViewportHeight
	}
	return m.Width, m.Height
}
