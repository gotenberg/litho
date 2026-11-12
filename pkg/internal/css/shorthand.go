package css

import "maps"

// shorthandDef defines a shorthand: the longhands it sets and how it
// expands a value into them. A longhand the value leaves out is reset to
// its initial value (CSS Cascade 4, 1.2).
type shorthandDef struct {
	longhands []propID
	expand    func(v []token, c *declCtx) ([]entry, bool)
}

// shorthands maps every shorthand and multi-longhand alias to its
// definition.
var shorthands map[string]shorthandDef

func ids(first propID, n int) []propID {
	out := make([]propID, n)
	for i := range out {
		out[i] = first + propID(i)
	}
	return out
}

// boxShorthand expands a 1-to-4 value box shorthand onto the longhands of
// top, right, bottom and left (CSS Box 4, 3.1).
func boxShorthand(longhands [4]propID) shorthandDef {
	return shorthandDef{
		longhands: longhands[:],
		expand: func(v []token, c *declCtx) ([]entry, bool) {
			if len(v) < 1 || len(v) > 4 {
				return nil, false
			}
			vals := make([]any, len(v))
			for i, t := range v {
				pv, ok := props[longhands[0]].parse([]token{t}, c)
				if !ok {
					return nil, false
				}
				vals[i] = pv
			}
			pick := [4][4]int{{0, 0, 0, 0}, {0, 1, 0, 1}, {0, 1, 2, 1}, {0, 1, 2, 3}}[len(v)-1]
			out := make([]entry, 4)
			for i := range out {
				out[i] = entry{id: longhands[i], v: vals[pick[i]]}
			}
			return out, true
		},
	}
}

// pairShorthand expands a 1-or-2 value logical shorthand (margin-block,
// padding-inline): start then end.
func pairShorthand(start, end propID) shorthandDef {
	return shorthandDef{
		longhands: []propID{start, end},
		expand: func(v []token, c *declCtx) ([]entry, bool) {
			if len(v) < 1 || len(v) > 2 {
				return nil, false
			}
			a, ok := props[start].parse(v[:1], c)
			if !ok {
				return nil, false
			}
			b := a
			if len(v) == 2 {
				if b, ok = props[end].parse(v[1:], c); !ok {
					return nil, false
				}
			}
			return []entry{{start, a}, {end, b}}, true
		},
	}
}

// borderSide parses <line-width> || <line-style> || <color> (CSS
// Backgrounds 3, 4.4) into width, style and colour values, initial for
// those left out.
func borderSide(v []token, c *declCtx) (width, style, colour any, ok bool) {
	if len(v) < 1 || len(v) > 3 {
		return nil, nil, nil, false
	}
	width, style, colour = wideInitial, wideInitial, wideInitial
	var w, s, col bool
	for _, t := range v {
		one := []token{t}
		if !s {
			if pv, ok := props[pBorderTopStyle].parse(one, c); ok {
				style, s = pv, true
				continue
			}
		}
		if !w {
			if pv, ok := parseBorderWidth(one, c); ok {
				width, w = pv, true
				continue
			}
		}
		if !col {
			if pv, ok := parseColor(t); ok {
				colour, col = pv, true
				continue
			}
		}
		return nil, nil, nil, false
	}
	return width, style, colour, true
}

func borderSideShorthand(side Side) shorthandDef {
	i := propID(side)
	return shorthandDef{
		longhands: []propID{pBorderTopWidth + i, pBorderTopStyle + i, pBorderTopColor + i},
		expand: func(v []token, c *declCtx) ([]entry, bool) {
			w, s, col, ok := borderSide(v, c)
			if !ok {
				return nil, false
			}
			return []entry{{pBorderTopWidth + i, w}, {pBorderTopStyle + i, s}, {pBorderTopColor + i, col}}, true
		},
	}
}

func initShorthands() {
	shorthands = map[string]shorthandDef{
		"margin":       boxShorthand([4]propID{pMarginTop, pMarginRight, pMarginBottom, pMarginLeft}),
		"padding":      boxShorthand([4]propID{pPaddingTop, pPaddingRight, pPaddingBottom, pPaddingLeft}),
		"inset":        boxShorthand([4]propID{pTop, pRight, pBottom, pLeft}),
		"border-width": boxShorthand([4]propID{pBorderTopWidth, pBorderRightWidth, pBorderBottomWidth, pBorderLeftWidth}),
		"border-style": boxShorthand([4]propID{pBorderTopStyle, pBorderRightStyle, pBorderBottomStyle, pBorderLeftStyle}),
		"border-color": boxShorthand([4]propID{pBorderTopColor, pBorderRightColor, pBorderBottomColor, pBorderLeftColor}),

		"margin-block":   pairShorthand(pMarginTop, pMarginBottom),
		"margin-inline":  pairShorthand(pMarginLeft, pMarginRight),
		"padding-block":  pairShorthand(pPaddingTop, pPaddingBottom),
		"padding-inline": pairShorthand(pPaddingLeft, pPaddingRight),
		"inset-block":    pairShorthand(pTop, pBottom),
		"inset-inline":   pairShorthand(pLeft, pRight),

		"border-top":    borderSideShorthand(Top),
		"border-right":  borderSideShorthand(Right),
		"border-bottom": borderSideShorthand(Bottom),
		"border-left":   borderSideShorthand(Left),
		"border": {
			longhands: append(append(append(ids(pBorderTopStyle, 4), ids(pBorderTopWidth, 4)...), ids(pBorderTopColor, 4)...), ids(pBorderImageSource, 5)...),
			expand: func(v []token, c *declCtx) ([]entry, bool) {
				w, s, col, ok := borderSide(v, c)
				if !ok {
					return nil, false
				}
				out := make([]entry, 0, 17)
				for i := range propID(4) {
					out = append(out, entry{pBorderTopStyle + i, s}, entry{pBorderTopWidth + i, w}, entry{pBorderTopColor + i, col})
				}
				// CSS Backgrounds 3, 4.4: border also resets border-image
				// to its initial value.
				for i := range propID(5) {
					out = append(out, entry{pBorderImageSource + i, wideInitial})
				}
				return out, true
			},
		},
		"border-radius": {longhands: ids(pRadiusTopLeft, 4), expand: expandRadius},
		"outline":       {longhands: ids(pOutlineStyle, 3), expand: expandOutline},
		"background":    {longhands: append(ids(pBgColor, 7), pBgAttachment), expand: expandBackground},
		"font": {
			longhands: []propID{pFontStyle, pFontVariant, pFontWeight, pFontSize, pLineHeight, pFontFamily, pFontStretch, pFontKerning},
			expand:    expandFont,
		},
		"list-style": {longhands: []propID{pListStyleType, pListStylePosition, pListStyleImage}, expand: expandListStyle},
		"text-decoration": {
			longhands: []propID{pTextDecoration, pTextDecorationStyle, pTextDecorationColor, pTextDecorationThickness},
			expand:    expandTextDecoration,
		},
		"word-break": {
			longhands: []propID{pWordBreak, pOverflowWrap},
			expand: func(v []token, c *declCtx) ([]entry, bool) {
				// CSS Text 3, 5.2: the legacy break-word value is
				// word-break: normal with overflow-wrap: anywhere.
				if k, ok := keyword(v); ok && k == "break-word" {
					return []entry{{pWordBreak, WordBreakNormal}, {pOverflowWrap, Anywhere}}, true
				}
				pv, ok := props[pWordBreak].parse(v, c)
				if !ok {
					return nil, false
				}
				return []entry{{pWordBreak, pv}}, true
			},
		},
		"border-image":      {longhands: ids(pBorderImageSource, 5), expand: expandBorderImage(pBorderImageSource)},
		"mask-border":       {longhands: ids(pMaskBorderSource, 5), expand: expandBorderImage(pMaskBorderSource)},
		"page-break-before": legacyBreak(pBreakBefore),
		"page-break-after":  legacyBreak(pBreakAfter),
	}
	maps.Copy(shorthands, flexShorthands())
	maps.Copy(shorthands, gridShorthands())
	maps.Copy(shorthands, textOverflowShorthands())
	maps.Copy(shorthands, logicalShorthands())
	maps.Copy(shorthands, textFontShorthands())
	maps.Copy(shorthands, multicolShorthands())
	// word-break is a shorthand for its legacy break-word value: the name
	// resolves through the shorthand alone.
	delete(longhandByName, "word-break")
}

// legacyBreak maps page-break-before and page-break-after onto
// break-before and break-after (CSS Fragmentation 3, 3.4): always is page.
func legacyBreak(id propID) shorthandDef {
	return shorthandDef{
		longhands: []propID{id},
		expand: func(v []token, _ *declCtx) ([]entry, bool) {
			k, ok := keyword(v)
			if !ok {
				return nil, false
			}
			b, ok := map[string]Break{
				"auto": BreakAuto, "always": Page, "avoid": BreakAvoid, "left": LeftPage, "right": RightPage,
			}[k]
			return []entry{{id, b}}, ok
		},
	}
}

// expandRadius expands border-radius (CSS Backgrounds 3, 5.1): one to four
// horizontal radii, optionally a slash and one to four vertical radii.
func expandRadius(v []token, _ *declCtx) ([]entry, bool) {
	hs, vs := v, []token(nil)
	for i, t := range v {
		if t.isDelim('/') {
			hs, vs = v[:i], v[i+1:]
			if len(vs) == 0 {
				return nil, false
			}
			break
		}
	}
	parse4 := func(l []token) ([4]length, bool) {
		var out [4]length
		if len(l) < 1 || len(l) > 4 {
			return out, false
		}
		vals := make([]length, len(l))
		for i, t := range l {
			x, ok := parseLength(t, allowPct|nonNeg)
			if !ok {
				return out, false
			}
			vals[i] = x
		}
		pick := [4][4]int{{0, 0, 0, 0}, {0, 1, 0, 1}, {0, 1, 2, 1}, {0, 1, 2, 3}}[len(l)-1]
		for i := range out {
			out[i] = vals[pick[i]]
		}
		return out, true
	}
	h, ok := parse4(hs)
	if !ok {
		return nil, false
	}
	vv := h
	if vs != nil {
		if vv, ok = parse4(vs); !ok {
			return nil, false
		}
	}
	out := make([]entry, 4)
	for i := range out {
		out[i] = entry{pRadiusTopLeft + propID(i), radiusSpec{h: h[i], v: vv[i]}}
	}
	return out, true
}

// systemFonts are the system font keywords of the font shorthand (CSS
// Fonts 4, 2.8).
var systemFonts = map[string]bool{
	"caption": true, "icon": true, "menu": true, "message-box": true, "small-caption": true, "status-bar": true,
}

// expandFont expands the font shorthand (CSS Fonts 4, 2.8): up to four of
// style, variant, weight and stretch keyword (normal matching any), the
// size, an optional slash and line height, then the
// families. A system font keyword resets every longhand with a warning.
func expandFont(v []token, c *declCtx) ([]entry, bool) {
	out := []entry{
		{pFontStyle, wideInitial},
		{pFontVariant, wideInitial},
		{pFontWeight, wideInitial},
		{pFontSize, wideInitial},
		{pLineHeight, wideInitial},
		{pFontFamily, wideInitial},
		{pFontStretch, wideInitial},
		// The shorthand resets font-kerning, which it cannot set (CSS
		// Fonts 4, 2.8).
		{pFontKerning, wideInitial},
	}
	if k, ok := keyword(v); ok && systemFonts[k] {
		c.warn("unsupported-css-value", "font: the system font %s is not supported and computes to the initial font", k)
		return out, true
	}
	i := 0
	var style, variant, weight, stretch bool
	for n := 0; i < len(v) && n < 4; n++ {
		t := v[i]
		one := v[i : i+1]
		if t.isIdent("normal") {
			i++
			continue
		}
		if !style {
			if pv, ok := parseFontStyle(one, c); ok {
				out[0].v, style = pv, true
				i++
				continue
			}
		}
		if !variant && t.isIdent("small-caps") {
			out[1].v, variant = SmallCaps, true
			i++
			continue
		}
		if !weight && (t.typ == tNumber || t.typ == tIdent) {
			if pv, ok := parseFontWeight(one, c); ok {
				out[2].v, weight = pv, true
				i++
				continue
			}
		}
		if p, ok := stretchKeywords[lower(t.value)]; ok && !stretch && t.typ == tIdent {
			out[6].v, stretch = p, true
			i++
			continue
		}
		break
	}
	if i >= len(v) {
		return nil, false
	}
	size, ok := parseFontSize(v[i:i+1], c)
	if !ok {
		return nil, false
	}
	out[3].v = size
	i++
	if i < len(v) && v[i].isDelim('/') {
		if i+1 >= len(v) {
			return nil, false
		}
		lh, ok := parseLineHeight(v[i+1:i+2], c)
		if !ok {
			return nil, false
		}
		out[4].v = lh
		i += 2
	}
	fam, ok := parseFontFamily(v[i:], c)
	if !ok {
		return nil, false
	}
	out[5].v = fam
	return out, true
}

// expandListStyle expands list-style (CSS Lists 3, 3.4): type, position
// and image in any order; none sets whichever of type and image is left.
func expandListStyle(v []token, c *declCtx) ([]entry, bool) {
	if len(v) < 1 || len(v) > 3 {
		return nil, false
	}
	var typ, pos, img any
	nones := 0
	for _, t := range v {
		one := []token{t}
		if t.isIdent("none") {
			nones++
			continue
		}
		if pos == nil {
			if pv, ok := props[pListStylePosition].parse(one, c); ok {
				pos = pv
				continue
			}
		}
		if img == nil && t.typ != tIdent {
			if pv, ok := parseImage(one, c); ok {
				img = pv
				continue
			}
		}
		if typ == nil {
			if pv, ok := parseListStyleType(one, c); ok {
				typ = pv
				continue
			}
		}
		return nil, false
	}
	for ; nones > 0; nones-- {
		switch {
		case typ == nil:
			typ = ListNone
		case img == nil:
			img = ""
		default:
			return nil, false
		}
	}
	if typ == nil {
		typ = wideInitial
	}
	if pos == nil {
		pos = wideInitial
	}
	if img == nil {
		img = wideInitial
	}
	return []entry{{pListStyleType, typ}, {pListStylePosition, pos}, {pListStyleImage, img}}, true
}

// decorationStyles are the text-decoration-style keywords.
var decorationStyles = map[string]DecorationStyle{
	"solid": DecorationSolid, "double": DecorationDouble, "dotted": DecorationDotted, "dashed": DecorationDashed, "wavy": DecorationWavy,
}

// expandTextDecoration expands text-decoration (CSS Text Decoration 3,
// 2.4, with the thickness of level 4) onto its line, style, colour and
// thickness.
func expandTextDecoration(v []token, c *declCtx) ([]entry, bool) {
	var lines []token
	var style, colour, thick any = wideInitial, wideInitial, wideInitial
	seenStyle, seenColour, seenThick := false, false, false
	for _, t := range v {
		switch {
		case t.typ == tIdent && !seenStyle && hasKey(decorationStyles, lower(t.value)):
			style, seenStyle = decorationStyles[lower(t.value)], true
		case t.typ == tIdent && (lower(t.value) == "underline" || lower(t.value) == "overline" ||
			lower(t.value) == "line-through" || lower(t.value) == "blink" || lower(t.value) == "none"):
			lines = append(lines, t)
		default:
			if col, ok := parseColor(t); ok && !seenColour {
				colour, seenColour = col, true
				continue
			}
			if d, ok := parseDim([]token{t}, allowPct|allowMixed, kwThickness, false); ok && !seenThick {
				thick, seenThick = d, true
				continue
			}
			return nil, false
		}
	}
	var line any = wideInitial
	if len(lines) > 0 {
		d, ok := parseDecorationLine(lines, c)
		if !ok {
			return nil, false
		}
		line = d
	}
	return []entry{{pTextDecoration, line}, {pTextDecorationStyle, style}, {pTextDecorationColor, colour}, {pTextDecorationThickness, thick}}, true
}

// hasKey reports whether m holds k.
func hasKey[V any](m map[string]V, k string) bool {
	_, ok := m[k]
	return ok
}
