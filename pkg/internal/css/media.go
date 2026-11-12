package css

import "math"

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
	// from the map keeps its default: light, no-preference and none, what
	// a printer and a headless browser report. Keys are lowercase; values
	// are compared case-insensitively.
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
		if v, ok := evalMediaQuery(significant(q), m); ok && v {
			return true
		}
	}
	return false
}

// evalMediaQuery evaluates one <media-query>: [not | only]? <media-type>
// [and <media-condition-without-or>]? or a <media-condition>.
func evalMediaQuery(q []token, m Media) (bool, bool) {
	if len(q) == 0 {
		return false, false
	}
	if q[0].typ != tIdent || (q[0].isIdent("not") && len(q) > 1 && q[1].typ != tIdent) {
		return evalCondition(q, func(t token) (bool, bool) { return evalFeature(t, m) })
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
		return false, false
	}
	typ := lower(q[i].value)
	switch typ {
	case "and", "or", "not", "only", "layer":
		return false, false
	}
	want := m.Type
	if want == "" {
		want = "print"
	}
	v := typ == "all" || typ == lower(want)
	i++
	if i < len(q) {
		if !q[i].isIdent("and") || i+1 >= len(q) {
			return false, false
		}
		rest := q[i+1:]
		for _, t := range rest {
			if t.isIdent("or") {
				return false, false
			}
		}
		c, ok := evalCondition(rest, func(t token) (bool, bool) { return evalFeature(t, m) })
		if !ok {
			return false, false
		}
		v = v && c
	}
	if neg {
		v = !v
	}
	return v, true
}

// evalCondition evaluates a condition of the shape Media Queries 4, 3 and
// CSS Conditional 3, 6 share: not <in>, <in> [and <in>]*, <in> [or <in>]*,
// where <in> is a parenthesised condition or what leaf evaluates. An
// unknown <in> (general-enclosed) is false.
func evalCondition(l []token, leaf func(token) (bool, bool)) (bool, bool) {
	if len(l) == 0 {
		return false, false
	}
	in := func(t token) (bool, bool) {
		if t.typ == tLParen {
			inner := significant(t.children)
			if len(inner) > 0 && (inner[0].typ == tLParen || inner[0].isIdent("not")) {
				return evalCondition(inner, leaf)
			}
		}
		return leaf(t)
	}
	if l[0].isIdent("not") {
		if len(l) != 2 {
			return false, false
		}
		v, ok := in(l[1])
		return !v && ok, ok
	}
	v, ok := in(l[0])
	if !ok {
		return false, false
	}
	if len(l) == 1 {
		return v, true
	}
	op := ""
	for i := 1; i < len(l); i += 2 {
		if l[i].typ != tIdent || i+1 >= len(l) {
			return false, false
		}
		o := lower(l[i].value)
		if (o != "and" && o != "or") || (op != "" && o != op) {
			return false, false
		}
		op = o
		w, ok := in(l[i+1])
		if !ok {
			return false, false
		}
		if op == "and" {
			v = v && w
		} else {
			v = v || w
		}
	}
	return v, true
}

// mediaLength evaluates a length in a media feature, in CSS pixels; em
// and rem are the initial font size, 16px (Media Queries 4, 1.3).
func mediaLength(t token) (float64, bool) {
	l, ok := parseLength(t, 0)
	if !ok || l.vw != 0 || l.vh != 0 || l.vmin != 0 || l.vmax != 0 {
		return 0, false
	}
	return (l.pt + (l.em+(l.ex+l.ch)/2+l.rem)*mediumFontSize) / Pt, true
}

// mediaRatio evaluates a <ratio>: a number, or two separated by a slash.
func mediaRatio(l []token) (float64, bool) {
	switch {
	case len(l) == 1 && l[0].typ == tNumber:
		return l[0].num, finite(l[0].num)
	case len(l) == 3 && l[0].typ == tNumber && l[1].isDelim('/') && l[2].typ == tNumber && l[2].num != 0:
		return l[0].num / l[2].num, finite(l[0].num / l[2].num)
	}
	return 0, false
}

// evalFeature evaluates a <media-in-parens> holding a feature (Media
// Queries 4, 4): width, height, aspect-ratio and orientation against the
// page box, a handful of discrete features with the values of a static
// printed page, and the range syntax for width and height. Any other
// feature is false.
func evalFeature(t token, m Media) (bool, bool) {
	if t.typ != tLParen {
		return false, t.typ == tFunction
	}
	in := significant(t.children)
	if len(in) == 0 || in[0].typ != tIdent {
		if len(in) >= 3 {
			return evalRange(in, m)
		}
		return false, true
	}
	name := lower(in[0].value)
	if len(in) == 1 {
		switch name {
		case "width", "height", "device-width", "device-height":
			return m.Width > 0 && m.Height > 0, true
		case "color", "orientation", "aspect-ratio":
			return true, true
		}
		return false, true
	}
	if len(in) >= 3 && in[1].typ == tDelim {
		return evalRange(in, m)
	}
	if in[1].typ != tColon || len(in) < 3 {
		return false, true
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
				return false, true
			}
			px, ok := mediaLength(val[0])
			if !ok || m.Width <= 0 || m.Height <= 0 {
				return false, true
			}
			got := w
			if base == "height" || base == "device-height" {
				got = h
			}
			return cmp(prefix, got, px), true
		case "aspect-ratio", "device-aspect-ratio":
			r, ok := mediaRatio(val)
			if !ok || h <= 0 {
				return false, true
			}
			return cmp(prefix, w/h, r), true
		}
	}
	if len(val) != 1 || val[0].typ != tIdent && val[0].typ != tNumber {
		return false, true
	}
	v := lower(val[0].value)
	if want, ok := m.Features[name]; ok {
		return v == lower(want), true
	}
	switch name {
	case "orientation":
		if h >= w {
			return v == "portrait", true
		}
		return v == "landscape", true
	case "prefers-color-scheme":
		return v == "light", true
	case "hover", "any-hover", "pointer", "any-pointer", "update", "scripting":
		return v == "none", true
	case "prefers-reduced-motion", "prefers-contrast", "prefers-reduced-transparency":
		return v == "no-preference", true
	case "color":
		return val[0].typ == tNumber && val[0].num <= 8, true
	case "monochrome", "grid":
		return val[0].typ == tNumber && val[0].num == 0, true
	}
	return false, true
}

// evalRange evaluates the range form of width and height (Media Queries
// 4, 2.4.3): "width >= 600px", "600px < width" and "400px <= width <=
// 800px".
func evalRange(in []token, m Media) (bool, bool) {
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
	if m.Width <= 0 || m.Height <= 0 {
		return false, true
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
			return false, true
		}
		v, ok := mediaLength(in[j])
		return ok && test(f, o, v), true
	}
	a, ok := mediaLength(in[0])
	if !ok {
		return false, true
	}
	o1, j, ok := readOp(1)
	if !ok || j >= len(in) {
		return false, true
	}
	f, ok := feature(in[j])
	if !ok {
		return false, true
	}
	if j == len(in)-1 {
		return test(a, o1, f), true
	}
	o2, k, ok := readOp(j + 1)
	if !ok || k != len(in)-1 {
		return false, true
	}
	b, ok := mediaLength(in[k])
	return ok && test(a, o1, f) && test(f, o2, b), true
}

// evalSupports evaluates an @supports condition (CSS Conditional 3, 6):
// a declaration is supported when litho parses it, selector() when the
// selector parses.
func evalSupports(l []token) bool {
	v, ok := evalCondition(significant(l), func(t token) (bool, bool) {
		switch t.typ { //nolint:exhaustive // the other token types are invalid here
		case tLParen:
			it, ok := declaration(trimWS(t.children))
			if !ok {
				return false, true
			}
			if len(it.name) > 2 && it.name[:2] == "--" {
				return true, true
			}
			name := lower(it.name)
			if !knownProperty(name) {
				return false, true
			}
			if containsVar(it.value) {
				return true, true
			}
			c := &declCtx{quiet: true}
			_, ok = c.expand(name, it.value)
			return ok, true
		case tFunction:
			if eqFold(t.value, "selector") {
				_, err := parseSelectorList(trimWS(t.children), 0, false)
				return err == nil, true
			}
			return false, true
		}
		return false, false
	})
	return v && ok
}

// viewport returns the size the viewport units resolve against.
func (m Media) viewport() (float64, float64) {
	if m.ViewportWidth > 0 && m.ViewportHeight > 0 {
		return m.ViewportWidth, m.ViewportHeight
	}
	return m.Width, m.Height
}
