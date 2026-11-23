package css

import (
	"maps"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// AnimationTime is the moment of the document timeline at which the
// cascade computes animated values ([ComputeOptions.AnimationTime]).
type AnimationTime uint8

// The moments a printed page can show.
const (
	// AnimationsAtStart computes every animation at time zero, the
	// document's first frame (Web Animations 1, 4.3.1): an animation
	// without a delay shows its first keyframe, one with a delay its
	// backwards fill. Chromium prints a page at that moment.
	AnimationsAtStart AnimationTime = iota
	// AnimationsAtEnd computes every animation at the end of its active
	// interval: its last keyframe, by direction and iteration count,
	// when its fill mode keeps it, the element's own style when it does
	// not. An animation that repeats forever has no end: it shows the end
	// of its first iteration, filled. A paused animation, and one driven
	// by a scroll or view timeline or by none, stays where time zero puts
	// it: waiting moves none of them.
	AnimationsAtEnd
)

// animatedValue is a cascaded value the cascade interpolated: the
// longhand's computed value held by st (Web Animations 1, 5.4.5).
type animatedValue struct{ st *Style }

// pendingAt is a longhand whose winning declaration holds var(), which an
// animated custom property substitutes again: id is the longhand, src the
// one the declaration names, its flow-relative side before mapping.
type pendingAt struct {
	id, src propID
	d       *Declaration
}

// animTiming is the timing of one animation (Web Animations 1, 4.6): its
// iteration duration, start delay and iteration count, its direction and
// fill mode.
type animTiming struct {
	duration, delay, iterations float64
	direction                   AnimationDirection
	fill                        FillMode
}

// animPhase is the phase of an animation effect (Web Animations 1,
// 4.6.6).
type animPhase uint8

const (
	phaseBefore animPhase = iota
	phaseActive
	phaseAfter
)

// progress returns the iteration progress of the timing at the local time
// t, directed but not eased, and the before flag of its easing (Web
// Animations 1, 4.6.6 and 4.7), or false when the effect has no active
// time and so no effect. before forces the before phase, for a timeline
// whose position precedes the animation's range.
func (a animTiming) progress(t float64, before bool) (float64, bool, bool) {
	dur := max(a.duration, 0)
	active := 0.0
	if dur > 0 && a.iterations > 0 {
		active = dur * a.iterations
	}
	end := max(a.delay+active, 0)
	beforeActive := max(min(a.delay, end), 0)
	activeAfter := max(min(a.delay+active, end), 0)
	phase := phaseActive
	switch {
	case before || t < beforeActive:
		phase = phaseBefore
	case t >= activeAfter:
		// The animation plays forwards: the active-after boundary belongs
		// to the after phase.
		phase = phaseAfter
	}
	var at float64
	switch phase {
	case phaseBefore:
		if a.fill != FillBackwards && a.fill != FillBoth {
			return 0, false, false
		}
		at = max(t-a.delay, 0)
		if before {
			at = 0
		}
	case phaseActive:
		at = t - a.delay
	case phaseAfter:
		if a.fill != FillForwards && a.fill != FillBoth {
			return 0, false, false
		}
		at = max(min(t-a.delay, active), 0)
	}
	var overall float64
	switch {
	case dur == 0 && phase == phaseBefore:
		overall = 0
	case dur == 0:
		overall = a.iterations
	default:
		overall = at / dur
	}
	simple := 0.0
	if !math.IsInf(overall, 1) {
		simple = math.Mod(overall, 1)
	}
	if simple == 0 && phase != phaseBefore && at == active && a.iterations != 0 {
		simple = 1
	}
	var iteration float64
	switch {
	case phase == phaseAfter && math.IsInf(a.iterations, 1):
		iteration = math.Inf(1)
	case simple == 1:
		iteration = math.Floor(overall) - 1
	default:
		iteration = math.Floor(overall)
	}
	forwards := true
	switch a.direction {
	case DirectionNormal:
	case DirectionReverse:
		forwards = false
	case DirectionAlternate, DirectionAlternateReverse:
		d := iteration
		if a.direction == DirectionAlternateReverse {
			d++
		}
		forwards = math.IsInf(d, 1) || math.Mod(d, 2) == 0
	}
	p := simple
	if !forwards {
		p = 1 - simple
	}
	flag := phase == phaseBefore && forwards || phase == phaseAfter && !forwards
	return p, flag, true
}

// animationNames returns the animation-name list the cascaded value v
// gives, parent's for inherit, nil for none.
func animationNames(v any, parent *Style) []string {
	switch x := v.(type) {
	case []string:
		return x
	case cssWide:
		if x == wideInherit && parent.Animation != nil {
			return parent.Animation.Names
		}
	}
	return nil
}

// keyframesOf returns the @keyframes rule called name that the element's
// animation-name refers to: the one of the tree its declaration comes
// from, else of the nearest tree around it, the document's last (CSS
// Scoping 1, 3.5).
func (cs *computeState) keyframesOf(name string) *keyframesRule {
	scope := cs.nameScope
	for {
		if k := cs.r.keyframes[scopedName{scope: scope, name: name}]; k != nil || scope == nil {
			return k
		}
		if h := cs.opts.Shadows.Host(scope); h != nil {
			scope = treeScope(h)
		} else {
			scope = nil
		}
	}
}

// animates reports whether the cascaded animation-name v names an
// @keyframes rule the resolver holds.
func (cs *computeState) animates(v any, parent *Style) bool {
	if v == nil || len(cs.r.keyframes) == 0 {
		return false
	}
	for _, n := range animationNames(v, parent) {
		if cs.keyframesOf(n) != nil {
			return true
		}
	}
	return false
}

// animate applies the element's animations to its cascaded values (CSS
// Animations 1, 2), cands being its declarations in cascade order: each
// animated longhand that no !important declaration sets takes the
// animation's value at the moment cs.opts.AnimationTime names, the last
// animation of the list winning, and the animated custom properties are
// resolved again with the declarations that read them. It returns the
// custom properties.
func (cs *computeState) animate(winners *[numProps]any, imp *[numProps]bool, pend []pendingAt, cands []cand, parent *Style, parentVars, vars map[string][]token, isRoot bool) map[string][]token {
	if !cs.m.charge(int(numProps)) {
		return vars
	}
	var base Style
	cs.computeInto(&base, winners, parent, isRoot)
	a := base.Animation
	if a == nil {
		return vars
	}
	cs.cur = &base
	defer func() { cs.cur = nil }()
	under := *winners
	var customAt map[string][]token
	type frame struct {
		rule   *keyframesRule
		p      float64
		before bool
		easing Easing
	}
	var frames []frame
	for i, name := range a.Names {
		rule := cs.keyframesOf(name)
		if rule == nil {
			continue
		}
		p, before, ok := cs.timing(a, i)
		if !ok {
			continue
		}
		if itemOf(a.Compositions, i, CompositionReplace) != CompositionReplace {
			cs.warns.add(Warning{Code: warn.CSSUnsupported, Effect: warn.Approximated, Message: "animation-composition add and accumulate are computed as replace"})
		}
		f := frame{rule: rule, p: p, before: before, easing: itemOf(a.Easings, i, easeEasing)}
		frames = append(frames, f)
		for _, n := range rule.custom {
			if !cs.m.charge(len(rule.blocks) + 1) {
				return vars
			}
			ka, kb, e := keyframeSegment(rule.propertyKeyframes(0, n), p, before, &f.easing)
			k := ka
			if e >= 0.5 {
				k = kb
			}
			if customAt == nil {
				customAt = map[string][]token{}
			}
			if k.decl == nil {
				delete(customAt, n)
			} else {
				customAt[n] = k.decl.custom
			}
		}
	}
	if len(customAt) > 0 {
		// The element's own custom properties, in cascade order, the
		// animated ones over them.
		declared := map[string][]token{}
		var names []string
		for _, c := range cands {
			if c.d.isCustom {
				if _, seen := declared[c.d.Property]; !seen {
					names = append(names, c.d.Property)
				}
				declared[c.d.Property] = c.d.custom
			}
		}
		for _, n := range slices.Sorted(maps.Keys(customAt)) {
			if _, ok := declared[n]; !ok {
				names = append(names, n)
			}
			declared[n] = customAt[n]
		}
		vars = cs.resolveCustom(names, declared, parentVars, parentVars)
		for _, pa := range pend {
			winners[pa.id] = wideUnset
			for _, e := range cs.substituteDecl(pa.d, vars) {
				if e.id == pa.src {
					winners[pa.id] = e.v
				}
			}
		}
		under = *winners
	}
	for _, f := range frames {
		for _, id := range f.rule.ids {
			if imp[id] {
				continue
			}
			if !cs.m.charge(len(f.rule.blocks) + 1) {
				return vars
			}
			ka, kb, e := keyframeSegment(f.rule.propertyKeyframes(id, ""), f.p, f.before, &f.easing)
			va, vb := cs.keyframeValue(ka, id, &under, vars), cs.keyframeValue(kb, id, &under, vars)
			switch e {
			case 0:
				winners[id] = va
			case 1:
				winners[id] = vb
			default:
				winners[id] = cs.interpolate(id, va, vb, e, &under, parent, isRoot)
			}
		}
	}
	return vars
}

// keyframeValue returns the cascaded value keyframe k gives longhand id:
// its declaration's, substituted with vars when it holds var(), or the
// underlying value for an implicit keyframe.
func (cs *computeState) keyframeValue(k keyframe, id propID, under *[numProps]any, vars map[string][]token) any {
	switch {
	case k.decl == nil:
		return under[id]
	case len(k.decl.pending) > 0:
		for _, e := range cs.substituteDecl(k.decl, vars) {
			if e.id == id {
				return e.v
			}
		}
		return under[id]
	}
	for _, e := range k.decl.entries {
		if e.id == id {
			return e.v
		}
	}
	return under[id]
}

// interpolate returns the cascaded value of longhand id at eased progress
// p from the value va to vb: the value interpolated from the styles they
// compute to, or the nearer of the two for values that animate
// discretely, which flip at the midpoint (Web Animations 1, 5.2).
func (cs *computeState) interpolate(id propID, va, vb any, p float64, under *[numProps]any, parent *Style, isRoot bool) any {
	def := &props[id]
	res := discrete
	if def.interp != nil && cs.m.charge(2*int(numProps)) {
		w := *under
		w[id] = va
		var sa, sb Style
		cs.computeInto(&sa, &w, parent, isRoot)
		w[id] = vb
		cs.computeInto(&sb, &w, parent, isRoot)
		dst := sa
		if res = def.interp(&dst, &sa, &sb, p); res == interpolated {
			return animatedValue{st: &dst}
		}
	} else if !discreteAnimation[id] {
		res = uninterpolated
	}
	if res == uninterpolated {
		cs.warns.add(Warning{Code: warn.CSSUnsupported, Effect: warn.Approximated, Message: "\"" + def.name + "\" is animated in discrete steps: litho does not interpolate its values"})
	}
	if p < 0.5 {
		return va
	}
	return vb
}

// keyframeSegment returns the keyframes around the iteration progress p
// in kfs and the progress between them, eased by the first's easing
// function or by easing, the animation's (CSS Animations 1, 3.1 and 4.3;
// Web Animations 1, 5.3.4). A progress out of [0, 1] takes the first or
// last two keyframes.
func keyframeSegment(kfs []keyframe, p float64, before bool, easing *Easing) (keyframe, keyframe, float64) {
	i := 0
	switch {
	case p >= 1:
		i = len(kfs) - 2
		for i > 0 && kfs[i].offset == 1 {
			i--
		}
	case p > 0:
		for i+2 < len(kfs) && kfs[i+1].offset <= p {
			i++
		}
	}
	a, b := kfs[i], kfs[i+1]
	e := easing
	if a.easing != nil {
		e = a.easing
	}
	local := (p - a.offset) / (b.offset - a.offset)
	return a, b, e.at(local, before)
}

// timing returns the iteration progress of the i-th animation of a, its
// before flag, and whether it has an effect, at the moment
// cs.opts.AnimationTime names on its timeline (CSS Animations 2, 4.11).
//
// A paused animation, and one whose timeline is none or names no
// timeline, holds the time zero it started at (Web Animations 1, 4.5.8).
// A scroll progress timeline sits at its start, the scroll position of an
// unscrolled page (Scroll-driven Animations 1, 2): the animation is at
// time zero, or before its range when the range starts past it. A view
// progress timeline puts its subject past the first screen of the page,
// before every range: the cascade knows no box positions, and a page's
// content mostly lies below its first screen.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints at time
//     zero in 9 of 9 runs of a 20 s and a 60 s sampler; after
//     waitDelay=3s at 3.0 s. A paused animation, one on the timeline
//     none and one naming no timeline print at time zero with and
//     without waitDelay.
//   - probe, html: Chromium prints scroll() and a named scroll timeline
//     on a page taller than its screen at time zero, delays counting as
//     on the document timeline, and before the range under
//     animation-range: 100px 500px; on a page that does not overflow, it
//     prints no effect. It prints view() at the subject's progress on the
//     first screen of 437 pixels (0.875 at 20 pixels down) and before
//     the range below it.
func (cs *computeState) timing(a *Animations, i int) (float64, bool, bool) {
	t := animTiming{
		duration:   itemOf(a.Durations, i, -1),
		delay:      itemOf(a.Delays, i, 0),
		iterations: itemOf(a.Iterations, i, 1),
		direction:  itemOf(a.Directions, i, DirectionNormal),
		fill:       itemOf(a.Fills, i, FillNone),
	}
	switch tl := itemOf(a.Timelines, i, AnimationTimeline{}); tl.Kind {
	case TimelineAuto:
	case TimelineNone:
		return t.progress(0, false)
	case TimelineScroll:
		return t.progress(0, rangeStartsLater(itemOf(a.RangeStarts, i, AnimationRange{Normal: true})))
	case TimelineView:
		cs.warnView()
		return t.progress(0, true)
	case TimelineNamed:
		switch cs.namedTimeline(tl.Name) { //nolint:exhaustive // a name that resolves to no timeline holds time zero
		case TimelineScroll:
			return t.progress(0, rangeStartsLater(itemOf(a.RangeStarts, i, AnimationRange{Normal: true})))
		case TimelineView:
			cs.warnView()
			return t.progress(0, true)
		}
		return t.progress(0, false)
	}
	if itemOf(a.Paused, i, false) || cs.opts.AnimationTime != AnimationsAtEnd {
		return t.progress(0, false)
	}
	if t.duration > 0 && math.IsInf(t.iterations, 1) {
		// FillForwards or FillBackwards is FillBoth.
		t.iterations = 1
		t.fill |= FillForwards
	}
	return t.progress(math.Inf(1), false)
}

// rangeStartsLater reports an animation-range-start past the start of a
// scroll timeline: a positive offset, or a named range, which a scroll
// timeline lacks.
func rangeStartsLater(r AnimationRange) bool {
	return !r.Normal && r.Offset.Value > 0
}

// warnView reports once that a view timeline was taken before its range.
func (cs *computeState) warnView() {
	cs.warns.add(Warning{Code: warn.CSSUnsupported, Effect: warn.Approximated, Message: "an animation on a view timeline is computed as if its subject were below the first screen"})
}

// namedTimeline returns the kind of the timeline a <dashed-ident>
// animation-timeline names: the scroll or view timeline the element or
// its nearest ancestor declaring the name declares (Scroll-driven
// Animations 1, 4.2), TimelineNone when none does.
func (cs *computeState) namedTimeline(name string) TimelineKind {
	for n := cs.node; isElement(n); n = n.Parent {
		st := cs.out[n]
		if n == cs.node && cs.cur != nil {
			st = cs.cur
		}
		if st == nil || st.Animation == nil {
			continue
		}
		if slices.Contains(st.Animation.ScrollTimelineNames, name) {
			return TimelineScroll
		}
		if slices.Contains(st.Animation.ViewTimelineNames, name) {
			return TimelineView
		}
	}
	return TimelineNone
}
