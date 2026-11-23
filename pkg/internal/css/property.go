package css

import (
	"maps"
	"strconv"
	"strings"
)

// maxSyntaxComponents caps the components of one @property syntax string:
// 32. Anti-DoS guard, not spec-mandated: CSS Properties and Values API 1,
// 5.1 sets no bound, and a registered property's syntax names one to a
// handful of types.
const maxSyntaxComponents = 32

// propertyRule is an @property rule: the registration of a custom
// property (CSS Properties and Values API 1, 3). One in a shadow tree
// registers nothing, in the tree or outside it, as Chromium reads it.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints var(--s, fallback) with its
//     fallback in a shadow tree whose style element registers --s with
//     an initial value, and outside the tree.
type propertyRule struct {
	name string
	reg  *registration
	cond *mediaCond
	// layer is the rule's cascade layer index in its sheet.
	layer int
}

// registration is a registered custom property (CSS Properties and Values
// API 1, 2): the values its syntax accepts, whether it inherits, and its
// initial value.
type registration struct {
	// universal is the syntax "*", which accepts any value.
	universal bool
	comps     []syntaxComponent
	inherits  bool
	// initial is the initial value, nil when the rule gives none, which
	// the universal syntax alone allows: the property is then
	// guaranteed-invalid until declared.
	initial []token
}

// syntaxComponent is one <syntax-component> of a syntax string (CSS
// Properties and Values API 1, 5.1): a data type name or a keyword, and
// its multiplier: 0 for none, '+' for a space-separated list, '#' for a
// comma-separated one.
type syntaxComponent struct {
	name  string
	ident bool
	mult  byte
}

// registrableTypes are the data type names a syntax string may name (CSS
// Properties and Values API 1, 5.1).
var registrableTypes = map[string]bool{
	"length": true, "number": true, "percentage": true, "length-percentage": true, "color": true,
	"image": true, "url": true, "integer": true, "angle": true, "time": true, "resolution": true,
	"transform-function": true, "custom-ident": true, "transform-list": true, "string": true,
}

// parsePropertyRule parses an @property rule (CSS Properties and Values
// API 1, 3): a custom property name, then the syntax and inherits
// descriptors, which the rule needs, and initial-value, which it needs
// unless the syntax is universal and which must then parse by the syntax
// and be computationally independent. Any other rule is invalid and
// registers nothing.
func parsePropertyRule(prelude, block []token) (*propertyRule, bool) {
	pre := significant(prelude)
	if len(pre) != 1 || pre[0].typ != tIdent || !strings.HasPrefix(pre[0].value, "--") || len(pre[0].value) < 3 {
		return nil, false
	}
	var reg registration
	var syntaxSet, inheritsSet, initialSet bool
	var initial []token
	valid := true
	newListParser(block).declarations(func(d item) bool {
		if d.kind != itemDeclaration {
			return true
		}
		v := significant(d.value)
		switch lower(d.name) {
		case "syntax":
			if len(v) != 1 || v[0].typ != tString {
				valid = false
				return true
			}
			comps, universal, ok := parseSyntaxString(v[0].value)
			if !ok {
				valid = false
				return true
			}
			reg.comps, reg.universal, syntaxSet = comps, universal, true
		case "inherits":
			k, ok := keyword(v)
			if !ok || k != "true" && k != "false" {
				valid = false
				return true
			}
			reg.inherits, inheritsSet = k == "true", true
		case "initial-value":
			initial, initialSet = trimWS(d.value), true
		}
		return true
	})
	if !valid || !syntaxSet || !inheritsSet {
		return nil, false
	}
	switch {
	case reg.universal:
		if initialSet && declarationValue(initial) && !containsVar(initial) && !containsSubstitution(initial) {
			reg.initial = initial
		} else if initialSet {
			return nil, false
		}
	case !initialSet || !reg.matches(significant(initial)) || !computationallyIndependent(initial):
		return nil, false
	default:
		reg.initial = initial
	}
	return &propertyRule{name: pre[0].value, reg: &reg}, true
}

// parseSyntaxString parses a syntax string (CSS Properties and Values API
// 1, 5.1): "*", or one or more syntax components separated by "|", each a
// data type name in angle brackets or a keyword, followed by "+" or "#"
// save for <transform-list>.
func parseSyntaxString(s string) ([]syntaxComponent, bool, bool) {
	s = strings.Trim(s, " \t\n\f\r")
	if s == "*" {
		return nil, true, true
	}
	if strings.Count(s, "|") >= maxSyntaxComponents {
		return nil, false, false
	}
	parts := strings.Split(s, "|")
	comps := make([]syntaxComponent, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, " \t\n\f\r")
		var c syntaxComponent
		if n := len(p); n > 0 && (p[n-1] == '+' || p[n-1] == '#') {
			c.mult, p = p[n-1], p[:n-1]
		}
		switch {
		case len(p) > 2 && p[0] == '<' && p[len(p)-1] == '>':
			c.name = p[1 : len(p)-1]
			if !registrableTypes[c.name] || c.name == "transform-list" && c.mult != 0 {
				return nil, false, false
			}
		case syntaxIdent(p):
			c.name, c.ident = p, true
		default:
			return nil, false, false
		}
		comps = append(comps, c)
	}
	return comps, false, true
}

// syntaxIdent reports a keyword of a syntax string: an identifier of
// letters, digits, hyphens and underscores, not starting with a digit,
// that is no CSS-wide keyword nor default (CSS Properties and Values API
// 1, 5.1).
func syntaxIdent(s string) bool {
	if s == "" || s[0] >= '0' && s[0] <= '9' || !customIdent(s) {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if c != '-' && c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c < 0x80 {
			return false
		}
	}
	return true
}

// matches reports whether the significant tokens v are a value of the
// registration's syntax (CSS Properties and Values API 1, 5.1), with
// every var(), attr() and env() already substituted.
func (r *registration) matches(v []token) bool {
	if r.universal {
		return true
	}
	if len(v) == 0 {
		return false
	}
	for _, c := range r.comps {
		if c.matches(v) {
			return true
		}
	}
	return false
}

// matches reports whether v is a value of the component c.
func (c syntaxComponent) matches(v []token) bool {
	switch c.mult {
	case '+':
		for _, t := range v {
			if !c.single(t) {
				return false
			}
		}
		return true
	case '#':
		for _, part := range splitCommas(v) {
			p := significant(part)
			if len(p) != 1 || !c.single(p[0]) {
				return false
			}
		}
		return true
	}
	if c.name == "transform-list" {
		_, ok := parseTransform(v, nil)
		return ok && !singleKeyword(v, "none")
	}
	return len(v) == 1 && c.single(v[0])
}

// singleKeyword reports whether v is the keyword k alone.
func singleKeyword(v []token, k string) bool {
	kw, ok := keyword(v)
	return ok && kw == k
}

// single reports whether the component value t is one value of the data
// type or keyword c names.
func (c syntaxComponent) single(t token) bool {
	if c.ident {
		return t.typ == tIdent && t.value == c.name
	}
	switch c.name {
	case "time":
		_, ok := parseTime(t)
		return ok
	case "resolution":
		_, ok := resolutionOf(t)
		return ok
	case "image":
		_, ok := bgImageValue(t, &declCtx{quiet: true}, "")
		return ok && t.typ != tIdent
	case "url":
		_, ok := parseURL(t, "")
		return ok
	case "transform-function":
		_, ok := parseTransform([]token{t}, nil)
		return ok && t.typ == tFunction
	}
	return matchesType(t, c.name)
}

// computationallyIndependent reports whether an initial value computes
// without the element it applies to (CSS Properties and Values API 1,
// 3.3): no length in a unit relative to a font, a line or the viewport,
// and no var(), attr() or env().
func computationallyIndependent(l []token) bool {
	for i := range l {
		t := &l[i]
		if t.typ == tDimension {
			if _, relative := lengthUnits[lower(t.value)]; relative {
				return false
			}
			if u := lower(t.value); len(u) > 2 && u[:2] == "cq" {
				return false
			}
		}
		if t.typ == tFunction && (eqFold(t.value, "var") || eqFold(t.value, "attr") || eqFold(t.value, "env")) {
			return false
		}
		if len(t.children) > 0 && !computationallyIndependent(t.children) {
			return false
		}
	}
	return true
}

// absolutize returns the custom properties vars of an element whose
// declared names hold registered lengths with each such length computed
// to pixels on the element (CSS Properties and Values API 1, 2.4: a
// registered <length> computes to an absolute length, the length part of
// a <length-percentage> too), so a child inherits the length the
// element's own font and line sizes give it. The percentage of a
// <length-percentage> stays, and so does every other type: a <color> of
// currentcolor computes to itself.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 computes a registered <length> of 2em on
//     a 10px element to 20px, which its 20px child takes, a <length>+ of
//     1em 2em to 10px 20px, a <length-percentage> of calc(1em + 10%) to
//     calc(10% + 10px), and keeps a <color> of currentcolor, which the
//     child resolves to its own colour.
func (cs *computeState) absolutize(vars map[string][]token, names []string) map[string][]token {
	out, cloned := vars, false
	for _, name := range names {
		reg := cs.r.registered[name]
		if reg == nil || !reg.takesLength() {
			continue
		}
		v, ok := out[name]
		if !ok {
			continue
		}
		abs, changed := absoluteLengths(v, cs.sty.lc)
		if !changed {
			continue
		}
		if !cloned {
			out, cloned = maps.Clone(vars), true
		}
		out[name] = abs
	}
	return out
}

// takesLength reports whether a registration's syntax accepts a <length>
// or a <length-percentage>.
func (r *registration) takesLength() bool {
	for _, c := range r.comps {
		if !c.ident && (c.name == "length" || c.name == "length-percentage") {
			return true
		}
	}
	return false
}

// absoluteLengths returns v with each top-level length or calculation of
// lengths in pixels on the length context lc, a calc() mixing lengths and
// a percentage as calc() of the percentage and the pixels; it reports
// whether a token changed. Numbers, idents and calculations that are not
// linear stay.
func absoluteLengths(v []token, lc lenCtx) ([]token, bool) {
	var out []token
	for i, t := range v {
		if t.typ != tDimension && t.typ != tFunction {
			continue
		}
		l, ok := parseLength(t, allowPct|allowMixed)
		if !ok || l.expr != nil && l.hasPct {
			continue
		}
		var abs token
		switch {
		case !l.hasPct:
			abs = pxToken(l.points(lc) / Pt)
		case l.hasLen:
			pct := token{typ: tPercentage, num: l.pct, repr: strconv.FormatFloat(l.pct, 'f', -1, 64)}
			abs = token{typ: tFunction, value: "calc", children: []token{pct, {typ: tWhitespace}, {typ: tDelim, delim: '+'}, {typ: tWhitespace}, pxToken(l.points(lc) / Pt)}}
		default:
			continue
		}
		if out == nil {
			out = append([]token(nil), v...)
		}
		out[i] = abs
	}
	if out == nil {
		return v, false
	}
	return out, true
}

// pxToken is the dimension token of px pixels.
func pxToken(px float64) token {
	return token{typ: tDimension, num: px, repr: strconv.FormatFloat(px, 'f', -1, 64), value: "px"}
}
