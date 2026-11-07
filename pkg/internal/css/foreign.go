package css

import (
	"golang.org/x/net/html"
)

// This file exposes the colour parser and the selector engine to the
// converters of foreign content, SVG, whose properties the cascade does
// not compute.

// ParseColor parses a CSS <color> value (CSS Color 4, 4.1): a named
// colour, transparent, a hex colour, rgb(), rgba(), hsl() or hsla(). It
// reports current for currentColor, whose colour the caller resolves, and
// ok false for anything else, surrounding white space allowed.
func ParseColor(s string) (c Color, current, ok bool) {
	toks, _ := parseComponentValues(s)
	toks = significant(toks)
	if len(toks) != 1 {
		return Color{}, false, false
	}
	v, ok := parseColor(toks[0])
	if !ok {
		return Color{}, false, false
	}
	return v.c, v.current, true
}

// Selector is a parsed selector list (Selectors 4, 4.1). It is immutable
// and safe for concurrent use.
type Selector struct {
	list []*complexSel
}

// ParseSelector parses a selector list, the prelude of a style rule. It
// reports false for an invalid list, or one that uses a construct litho does
// not match: the rule it heads is then dropped (CSS Syntax 3, 9). A
// selector that targets a pseudo-element never matches an element.
func ParseSelector(src string) (*Selector, bool) {
	if len(src) > maxStylesheetBytes {
		return nil, false
	}
	toks, p := parseComponentValues(src)
	if p.tz.over || p.deep {
		return nil, false
	}
	list, err := parseSelectorList(trimWS(toks), 0, false)
	if err != nil {
		return nil, false
	}
	return &Selector{list: list}, true
}

// Matcher matches [Selector] values against the elements of one x/net/html
// tree under a step budget. It caches what the structural pseudo-classes
// read, so it must not outlive changes to the tree. It is not safe for
// concurrent use.
type Matcher struct {
	m *matcher
}

// NewMatcher returns a [Matcher] that spends at most maxSteps compound
// tests over its life; past them every match fails. lang is the document
// language :lang() falls back to, "" for none.
func NewMatcher(lang string, maxSteps int) *Matcher {
	return &Matcher{m: newMatcher(lang, maxSteps)}
}

// Match reports whether the element n matches one of the selectors of s,
// and the highest specificity among those that match (Selectors 4, 17),
// packed so that a larger value is more specific.
func (m *Matcher) Match(s *Selector, n *html.Node) (uint32, bool) {
	if s == nil || !isElement(n) {
		return 0, false
	}
	best, ok := uint32(0), false
	for _, c := range s.list {
		if c.pseudoElement != "" || !m.m.matches(c, n) {
			continue
		}
		if !ok || c.spec > best {
			best = c.spec
		}
		ok = true
	}
	return best, ok
}

// Exhausted reports whether the matcher ran out of steps.
func (m *Matcher) Exhausted() bool { return m.m.over }
