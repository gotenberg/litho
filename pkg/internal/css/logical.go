package css

import "strings"

// The flow-relative properties of CSS Logical Properties 1 whose
// physical side depends on the inline direction: the inline-start side is
// the left one in a left-to-right element and the right one in a
// right-to-left one (CSS Logical Properties 1, 4, horizontal writing
// modes alone). The parser maps them onto their left-to-right sides; the
// cascade mirrors them in a right-to-left element (see [cascade]).

// isInlineLogical reports a property whose sides follow the inline
// direction.
func isInlineLogical(name string) bool {
	return strings.Contains(name, "inline-start") || strings.Contains(name, "inline-end") ||
		name == "margin-inline" || name == "padding-inline" || name == "inset-inline" ||
		name == "border-inline" || name == "border-inline-width" || name == "border-inline-style" || name == "border-inline-color"
}

// mirrorInline returns the longhand of the side opposite id across the
// inline axis: left for right and right for left, id itself for any
// other.
func mirrorInline(id propID) propID {
	if m, ok := inlineMirrors[id]; ok {
		return m
	}
	return id
}

// inlineMirrors maps each left or right longhand to the other side's.
var inlineMirrors = map[propID]propID{
	pMarginLeft: pMarginRight, pMarginRight: pMarginLeft,
	pPaddingLeft: pPaddingRight, pPaddingRight: pPaddingLeft,
	pLeft: pRight, pRight: pLeft,
	pBorderLeftWidth: pBorderRightWidth, pBorderRightWidth: pBorderLeftWidth,
	pBorderLeftStyle: pBorderRightStyle, pBorderRightStyle: pBorderLeftStyle,
	pBorderLeftColor: pBorderRightColor, pBorderRightColor: pBorderLeftColor,
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

// cascadedRTL reports whether the element whose declarations cands holds,
// sorted in cascade order, and whose parent's style is parent, lays out
// right to left: the direction the cascade gives it, which the
// flow-relative properties among cands map their sides by.
func cascadedRTL(cands []cand, parent *Style) bool {
	rtl := parent != nil && parent.Direction == RTL
	for _, c := range cands {
		for _, e := range c.d.entries {
			if e.id != pDirection {
				continue
			}
			switch v := e.v.(type) {
			case Direction:
				rtl = v == RTL
			case cssWide:
				rtl = v != wideInitial && parent != nil && parent.Direction == RTL
			}
		}
	}
	return rtl
}
