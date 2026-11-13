package css

import "strings"

// The flow-relative properties of CSS Logical Properties 1 take their
// physical sides from the element's writing mode and direction (CSS
// Logical Properties 1, 4). The parser maps them onto the sides of a
// horizontal-tb, left-to-right element: block-start onto top, inline-end
// onto right, block-end onto bottom, inline-start onto left, the inline
// size onto the width and the block size onto the height; the cascade
// maps them again for an element of another direction or writing mode
// (see [cascade]). A later declaration of the same physical side wins,
// logical or physical (4).
//
// The layout engine lays a box of a vertical writing mode out as an
// orthogonal flow: its content is a horizontal layout turned a quarter
// turn (see [WritingMode]). The vertical mapping applies to the root of
// that flow alone, whose box sits in its horizontal parent; inside it the
// horizontal mapping draws the flow-relative sides where they belong once
// turned.

// isFlowRelative reports a flow-relative property name, one of CSS
// Logical Properties 1, 4 and 5.
func isFlowRelative(name string) bool {
	return strings.Contains(name, "-inline") || strings.Contains(name, "-block") ||
		strings.HasPrefix(name, "inline-") || strings.HasPrefix(name, "block-") ||
		strings.HasPrefix(name, "border-start-") || strings.HasPrefix(name, "border-end-")
}

// flowMap maps the longhands a flow-relative property is parsed onto, the
// sides of a horizontal-tb, left-to-right element, onto the sides of an
// element of writing mode wm and direction rtl.
type flowMap struct {
	// side holds the physical side of each side a horizontal
	// left-to-right element would take, by [Side].
	side [4]Side
	// swap reports a vertical writing mode, where the inline size is the
	// height.
	swap bool
}

// identity reports a map that changes no side.
func (m flowMap) identity() bool {
	return !m.swap && m.side == [4]Side{Top, Right, Bottom, Left}
}

// newFlowMap returns the flowMap of writing mode wm and direction rtl
// (CSS Writing Modes 4, 6.1 and 6.2).
func newFlowMap(wm WritingMode, rtl bool) flowMap {
	blockStart, inlineStart := Top, Left
	switch wm {
	case HorizontalTB:
		if rtl {
			inlineStart = Right
		}
	case VerticalRL, SidewaysRL:
		blockStart, inlineStart = Right, Top
		if rtl {
			inlineStart = Bottom
		}
	case VerticalLR:
		blockStart, inlineStart = Left, Top
		if rtl {
			inlineStart = Bottom
		}
	case SidewaysLR:
		blockStart, inlineStart = Left, Bottom
		if rtl {
			inlineStart = Top
		}
	}
	return flowMap{
		side: [4]Side{Top: blockStart, Right: opposite(inlineStart), Bottom: opposite(blockStart), Left: inlineStart},
		swap: wm.Vertical(),
	}
}

// opposite returns the side across the box from s.
func opposite(s Side) Side { return (s + 2) % 4 }

// sideGroups are the first longhands of the groups of four indexed by
// [Side].
var sideGroups = []propID{pMarginTop, pPaddingTop, pTop, pBorderTopStyle, pBorderTopWidth, pBorderTopColor}

// sizePairs are the width-like longhands and their height-like partners.
var sizePairs = [][2]propID{{pWidth, pHeight}, {pMinWidth, pMinHeight}, {pMaxWidth, pMaxHeight}}

// mapID returns the longhand id maps onto under m.
func (m flowMap) mapID(id propID) propID {
	for _, g := range sideGroups {
		if id >= g && id < g+4 {
			return g + propID(m.side[id-g])
		}
	}
	if id >= pRadiusTopLeft && id < pRadiusTopLeft+4 {
		// A corner joins a block side and an inline side.
		v, h := cornerSides(Corner(id - pRadiusTopLeft))
		return pRadiusTopLeft + propID(cornerOf(m.side[v], m.side[h]))
	}
	if m.swap {
		for _, p := range sizePairs {
			switch id {
			case p[0]:
				return p[1]
			case p[1]:
				return p[0]
			}
		}
	}
	return id
}

// cornerSides returns the vertical and horizontal sides corner c joins.
func cornerSides(c Corner) (Side, Side) {
	switch c {
	case TopRight:
		return Top, Right
	case BottomRight:
		return Bottom, Right
	case BottomLeft:
		return Bottom, Left
	}
	return Top, Left
}

// cornerOf returns the corner two adjacent sides join, in either order.
func cornerOf(a, b Side) Corner {
	if a == Left || a == Right {
		a, b = b, a
	}
	switch {
	case a == Top && b == Right:
		return TopRight
	case a == Bottom && b == Right:
		return BottomRight
	case a == Bottom && b == Left:
		return BottomLeft
	}
	return TopLeft
}

// logicalRadii are the flow-relative corner radii (CSS Logical
// Properties 1, 4.6), mapped onto the corners of a horizontal-tb,
// left-to-right element: block side first, then inline side.
var logicalRadii = map[string]propID{
	"border-start-start-radius": pRadiusTopLeft + propID(TopLeft),
	"border-start-end-radius":   pRadiusTopLeft + propID(TopRight),
	"border-end-start-radius":   pRadiusTopLeft + propID(BottomLeft),
	"border-end-end-radius":     pRadiusTopLeft + propID(BottomRight),
}

// logicalAliases are the flow-relative border longhands, mapped onto
// their physical sides in a left-to-right horizontal writing mode.
func logicalAliases() map[string]propID {
	out := map[string]propID{}
	for _, s := range []struct {
		name string
		side Side
	}{{"block-start", Top}, {"block-end", Bottom}, {"inline-start", Left}, {"inline-end", Right}} {
		i := propID(s.side)
		out["border-"+s.name+"-width"] = pBorderTopWidth + i
		out["border-"+s.name+"-style"] = pBorderTopStyle + i
		out["border-"+s.name+"-color"] = pBorderTopColor + i
	}
	return out
}

// logicalShorthands are the flow-relative border shorthands (CSS Logical
// Properties 1, 4.5): one side, or both sides of an axis.
func logicalShorthands() map[string]shorthandDef {
	both := func(a, b Side) shorthandDef {
		ia, ib := propID(a), propID(b)
		return shorthandDef{
			longhands: []propID{pBorderTopWidth + ia, pBorderTopStyle + ia, pBorderTopColor + ia, pBorderTopWidth + ib, pBorderTopStyle + ib, pBorderTopColor + ib},
			expand: func(v []token, c *declCtx) ([]entry, bool) {
				w, s, col, ok := borderSide(v, c)
				if !ok {
					return nil, false
				}
				return []entry{
					{pBorderTopWidth + ia, w},
					{pBorderTopStyle + ia, s},
					{pBorderTopColor + ia, col},
					{pBorderTopWidth + ib, w},
					{pBorderTopStyle + ib, s},
					{pBorderTopColor + ib, col},
				}, true
			},
		}
	}
	return map[string]shorthandDef{
		"border-block-start":  borderSideShorthand(Top),
		"border-block-end":    borderSideShorthand(Bottom),
		"border-inline-start": borderSideShorthand(Left),
		"border-inline-end":   borderSideShorthand(Right),
		"border-block":        both(Top, Bottom),
		"border-inline":       both(Left, Right),
		"border-block-width":  pairShorthand(pBorderTopWidth, pBorderBottomWidth),
		"border-block-style":  pairShorthand(pBorderTopStyle, pBorderBottomStyle),
		"border-block-color":  pairShorthand(pBorderTopColor, pBorderBottomColor),
		"border-inline-width": pairShorthand(pBorderLeftWidth, pBorderRightWidth),
		"border-inline-style": pairShorthand(pBorderLeftStyle, pBorderRightStyle),
		"border-inline-color": pairShorthand(pBorderLeftColor, pBorderRightColor),
	}
}

// cascadedFlow returns the flowMap of the element whose declarations
// cands holds, sorted in cascade order, and whose parent's style is
// parent: the direction and writing mode the cascade gives it, which the
// flow-relative properties among cands map their sides by. An element
// whose parent is of a vertical writing mode maps as a horizontal one, as
// the layout draws it inside a turned flow.
func cascadedFlow(cands []cand, parent *Style) flowMap {
	rtl := parent != nil && parent.Direction == RTL
	wm := HorizontalTB
	if parent != nil {
		wm = parent.WritingMode
	}
	for _, c := range cands {
		for _, e := range c.d.entries {
			switch e.id { //nolint:exhaustive // direction and writing-mode alone
			case pDirection:
				switch v := e.v.(type) {
				case Direction:
					rtl = v == RTL
				case cssWide:
					rtl = v != wideInitial && parent != nil && parent.Direction == RTL
				}
			case pWritingMode:
				switch v := e.v.(type) {
				case WritingMode:
					wm = v
				case cssWide:
					wm = HorizontalTB
					if v != wideInitial && parent != nil {
						wm = parent.WritingMode
					}
				}
			}
		}
	}
	if parent != nil && parent.WritingMode.Vertical() {
		wm = HorizontalTB
	}
	return newFlowMap(wm, rtl)
}

// inlineSide is the specified inline-start or inline-end value of float or
// clear.
type inlineSide struct{ end bool }

// flowRelative adds the inline-start and inline-end values to def, float
// or clear (CSS Logical Properties 1, 2.1): set stores the physical side
// they compute to, the left one for the start of a left-to-right
// containing block and for the end of a right-to-left one. The parent's
// direction stands for the containing block's, as the parent holds a float
// in flow.
func flowRelative(def propDef, set func(st *Style, end, rtl bool)) propDef {
	parse, compute := def.parse, def.compute
	def.parse = func(v []token, c *declCtx) (any, bool) {
		if k, ok := keyword(v); ok && (k == "inline-start" || k == "inline-end") {
			return inlineSide{end: k == "inline-end"}, true
		}
		return parse(v, c)
	}
	def.compute = func(s *styler, st *Style, v any) {
		if side, ok := v.(inlineSide); ok {
			set(st, side.end, s.parent != nil && s.parent.Direction == RTL)
			return
		}
		compute(s, st, v)
	}
	return def
}
