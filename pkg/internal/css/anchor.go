package css

import (
	"slices"
)

// This file holds the properties, functions and at-rule of CSS Anchor
// Positioning 1: anchor-name, anchor-scope, position-anchor,
// position-area, position-try-fallbacks, position-try-order, the
// position-try shorthand, position-visibility, the anchor() and
// anchor-size() functions in the inset, sizing and margin properties, the
// anchor-center self-alignment value and @position-try. The cascade
// computes them; the layout engine finds the anchors and resolves the
// functions once the anchors are laid out.

// AnchorPos holds the anchor positioning longhands of an element (CSS
// Anchor Positioning 1, 2 to 5), nil in a [Style] while every one is
// initial. It is shared and never mutated.
type AnchorPos struct {
	// Names is anchor-name (2.1), nil for none.
	Names []string
	// ScopeAll and ScopeNames are anchor-scope (2.1.1): all, or the names
	// scoped to the element's subtree, neither for none.
	ScopeAll   bool
	ScopeNames []string
	// Default is position-anchor (2.3).
	Default PositionAnchor
	// Area is position-area (3.1).
	Area PositionArea
	// Fallbacks is position-try-fallbacks (5.1), nil for none.
	Fallbacks []TryOption
	// Order is position-try-order (5.2).
	Order TryOrder
	// Visibility is position-visibility (5.5).
	Visibility PositionVisibility
}

// initial reports anchor positioning longhands that are all initial.
func (a *AnchorPos) initial() bool {
	return len(a.Names) == 0 && !a.ScopeAll && len(a.ScopeNames) == 0 && a.Default == PositionAnchor{} &&
		a.Area.None() && len(a.Fallbacks) == 0 && a.Order == TryNormal && a.Visibility == 0
}

// PositionAnchorKind is the kind of a position-anchor value (CSS Anchor
// Positioning 1, 2.3).
type PositionAnchorKind uint8

// The position-anchor kinds; the zero value is the initial normal.
const (
	AnchorNormal PositionAnchorKind = iota
	AnchorNone
	AnchorAuto
	AnchorNamed
	AnchorMatchParent
)

// PositionAnchor is a position-anchor value: its kind and, for
// AnchorNamed, the <anchor-name>.
type PositionAnchor struct {
	Kind PositionAnchorKind
	Name string
}

// AreaAxis is the axis a <position-area> keyword selects tracks in.
type AreaAxis uint8

// The axes of the position-area grid (CSS Anchor Positioning 1, 3.1.2): x
// and y are physical, block and inline those of the containing block's
// writing mode, or of the box's own for a self- keyword.
const (
	AreaX AreaAxis = iota
	AreaY
	AreaBlock
	AreaInline
)

// AreaRef is how a <position-area> keyword reads its tracks' order.
type AreaRef uint8

// The orders of a keyword's tracks: physical (left to right, top to
// bottom), logical in the containing block's writing mode, or logical in
// the box's own (the self- keywords).
const (
	AreaPhysical AreaRef = iota
	AreaLogical
	AreaSelf
)

// The tracks of one axis of the position-area grid, as a set: the start
// track (left or top for a physical keyword), the centre one, the end one.
const (
	TrackStart uint8 = 1 << iota
	TrackCenter
	TrackEnd
	TrackAll = TrackStart | TrackCenter | TrackEnd
)

// AreaSpan is the tracks a <position-area> selects in one axis.
type AreaSpan struct {
	Axis   AreaAxis
	Ref    AreaRef
	Tracks uint8
}

// PositionArea is a position-area value: the tracks of the two axes the
// value names, in the order of its grammar (CSS Anchor Positioning 1,
// 3.1.3). The zero value is none.
type PositionArea struct {
	A, B AreaSpan
}

// None reports position-area: none.
func (p PositionArea) None() bool { return p.A.Tracks == 0 }

// TryTactic is a <try-tactic> keyword (CSS Anchor Positioning 1, 5.1).
type TryTactic uint8

// The try tactics.
const (
	FlipBlock TryTactic = iota + 1
	FlipInline
	FlipStart
	FlipX
	FlipY
)

// TryOption is one entry of position-try-fallbacks (CSS Anchor
// Positioning 1, 5.1): a @position-try rule by name, try tactics, both,
// or a <position-area>.
type TryOption struct {
	// Rule is the <dashed-ident> of a @position-try rule, "" for none.
	Rule string
	// Tactics are applied in order.
	Tactics []TryTactic
	// Area is the <position-area> of an entry that is one, Area.None()
	// otherwise.
	Area PositionArea
	// Try is the position option of the rule Rule names, computed for the
	// element, nil when Rule is "" or names no rule: such an entry adds no
	// option.
	Try *TryStyle
}

// TryOrder is position-try-order (CSS Anchor Positioning 1, 5.2).
type TryOrder uint8

// The position-try-order values.
const (
	TryNormal TryOrder = iota
	TryMostWidth
	TryMostHeight
	TryMostBlockSize
	TryMostInlineSize
)

// PositionVisibility is position-visibility (CSS Anchor Positioning 1,
// 5.5) as a set of conditions; the zero value is the initial
// anchor-visible.
type PositionVisibility uint8

// The position-visibility conditions. VisibilityAlways is always, which
// hides the box under no condition.
const (
	VisibilityAnchorValid PositionVisibility = 1 << iota
	VisibilityAnchorVisible
	VisibilityNoOverflow
	VisibilityAlways
)

// Has reports whether the condition c hides the box.
func (v PositionVisibility) Has(c PositionVisibility) bool {
	if v == 0 {
		v = VisibilityAnchorVisible
	}
	return v&c != 0
}

// TryStyle holds the accepted @position-try properties of a position
// option (CSS Anchor Positioning 1, 5.3): the insets, margins, sizes and
// self-alignment indexed as in [Style], position-anchor and position-area.
type TryStyle struct {
	Inset                                                   [4]Dimension
	Margin                                                  [4]Dimension
	Width, Height, MinWidth, MinHeight, MaxWidth, MaxHeight Dimension
	MinWidthAuto, MinHeightAuto                             bool
	AlignSelf, JustifySelf                                  BoxAlign
	Default                                                 PositionAnchor
	Area                                                    PositionArea
}

// AnchorPositioning returns the element's anchor positioning longhands,
// the initial values when it sets none. The result is shared: never modify
// it.
func (s *Style) AnchorPositioning() *AnchorPos {
	if s.Anchor == nil {
		return &initialAnchor
	}
	return s.Anchor
}

var initialAnchor AnchorPos

// TryBase returns the accepted @position-try properties of the style: the
// fallback base styles of its position options list.
func (s *Style) TryBase() TryStyle {
	a := s.AnchorPositioning()
	return TryStyle{
		Inset:  [4]Dimension{s.Top, s.Right, s.Bottom, s.Left},
		Margin: s.Margin,
		Width:  s.Width, Height: s.Height, MinWidth: s.MinWidth, MinHeight: s.MinHeight,
		MaxWidth: s.MaxWidth, MaxHeight: s.MaxHeight,
		MinWidthAuto: s.MinWidthAuto, MinHeightAuto: s.MinHeightAuto,
		AlignSelf: s.AlignSelf, JustifySelf: s.JustifySelf,
		Default: a.Default, Area: a.Area,
	}
}

// WithTry returns a copy of the style holding the position option t in
// place of its own accepted @position-try properties. The receiver is not
// modified.
func (s *Style) WithTry(t *TryStyle) *Style {
	c := *s
	c.Top, c.Right, c.Bottom, c.Left = t.Inset[Top], t.Inset[Right], t.Inset[Bottom], t.Inset[Left]
	c.Margin = t.Margin
	c.Width, c.Height, c.MinWidth, c.MinHeight, c.MaxWidth, c.MaxHeight = t.Width, t.Height, t.MinWidth, t.MinHeight, t.MaxWidth, t.MaxHeight
	c.MinWidthAuto, c.MinHeightAuto = t.MinWidthAuto, t.MinHeightAuto
	c.AlignSelf, c.JustifySelf = t.AlignSelf, t.JustifySelf
	a := *s.AnchorPositioning()
	a.Default, a.Area = t.Default, t.Area
	c.Anchor = &a
	return &c
}

// anchorProps returns the longhands of CSS Anchor Positioning 1.
func anchorProps() map[propID]propDef {
	return map[propID]propDef{
		pAnchorName: anchorLonghand("anchor-name", parseAnchorNames, func(a *AnchorPos, v any) {
			a.Names, _ = v.([]string)
		}, func(dst, src *AnchorPos) { dst.Names = src.Names }),
		pAnchorScope: anchorLonghand("anchor-scope", parseAnchorScope, func(a *AnchorPos, v any) {
			sc, _ := v.(anchorScope)
			a.ScopeAll, a.ScopeNames = sc.all, sc.names
		}, func(dst, src *AnchorPos) { dst.ScopeAll, dst.ScopeNames = src.ScopeAll, src.ScopeNames }),
		pPositionAnchor: anchorLonghand("position-anchor", parsePositionAnchor, func(a *AnchorPos, v any) {
			a.Default, _ = v.(PositionAnchor)
		}, func(dst, src *AnchorPos) { dst.Default = src.Default }),
		pPositionArea: anchorLonghand("position-area", parsePositionAreaValue, func(a *AnchorPos, v any) {
			a.Area, _ = v.(PositionArea)
		}, func(dst, src *AnchorPos) { dst.Area = src.Area }),
		pPositionTryFallbacks: anchorLonghand("position-try-fallbacks", parseTryFallbacks, func(a *AnchorPos, v any) {
			a.Fallbacks, _ = v.([]TryOption)
		}, func(dst, src *AnchorPos) { dst.Fallbacks = src.Fallbacks }),
		pPositionTryOrder: anchorLonghand("position-try-order", parseTryOrder, func(a *AnchorPos, v any) {
			a.Order, _ = v.(TryOrder)
		}, func(dst, src *AnchorPos) { dst.Order = src.Order }),
		pPositionVisibility: anchorLonghand("position-visibility", parsePositionVisibility, func(a *AnchorPos, v any) {
			a.Visibility, _ = v.(PositionVisibility)
		}, func(dst, src *AnchorPos) { dst.Visibility = src.Visibility }),
	}
}

// anchorLonghand defines a non-inherited longhand held in [Style.Anchor]:
// set stores a parsed value into a copy, copy copies the field.
func anchorLonghand(name string, parse func(v []token) (any, bool), set func(*AnchorPos, any), cp func(dst, src *AnchorPos)) propDef {
	return propDef{
		name:  name,
		parse: func(v []token, _ *declCtx) (any, bool) { return parse(v) },
		compute: func(_ *styler, st *Style, v any) {
			setAnchor(st, func(a *AnchorPos) { set(a, v) })
		},
		copyFrom: func(dst, src *Style) {
			if dst.Anchor == nil && src.Anchor == nil {
				// The common case, both initial, allocates nothing.
				return
			}
			setAnchor(dst, func(a *AnchorPos) { cp(a, src.AnchorPositioning()) })
		},
	}
}

// setAnchor applies f to a copy of st's anchor positioning longhands,
// storing nil when every one ends initial. The shared value is never
// mutated.
func setAnchor(st *Style, f func(*AnchorPos)) {
	n := AnchorPos{}
	if st.Anchor != nil {
		n = *st.Anchor
	}
	f(&n)
	if n.initial() {
		st.Anchor = nil
		return
	}
	st.Anchor = &n
}

// anchorScope is a parsed anchor-scope value.
type anchorScope struct {
	all   bool
	names []string
}

// dashedList parses a comma-separated list of <dashed-ident>s.
func dashedList(v []token) ([]string, bool) {
	items, ok := commaList(v)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		t, ok := single(it)
		if !ok || !dashedIdent(t) {
			return nil, false
		}
		out = append(out, t.value)
	}
	return out, true
}

// parseAnchorNames parses anchor-name: none | <anchor-name># (CSS Anchor
// Positioning 1, 2.1).
func parseAnchorNames(v []token) (any, bool) {
	if k, ok := keyword(v); ok && k == "none" {
		return []string(nil), true
	}
	return dashedList(v)
}

// parseAnchorScope parses anchor-scope: none | all | <anchor-name># (CSS
// Anchor Positioning 1, 2.1.1).
func parseAnchorScope(v []token) (any, bool) {
	if k, ok := keyword(v); ok {
		switch k {
		case "none":
			return anchorScope{}, true
		case "all":
			return anchorScope{all: true}, true
		}
	}
	names, ok := dashedList(v)
	return anchorScope{names: names}, ok
}

// parsePositionAnchor parses position-anchor: normal | none | auto |
// <anchor-name> | match-parent (CSS Anchor Positioning 1, 2.3).
func parsePositionAnchor(v []token) (any, bool) {
	t, ok := single(v)
	if !ok || t.typ != tIdent {
		return nil, false
	}
	if dashedIdent(t) {
		return PositionAnchor{Kind: AnchorNamed, Name: t.value}, true
	}
	switch lower(t.value) {
	case "normal":
		return PositionAnchor{}, true
	case "none":
		return PositionAnchor{Kind: AnchorNone}, true
	case "auto":
		return PositionAnchor{Kind: AnchorAuto}, true
	case "match-parent":
		return PositionAnchor{Kind: AnchorMatchParent}, true
	}
	return nil, false
}

// areaKeyword is one keyword of the <position-area> grammar: its axis, or
// none for center, span-all and the start/end keywords that name no axis,
// its order, its tracks, and its group of the grammar.
type areaKeyword struct {
	axis      AreaAxis
	ambiguous bool
	ref       AreaRef
	tracks    uint8
	// group is the alternative of the grammar the keyword belongs to:
	// physical (with x- and y- keywords), logical, self-logical, the
	// short logical start/end, the short self-logical start/end; center and
	// span-all belong to every group.
	group areaGroup
}

type areaGroup uint8

const (
	groupAny areaGroup = iota
	groupPhysical
	groupLogical
	groupSelfLogical
	groupShort
	groupSelfShort
)

// areaKeywords maps the <position-area> keywords to what they select (CSS
// Anchor Positioning 1, 3.1.2).
var areaKeywords = func() map[string]areaKeyword {
	m := map[string]areaKeyword{
		"center":   {ambiguous: true, tracks: TrackCenter},
		"span-all": {ambiguous: true, tracks: TrackAll},
	}
	add := func(prefix string, axis AreaAxis, ambiguous bool, ref AreaRef, group areaGroup, start, end string) {
		m[prefix+start] = areaKeyword{axis: axis, ambiguous: ambiguous, ref: ref, group: group, tracks: TrackStart}
		m[prefix+end] = areaKeyword{axis: axis, ambiguous: ambiguous, ref: ref, group: group, tracks: TrackEnd}
		m["span-"+prefix+start] = areaKeyword{axis: axis, ambiguous: ambiguous, ref: ref, group: group, tracks: TrackStart | TrackCenter}
		m["span-"+prefix+end] = areaKeyword{axis: axis, ambiguous: ambiguous, ref: ref, group: group, tracks: TrackCenter | TrackEnd}
	}
	add("", AreaX, false, AreaPhysical, groupPhysical, "left", "right")
	add("", AreaY, false, AreaPhysical, groupPhysical, "top", "bottom")
	add("x-", AreaX, false, AreaLogical, groupPhysical, "start", "end")
	add("y-", AreaY, false, AreaLogical, groupPhysical, "start", "end")
	add("self-x-", AreaX, false, AreaSelf, groupPhysical, "start", "end")
	add("self-y-", AreaY, false, AreaSelf, groupPhysical, "start", "end")
	add("block-", AreaBlock, false, AreaLogical, groupLogical, "start", "end")
	add("inline-", AreaInline, false, AreaLogical, groupLogical, "start", "end")
	add("self-block-", AreaBlock, false, AreaSelf, groupSelfLogical, "start", "end")
	add("self-inline-", AreaInline, false, AreaSelf, groupSelfLogical, "start", "end")
	add("", AreaBlock, true, AreaLogical, groupShort, "start", "end")
	add("self-", AreaBlock, true, AreaSelf, groupSelfShort, "start", "end")
	return m
}()

// parsePositionArea parses a <position-area> of one or two keywords (CSS
// Anchor Positioning 1, 3.1.2): the two must come from one alternative of
// the grammar and name two axes; an ambiguous keyword takes the axis the
// other leaves, the first of two ambiguous ones the block axis; a single
// unambiguous keyword spans the other axis whole, a single ambiguous one
// is repeated.
func parsePositionArea(v []token) (PositionArea, bool) {
	if len(v) < 1 || len(v) > 2 {
		return PositionArea{}, false
	}
	kws := make([]areaKeyword, len(v))
	for i, t := range v {
		if t.typ != tIdent {
			return PositionArea{}, false
		}
		k, ok := areaKeywords[lower(t.value)]
		if !ok {
			return PositionArea{}, false
		}
		kws[i] = k
	}
	if len(kws) == 1 {
		k := kws[0]
		if k.ambiguous {
			kws = append(kws, k)
		} else {
			kws = append(kws, areaKeyword{ambiguous: true, tracks: TrackAll})
		}
	}
	a, b := kws[0], kws[1]
	if a.group != groupAny && b.group != groupAny && a.group != b.group {
		return PositionArea{}, false
	}
	group := max(a.group, b.group)
	if !a.ambiguous && !b.ambiguous {
		if a.axis == b.axis || (group == groupPhysical) != (a.axis <= AreaY) {
			return PositionArea{}, false
		}
		return PositionArea{A: a.span(), B: b.span()}, true
	}
	ref := AreaLogical
	switch group { //nolint:exhaustive // the logical groups read the containing block's writing mode
	case groupSelfLogical, groupSelfShort:
		ref = AreaSelf
	case groupPhysical:
		ref = AreaPhysical
	}
	resolve := func(k *areaKeyword, axis AreaAxis) {
		if k.ambiguous {
			k.axis, k.ambiguous = axis, false
			if k.group == groupAny {
				k.ref = ref
			}
		}
	}
	switch {
	case a.ambiguous && b.ambiguous:
		if group == groupPhysical {
			// center and span-all with each other.
			resolve(&a, AreaY)
			resolve(&b, AreaX)
		} else {
			resolve(&a, AreaBlock)
			resolve(&b, AreaInline)
		}
	case a.ambiguous:
		resolve(&a, otherAxis(b.axis))
	default:
		resolve(&b, otherAxis(a.axis))
	}
	if a.axis == b.axis {
		return PositionArea{}, false
	}
	return PositionArea{A: a.span(), B: b.span()}, true
}

func (k areaKeyword) span() AreaSpan { return AreaSpan{Axis: k.axis, Ref: k.ref, Tracks: k.tracks} }

// otherAxis returns the axis paired with a.
func otherAxis(a AreaAxis) AreaAxis {
	switch a {
	case AreaX:
		return AreaY
	case AreaY:
		return AreaX
	case AreaBlock:
		return AreaInline
	case AreaInline:
	}
	return AreaBlock
}

// parsePositionAreaValue parses position-area: none | <position-area>.
func parsePositionAreaValue(v []token) (any, bool) {
	if k, ok := keyword(v); ok && k == "none" {
		return PositionArea{}, true
	}
	p, ok := parsePositionArea(v)
	return p, ok
}

// tryTactics maps the <try-tactic> keywords (CSS Anchor Positioning 1,
// 5.1).
var tryTactics = map[string]TryTactic{
	"flip-block": FlipBlock, "flip-inline": FlipInline, "flip-start": FlipStart, "flip-x": FlipX, "flip-y": FlipY,
}

// maxTryOptions caps the entries of one position-try-fallbacks value: 64.
// Anti-DoS guard, not spec-mandated: each entry may lay the box out once
// more, CSS Anchor Positioning 1, 5.4 lets an implementation cap the list
// at five or more, and real styles list two to four.
const maxTryOptions = 64

// parseTryFallbacks parses position-try-fallbacks: none | [
// [<dashed-ident> || <try-tactic>] | <position-area> ]# (CSS Anchor
// Positioning 1, 5.1).
func parseTryFallbacks(v []token) (any, bool) {
	if k, ok := keyword(v); ok && k == "none" {
		return []TryOption(nil), true
	}
	items, ok := commaList(v)
	if !ok || len(items) > maxTryOptions {
		return nil, false
	}
	out := make([]TryOption, 0, len(items))
	for _, it := range items {
		o, ok := parseTryOption(it)
		if !ok {
			return nil, false
		}
		out = append(out, o)
	}
	return out, true
}

// parseTryOption parses one entry of position-try-fallbacks: a
// <position-area>, or <dashed-ident> || <try-tactic>, the name before or
// after the tactics, each tactic once.
func parseTryOption(it []token) (TryOption, bool) {
	if p, ok := parsePositionArea(it); ok {
		return TryOption{Area: p}, true
	}
	var o TryOption
	seen := map[TryTactic]bool{}
	for i, t := range it {
		if t.typ != tIdent {
			return TryOption{}, false
		}
		if dashedIdent(t) {
			if o.Rule != "" || i != 0 && i != len(it)-1 {
				return TryOption{}, false
			}
			o.Rule = t.value
			continue
		}
		tt, ok := tryTactics[lower(t.value)]
		if !ok || seen[tt] {
			return TryOption{}, false
		}
		seen[tt] = true
		o.Tactics = append(o.Tactics, tt)
	}
	return o, len(it) > 0
}

// parseTryOrder parses position-try-order: normal | <try-size> (CSS
// Anchor Positioning 1, 5.2).
func parseTryOrder(v []token) (any, bool) {
	k, ok := keyword(v)
	if !ok {
		return nil, false
	}
	o, ok := map[string]TryOrder{
		"normal": TryNormal, "most-width": TryMostWidth, "most-height": TryMostHeight,
		"most-block-size": TryMostBlockSize, "most-inline-size": TryMostInlineSize,
	}[k]
	return o, ok
}

// parsePositionVisibility parses position-visibility: always | [
// anchor-valid || anchor-visible || no-overflow ] (CSS Anchor Positioning
// 1, 5.5). The legacy value aliases anchors-valid and anchors-visible,
// which the clause lets a user agent accept, are not: Chromium parses
// neither anchor-valid nor anchors-valid.
func parsePositionVisibility(v []token) (any, bool) {
	if len(v) == 0 || len(v) > 3 {
		return nil, false
	}
	if k, ok := keyword(v); ok && k == "always" {
		return VisibilityAlways, true
	}
	var out PositionVisibility
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		var c PositionVisibility
		switch lower(t.value) {
		case "anchor-valid":
			c = VisibilityAnchorValid
		case "anchor-visible":
			c = VisibilityAnchorVisible
		case "no-overflow":
			c = VisibilityNoOverflow
		default:
			return nil, false
		}
		if out&c != 0 {
			return nil, false
		}
		out |= c
	}
	if out == VisibilityAnchorVisible {
		// The initial value is the zero value.
		out = 0
	}
	return out, true
}

// positionTryShorthand is position-try: <'position-try-order'>?
// <'position-try-fallbacks'> (CSS Anchor Positioning 1, 5.3).
func positionTryShorthand() shorthandDef {
	return shorthandDef{
		longhands: []propID{pPositionTryOrder, pPositionTryFallbacks},
		expand: func(v []token, _ *declCtx) ([]entry, bool) {
			order := any(TryNormal)
			if len(v) > 1 {
				if o, ok := parseTryOrder(v[:1]); ok {
					order, v = o, v[1:]
				}
			}
			f, ok := parseTryFallbacks(v)
			if !ok {
				return nil, false
			}
			return []entry{{pPositionTryOrder, order}, {pPositionTryFallbacks, f}}, true
		},
	}
}

// AnchorSide is an <anchor-side> of anchor() (CSS Anchor Positioning 1,
// 3.2).
type AnchorSide uint8

// The anchor sides. center is AnchorPercent at one half.
const (
	AnchorInside AnchorSide = iota
	AnchorOutside
	AnchorTop
	AnchorLeft
	AnchorRight
	AnchorBottom
	AnchorStart
	AnchorEnd
	AnchorSelfStart
	AnchorSelfEnd
	AnchorPercent
)

// AnchorDim is an <anchor-size> of anchor-size() (CSS Anchor Positioning
// 1, 4.1), AnchorDimAuto when omitted: the axis of the property.
type AnchorDim uint8

// The anchor sizes.
const (
	AnchorDimAuto AnchorDim = iota
	AnchorWidth
	AnchorHeight
	AnchorBlock
	AnchorInline
	AnchorSelfBlock
	AnchorSelfInline
)

// AnchorFn is one anchor() or anchor-size() function of a length.
type AnchorFn struct {
	// Size reports anchor-size(), anchor() otherwise.
	Size bool
	// Name is the <anchor-name>, "" for the default anchor.
	Name string
	// Side and Percent are the <anchor-side> of anchor(): Percent is the
	// fraction for AnchorPercent.
	Side    AnchorSide
	Percent float64
	// Dim is the <anchor-size> of anchor-size().
	Dim AnchorDim
}

// physicalVertical reports a side keyword of the vertical axis, and
// physicalHorizontal one of the horizontal axis.
func (f AnchorFn) physicalVertical() bool   { return f.Side == AnchorTop || f.Side == AnchorBottom }
func (f AnchorFn) physicalHorizontal() bool { return f.Side == AnchorLeft || f.Side == AnchorRight }

var anchorSides = map[string]AnchorSide{
	"inside": AnchorInside, "outside": AnchorOutside, "top": AnchorTop, "left": AnchorLeft,
	"right": AnchorRight, "bottom": AnchorBottom, "start": AnchorStart, "end": AnchorEnd,
	"self-start": AnchorSelfStart, "self-end": AnchorSelfEnd,
}

var anchorDims = map[string]AnchorDim{
	"width": AnchorWidth, "height": AnchorHeight, "block": AnchorBlock, "inline": AnchorInline,
	"self-block": AnchorSelfBlock, "self-inline": AnchorSelfInline,
}

// anchorFn parses anchor( <anchor-name>? && <anchor-side>,
// <length-percentage>? ) or, with size set, anchor-size( [ <anchor-name>
// || <anchor-size> ]? , <length-percentage>? ) (CSS Anchor Positioning 1,
// 3.2 and 4.1) into a node whose child, when present, is the fallback.
func (p *calcParser) anchorFn(t *token, size bool, depth int) (*calcNode, calcType, bool) {
	parts := splitCommas(t.children)
	commas := 0
	for _, c := range t.children {
		if c.typ == tComma {
			commas++
		}
	}
	if len(parts) == 0 && size && commas == 0 {
		parts = [][]token{nil}
	}
	if len(parts) == 0 || len(parts) > 2 || commas != len(parts)-1 {
		return nil, calcType{}, false
	}
	f := &AnchorFn{Size: size}
	head := significant(parts[0])
	var fallback []token
	if len(parts) == 2 {
		fallback = parts[1]
		if len(significant(fallback)) == 0 {
			return nil, calcType{}, false
		}
	}
	if size && len(parts) == 1 && len(head) > 0 && head[0].typ != tIdent {
		// anchor-size(<length-percentage>): the leading optional part is
		// omitted, and its comma with it.
		fallback, head = parts[0], nil
	}
	named, keyed := false, false
	for _, h := range head {
		switch {
		case h.typ == tIdent && dashedIdent(h) && !named:
			f.Name, named = h.value, true
		case !size && !keyed && h.typ == tIdent:
			s, ok := anchorSides[lower(h.value)]
			if !ok {
				if !eqFold(h.value, "center") {
					return nil, calcType{}, false
				}
				s, f.Percent = AnchorPercent, 0.5
			}
			f.Side, keyed = s, true
		case !size && !keyed && h.typ == tPercentage:
			f.Side, f.Percent, keyed = AnchorPercent, clampLen(h.num/100), true
		case !size && !keyed && h.typ == tFunction && isCalc(&h):
			v, ty, ok := calcConst(&h, nil)
			if !ok || ty != typePct {
				return nil, calcType{}, false
			}
			f.Side, f.Percent, keyed = AnchorPercent, clampLen(v/100), true
		case size && !keyed && h.typ == tIdent:
			d, ok := anchorDims[lower(h.value)]
			if !ok {
				return nil, calcType{}, false
			}
			f.Dim, keyed = d, true
		default:
			return nil, calcType{}, false
		}
	}
	if !size && !keyed {
		return nil, calcType{}, false
	}
	n := &calcNode{op: opAnchor, anchor: f}
	if fallback != nil {
		if f := significant(fallback); len(f) == 1 && f[0].typ == tNumber && f[0].num == 0 {
			// A unitless zero is a <length> (CSS Values 4, 6.1).
			n.kids = []*calcNode{{op: opLen, unit: uPt}}
		} else {
			fb, ty, ok := p.sum(fallback, depth+1)
			if !ok || ty != typeLength {
				return nil, calcType{}, false
			}
			n.kids = []*calcNode{fb}
		}
	}
	if !p.count() {
		return nil, calcType{}, false
	}
	return n, typeLength, true
}

// anchored reports whether the tree holds an anchor() or anchor-size().
func (n *calcNode) anchored() bool {
	if n.op == opAnchor {
		return true
	}
	return slices.ContainsFunc(n.kids, (*calcNode).anchored)
}

// Anchored reports whether d holds an anchor() or anchor-size() function,
// which the layout engine resolves with [Dimension.ResolveAnchors].
func (d Dimension) Anchored() bool { return d.expr != nil && d.expr.anchored() }

// ResolveAnchors returns d with each anchor() and anchor-size() replaced
// by the length in points f gives it, or by its fallback when f gives
// none (CSS Anchor Positioning 1, 3.2.1 and 4.1.1). It returns false when
// a function f gives no length for has no fallback either: the
// declaration is then invalid at computed-value time. A d that holds no
// anchor function is returned as it is.
func (d Dimension) ResolveAnchors(f func(AnchorFn) (float64, bool)) (Dimension, bool) {
	if !d.Anchored() {
		return d, true
	}
	n, ok := d.expr.substituteAnchors(f, 0)
	if !ok {
		return Dimension{}, false
	}
	return treeDim(n), true
}

// maxAnchorDepth bounds the fallbacks nested in anchor functions that
// substitution follows. Anti-DoS guard, not spec-mandated: the calculation
// tree's own depth cap already bounds it; this keeps the walk's stack
// small.
const maxAnchorDepth = maxCalcDepth

// substituteAnchors returns a copy of the tree with its anchor functions
// substituted by f, false when one resolves to nothing and has no
// fallback.
func (n *calcNode) substituteAnchors(f func(AnchorFn) (float64, bool), depth int) (*calcNode, bool) {
	if depth > maxAnchorDepth {
		return nil, false
	}
	if n.op == opAnchor {
		if v, ok := f(*n.anchor); ok {
			return &calcNode{op: opLen, unit: uPt, v: v}, true
		}
		if len(n.kids) == 0 {
			return nil, false
		}
		return n.kids[0].substituteAnchors(f, depth+1)
	}
	if !n.anchored() {
		return n, true
	}
	c := *n
	c.kids = make([]*calcNode, len(n.kids))
	for i, k := range n.kids {
		s, ok := k.substituteAnchors(f, depth+1)
		if !ok {
			return nil, false
		}
		c.kids[i] = s
	}
	return &c, true
}

// UsesDefaultAnchor reports whether d holds an anchor function that names
// no anchor, which takes the default anchor.
func (d Dimension) UsesDefaultAnchor() bool {
	return d.expr != nil && d.expr.namesDefault()
}

func (n *calcNode) namesDefault() bool {
	if n.op == opAnchor && n.anchor.Name == "" {
		return true
	}
	return slices.ContainsFunc(n.kids, (*calcNode).namesDefault)
}

// MapAnchors returns d with each anchor function replaced by what f makes
// of it, its fallback kept: a try tactic's change of the sides and sizes
// the functions name (CSS Anchor Positioning 1, 5.4.2).
func (d Dimension) MapAnchors(f func(AnchorFn) AnchorFn) Dimension {
	if !d.Anchored() {
		return d
	}
	d.expr = d.expr.mapAnchors(f)
	return d
}

func (n *calcNode) mapAnchors(f func(AnchorFn) AnchorFn) *calcNode {
	if !n.anchored() {
		return n
	}
	c := *n
	if n.op == opAnchor {
		a := f(*n.anchor)
		c.anchor = &a
	}
	c.kids = make([]*calcNode, len(n.kids))
	for i, k := range n.kids {
		c.kids[i] = k.mapAnchors(f)
	}
	return &c
}

// treeDim returns the Dimension a calculation tree of lengths in points
// and percentages stands for: a length, a percentage or a sum of both when
// it folds to one, else the tree.
func treeDim(n *calcNode) Dimension {
	if !n.relative() {
		return Pts(clampLen(censor(n.eval(&calcScope{}))))
	}
	if lin, ok := n.linear(nil); ok {
		if !lin.hasLen {
			return Dimension{Kind: Percent, Value: clampLen(censor(lin.pct / 100))}
		}
		return Dimension{Kind: Calc, Value: clampLen(censor(lin.u[uPt])), Fraction: clampLen(censor(lin.pct / 100))}
	}
	return Dimension{Kind: Calc, expr: n}
}

// anchorFunctionToken reports whether t is an anchor() or, with size set,
// an anchor-size() function, or a math function holding one, which the
// properties that take them parse with [anchorLength].
func anchorFunctionToken(t *token) bool {
	if t.typ != tFunction {
		return false
	}
	switch lower(t.value) {
	case "anchor", "anchor-size":
		return true
	}
	if !isCalc(t) {
		return false
	}
	for i := range t.children {
		if anchorFunctionToken(&t.children[i]) {
			return true
		}
	}
	return false
}

// anchorLength parses a <length-percentage> that holds anchor functions:
// anchor() when insets is set, anchor-size() always (CSS Anchor
// Positioning 1, 3.2 and 4.1).
func anchorLength(t token, insets bool) (length, bool) {
	p := &calcParser{pctLength: true, anchor: insets, anchorSize: true}
	var n *calcNode
	var ty calcType
	var ok bool
	if isCalc(&t) {
		n, ty, ok = p.fn(&t, 0)
	} else {
		n, ty, ok = p.sum([]token{t}, 0)
	}
	if !ok || ty != typeLength {
		return length{}, false
	}
	return length{expr: n, hasLen: true, hasPct: n.hasPct()}, true
}

// anchorAxis is the axis of the property an anchor function is used in.
type anchorAxis uint8

const (
	axisNone anchorAxis = iota
	axisVertical
	axisHorizontal
)

// withAnchors returns def, a length longhand stored at f, taking the
// anchor functions as well: anchor() and anchor-size() when axis is that
// of an inset property, anchor-size() alone otherwise. At computed-value
// time a function on a box that is not absolutely positioned, or an
// anchor() whose physical side is of the other axis, resolves to its
// fallback; without one the declaration is invalid at computed-value
// time and the longhand takes its initial value (CSS Anchor Positioning 1,
// 3.2.1 and 4.1.1; CSS Values 5, invalid at computed-value time).
func withAnchors(def propDef, f func(*Style) *Dimension, insetAxis anchorAxis) propDef {
	parse, compute := def.parse, def.compute
	def.parse = func(v []token, c *declCtx) (any, bool) {
		// The token is read in place: a copy whose address is taken would
		// cost every length declaration an allocation.
		if len(v) == 1 && v[0].typ == tFunction && anchorFunctionToken(&v[0]) {
			l, ok := anchorLength(v[0], insetAxis != axisNone)
			if !ok {
				return nil, false
			}
			return dimSpec{kind: Points, l: l}, true
		}
		return parse(v, c)
	}
	def.compute = func(s *styler, st *Style, v any) {
		compute(s, st, v)
		d := *f(st)
		if !d.Anchored() {
			return
		}
		abs := st.Position == Absolute || st.Position == Fixed
		r, ok := d.ResolveAnchors(func(a AnchorFn) (float64, bool) {
			return 0, false
		})
		if abs {
			r, ok = d, true
			if insetAxis != axisNone {
				r, ok = d.checkAxes(insetAxis)
			}
		}
		if !ok {
			def.copyFrom(st, initialStyle)
			return
		}
		*f(st) = r
	}
	return def
}

// checkAxes returns d with each anchor() whose physical side is of another
// axis than axis replaced by its fallback, false when one has none.
func (d Dimension) checkAxes(axis anchorAxis) (Dimension, bool) {
	wrong := func(a AnchorFn) bool {
		return !a.Size && (axis == axisVertical && a.physicalHorizontal() || axis == axisHorizontal && a.physicalVertical())
	}
	n, ok := d.expr.substituteWrong(wrong, 0)
	if !ok {
		return Dimension{}, false
	}
	if !n.anchored() {
		return treeDim(n), true
	}
	return Dimension{Kind: Calc, expr: n}, true
}

func (n *calcNode) substituteWrong(wrong func(AnchorFn) bool, depth int) (*calcNode, bool) {
	if depth > maxAnchorDepth {
		return nil, false
	}
	if !n.anchored() {
		return n, true
	}
	if n.op == opAnchor && wrong(*n.anchor) {
		if len(n.kids) == 0 {
			return nil, false
		}
		return n.kids[0].substituteWrong(wrong, depth+1)
	}
	c := *n
	c.kids = make([]*calcNode, len(n.kids))
	for i, k := range n.kids {
		s, ok := k.substituteWrong(wrong, depth+1)
		if !ok {
			return nil, false
		}
		c.kids[i] = s
	}
	return &c, true
}

// anchorLengths makes the inset properties take anchor() and
// anchor-size(), and the sizing and margin properties anchor-size() (CSS
// Anchor Positioning 1, 3.2 and 4.1).
func anchorLengths() {
	insets := [4]struct {
		id   propID
		f    func(*Style) *Dimension
		axis anchorAxis
	}{
		{pTop, func(s *Style) *Dimension { return &s.Top }, axisVertical},
		{pRight, func(s *Style) *Dimension { return &s.Right }, axisHorizontal},
		{pBottom, func(s *Style) *Dimension { return &s.Bottom }, axisVertical},
		{pLeft, func(s *Style) *Dimension { return &s.Left }, axisHorizontal},
	}
	for _, in := range insets {
		props[in.id] = withAnchors(props[in.id], in.f, in.axis)
	}
	sizes := []struct {
		id propID
		f  func(*Style) *Dimension
	}{
		{pWidth, func(s *Style) *Dimension { return &s.Width }},
		{pHeight, func(s *Style) *Dimension { return &s.Height }},
		{pMinWidth, func(s *Style) *Dimension { return &s.MinWidth }},
		{pMinHeight, func(s *Style) *Dimension { return &s.MinHeight }},
		{pMaxWidth, func(s *Style) *Dimension { return &s.MaxWidth }},
		{pMaxHeight, func(s *Style) *Dimension { return &s.MaxHeight }},
	}
	for _, sz := range sizes {
		props[sz.id] = withAnchors(props[sz.id], sz.f, axisNone)
	}
	for i := range 4 {
		side := i
		props[pMarginTop+propID(i)] = withAnchors(props[pMarginTop+propID(i)], func(s *Style) *Dimension { return &s.Margin[side] }, axisNone)
	}
}

// positionTryRule is a @position-try rule (CSS Anchor Positioning 1, 5.3):
// its name and the declarations of accepted properties it holds.
type positionTryRule struct {
	name  string
	decls []Declaration
	cond  *mediaCond
	layer int
}

// acceptedTry are the accepted @position-try properties (CSS Anchor
// Positioning 1, 5.3): the inset, margin, sizing and self-alignment
// properties, position-anchor and position-area.
var acceptedTry = func() [numProps]bool {
	var a [numProps]bool
	for _, id := range []propID{
		pTop, pRight, pBottom, pLeft, pMarginTop, pMarginRight, pMarginBottom, pMarginLeft,
		pWidth, pHeight, pMinWidth, pMinHeight, pMaxWidth, pMaxHeight, pAlignSelf, pJustifySelf,
		pPositionAnchor, pPositionArea,
	} {
		a[id] = true
	}
	return a
}()

// parsePositionTryRule parses @position-try <dashed-ident> {
// <declaration-list> }. A declaration of a property the rule does not
// accept, or one marked !important, is dropped; the rule stays (CSS Anchor
// Positioning 1, 5.3).
func parsePositionTryRule(prelude, block []token, c *declCtx) (*positionTryRule, bool) {
	pre := significant(prelude)
	if len(pre) != 1 || !dashedIdent(pre[0]) {
		return nil, false
	}
	r := &positionTryRule{name: pre[0].value}
	for _, d := range parseDeclList(block, c) {
		if d.Important || d.isCustom {
			continue
		}
		ids := longhandsOf(d.Property)
		if len(ids) == 0 || slices.ContainsFunc(ids, func(id propID) bool { return !acceptedTry[id] }) {
			continue
		}
		r.decls = append(r.decls, d)
	}
	return r, true
}

// tryStyleCost is what one position option holds once computed: a
// [TryStyle] and its share of the fallback list.
const tryStyleCost = 640

// attachTries computes, for each entry of st's position-try-fallbacks that
// names a @position-try rule, the position option the rule makes: the
// element's cascade with the rule's declarations in the position fallback
// origin, above the author's normal declarations and below the important
// ones and the animations (CSS Anchor Positioning 1, 5.3). A rule's
// flow-relative properties take their sides from the element's writing
// mode and direction. winners, imp and vars are the element's cascade.
func (cs *computeState) attachTries(st *Style, winners *[numProps]any, imp *[numProps]bool, vars map[string][]token, parent *Style, isRoot bool) {
	a := st.Anchor
	if a == nil || len(cs.r.positionTries) == 0 || !slices.ContainsFunc(a.Fallbacks, func(o TryOption) bool { return o.Rule != "" }) {
		return
	}
	flow := newFlowMap(st.WritingMode, st.Direction == RTL)
	mapped := !flow.identity()
	out := slices.Clone(a.Fallbacks)
	for i := range out {
		r := cs.r.positionTries[out[i].Rule]
		if r == nil {
			continue
		}
		if !cs.mem.Take(1, tryStyleCost, "position options") {
			return
		}
		w := *winners
		for j := range r.decls {
			d := &r.decls[j]
			es := d.entries
			if len(d.pending) > 0 {
				es = cs.substituteDecl(d, vars)
			}
			for _, e := range es {
				id := e.id
				if mapped && isFlowRelative(d.Property) {
					id = flow.mapID(id)
				}
				if _, animated := w[id].(animatedValue); imp[id] || animated || !acceptedTry[id] {
					continue
				}
				w[id] = e.v
			}
		}
		tmp := new(Style)
		cs.computeInto(tmp, &w, parent, isRoot)
		t := tmp.TryBase()
		out[i].Try = &t
	}
	n := *a
	n.Fallbacks = out
	st.Anchor = &n
}
