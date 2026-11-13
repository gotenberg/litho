package css

// parsedOnly builds a longhand litho parses and does not hold: parse reads
// the whole value.
func parsedOnly(name string, parse func(v []token) bool) propDef {
	return propDef{
		name: name,
		parse: func(v []token, _ *declCtx) (any, bool) {
			return struct{}{}, parse(v)
		},
		compute:  func(*styler, *Style, any) {},
		copyFrom: func(dst, src *Style) {},
	}
}

// listOf builds the parser of a comma-separated list whose items item
// accepts.
func listOf(item func(it []token) bool) func(v []token) bool {
	return func(v []token) bool {
		items, ok := commaList(v)
		if !ok {
			return false
		}
		for _, it := range items {
			if !item(it) {
				return false
			}
		}
		return true
	}
}

// transitionPropertyItem reports a <single-transition-property> (CSS
// Transitions 1, 2.1): all or a <custom-ident>, which may name no
// property.
func transitionPropertyItem(it []token) bool {
	t, ok := single(it)
	return ok && t.typ == tIdent && customIdent(t.value) && !eqFold(t.value, "none")
}

// parseTransitionProperty reports a transition-property value: none, or a
// list of <single-transition-property>.
func parseTransitionProperty(v []token) bool {
	if k, ok := keyword(v); ok && k == "none" {
		return true
	}
	return listOf(transitionPropertyItem)(v)
}

// transitionBehaviorKeywords are the values of transition-behavior (CSS
// Transitions 2, 2.5).
var transitionBehaviorKeywords = map[string]bool{"normal": true, "allow-discrete": true}

// parseSingleTransition reports a <single-transition> (CSS Transitions 2,
// 2.6): [ none | <single-transition-property> ] || <time> || <easing-
// function> || <time> || <transition-behavior-value>, none allowed when
// noneOK is set.
func parseSingleTransition(it []token, noneOK bool) bool {
	var prop, easing, behavior bool
	times := 0
	for _, t := range it {
		switch {
		case !behavior && t.typ == tIdent && transitionBehaviorKeywords[lower(t.value)]:
			behavior = true
		case timeToken(t):
			if times == 2 {
				return false
			}
			times++
		case !easing && easingToken(t):
			easing = true
		case !prop && t.isIdent("none"):
			if !noneOK {
				return false
			}
			prop = true
		case !prop && transitionPropertyItem([]token{t}):
			prop = true
		default:
			return false
		}
	}
	return len(it) > 0
}

// timeToken reports a <time>.
func timeToken(t token) bool {
	_, ok := parseTime(t)
	return ok
}

// easingToken reports an <easing-function>.
func easingToken(t token) bool {
	_, ok := parseEasing(t)
	return ok
}

// transitionDurationItem reports a transition-duration item: a time of 0s
// or more (CSS Transitions 1, 2.2).
func transitionDurationItem(it []token) bool {
	t, ok := single(it)
	if !ok {
		return false
	}
	d, ok := parseTime(t)
	return ok && d >= 0
}

// timelineScope reports a timeline-scope value: none or a list of
// <dashed-ident> (Scroll-driven Animations 1, Appendix B). Chromium does
// not parse all, which the draft adds.
func timelineScope(v []token) bool {
	if k, ok := keyword(v); ok && k == "none" {
		return true
	}
	return listOf(func(it []token) bool {
		t, ok := single(it)
		return ok && dashedIdent(t)
	})(v)
}

// axisItem reports a scroll-timeline-axis or view-timeline-axis item.
func axisItem(it []token) bool {
	k, ok := keyword(it)
	return ok && axisKeywords[k]
}

// motionParsedProps returns the transition longhands (CSS Transitions 1,
// 2 and 2, 2.5) and the timeline longhands litho parses and does not
// hold (Scroll-driven Animations 1, 2.3.2, 3.4.2 and 3.4.3, and Appendix
// B).
//
// A transition runs when a style change event changes a property's
// computed value (CSS Transitions 1, 3). A converted document has one
// style, computed once from its stylesheets: no script, no hover and no
// later sheet changes it, so no transition starts and every element
// prints in its own style. The transition longhands, and the timeline
// longhands that only move a progress timeline litho holds at its start,
// are parsed and hold no computed value.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints the
//     element's own style for a 100 s opacity transition from
//     @starting-style (CSS Transitions 2, 3.3), with or without
//     transition-behavior: allow-discrete, from a visibility in
//     @starting-style, and between a screen and a print rule, under
//     print and under screen emulation.
//   - probe, html: Chromium prints the end value of a transition a script
//     starts by adding a class at DOMContentLoaded or after load; litho
//     runs no script, and prints the element's style without the class.
func motionParsedProps() map[propID]propDef {
	return map[propID]propDef{
		pTransitionProperty:       parsedOnly("transition-property", parseTransitionProperty),
		pTransitionDuration:       parsedOnly("transition-duration", listOf(transitionDurationItem)),
		pTransitionTimingFunction: parsedOnly("transition-timing-function", listOf(func(it []token) bool { _, ok := parseEasingItem(it); return ok })),
		pTransitionDelay:          parsedOnly("transition-delay", listOf(func(it []token) bool { _, ok := parseDelay(it); return ok })),
		pTransitionBehavior: parsedOnly("transition-behavior", listOf(func(it []token) bool {
			k, ok := keyword(it)
			return ok && transitionBehaviorKeywords[k]
		})),
		pScrollTimelineAxis: parsedOnly("scroll-timeline-axis", listOf(axisItem)),
		pViewTimelineAxis:   parsedOnly("view-timeline-axis", listOf(axisItem)),
		pViewTimelineInset:  parsedOnly("view-timeline-inset", listOf(func(it []token) bool { return insetArgs(it, false) })),
		pTimelineScope:      parsedOnly("timeline-scope", timelineScope),
	}
}

// transitionShorthand is transition (CSS Transitions 2, 2.6): a list of
// <single-transition>, none only alone, onto the five transition
// longhands, which hold nothing.
func transitionShorthand() shorthandDef {
	ids := []propID{pTransitionProperty, pTransitionDuration, pTransitionTimingFunction, pTransitionDelay, pTransitionBehavior}
	return shorthandDef{
		longhands: ids,
		expand: func(v []token, _ *declCtx) ([]entry, bool) {
			items, ok := commaList(v)
			if !ok {
				return nil, false
			}
			for _, it := range items {
				if !parseSingleTransition(it, len(items) == 1) {
					return nil, false
				}
			}
			out := make([]entry, len(ids))
			for i, id := range ids {
				out[i] = entry{id: id, v: struct{}{}}
			}
			return out, true
		},
	}
}
