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
// revert-layer parse as unset; the cascade rolls a declaration of revert
// back to the value the origins below its own give, and a declaration of
// revert-layer back to the layers below its own (see [Declaration]).
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
	// quirks reports a pending value of a sheet parsed in quirks mode,
	// parsed so once substituted.
	quirks bool
	// revert marks a value of revert, whose entries hold unset: it takes
	// the value the origins below its own give each longhand, the user
	// and user agent origins' below the author origin, the user agent
	// origin's below the user origin (CSS Cascade 4, 7.3.4).
	revert bool
	// revertLayer marks a value of revert-layer, whose entries hold unset:
	// it takes the value the layers below its own give each longhand
	// (CSS Cascade 5, 7.3.5).
	revertLayer bool
	// containerUnits marks a value holding a container query unit, which
	// resolves against the element's containers (CSS Conditional 5, 7).
	containerUnits bool
	warns          []Warning
}

// declCtx carries what parsing a value needs.
type declCtx struct {
	baseURL string
	warns   []Warning
	// quiet drops warnings, for @supports evaluation.
	quiet bool
	// userAgent reports a declaration of the user agent sheet, which alone
	// may use the internal values the sheet is written with
	// (-internal-th-center): an author or user sheet that writes one
	// writes an invalid value, as Chromium parses its own -internal-
	// values in its user agent sheet alone.
	userAgent bool
	// quirks parses the quirky lengths and colours of a document in quirks
	// mode ([quirkyValue]).
	quirks bool
}

func (c *declCtx) warn(code warn.Code, effect warn.Effect, format string, args ...any) {
	if c.quiet {
		return
	}
	c.warns = append(c.warns, Warning{Code: code, Effect: effect, Message: fmt.Sprintf(format, args...)})
}

// silentProperties are properties with no effect on a static paged
// rendering, ignored without a warning.
//
// # Corpus evidence
//
//   - probe, html: Chromium writes one image XObject, with no
//     /Interpolate, for a 4px PNG scaled to 80px under image-rendering
//     auto, pixelated and crisp-edges.
//   - probe, html: Chromium prints a JPEG of EXIF orientation 6 turned a
//     quarter under image-orientation: none as under from-image.
//   - probe, html: Chromium 156 writes the same PDF content for an
//     inline svg drawing a filled and a stroked circle, a line, a
//     gradient, a translucent group, a blurred rectangle, text and an
//     image whether every element states color-interpolation: linearRGB,
//     color-rendering: optimizeSpeed, buffered-rendering: static or
//     none of them.
//   - probe, html: Chromium 156 prints a mandatory snap container scrolled
//     to its snap position, moved by scroll-padding and scroll-margin, and
//     a scroll container scrolled to its scroll-initial-target:
//     scroll-snap-type, scroll-snap-align and scroll-initial-target warn,
//     scroll-padding and scroll-margin, which only move that position,
//     stay silent.
//   - probe, html: Chromium 156 prints an animation whose animation-trigger
//     names a trigger its element never reaches at its start state, as
//     without the trigger; a paragraph of text-size-adjust: 300% or
//     -webkit-text-size-adjust: 300% at its own font size; a box of
//     view-transition-class, -group or -scope, interactivity: inert,
//     overlay, scroll-target-group or dynamic-range-limit without a
//     stacking context, where view-transition-name makes one.
var silentProperties = map[string]bool{
	"cursor": true, "pointer-events": true, "user-select": true, "touch-action": true,
	"scroll-behavior": true, "caret-color": true,
	"overscroll-behavior": true, "scrollbar-width": true, "scrollbar-color": true,
	"scrollbar-gutter": true, "color-adjust": true,
	"text-rendering": true, "font-smooth": true, "speak": true,
	// Chromium applies these to SVG text alone (baselineProps).
	"alignment-baseline": true, "dominant-baseline": true, "baseline-shift": true,
	// image-rendering picks a scaling filter (CSS Images 3, 5.4) and a PDF
	// image holds only /Interpolate, which Chromium writes for no value;
	// image-orientation: none (5.3) Chromium ignores for a picture another
	// origin serves, as every file of a conversion is, and applies the
	// EXIF orientation.
	"image-rendering": true, "image-orientation": true,
	// interpolate-size decides whether a sizing keyword interpolates
	// with a length (CSS Sizing 4, 7.3): an animation prints at its start
	// or its end, where no keyword and length are ever between.
	"interpolate-size": true,
	// forced-color-adjust opts an element out of forced colors mode (CSS
	// Color Adjustment 1, 3.2), which a printed page is never in: its
	// media feature forced-colors is none (Media Queries 5, 12.4).
	"forced-color-adjust": true,
	// overflow-anchor selects the node scroll anchoring keeps in view
	// while content above it changes size (CSS Scroll Anchoring 1, 3): a
	// printed page does not scroll.
	"overflow-anchor": true,
	// window-drag marks the area that moves the application window, and
	// app-region is its legacy name (CSS UI 4, 8.1): a printed page has
	// no window.
	"window-drag": true, "app-region": true,
	// color-interpolation names the colour space gradients, animations
	// and compositing of an SVG element work in (SVG 2, 13.9); Chromium
	// parses it and works in sRGB whatever it says, in print as on
	// screen. color-rendering (SVG 1.1, 11.7.2) and buffered-rendering
	// (SVG Tiny 1.2, 11.10.5) are hints a renderer may ignore.
	"color-interpolation": true, "color-rendering": true, "buffered-rendering": true,
	// scroll-margin and scroll-padding move the snap positions of a snap
	// container and the target of a scroll into view (CSS Scroll Snap 1,
	// 4.1 and 4.2): a printed page scrolls to no target, and the initial
	// snap position they refine warns through scroll-snap-type and
	// scroll-snap-align. scroll-snap-stop only bounds how far one scroll
	// gesture travels (6.2).
	"scroll-margin": true, "scroll-margin-block": true, "scroll-margin-block-start": true,
	"scroll-margin-block-end": true, "scroll-margin-inline": true, "scroll-margin-inline-start": true,
	"scroll-margin-inline-end": true, "scroll-margin-top": true, "scroll-margin-right": true,
	"scroll-margin-bottom": true, "scroll-margin-left": true,
	"scroll-padding": true, "scroll-padding-block": true, "scroll-padding-block-start": true,
	"scroll-padding-block-end": true, "scroll-padding-inline": true, "scroll-padding-inline-start": true,
	"scroll-padding-inline-end": true, "scroll-padding-top": true, "scroll-padding-right": true,
	"scroll-padding-bottom": true, "scroll-padding-left": true,
	"scroll-snap-stop": true,
	// The longhands of overscroll-behavior: what a scroll past the edge
	// of a scroll container chains to (CSS Overscroll Behavior 1, 3).
	"overscroll-behavior-x": true, "overscroll-behavior-y": true,
	"overscroll-behavior-block": true, "overscroll-behavior-inline": true,
	// Triggers start an animation when a scroll brings an element into a
	// range (CSS Animations 2, triggers): an animation prints at its start
	// whether or not a trigger waits for it.
	"timeline-trigger": true, "timeline-trigger-name": true, "timeline-trigger-source": true,
	"timeline-trigger-activation-range": true, "timeline-trigger-activation-range-start": true,
	"timeline-trigger-activation-range-end": true, "timeline-trigger-active-range": true,
	"timeline-trigger-active-range-start": true, "timeline-trigger-active-range-end": true,
	"animation-trigger": true, "trigger-scope": true,
	// The class, group and scope of a view transition (CSS View
	// Transitions 2) only shape a transition between two states of a
	// document; view-transition-name, which makes a stacking context,
	// warns.
	"view-transition-class": true, "view-transition-group": true, "view-transition-scope": true,
	// interest-delay times the hover and focus that show an interest
	// target (HTML interest invokers); caret-shape and caret-animation
	// draw the text insertion caret (CSS UI 4, 6.1); interactivity: inert
	// takes an element out of focus and hit testing (CSS UI 4, 7);
	// overlay only keeps an element in the top layer during a transition
	// (CSS Position 4, 3.2); scroll-target-group and scroll-axis-lock act
	// on scrolling (CSS Overflow 5).
	"interest-delay": true, "interest-delay-start": true, "interest-delay-end": true,
	"caret-shape": true, "caret-animation": true, "interactivity": true, "overlay": true,
	"scroll-target-group": true, "scroll-axis-lock": true,
	// text-size-adjust and its -webkit- name inflate text on a small
	// screen (CSS Mobile Text Size Adjustment 1, 2): a page is no small
	// screen. dynamic-range-limit caps the brightness of HDR content (CSS
	// Color HDR 1): a PDF page holds none.
	"text-size-adjust": true, "dynamic-range-limit": true,
}

// svgProperties are the properties of SVG elements alone that package svg
// reads from the style sheets of the document an svg element sits in,
// with their presentation attributes (SVG 2, 6.6 and 13): the cascade of
// an HTML element ignores them without a warning, as they give an HTML
// box nothing to draw. [SVGProperty] reports them.
var svgProperties = map[string]bool{
	"fill": true, "fill-opacity": true, "fill-rule": true, "stroke": true, "stroke-width": true,
	"stroke-opacity": true, "stroke-linecap": true, "stroke-linejoin": true, "stroke-miterlimit": true,
	"stroke-dasharray": true, "stroke-dashoffset": true, "clip-rule": true, "stop-color": true,
	"stop-opacity": true, "marker": true, "marker-start": true, "marker-mid": true, "marker-end": true,
	"text-anchor": true, "flood-color": true, "flood-opacity": true, "color-interpolation-filters": true,
	"vector-effect": true, "shape-rendering": true,
}

// SVGProperty reports whether package svg, not the cascade of HTML
// elements, reads the property name.
func SVGProperty(name string) bool { return svgProperties[name] }

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

// declarationValue reports whether l matches <declaration-value>? (CSS
// Syntax 3, 4.2; CSS Variables 1, 2): no bad string or bad URL token
// anywhere, no unmatched ")", "]" or "}", and no top-level ";" or "!"
// (the "!important" of a declaration is already removed). Every var() in
// l must itself be valid (validVars).
func declarationValue(l []token) bool {
	for i := range l {
		t := &l[i]
		if t.typ == tSemicolon || t.typ == tRParen || t.typ == tRBracket || t.typ == tRBrace || t.isDelim('!') {
			return false
		}
	}
	return validVars(l)
}

// validVars reports whether every var() in l is syntactically valid (CSS
// Variables 1, 3): a custom property name, then optionally a comma and a
// fallback that matches <declaration-value>?. A value holding an invalid
// var() is invalid at parse time; no bad string or bad URL token appears
// at any depth.
func validVars(l []token) bool {
	for i := range l {
		t := &l[i]
		if t.typ == tBadString || t.typ == tBadURL {
			return false
		}
		if t.typ != tFunction || !eqFold(t.value, "var") {
			if len(t.children) > 0 && !validVars(t.children) {
				return false
			}
			continue
		}
		args := t.children
		j := 0
		for j < len(args) && args[j].typ == tWhitespace {
			j++
		}
		if j >= len(args) || args[j].typ != tIdent || !strings.HasPrefix(args[j].value, "--") {
			return false
		}
		j++
		for j < len(args) && args[j].typ == tWhitespace {
			j++
		}
		if j < len(args) && (args[j].typ != tComma || !declarationValue(args[j+1:])) {
			return false
		}
	}
	return true
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
		if !declarationValue(value) {
			// CSS Variables 1, 2: a custom property's value is a
			// <declaration-value>?, else the declaration is invalid.
			c.warn(warn.CSSInvalid, warn.Approximated, "invalid value for %q was ignored", name)
			return Declaration{Property: name}
		}
		return Declaration{Property: name, Important: important, custom: trimWS(value), isCustom: true}
	}
	written := lower(name)
	name = propertyName(written)
	d := Declaration{Property: name, Important: important}
	if !knownProperty(name) {
		// An unknown vendor-prefixed name is ignored without a warning; a
		// legacy name alias warns as the property it names.
		if (!strings.HasPrefix(written, "-") || written != name) && !silentProperties[name] && !svgProperties[name] {
			c.warn(warn.CSSUnsupported, warn.Approximated, "property %q is not supported and was ignored", written)
		}
		return d
	}
	if containsVar(value) {
		if !declarationValue(value) {
			// CSS Variables 1, 3: a value holding var() is assumed valid
			// at parse time only when it is a <declaration-value> whose
			// var() functions are valid.
			c.warn(warn.CSSInvalid, warn.Approximated, "invalid value for %q was ignored", name)
			return d
		}
		d.pending, d.base, d.quirks = value, c.baseURL, c.quirks
		return d
	}
	entries, ok := c.expand(name, value)
	if !ok && containsSubstitution(value) {
		// attr() and env() substitute at computed-value time (CSS Values
		// 5, 7.7; CSS Environment Variables 1, 3). content reads an
		// untyped attr() itself, and only waits when it cannot.
		d.pending, d.base, d.quirks = value, c.baseURL, c.quirks
		return d
	}
	if v := significant(value); len(v) == 1 && v[0].typ == tIdent {
		k := lower(v[0].value)
		d.revert, d.revertLayer = k == "revert", k == "revert-layer"
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
	d.containerUnits = hasContainerUnit(value)
	return d
}

// hasContainerUnit reports whether a value holds a dimension in a
// container query unit, at any depth.
func hasContainerUnit(l []token) bool {
	for i := range l {
		t := &l[i]
		if t.typ == tDimension && len(t.value) > 2 && (t.value[0] == 'c' || t.value[0] == 'C') && (t.value[1] == 'q' || t.value[1] == 'Q') {
			if u, ok := lengthUnits[lower(t.value)]; ok && u >= numLinearUnits {
				return true
			}
		}
		if len(t.children) > 0 && hasContainerUnit(t.children) {
			return true
		}
	}
	return false
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
	if c.quirks {
		v = quirkyValue(name, v)
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
			if l, ok := calcLength(t, false); ok && l.mixed() {
				return true
			}
		}
		if len(t.children) > 0 && mixedCalc(t.children) {
			return true
		}
	}
	return false
}
