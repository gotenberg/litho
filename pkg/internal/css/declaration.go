package css

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// Warning is one construct of a stylesheet or a document the cascade did
// not apply as written.
type Warning struct {
	// Code names the kind of construct: [warn.CSSUnsupported],
	// [warn.CSSInvalid], [warn.ResourceFailed] for an @import that does not
	// load, or [warn.LimitReached] past a cap of the cascade.
	Code warn.Code
	// Effect says what the construct's fault cost the output: content
	// left out, or content printed differently.
	Effect warn.Effect
	// Message names the construct and says what was done instead.
	Message string
}

// warnings collects warnings, deduplicated and capped at maxWarnings.
type warnings struct {
	list    []Warning
	seen    map[Warning]bool
	dropped int
	// lost records that a dropped warning reported omitted content.
	lost bool
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
		w.lost = w.lost || x.Effect == warn.Omitted
		return
	}
	w.seen[x] = true
	w.list = append(w.list, x)
}

// result returns the warnings in the order they were first seen, with a
// last one counting the dropped ones. That one stands for them all: its
// effect is omitted content when any of them reported some.
func (w *warnings) result() []Warning {
	out := append([]Warning(nil), w.list...)
	if w.dropped > 0 {
		effect := warn.Approximated
		if w.lost {
			effect = warn.Omitted
		}
		out = append(out, Warning{Code: warn.LimitReached, Effect: effect, Message: fmt.Sprintf("%d more CSS warnings were dropped", w.dropped)})
	}
	return out
}

// cssWide is a CSS-wide keyword (CSS Cascade 4, 7.3). revert and
// revert-layer parse as unset; the cascade rolls an author declaration of
// revert back to the user origin's value (see [Declaration]), and behaves
// as unset otherwise: litho has no layers, and rolling back to the user
// agent's value is not supported.
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
	base string
	// revert marks a value of revert or revert-layer, whose entries hold
	// unset: in the author origin it takes the user origin's value for
	// each longhand that origin sets (CSS Cascade 4, 7.3.4).
	revert bool
	warns  []Warning
}

// declCtx carries what parsing a value needs.
type declCtx struct {
	baseURL string
	warns   []Warning
	// quiet drops warnings, for @supports evaluation.
	quiet bool
}

func (c *declCtx) warn(code warn.Code, effect warn.Effect, format string, args ...any) {
	if c.quiet {
		return
	}
	c.warns = append(c.warns, Warning{Code: code, Effect: effect, Message: fmt.Sprintf(format, args...)})
}

// silentProperties are properties with no effect on a static paged
// rendering, ignored without a warning. font-palette picks the palette of
// a colour font (CSS Fonts 4, 9.1), and litho draws no colour glyphs: an
// outline face draws them, in the text's colour, whatever the palette.
var silentProperties = map[string]bool{
	"cursor": true, "pointer-events": true, "user-select": true, "touch-action": true,
	"will-change": true, "resize": true, "scroll-behavior": true, "caret-color": true, "accent-color": true,
	"overscroll-behavior": true, "scrollbar-width": true, "scrollbar-color": true,
	"scrollbar-gutter": true, "print-color-adjust": true, "color-adjust": true,
	"text-rendering": true, "font-smooth": true, "speak": true, "zoom": true,
	"font-palette": true,
	// Chromium applies these to SVG text alone (baselineProps).
	"alignment-baseline": true, "dominant-baseline": true, "baseline-shift": true,
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

// containsSubstitution reports whether a value calls attr() or env()
// anywhere.
func containsSubstitution(l []token) bool {
	for i := range l {
		t := &l[i]
		if t.typ == tFunction && (eqFold(t.value, "attr") || eqFold(t.value, "env")) {
			return true
		}
		if len(t.children) > 0 && containsSubstitution(t.children) {
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
	written := lower(name)
	name = propertyName(written)
	d := Declaration{Property: name, Important: important}
	if !knownProperty(name) {
		// An unknown vendor-prefixed name is ignored without a warning; a
		// legacy name alias warns as the property it names.
		if (!strings.HasPrefix(written, "-") || written != name) && !silentProperties[name] {
			c.warn(warn.CSSUnsupported, warn.Approximated, "property %q is not supported and was ignored", written)
		}
		return d
	}
	if containsVar(value) {
		d.pending, d.base = value, c.baseURL
		return d
	}
	entries, ok := c.expand(name, value)
	if !ok && containsSubstitution(value) {
		// attr() and env() substitute at computed-value time (CSS Values
		// 5, 7.7; CSS Environment Variables 1, 3). content reads an
		// untyped attr() itself, and only waits when it cannot.
		d.pending, d.base = value, c.baseURL
		return d
	}
	if v := significant(value); len(v) == 1 && v[0].typ == tIdent {
		k := lower(v[0].value)
		d.revert = k == "revert" || k == "revert-layer"
	}
	if !ok {
		if mixedCalc(value) {
			c.warn(warn.CSSInvalid, warn.Approximated, "%q: a calc() mixing lengths and percentages cannot be computed here and was ignored", name)
		} else {
			c.warn(warn.CSSInvalid, warn.Approximated, "invalid value for %q was ignored", name)
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
			if l, ok := calcLength(t); ok && l.mixed() {
				return true
			}
		}
		if len(t.children) > 0 && mixedCalc(t.children) {
			return true
		}
	}
	return false
}
