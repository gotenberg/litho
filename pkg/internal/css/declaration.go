package css

import (
	"fmt"
	"strings"
)

// Warning is one construct of a stylesheet or a document the cascade did
// not apply as written.
type Warning struct {
	// Code is a stable kebab-case identifier: unsupported-css-property,
	// unsupported-css-value, invalid-css-value, unsupported-css-selector,
	// unsupported-css-at-rule, css-import-failed or css-limit.
	Code string
	// Message names the construct and says what was done instead.
	Message string
}

// warnings collects warnings, deduplicated and capped at maxWarnings.
type warnings struct {
	list    []Warning
	seen    map[Warning]bool
	dropped int
}

func (w *warnings) add(x Warning) {
	if w.seen == nil {
		w.seen = map[Warning]bool{}
	}
	if w.seen[x] {
		return
	}
	if len(w.list) >= maxWarnings {
		w.dropped++
		return
	}
	w.seen[x] = true
	w.list = append(w.list, x)
}

// result returns the warnings in the order they were first seen, with a
// last one counting the dropped ones.
func (w *warnings) result() []Warning {
	out := append([]Warning(nil), w.list...)
	if w.dropped > 0 {
		out = append(out, Warning{Code: "css-limit", Message: fmt.Sprintf("%d more CSS warnings were dropped", w.dropped)})
	}
	return out
}

// cssWide is a CSS-wide keyword (CSS Cascade 4, 7.3). revert and
// revert-layer behave as unset: litho has no user origin and no layers.
type cssWide uint8

const (
	wideInherit cssWide = iota + 1
	wideInitial
	wideUnset
)

// entry is a longhand and its specified value.
type entry struct {
	id propID
	v  any
}

// Declaration is one parsed declaration: a property, its value and its
// importance. The zero value declares nothing.
type Declaration struct {
	// Property is the property name as the cascade knows it: lowercased,
	// except a custom property's, which is case-sensitive.
	Property string
	// Important is the !important flag (CSS Cascade 4, 6.4).
	Important bool

	entries []entry
	// custom is a custom property's value; isCustom tells an empty one
	// from none.
	custom   []token
	isCustom bool
	// pending is a value holding var(), substituted and parsed at
	// computed-value time (CSS Variables 1, 3).
	pending []token
	// base is the URL a pending value's url() resolves against.
	base  string
	warns []Warning
}

// declCtx carries what parsing a value needs.
type declCtx struct {
	baseURL string
	warns   []Warning
	// quiet drops warnings, for @supports evaluation.
	quiet bool
}

func (c *declCtx) warn(code, format string, args ...any) {
	if c.quiet {
		return
	}
	c.warns = append(c.warns, Warning{Code: code, Message: fmt.Sprintf(format, args...)})
}

// silentProperties are properties with no effect on a static paged
// rendering, ignored without a warning.
var silentProperties = map[string]bool{
	"cursor": true, "pointer-events": true, "user-select": true, "touch-action": true,
	"transition": true, "transition-property": true, "transition-duration": true,
	"transition-timing-function": true, "transition-delay": true, "will-change": true,
	"resize": true, "scroll-behavior": true, "caret-color": true, "accent-color": true,
	"overscroll-behavior": true, "scrollbar-width": true, "scrollbar-color": true,
	"scrollbar-gutter": true, "print-color-adjust": true, "color-adjust": true,
	"text-rendering": true, "font-smooth": true, "speak": true, "zoom": true,
}

// knownProperty reports whether litho parses the property name.
func knownProperty(name string) bool {
	if _, ok := longhandByName[name]; ok {
		return true
	}
	_, ok := shorthands[name]
	return ok
}

// containsVar reports whether a value calls var() anywhere.
func containsVar(l []token) bool {
	for i := range l {
		t := &l[i]
		if t.typ == tFunction && eqFold(t.value, "var") {
			return true
		}
		if len(t.children) > 0 && containsVar(t.children) {
			return true
		}
	}
	return false
}

// declare turns a parsed declaration into a [Declaration]: a custom
// property keeps its tokens, a value with var() waits for computed-value
// time, anything else is parsed and expanded into longhands now. An
// unknown property or an invalid value yields a Declaration that declares
// nothing (CSS Syntax 3, 5.4.6 and CSS 2.1, 4.2).
func (c *declCtx) declare(name string, value []token, important bool) Declaration {
	if strings.HasPrefix(name, "--") {
		return Declaration{Property: name, Important: important, custom: trimWS(value), isCustom: true}
	}
	name = lower(name)
	d := Declaration{Property: name, Important: important}
	if !knownProperty(name) {
		if !strings.HasPrefix(name, "-") && !silentProperties[name] {
			c.warn("unsupported-css-property", "property %q is not supported and was ignored", name)
		}
		return d
	}
	if containsVar(value) {
		d.pending, d.base = value, c.baseURL
		return d
	}
	entries, ok := c.expand(name, value)
	if !ok {
		if mixedCalc(value) {
			c.warn("invalid-css-value", "%q: a calc() mixing lengths and percentages cannot be computed here and was ignored", name)
		} else {
			c.warn("invalid-css-value", "invalid value for %q was ignored", name)
		}
		return d
	}
	d.entries = entries
	return d
}

// wideKeyword recognizes a CSS-wide keyword value.
func wideKeyword(v []token) (cssWide, bool) {
	if len(v) != 1 || v[0].typ != tIdent {
		return 0, false
	}
	switch lower(v[0].value) {
	case "inherit":
		return wideInherit, true
	case "initial":
		return wideInitial, true
	case "unset", "revert", "revert-layer":
		return wideUnset, true
	}
	return 0, false
}

// expand parses a value without var() for the property name, returning
// an entry per longhand it sets.
func (c *declCtx) expand(name string, value []token) ([]entry, bool) {
	v := significant(value)
	if len(v) == 0 {
		return nil, false
	}
	if w, ok := wideKeyword(v); ok {
		ids := longhandsOf(name)
		out := make([]entry, len(ids))
		for i, id := range ids {
			out[i] = entry{id: id, v: w}
		}
		return out, true
	}
	for _, t := range v {
		if t.typ == tBadString || t.typ == tBadURL || t.typ == tRParen || t.typ == tRBracket || t.typ == tRBrace {
			return nil, false
		}
	}
	if id, ok := longhandByName[name]; ok {
		pv, ok := props[id].parse(v, c)
		if !ok {
			return nil, false
		}
		return []entry{{id: id, v: pv}}, true
	}
	sh, ok := shorthands[name]
	if !ok {
		return nil, false
	}
	return sh.expand(v, c)
}

// longhandsOf returns the longhands a property sets.
func longhandsOf(name string) []propID {
	if id, ok := longhandByName[name]; ok {
		return []propID{id}
	}
	return shorthands[name].longhands
}

// mixedCalc reports whether a value holds a calc() that mixes lengths and
// percentages, which [Style] cannot hold for most properties (CSS Values
// 4, 10.8).
func mixedCalc(l []token) bool {
	for i := range l {
		t := &l[i]
		if isCalc(t) {
			if v, ok := calcOf(t, 0); ok && !v.isNum && v.l.mixed() {
				return true
			}
		}
		if len(t.children) > 0 && mixedCalc(t.children) {
			return true
		}
	}
	return false
}
