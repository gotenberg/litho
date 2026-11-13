package css

// singleAnimation is one item of the animation shorthand.
type singleAnimation struct {
	name       string
	duration   float64
	easing     Easing
	iterations float64
	direction  AnimationDirection
	fill       FillMode
	paused     bool
	delay      float64
}

// parseSingleAnimation parses a <single-animation> (CSS Animations 1, 4.9
// and 2, 4.12): a duration, an easing function, a delay, an iteration
// count, a direction, a fill mode, a play state and a name, in any order,
// each at most once. The first time is the duration, the second the delay;
// a keyword goes to the first of the other properties that takes it and is
// not yet set, and only then to the name. Chromium parses neither the
// timeline nor the delay's end the draft adds to the grammar: the timeline
// is reset with the other reset-only longhands.
func parseSingleAnimation(it []token) (singleAnimation, bool) {
	a := singleAnimation{duration: -1, easing: easeEasing, iterations: 1}
	var hasDur, hasDelay, hasEasing, hasIter, hasDir, hasFill, hasPlay, hasName bool
	for _, t := range it {
		one := []token{t}
		if !hasDur || !hasDelay {
			if d, ok := parseTime(t); ok {
				if !hasDur {
					if d < 0 {
						return a, false
					}
					a.duration, hasDur = d, true
				} else {
					a.delay, hasDelay = d, true
				}
				continue
			}
		}
		if !hasDur && t.isIdent("auto") {
			hasDur = true
			continue
		}
		if !hasEasing {
			if e, ok := parseEasing(t); ok {
				a.easing, hasEasing = e, true
				continue
			}
		}
		if !hasIter {
			if n, ok := parseIterations(one); ok {
				a.iterations, hasIter = n, true
				continue
			}
		}
		if !hasDir {
			if d, ok := keywordItem(directionKeywords)(one); ok {
				a.direction, hasDir = d, true
				continue
			}
		}
		if !hasFill {
			if f, ok := keywordItem(fillKeywords)(one); ok {
				a.fill, hasFill = f, true
				continue
			}
		}
		if !hasPlay {
			if p, ok := keywordItem(playStateKeywords)(one); ok {
				a.paused, hasPlay = p, true
				continue
			}
		}
		if !hasName {
			if n, ok := parseAnimationName(one); ok {
				a.name, hasName = n, true
				continue
			}
		}
		return a, false
	}
	return a, len(it) > 0
}

// animationShorthand is animation (CSS Animations 1, 4.9 and 2, 4.12): a
// list of <single-animation> onto the eight longhands it names, and the
// reset-only longhands animation-composition, animation-timeline and the
// animation-range longhands back to their initial values (Scroll-driven
// Animations 1, Appendix A).
func animationShorthand() shorthandDef {
	set := []propID{
		pAnimationName, pAnimationDuration, pAnimationTimingFunction, pAnimationIterationCount,
		pAnimationDirection, pAnimationFillMode, pAnimationPlayState, pAnimationDelay,
	}
	reset := []propID{pAnimationComposition, pAnimationTimeline, pAnimationRangeStart, pAnimationRangeEnd}
	return shorthandDef{
		longhands: append(append([]propID(nil), set...), reset...),
		expand: func(v []token, _ *declCtx) ([]entry, bool) {
			items, ok := commaList(v)
			if !ok {
				return nil, false
			}
			n := len(items)
			names, durs, easings, iters := make([]string, n), make([]float64, n), make([]Easing, n), make([]float64, n)
			dirs, fills, paused, delays := make([]AnimationDirection, n), make([]FillMode, n), make([]bool, n), make([]float64, n)
			for i, it := range items {
				a, ok := parseSingleAnimation(it)
				if !ok {
					return nil, false
				}
				names[i], durs[i], easings[i], iters[i] = a.name, a.duration, a.easing, a.iterations
				dirs[i], fills[i], paused[i], delays[i] = a.direction, a.fill, a.paused, a.delay
			}
			out := []entry{
				{pAnimationName, initialList(names, "")},
				{pAnimationDuration, initialList(durs, -1)},
				{pAnimationTimingFunction, initialListFunc(easings, easeEasing, equalEasing)},
				{pAnimationIterationCount, initialList(iters, 1)},
				{pAnimationDirection, initialList(dirs, DirectionNormal)},
				{pAnimationFillMode, initialList(fills, FillNone)},
				{pAnimationPlayState, initialList(paused, false)},
				{pAnimationDelay, initialList(delays, 0)},
			}
			for _, id := range reset {
				out = append(out, entry{id, wideInitial})
			}
			return out, true
		},
	}
}

// initialListFunc is [initialList] for an item type eq compares.
func initialListFunc[T any](l []T, init T, eq func(a, b T) bool) []T {
	if len(l) == 1 && eq(l[0], init) {
		return nil
	}
	return l
}

// animationRangeShorthand is animation-range (Scroll-driven Animations 1,
// Appendix A): a list of a start and an optional end; an end left out is the
// start's range at 100% when the start names one, else normal.
func animationRangeShorthand() shorthandDef {
	return shorthandDef{
		longhands: []propID{pAnimationRangeStart, pAnimationRangeEnd},
		expand: func(v []token, _ *declCtx) ([]entry, bool) {
			items, ok := commaList(v)
			if !ok {
				return nil, false
			}
			starts, ends := make([]AnimationRange, len(items)), make([]AnimationRange, len(items))
			for i, it := range items {
				n := rangeStartLen(it)
				s, ok := parseRange(it[:n], 0)
				if !ok {
					return nil, false
				}
				e := AnimationRange{Normal: true}
				if s.Name != RangeNone {
					e = AnimationRange{Name: s.Name, Offset: Dimension{Kind: Percent, Value: 1}}
				}
				if n < len(it) {
					if e, ok = parseRange(it[n:], 1); !ok {
						return nil, false
					}
				}
				starts[i], ends[i] = s, e
			}
			normal := AnimationRange{Normal: true}
			return []entry{
				{pAnimationRangeStart, initialListFunc(starts, normal, equalRange)},
				{pAnimationRangeEnd, initialListFunc(ends, normal, equalRange)},
			}, true
		},
	}
}

// rangeStartLen returns how many component values of an animation-range
// item its start takes: normal or a length-percentage alone, or a range
// name and the length-percentage after it.
func rangeStartLen(it []token) int {
	if len(it) > 1 && it[0].typ == tIdent && rangeNames[lower(it[0].value)] != RangeNone && it[1].typ != tIdent {
		return 2
	}
	return min(1, len(it))
}

// timelineShorthand is scroll-timeline or view-timeline (Scroll-driven
// Animations 1, 2.3.3 and 3.4.4): a list of a name and its axis, and for
// a view timeline its inset, onto the name longhand, which litho holds,
// and the others, which it parses.
func timelineShorthand(name propID, rest []propID, view bool) shorthandDef {
	return shorthandDef{
		longhands: append([]propID{name}, rest...),
		expand: func(v []token, _ *declCtx) ([]entry, bool) {
			items, ok := commaList(v)
			if !ok {
				return nil, false
			}
			names := make([]string, len(items))
			for i, it := range items {
				if names[i], ok = parseTimelineName(it[:1]); !ok {
					return nil, false
				}
				tail := it[1:]
				if len(tail) > 0 && axisItem(tail[:1]) {
					tail = tail[1:]
				} else if len(tail) > 0 && axisItem(tail[len(tail)-1:]) {
					tail = tail[:len(tail)-1]
				}
				if len(tail) > 0 && (!view || !insetArgs(tail, false)) {
					return nil, false
				}
			}
			out := []entry{{name, initialList(names, "")}}
			for _, id := range rest {
				out = append(out, entry{id, struct{}{}})
			}
			return out, true
		},
	}
}

// animationShorthands returns the shorthands of the animation, transition
// and timeline longhands.
func animationShorthands() map[string]shorthandDef {
	return map[string]shorthandDef{
		"animation":       animationShorthand(),
		"animation-range": animationRangeShorthand(),
		"transition":      transitionShorthand(),
		"scroll-timeline": timelineShorthand(pScrollTimelineName, []propID{pScrollTimelineAxis}, false),
		"view-timeline":   timelineShorthand(pViewTimelineName, []propID{pViewTimelineAxis, pViewTimelineInset}, true),
	}
}
