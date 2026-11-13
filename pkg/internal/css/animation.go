package css

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// AnimationDirection is one item of animation-direction (CSS Animations 1,
// 4.5).
type AnimationDirection uint8

// The animation directions.
const (
	DirectionNormal AnimationDirection = iota
	DirectionReverse
	DirectionAlternate
	DirectionAlternateReverse
)

// FillMode is one item of animation-fill-mode (CSS Animations 1, 4.8).
type FillMode uint8

// The fill modes.
const (
	FillNone FillMode = iota
	FillForwards
	FillBackwards
	FillBoth
)

// Composition is one item of animation-composition (CSS Animations 2,
// 4.10).
type Composition uint8

// The composite operations.
const (
	CompositionReplace Composition = iota
	CompositionAdd
	CompositionAccumulate
)

// TimelineKind says what an [AnimationTimeline] is (CSS Animations 2,
// 4.11).
type TimelineKind uint8

// The animation timelines.
const (
	// TimelineAuto is the document's default timeline.
	TimelineAuto TimelineKind = iota
	// TimelineNone is no timeline.
	TimelineNone
	// TimelineNamed is a scroll or view progress timeline a
	// scroll-timeline-name or view-timeline-name declares.
	TimelineNamed
	// TimelineScroll is scroll() and TimelineView view(), anonymous
	// progress timelines (Scroll-driven Animations 1, 2.2.1 and 3.3.1).
	TimelineScroll
	TimelineView
)

// AnimationTimeline is one item of animation-timeline.
type AnimationTimeline struct {
	Kind TimelineKind
	// Name is the dashed ident of TimelineNamed.
	Name string
}

// RangeName is the named timeline range of an animation-range-start or
// -end item (Scroll-driven Animations 1, 3.1), RangeNone for none.
type RangeName uint8

// The timeline range names.
const (
	RangeNone RangeName = iota
	RangeCover
	RangeContain
	RangeEntry
	RangeExit
	RangeEntryCrossing
	RangeExitCrossing
)

// rangeNames maps each <timeline-range-name> to its RangeName.
var rangeNames = map[string]RangeName{
	"cover": RangeCover, "contain": RangeContain, "entry": RangeEntry, "exit": RangeExit,
	"entry-crossing": RangeEntryCrossing, "exit-crossing": RangeExitCrossing,
}

// AnimationRange is one item of animation-range-start or -end
// (Scroll-driven Animations 1, Appendix A): normal, or an offset into the
// named range Name, the whole timeline for RangeNone.
type AnimationRange struct {
	Normal bool
	Name   RangeName
	// Offset is Points or Percent of the range.
	Offset Dimension
}

// Animations holds the computed animation longhands of an element (CSS
// Animations 1, 4 and 2, 4), each a list as written, and the timeline
// names it declares (Scroll-driven Animations 1, 2.3.1 and 3.4.1). A nil
// list holds the property's initial value. The animation-name list sets
// how many animations there are; the other lists repeat or are cut to its
// length (CSS Values 4, Appendix A).
type Animations struct {
	// Names are the @keyframes names, "" for none.
	Names []string
	// Durations are in seconds, negative for auto.
	Durations []float64
	Easings   []Easing
	// Iterations are the iteration counts, +Inf for infinite.
	Iterations   []float64
	Directions   []AnimationDirection
	Fills        []FillMode
	Paused       []bool
	Delays       []float64
	Compositions []Composition
	Timelines    []AnimationTimeline
	RangeStarts  []AnimationRange
	RangeEnds    []AnimationRange
	// ScrollTimelineNames and ViewTimelineNames are the timelines the
	// element declares, "" for none.
	ScrollTimelineNames []string
	ViewTimelineNames   []string
}

// itemOf returns the item of l for the i-th animation, l repeated as
// needed (CSS Values 4, Appendix A), or init for an empty list.
func itemOf[T any](l []T, i int, init T) T {
	if len(l) == 0 {
		return init
	}
	return l[i%len(l)]
}

// setAnimations sets the list f names of st's animations to v, nil for the
// initial value. It allocates only for a list that differs from st's, and
// drops the pointer when every list is initial. The shared value is never
// mutated.
func setAnimations[T any](st *Style, f func(*Animations) *[]T, v []T) {
	var cur []T
	if st.Animation != nil {
		cur = *f(st.Animation)
	}
	if len(cur) == 0 && len(v) == 0 {
		return
	}
	n := Animations{}
	if st.Animation != nil {
		n = *st.Animation
	}
	*f(&n) = v
	if n.initial() {
		st.Animation = nil
		return
	}
	st.Animation = &n
}

// initial reports animations whose lists are all initial.
func (a *Animations) initial() bool {
	return len(a.Names) == 0 && len(a.Durations) == 0 && len(a.Easings) == 0 && len(a.Iterations) == 0 &&
		len(a.Directions) == 0 && len(a.Fills) == 0 && len(a.Paused) == 0 && len(a.Delays) == 0 &&
		len(a.Compositions) == 0 && len(a.Timelines) == 0 && len(a.RangeStarts) == 0 && len(a.RangeEnds) == 0 &&
		len(a.ScrollTimelineNames) == 0 && len(a.ViewTimelineNames) == 0
}

// animList builds the definition of a list-valued animation longhand
// stored at f: parse reads one item, init is the initial item, and a list
// of the initial item alone is stored as nil.
func animList[T any](name string, f func(*Animations) *[]T, parse func(t []token) (T, bool), init T, eq func(a, b T) bool) propDef {
	return propDef{
		name: name,
		parse: func(v []token, _ *declCtx) (any, bool) {
			items, ok := commaList(v)
			if !ok {
				return nil, false
			}
			out := make([]T, len(items))
			for i, it := range items {
				if out[i], ok = parse(it); !ok {
					return nil, false
				}
			}
			if len(out) == 1 && eq(out[0], init) {
				out = nil
			}
			return out, true
		},
		compute: func(_ *styler, st *Style, v any) {
			if l, ok := v.([]T); ok {
				setAnimations(st, f, l)
			}
		},
		copyFrom: func(dst, src *Style) {
			var l []T
			if src.Animation != nil {
				l = *f(src.Animation)
			}
			setAnimations(dst, f, l)
		},
	}
}

// maxListItems caps the items of a comma-separated list value: 1000.
// Anti-DoS guard, not spec-mandated: the animations and transitions of an
// element are computed once per item, and real pages list a few.
const maxListItems = 1000

// commaList splits a value at its commas into items of one or more
// component values; an empty item fails it.
func commaList(v []token) ([][]token, bool) {
	var out [][]token
	start := 0
	for i := 0; i <= len(v); i++ {
		if i < len(v) && v[i].typ != tComma {
			continue
		}
		if i == start || len(out) == maxListItems {
			return nil, false
		}
		out = append(out, v[start:i])
		start = i + 1
	}
	return out, len(out) > 0
}

// parseTime parses a <time>, a math function of type time included, in
// seconds (CSS Values 4, 7.2).
func parseTime(t token) (float64, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tDimension:
		if !numeric.Finite(t.num) {
			return 0, false
		}
		if o, ok := otherUnits[lower(t.value)]; ok && o.t == typeTime {
			return t.num * o.k, true
		}
	case tFunction:
		if isCalc(&t) {
			if v, ty, ok := calcConst(&t, nil); ok && ty == typeTime {
				return v, true
			}
		}
	}
	return 0, false
}

// parseKeyframesName parses a <keyframes-name> (CSS Animations 1, 3): a
// <custom-ident> other than none, or a non-empty string.
func parseKeyframesName(t token) (string, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tIdent:
		return t.value, customIdent(t.value) && !eqFold(t.value, "none")
	case tString:
		return t.value, t.value != ""
	}
	return "", false
}

// parseAnimationName parses an animation-name item: none, "", or a
// <keyframes-name>.
func parseAnimationName(it []token) (string, bool) {
	t, ok := single(it)
	if !ok {
		return "", false
	}
	if t.isIdent("none") {
		return "", true
	}
	return parseKeyframesName(t)
}

// parseDuration parses an animation-duration item (CSS Animations 2,
// 4.1): auto, as -1, or a time of 0s or more.
func parseDuration(it []token) (float64, bool) {
	t, ok := single(it)
	if !ok {
		return 0, false
	}
	if t.isIdent("auto") {
		return -1, true
	}
	d, ok := parseTime(t)
	return d, ok && d >= 0
}

// parseDelay parses an animation-delay or transition-delay item: a time.
func parseDelay(it []token) (float64, bool) {
	t, ok := single(it)
	if !ok {
		return 0, false
	}
	return parseTime(t)
}

// parseEasingItem parses an animation-timing-function item.
func parseEasingItem(it []token) (Easing, bool) {
	t, ok := single(it)
	if !ok {
		return Easing{}, false
	}
	return parseEasing(t)
}

// parseIterations parses an animation-iteration-count item (CSS
// Animations 1, 4.4): infinite, as +Inf, or a number of 0 or more.
func parseIterations(it []token) (float64, bool) {
	t, ok := single(it)
	if !ok {
		return 0, false
	}
	if t.isIdent("infinite") {
		return math.Inf(1), true
	}
	n, ok := parseNumber(t)
	return n, ok && n >= 0
}

// keywordItem builds the parser of an item that is one keyword of vals.
func keywordItem[T any](vals map[string]T) func(it []token) (T, bool) {
	return func(it []token) (T, bool) {
		k, ok := keyword(it)
		if !ok {
			var zero T
			return zero, false
		}
		v, ok := vals[k]
		return v, ok
	}
}

// The keywords of the animation longhands.
var (
	directionKeywords = map[string]AnimationDirection{
		"normal": DirectionNormal, "reverse": DirectionReverse,
		"alternate": DirectionAlternate, "alternate-reverse": DirectionAlternateReverse,
	}
	fillKeywords = map[string]FillMode{
		"none": FillNone, "forwards": FillForwards, "backwards": FillBackwards, "both": FillBoth,
	}
	playStateKeywords   = map[string]bool{"running": false, "paused": true}
	compositionKeywords = map[string]Composition{
		"replace": CompositionReplace, "add": CompositionAdd, "accumulate": CompositionAccumulate,
	}
	axisKeywords = map[string]bool{"block": true, "inline": true, "x": true, "y": true}
)

// dashedIdent reports an ident that starts with two dashes, a
// <dashed-ident> (CSS Values 4, 4.2).
func dashedIdent(t token) bool {
	return t.typ == tIdent && len(t.value) > 2 && t.value[:2] == "--"
}

// parseTimeline parses an animation-timeline item (CSS Animations 2,
// 4.11, and Scroll-driven Animations 1, 2.2.1 and 3.3.1): auto, none, a
// <dashed-ident>, scroll() or view().
func parseTimeline(it []token) (AnimationTimeline, bool) {
	t, ok := single(it)
	if !ok {
		return AnimationTimeline{}, false
	}
	switch {
	case t.isIdent("auto"):
		return AnimationTimeline{}, true
	case t.isIdent("none"):
		return AnimationTimeline{Kind: TimelineNone}, true
	case dashedIdent(t):
		return AnimationTimeline{Kind: TimelineNamed, Name: t.value}, true
	case t.typ == tFunction && eqFold(t.value, "scroll"):
		return AnimationTimeline{Kind: TimelineScroll}, scrollArgs(significant(t.children))
	case t.typ == tFunction && eqFold(t.value, "view"):
		return AnimationTimeline{Kind: TimelineView}, viewArgs(significant(t.children))
	}
	return AnimationTimeline{}, false
}

// scrollArgs reports the arguments of scroll(): [<scroller> || <axis>]?
// (Scroll-driven Animations 1, 2.2.1).
func scrollArgs(args []token) bool {
	var scroller, axis bool
	for _, a := range args {
		switch k, _ := keyword([]token{a}); {
		case !scroller && (k == "root" || k == "nearest" || k == "self"):
			scroller = true
		case !axis && axisKeywords[k]:
			axis = true
		default:
			return false
		}
	}
	return true
}

// viewArgs reports the arguments of view(): [<axis> || <'view-timeline-
// inset'>]? (Scroll-driven Animations 1, 3.3.1).
func viewArgs(args []token) bool {
	if len(args) > 0 && args[0].typ == tIdent && axisKeywords[lower(args[0].value)] {
		return insetArgs(args[1:], true)
	}
	if len(args) > 0 && args[len(args)-1].typ == tIdent && axisKeywords[lower(args[len(args)-1].value)] {
		return insetArgs(args[:len(args)-1], true)
	}
	return insetArgs(args, true)
}

// insetArgs reports one view-timeline-inset item (Scroll-driven
// Animations 1, 3.4.3): one or two of auto or a <length-percentage>, or
// none of them when empty is set.
func insetArgs(args []token, empty bool) bool {
	if len(args) == 0 {
		return empty
	}
	if len(args) > 2 {
		return false
	}
	for _, a := range args {
		if a.isIdent("auto") {
			continue
		}
		if _, ok := parseLength(a, allowPct); !ok {
			return false
		}
	}
	return true
}

// parseRange parses an animation-range-start or -end item (Scroll-driven
// Animations 1, Appendix A): normal, a <length-percentage>, or a
// <timeline-range-name> and an optional <length-percentage>, whose
// default is def.
func parseRange(it []token, def float64) (AnimationRange, bool) {
	if len(it) == 1 && it[0].isIdent("normal") {
		return AnimationRange{Normal: true}, true
	}
	r := AnimationRange{Offset: Dimension{Kind: Percent, Value: def}}
	if len(it) > 0 && it[0].typ == tIdent {
		n, ok := rangeNames[lower(it[0].value)]
		if !ok {
			return AnimationRange{}, false
		}
		r.Name = n
		it = it[1:]
		if len(it) == 0 {
			return r, true
		}
	}
	if len(it) != 1 {
		return AnimationRange{}, false
	}
	d, ok := parseDim(it, allowPct, kwNoKw, false)
	if !ok {
		return AnimationRange{}, false
	}
	r.Offset = specOffset(d)
	return r, true
}

// specOffset is the offset of a range item: a percentage, or an absolute
// length in points; a relative length counts as its points at the
// initial font size, an offset only compared with zero.
func specOffset(d dimSpec) Dimension {
	if d.l.hasPct && !d.l.hasLen {
		return Dimension{Kind: Percent, Value: d.l.pct / 100}
	}
	return Dimension{Kind: Points, Value: d.l.points(lenCtx{fontSize: mediumFontSize, rootFontSize: mediumFontSize})}
}

// equalRange reports two equal range items.
func equalRange(a, b AnimationRange) bool {
	return a.Normal == b.Normal && a.Name == b.Name && a.Offset.Kind == b.Offset.Kind && a.Offset.Value == b.Offset.Value
}

// parseTimelineName parses a scroll-timeline-name or view-timeline-name
// item: none, as "", or a <dashed-ident>.
func parseTimelineName(it []token) (string, bool) {
	t, ok := single(it)
	if !ok {
		return "", false
	}
	if t.isIdent("none") {
		return "", true
	}
	return t.value, dashedIdent(t)
}

// eqItem is == for the comparable item types.
func eqItem[T comparable](a, b T) bool { return a == b }

// equalEasing reports two equal easing functions.
func equalEasing(a, b Easing) bool {
	return a.Kind == b.Kind && a.X1 == b.X1 && a.Y1 == b.Y1 && a.X2 == b.X2 && a.Y2 == b.Y2 &&
		a.Steps == b.Steps && a.Position == b.Position && slices.Equal(a.Stops, b.Stops)
}

// animationProps returns the animation longhands (CSS Animations 1, 4 and
// 2, 4; Scroll-driven Animations 1, Appendix A) and the timeline names
// (Scroll-driven Animations 1, 2.3.1 and 3.4.1), none inherited. They
// share one pointer of [Style], nil while every one is initial.
func animationProps() map[propID]propDef {
	return map[propID]propDef{
		pAnimationName: animList("animation-name", func(a *Animations) *[]string { return &a.Names },
			parseAnimationName, "", eqItem[string]),
		pAnimationDuration: animList("animation-duration", func(a *Animations) *[]float64 { return &a.Durations },
			parseDuration, -1, eqItem[float64]),
		pAnimationTimingFunction: animList("animation-timing-function", func(a *Animations) *[]Easing { return &a.Easings },
			parseEasingItem, easeEasing, equalEasing),
		pAnimationIterationCount: animList("animation-iteration-count", func(a *Animations) *[]float64 { return &a.Iterations },
			parseIterations, 1, eqItem[float64]),
		pAnimationDirection: animList("animation-direction", func(a *Animations) *[]AnimationDirection { return &a.Directions },
			keywordItem(directionKeywords), DirectionNormal, eqItem[AnimationDirection]),
		pAnimationFillMode: animList("animation-fill-mode", func(a *Animations) *[]FillMode { return &a.Fills },
			keywordItem(fillKeywords), FillNone, eqItem[FillMode]),
		pAnimationPlayState: animList("animation-play-state", func(a *Animations) *[]bool { return &a.Paused },
			keywordItem(playStateKeywords), false, eqItem[bool]),
		pAnimationDelay: animList("animation-delay", func(a *Animations) *[]float64 { return &a.Delays },
			parseDelay, 0, eqItem[float64]),
		pAnimationComposition: animList("animation-composition", func(a *Animations) *[]Composition { return &a.Compositions },
			keywordItem(compositionKeywords), CompositionReplace, eqItem[Composition]),
		pAnimationTimeline: animList("animation-timeline", func(a *Animations) *[]AnimationTimeline { return &a.Timelines },
			parseTimeline, AnimationTimeline{}, eqItem[AnimationTimeline]),
		pAnimationRangeStart: animList("animation-range-start", func(a *Animations) *[]AnimationRange { return &a.RangeStarts },
			func(it []token) (AnimationRange, bool) { return parseRange(it, 0) }, AnimationRange{Normal: true}, equalRange),
		pAnimationRangeEnd: animList("animation-range-end", func(a *Animations) *[]AnimationRange { return &a.RangeEnds },
			func(it []token) (AnimationRange, bool) { return parseRange(it, 1) }, AnimationRange{Normal: true}, equalRange),
		pScrollTimelineName: animList("scroll-timeline-name", func(a *Animations) *[]string { return &a.ScrollTimelineNames },
			parseTimelineName, "", eqItem[string]),
		pViewTimelineName: animList("view-timeline-name", func(a *Animations) *[]string { return &a.ViewTimelineNames },
			parseTimelineName, "", eqItem[string]),
	}
}
