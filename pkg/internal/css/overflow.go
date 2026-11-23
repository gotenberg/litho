package css

// overflowAxis is the computed value of overflow-x or overflow-y (CSS
// Overflow 3, 3.1). scroll and auto are one value: paged output prints a
// scroll container at its initial scroll position, clipped, whichever
// asked for it; overlay is a legacy alias of auto, as Chromium parses it.
type overflowAxis uint8

// The overflow-x and overflow-y values.
const (
	axisVisible overflowAxis = iota
	axisHidden
	axisClip
	axisScroll
)

// overflowKeywords maps the overflow-x and overflow-y keywords to their
// value (CSS Overflow 3, 3.1).
var overflowKeywords = map[string]overflowAxis{
	"visible": axisVisible, "hidden": axisHidden, "clip": axisClip,
	"scroll": axisScroll, "auto": axisScroll, "overlay": axisScroll,
}

// scrollable reports a value that makes a box a scroll container.
func (a overflowAxis) scrollable() bool { return a == axisHidden || a == axisScroll }

// overflowAxisProp defines overflow-x or overflow-y, which f selects: one
// keyword of [overflowKeywords], not inherited. The overflow shorthand
// sets both, the first value for x and the second, when given, for y;
// overflow-inline and overflow-block map onto them by writing mode (CSS
// Logical Properties 1, 4).
func overflowAxisProp(name string, f func(*Style) *overflowAxis) propDef {
	return propDef{
		name: name,
		parse: func(v []token, _ *declCtx) (any, bool) {
			t, ok := single(v)
			if !ok || t.typ != tIdent {
				return nil, false
			}
			a, ok := overflowKeywords[lower(t.value)]
			return a, ok
		},
		compute: func(_ *styler, st *Style, v any) {
			if a, ok := v.(overflowAxis); ok {
				*f(st) = a
			}
		},
		copyFrom: field(f),
	}
}

// fixupOverflow applies the rule of CSS Overflow 3, 3.1 that computes one
// axis from the other: beside an axis that is hidden, scroll or auto,
// visible computes to auto and clip to hidden, so a box is a scroll
// container in both axes or in neither. It then sets the box's Overflow.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 computes
//     overflow-x: hidden alone to hidden auto, and clips its content in
//     both axes, on screen and in print; it reports overflow-x: clip
//     beside overflow-y: hidden as clip, but clips both axes at the
//     padding box and ignores overflow-clip-margin there, as hidden does.
func fixupOverflow(st *Style) {
	x, y := st.overflowX, st.overflowY
	switch {
	case x.scrollable() && !y.scrollable():
		y = scrollPartner(y)
	case y.scrollable() && !x.scrollable():
		x = scrollPartner(x)
	}
	st.overflowX, st.overflowY = x, y
	st.Overflow = OverflowVisible
	if x != axisVisible || y != axisVisible {
		st.Overflow = OverflowHidden
	}
}

// scrollPartner returns what a visible or clip axis computes to beside a
// scrollable one (CSS Overflow 3, 3.1).
func scrollPartner(a overflowAxis) overflowAxis {
	if a == axisClip {
		return axisHidden
	}
	return axisScroll
}

// ScrollContainer reports an overflow-x or overflow-y of scroll or auto.
// Paged media prints such a box clipped, as Chromium does, and it stays a
// scroll container, which is monolithic in a fragmentation context (CSS
// Fragmentation 3, 4.1), as Chromium lays it out in columns. A box of
// overflow hidden is a scroll container too (CSS Overflow 3, 3.1) but
// fragments: [Style.ClipOnly] tells it from a box that clips without
// being one.
func (s *Style) ScrollContainer() bool {
	return s.Overflow == OverflowHidden && (s.overflowX == axisScroll || s.overflowY == axisScroll)
}

// ClipOnly reports a box that clips its content in one axis or both
// without being a scroll container: overflow clip, beside visible or
// clip. It establishes no formatting context (CSS Overflow 3, 3.1), so it
// fragments as a block in flow does.
func (s *Style) ClipOnly() bool {
	return s.Overflow == OverflowHidden && (s.overflowX == axisClip || s.overflowY == axisClip)
}

// ClipsX reports a box that clips what overflows its sides: an overflow-x
// other than visible.
func (s *Style) ClipsX() bool {
	return s.Overflow == OverflowHidden && (s.overflowX != axisVisible || s.overflowY == axisVisible)
}

// ClipsY reports a box that clips what overflows its top and bottom: an
// overflow-y other than visible.
func (s *Style) ClipsY() bool {
	return s.Overflow == OverflowHidden && (s.overflowY != axisVisible || s.overflowX == axisVisible)
}

// SetClipOnly sets the box's overflow to clip in both axes (CSS Overflow
// 3, 3.1), as the overflow property does.
func (s *Style) SetClipOnly() {
	s.Overflow, s.overflowX, s.overflowY = OverflowHidden, axisClip, axisClip
}
