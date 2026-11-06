package css

import "math"

// propID names a longhand. The cascade computes longhands in this order:
// font-size first, since em units resolve against it, then color, since
// currentColor resolves to it.
type propID uint8

const (
	pFontSize propID = iota
	pColor
	pDisplay
	pPosition
	pFloat
	pClear
	pVisibility
	pOverflow
	pBoxSizing
	pOpacity
	pWidth
	pHeight
	pMinWidth
	pMinHeight
	pMaxWidth
	pMaxHeight
	pTop
	pRight
	pBottom
	pLeft
	pMarginTop
	pMarginRight
	pMarginBottom
	pMarginLeft
	pPaddingTop
	pPaddingRight
	pPaddingBottom
	pPaddingLeft
	pBorderTopStyle
	pBorderRightStyle
	pBorderBottomStyle
	pBorderLeftStyle
	pBorderTopWidth
	pBorderRightWidth
	pBorderBottomWidth
	pBorderLeftWidth
	pBorderTopColor
	pBorderRightColor
	pBorderBottomColor
	pBorderLeftColor
	pRadiusTopLeft
	pRadiusTopRight
	pRadiusBottomRight
	pRadiusBottomLeft
	pBgColor
	pBgImage
	pBgRepeat
	pBgPosition
	pBgSize
	pBgOrigin
	pBgClip
	pFontFamily
	pFontWeight
	pFontStyle
	pFontVariant
	pLineHeight
	pTextAlign
	pTextDecoration
	pTextAlignLast
	pTextDecorationColor
	pTextDecorationStyle
	pTextDecorationThickness
	pTextUnderlineOffset
	pTextIndent
	pTextTransform
	pLetterSpacing
	pWordSpacing
	pWhiteSpace
	pWordBreak
	pOverflowWrap
	pLineBreak
	pWordSpaceTransform
	pTabSize
	pVerticalAlign
	pDirection
	pUnicodeBidi
	pListStyleType
	pListStylePosition
	pListStyleImage
	pBorderCollapse
	pBorderSpacing
	pCaptionSide
	pEmptyCells
	pTableLayout
	pBreakBefore
	pBreakAfter
	pBreakInside
	pOrphans
	pWidows
	pOutlineStyle
	pOutlineWidth
	pOutlineColor
	pOutlineOffset
	pBoxShadow
	pObjectFit
	pObjectPosition
	pContain
	pZIndex
	pTransform
	pTransformOrigin
	pContent
	pCounterReset
	pCounterIncrement
	pCounterSet
	pQuotes
	numProps
)

// parseFn parses the significant component values of a value into a
// specified value.
type parseFn func(v []token, c *declCtx) (any, bool)

// propDef defines a longhand.
type propDef struct {
	name      string
	inherited bool
	parse     parseFn
	// compute sets the longhand's computed value from a specified value.
	compute func(s *styler, st *Style, v any)
	// copyFrom copies the longhand's computed value.
	copyFrom func(dst, src *Style)
	// initial, when set, is the specified initial value: the cascade
	// computes it instead of copying [Initial]'s field, for an initial
	// value that depends on other properties (medium, currentColor).
	initial any
}

// styler computes one element's style.
type styler struct {
	parent *Style
	lc     lenCtx
	// root is the root element's style, nil while computing the root.
	root *Style
}

// field builds the copy function of a longhand stored at field.
func field[T any](f func(*Style) *T) func(dst, src *Style) {
	return func(dst, src *Style) { *f(dst) = *f(src) }
}

// single returns the one component value of v.
func single(v []token) (token, bool) {
	if len(v) != 1 {
		return token{}, false
	}
	return v[0], true
}

// keyword returns the lowercased ident v consists of.
func keyword(v []token) (string, bool) {
	t, ok := single(v)
	if !ok || t.typ != tIdent {
		return "", false
	}
	return lower(t.value), true
}

// enumProp defines a keyword longhand.
func enumProp[T ~uint8](name string, inh bool, f func(*Style) *T, vals map[string]T) propDef {
	return propDef{
		name: name, inherited: inh,
		parse: func(v []token, _ *declCtx) (any, bool) {
			k, ok := keyword(v)
			if !ok {
				return nil, false
			}
			x, ok := vals[k]
			return x, ok
		},
		compute: func(_ *styler, st *Style, v any) {
			if x, ok := v.(T); ok {
				*f(st) = x
			}
		},
		copyFrom: field(f),
	}
}

// dimSpec is a specified length or keyword.
type dimSpec struct {
	kind DimKind
	l    length
	num  float64
}

// parseDim parses a length-percentage, one of the keywords kws, or, with
// numbers set, a number.
func parseDim(v []token, f lengthFlags, kws map[string]DimKind, numbers bool) (dimSpec, bool) {
	t, ok := single(v)
	if !ok {
		return dimSpec{}, false
	}
	if t.typ == tIdent {
		k, ok := kws[lower(t.value)]
		return dimSpec{kind: k}, ok
	}
	if numbers {
		if t.typ == tNumber || (t.typ == tFunction && isCalc(&t)) {
			if n, ok := parseNumber(t); ok {
				if f&nonNeg != 0 && n < 0 {
					return dimSpec{}, false
				}
				return dimSpec{kind: Number, num: n}, true
			}
		}
	}
	l, ok := parseLength(t, f)
	if !ok {
		return dimSpec{}, false
	}
	return dimSpec{kind: Points, l: l}, true
}

// computeDim computes a dimSpec against the element's lengths.
func (s *styler) computeDim(d dimSpec, nonNegative bool) Dimension {
	switch d.kind { //nolint:exhaustive // the other kinds take the default
	case Points:
		return d.l.dim(s.lc, nonNegative)
	case Number:
		return Dimension{Kind: Number, Value: clampLen(d.num)}
	}
	return Dimension{Kind: d.kind}
}

// dimProp defines a length longhand stored at f.
func dimProp(name string, inh bool, f func(*Style) *Dimension, lf lengthFlags, kws map[string]DimKind, numbers bool) propDef {
	return propDef{
		name: name, inherited: inh,
		parse: func(v []token, _ *declCtx) (any, bool) {
			d, ok := parseDim(v, lf, kws, numbers)
			return d, ok
		},
		compute: func(s *styler, st *Style, v any) {
			if d, ok := v.(dimSpec); ok {
				*f(st) = s.computeDim(d, lf&nonNeg != 0)
			}
		},
		copyFrom: field(f),
	}
}

// colorProp defines a colour longhand stored at f, slot its bit in
// [Style] currentColor: a currentColor value stays a keyword the element
// resolves against its own color, even when a child inherits it
// explicitly (CSS Color 4, 4.4).
func colorProp(name string, f func(*Style) *Color, slot colorSlots, initial any) propDef {
	return propDef{
		name: name,
		parse: func(v []token, _ *declCtx) (any, bool) {
			t, ok := single(v)
			if !ok {
				return nil, false
			}
			c, ok := parseColor(t)
			return c, ok
		},
		compute: func(_ *styler, st *Style, v any) {
			if c, ok := v.(colorValue); ok {
				if c.current {
					*f(st) = st.Color
					st.currentColor |= slot
				} else {
					*f(st) = c.c
					st.currentColor &^= slot
				}
			}
		},
		copyFrom: func(dst, src *Style) {
			*f(dst) = *f(src)
			dst.currentColor = dst.currentColor&^slot | src.currentColor&slot
			if dst.currentColor&slot != 0 {
				*f(dst) = dst.Color
			}
		},
		initial: initial,
	}
}

var (
	// kwThickness are the keywords of text-decoration-thickness:
	// from-font computes to Normal.
	kwThickness = map[string]DimKind{"auto": Auto, "from-font": Normal}
	kwAuto      = map[string]DimKind{"auto": Auto}
	kwNormal    = map[string]DimKind{"normal": Normal}
	kwNoKw      = map[string]DimKind{}
	// kwSizing are the intrinsic sizing keywords of CSS Sizing 3, 3.1,
	// which compute to auto in this version.
	kwSizing = map[string]DimKind{
		"auto": Auto, "min-content": Auto, "max-content": Auto, "fit-content": Auto,
		"-moz-fit-content": Auto, "-webkit-fit-content": Auto, "stretch": Auto,
		"-webkit-fill-available": Auto, "-moz-available": Auto,
	}
	kwMinSizing = map[string]DimKind{
		"auto": Points, "min-content": Points, "max-content": Points, "fit-content": Points,
	}
	kwMaxSizing = map[string]DimKind{
		"none": None, "min-content": None, "max-content": None, "fit-content": None,
	}
)

// sizeProp defines width or height: auto, a non-negative length or
// percentage, or an intrinsic sizing keyword computed to auto with a
// warning.
func sizeProp(name string, f func(*Style) *Dimension, kws map[string]DimKind) propDef {
	def := dimProp(name, false, f, allowPct|nonNeg, kws, false)
	base := def.parse
	def.parse = func(v []token, c *declCtx) (any, bool) {
		if k, ok := keyword(v); ok && k != "auto" && k != "none" {
			if _, known := kws[k]; known {
				c.warn("unsupported-css-value", "%s: %s is not supported and computes to its initial value", name, k)
			}
		}
		return base(v, c)
	}
	return def
}

var displays = map[string]Display{
	"inline": Inline, "block": Block, "inline-block": InlineBlock, "list-item": ListItem,
	"table": Table, "inline-table": InlineTable, "table-row-group": TableRowGroup,
	"table-header-group": TableHeaderGroup, "table-footer-group": TableFooterGroup,
	"table-row": TableRow, "table-column-group": TableColumnGroup, "table-column": TableColumn,
	"table-cell": TableCell, "table-caption": TableCaption, "none": DisplayNone,
	"run-in": Block,
}

// displays3 are the CSS Display 3 values laid out as Block or InlineBlock
// in this version, with a warning.
var displays3 = map[string]Display{
	"flex": Block, "grid": Block, "flow-root": Block, "contents": Block,
	"inline-flex": InlineBlock, "inline-grid": InlineBlock, "-webkit-box": Block,
	"-webkit-flex": Block, "-ms-flexbox": Block, "-webkit-inline-box": InlineBlock,
	"-webkit-inline-flex": InlineBlock, "-ms-inline-flexbox": InlineBlock,
	"ruby": Inline, "ruby-text": Inline, "ruby-base": Inline,
}

// parseDisplay parses display (CSS Display 3, 2), the one- and two-keyword
// forms.
func parseDisplay(v []token, c *declCtx) (any, bool) {
	if k, ok := keyword(v); ok {
		if d, ok := displays[k]; ok {
			return d, true
		}
		if d, ok := displays3[k]; ok {
			c.warn("unsupported-css-value", "display: %s is laid out as %s", k, displayName(d))
			return d, true
		}
		return nil, false
	}
	if len(v) < 2 || len(v) > 3 {
		return nil, false
	}
	words := map[string]bool{}
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		words[lower(t.value)] = true
	}
	if len(words) != len(v) {
		return nil, false
	}
	inline := words["inline"]
	if words["list-item"] {
		// CSS Display 3, 2: list-item with an outer display and flow or
		// flow-root inside; litho lays flow-root out as flow.
		for w := range words {
			if w != "list-item" && w != "inline" && w != "block" && w != "flow" && w != "flow-root" {
				return nil, false
			}
		}
		if inline && words["block"] || words["flow"] && words["flow-root"] {
			return nil, false
		}
		if inline {
			return InlineListItem, true
		}
		return ListItem, true
	}
	switch {
	case words["flow"]:
		if inline {
			return Inline, true
		}
		return Block, true
	case words["table"]:
		if inline {
			return InlineTable, true
		}
		return Table, true
	case words["flow-root"], words["flex"], words["grid"]:
		d := Block
		if inline {
			d = InlineBlock
		}
		c.warn("unsupported-css-value", "a two-keyword display is laid out as %s", displayName(d))
		return d, true
	}
	return nil, false
}

func displayName(d Display) string {
	if d == InlineBlock {
		return "inline-block"
	}
	if d == Inline {
		return "inline"
	}
	return "block"
}

// parseOverflow parses overflow, overflow-x and overflow-y (CSS Overflow
// 3, 3): scroll and auto compute to visible on paged media, clip to
// hidden. Two values compute to hidden when either clips.
func parseOverflow(v []token, _ *declCtx) (any, bool) {
	if len(v) < 1 || len(v) > 2 {
		return nil, false
	}
	out := OverflowVisible
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		switch lower(t.value) {
		case "visible", "scroll", "auto", "overlay":
		case "hidden", "clip":
			out = OverflowHidden
		default:
			return nil, false
		}
	}
	return out, true
}

func parseOpacity(v []token, _ *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	n, _, ok := numberOrPct(t, false)
	if !ok {
		return nil, false
	}
	return clamp01(n), true
}

// borderWidthKeywords are thin, medium and thick (CSS Backgrounds 3,
// 4.3), at the widths browsers use.
var borderWidthKeywords = map[string]float64{"thin": 1, "medium": 3, "thick": 5}

func parseBorderWidth(v []token, _ *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	if t.typ == tIdent {
		px, ok := borderWidthKeywords[lower(t.value)]
		return length{pt: px * Pt, hasLen: true}, ok
	}
	l, ok := parseLength(t, nonNeg)
	return l, ok
}

var borderStyles = map[string]BorderStyle{
	"none": BorderNone, "hidden": BorderHidden, "dotted": Dotted, "dashed": Dashed,
	"solid": Solid, "double": Double, "groove": Groove, "ridge": Ridge,
	"inset": Inset, "outset": Outset,
}

// borderProps defines the style, width and colour longhands of one side.
func borderProps(side Side, name string) (style, width, colour propDef) {
	style = enumProp(name+"-style", false, func(s *Style) *BorderStyle { return &s.Border[side].Style }, borderStyles)
	width = propDef{
		name:  name + "-width",
		parse: parseBorderWidth,
		compute: func(s *styler, st *Style, v any) {
			// CSS Backgrounds 3, 4.3: the computed width keeps the
			// specified length whatever the style, as Blink, Gecko and
			// WebKit do; the layout engine uses 0 for none and hidden.
			if w, ok := computeLineWidth(s, v); ok {
				st.Border[side].Width = w
			}
		},
		copyFrom: field(func(s *Style) *float64 { return &s.Border[side].Width }),
		initial:  length{pt: 3 * Pt, hasLen: true},
	}
	colour = colorProp(name+"-color", func(s *Style) *Color { return &s.Border[side].Color }, slotBorderTop<<side, colorValue{current: true})
	return style, width, colour
}

// radiusSpec is a specified corner radius.
type radiusSpec struct{ h, v length }

func parseRadius(v []token, _ *declCtx) (any, bool) {
	if len(v) < 1 || len(v) > 2 {
		return nil, false
	}
	h, ok := parseLength(v[0], allowPct|nonNeg)
	if !ok {
		return nil, false
	}
	r := radiusSpec{h: h, v: h}
	if len(v) == 2 {
		if r.v, ok = parseLength(v[1], allowPct|nonNeg); !ok {
			return nil, false
		}
	}
	return r, true
}

func radiusProp(c Corner, name string) propDef {
	return propDef{
		name:  name,
		parse: parseRadius,
		compute: func(s *styler, st *Style, v any) {
			if r, ok := v.(radiusSpec); ok {
				st.BorderRadius[c] = Radius{H: r.h.dim(s.lc, true), V: r.v.dim(s.lc, true)}
			}
		},
		copyFrom: field(func(s *Style) *Radius { return &s.BorderRadius[c] }),
	}
}

// edgeProps defines the four longhands of a box edge property.
func edgeProps(prefix, suffix string, f func(*Style) *[4]Dimension, lf lengthFlags, kws map[string]DimKind) [4]propDef {
	names := [4]string{"top", "right", "bottom", "left"}
	var out [4]propDef
	for i := range out {
		side := i
		out[i] = dimProp(prefix+names[i]+suffix, false, func(s *Style) *Dimension { return &f(s)[side] }, lf, kws, false)
	}
	return out
}

// props defines every longhand, indexed by propID.
var props [numProps]propDef

// longhandByName maps a property name, aliases included, to its longhand.
var longhandByName map[string]propID

func init() {
	defs := []struct {
		id  propID
		def propDef
	}{
		{pFontSize, propDef{name: "font-size", inherited: true, parse: parseFontSize, compute: computeFontSize, copyFrom: field(func(s *Style) *float64 { return &s.FontSize })}},
		{pColor, propDef{
			name: "color", inherited: true,
			parse: func(v []token, _ *declCtx) (any, bool) {
				t, ok := single(v)
				if !ok {
					return nil, false
				}
				c, ok := parseColor(t)
				return c, ok
			},
			compute: func(s *styler, st *Style, v any) {
				if c, ok := v.(colorValue); ok {
					// CSS Color 4, 4.4: currentColor on color itself
					// is inherit.
					if c.current {
						st.Color = s.parent.Color
					} else {
						st.Color = c.c
					}
				}
			},
			copyFrom: field(func(s *Style) *Color { return &s.Color }),
		}},
		{pDisplay, propDef{name: "display", parse: parseDisplay, compute: func(_ *styler, st *Style, v any) {
			if d, ok := v.(Display); ok {
				st.Display = d
			}
		}, copyFrom: field(func(s *Style) *Display { return &s.Display })}},
		{pPosition, enumProp("position", false, func(s *Style) *Position { return &s.Position }, map[string]Position{
			"static": Static, "relative": Relative, "absolute": Absolute, "fixed": Fixed, "sticky": Relative, "-webkit-sticky": Relative,
		})},
		{pFloat, enumProp("float", false, func(s *Style) *Float { return &s.Float }, map[string]Float{
			"none": FloatNone, "left": FloatLeft, "right": FloatRight, "inline-start": FloatLeft, "inline-end": FloatRight,
		})},
		{pClear, enumProp("clear", false, func(s *Style) *Clear { return &s.Clear }, map[string]Clear{
			"none": ClearNone, "left": ClearLeft, "right": ClearRight, "both": ClearBoth, "inline-start": ClearLeft, "inline-end": ClearRight,
		})},
		{pVisibility, enumProp("visibility", true, func(s *Style) *Visibility { return &s.Visibility }, map[string]Visibility{
			"visible": Visible, "hidden": Hidden, "collapse": VisibilityCollapse,
		})},
		{pOverflow, propDef{name: "overflow", parse: parseOverflow, compute: func(_ *styler, st *Style, v any) {
			if o, ok := v.(Overflow); ok {
				st.Overflow = o
			}
		}, copyFrom: field(func(s *Style) *Overflow { return &s.Overflow })}},
		{pBoxSizing, enumProp("box-sizing", false, func(s *Style) *BoxSizing { return &s.BoxSizing }, map[string]BoxSizing{
			"content-box": ContentBox, "border-box": BorderBox,
		})},
		{pOpacity, propDef{name: "opacity", parse: parseOpacity, compute: func(_ *styler, st *Style, v any) {
			if o, ok := v.(float64); ok {
				st.Opacity = o
			}
		}, copyFrom: field(func(s *Style) *float64 { return &s.Opacity })}},
		{pZIndex, zIndexProp()},
		{pTransform, transformProp()},
		{pTransformOrigin, transformOriginProp()},
		{pWidth, sizeProp("width", func(s *Style) *Dimension { return &s.Width }, kwSizing)},
		{pHeight, sizeProp("height", func(s *Style) *Dimension { return &s.Height }, kwSizing)},
		{pMinWidth, minSizeProp("min-width", func(s *Style) *Dimension { return &s.MinWidth })},
		{pMinHeight, minSizeProp("min-height", func(s *Style) *Dimension { return &s.MinHeight })},
		{pMaxWidth, sizeProp("max-width", func(s *Style) *Dimension { return &s.MaxWidth }, kwMaxSizing)},
		{pMaxHeight, sizeProp("max-height", func(s *Style) *Dimension { return &s.MaxHeight }, kwMaxSizing)},
		{pTop, dimProp("top", false, func(s *Style) *Dimension { return &s.Top }, allowPct, kwAuto, false)},
		{pRight, dimProp("right", false, func(s *Style) *Dimension { return &s.Right }, allowPct, kwAuto, false)},
		{pBottom, dimProp("bottom", false, func(s *Style) *Dimension { return &s.Bottom }, allowPct, kwAuto, false)},
		{pLeft, dimProp("left", false, func(s *Style) *Dimension { return &s.Left }, allowPct, kwAuto, false)},
		{pBgColor, colorProp("background-color", func(s *Style) *Color { return &s.BackgroundColor }, slotBackground, nil)},
		{pBgImage, propDef{name: "background-image", parse: parseBgImage, compute: func(s *styler, st *Style, v any) {
			if im, ok := v.(imageSpec); ok {
				st.BackgroundImage, st.BackgroundGradient = im.url, nil
				if im.grad != nil {
					st.BackgroundGradient = im.grad.compute(s.lc, st.Color)
				}
			}
		}, copyFrom: func(dst, src *Style) {
			dst.BackgroundImage, dst.BackgroundGradient = src.BackgroundImage, src.BackgroundGradient
		}}},
		{pBgRepeat, propDef{name: "background-repeat", parse: parseBgRepeat, compute: func(_ *styler, st *Style, v any) {
			if r, ok := v.([2]Repeat); ok {
				st.BackgroundRepeat = r
			}
		}, copyFrom: field(func(s *Style) *[2]Repeat { return &s.BackgroundRepeat })}},
		{pBgPosition, propDef{name: "background-position", parse: parseBgPosition, compute: func(s *styler, st *Style, v any) {
			if p, ok := v.([2]length); ok {
				st.BackgroundPosition = [2]Dimension{p[0].dim(s.lc, false), p[1].dim(s.lc, false)}
			}
		}, copyFrom: field(func(s *Style) *[2]Dimension { return &s.BackgroundPosition })}},
		{pBgSize, propDef{name: "background-size", parse: parseBgSize, compute: func(s *styler, st *Style, v any) {
			if b, ok := v.(bgSizeSpec); ok {
				st.BackgroundSize = BackgroundSize{Kind: b.kind, Width: s.computeDim(b.w, true), Height: s.computeDim(b.h, true)}
			}
		}, copyFrom: field(func(s *Style) *BackgroundSize { return &s.BackgroundSize })}},
		{pBgOrigin, originProp()},
		{pBgClip, propDef{name: "background-clip", parse: parseBgClip, compute: func(_ *styler, st *Style, v any) {
			if e, ok := v.(BoxEdge); ok {
				st.BackgroundClip = e
			}
		}, copyFrom: field(func(s *Style) *BoxEdge { return &s.BackgroundClip })}},
		{pFontFamily, propDef{name: "font-family", inherited: true, parse: parseFontFamily, compute: func(_ *styler, st *Style, v any) {
			if f, ok := v.([]string); ok {
				st.FontFamilies = f
			}
		}, copyFrom: field(func(s *Style) *[]string { return &s.FontFamilies })}},
		{pFontWeight, propDef{name: "font-weight", inherited: true, parse: parseFontWeight, compute: computeFontWeight, copyFrom: field(func(s *Style) *int { return &s.FontWeight })}},
		{pFontStyle, propDef{name: "font-style", inherited: true, parse: parseFontStyle, compute: func(_ *styler, st *Style, v any) {
			if f, ok := v.(FontStyle); ok {
				st.FontStyle = f
			}
		}, copyFrom: field(func(s *Style) *FontStyle { return &s.FontStyle })}},
		{pFontVariant, enumProp("font-variant", true, func(s *Style) *FontVariant { return &s.FontVariant }, map[string]FontVariant{
			"normal": VariantNormal, "small-caps": SmallCaps, "all-small-caps": SmallCaps,
		})},
		{pLineHeight, propDef{name: "line-height", inherited: true, parse: parseLineHeight, compute: computeLineHeight, copyFrom: field(func(s *Style) *Dimension { return &s.LineHeight })}},
		{pTextAlign, textAlignProp(enumProp("text-align", true, func(s *Style) *TextAlign { return &s.TextAlign }, map[string]TextAlign{
			"start": AlignStart, "end": AlignEnd, "left": AlignLeft, "right": AlignRight,
			"center": AlignCenter, "justify": AlignJustify, "justify-all": AlignJustifyAll,
			"-webkit-left": AlignLeft, "-webkit-right": AlignRight,
			"-webkit-center": AlignCenter, "-moz-left": AlignLeft, "-moz-right": AlignRight,
			"-moz-center": AlignCenter, "-webkit-auto": AlignStart,
		}))},
		{pTextDecoration, propDef{name: "text-decoration-line", parse: parseDecorationLine, compute: func(_ *styler, st *Style, v any) {
			if d, ok := v.(TextDecoration); ok {
				st.TextDecoration = d
			}
		}, copyFrom: field(func(s *Style) *TextDecoration { return &s.TextDecoration })}},
		{pTextAlignLast, textAlignLastProp()},
		{pTextDecorationColor, colorProp("text-decoration-color", func(s *Style) *Color { return &s.TextDecorationColor }, slotTextDecoration, colorValue{current: true})},
		{pTextDecorationStyle, enumProp("text-decoration-style", false, func(s *Style) *DecorationStyle { return &s.TextDecorationStyle }, decorationStyles)},
		{pTextDecorationThickness, dimProp("text-decoration-thickness", false, func(s *Style) *Dimension { return &s.TextDecorationThickness }, allowPct|allowMixed, kwThickness, false)},
		{pTextUnderlineOffset, dimProp("text-underline-offset", true, func(s *Style) *Dimension { return &s.TextUnderlineOffset }, allowPct|allowMixed, kwAuto, false)},
		{pTextIndent, propDef{name: "text-indent", inherited: true, parse: parseTextIndent, compute: func(s *styler, st *Style, v any) {
			if l, ok := v.(length); ok {
				st.TextIndent = l.dim(s.lc, false)
			}
		}, copyFrom: field(func(s *Style) *Dimension { return &s.TextIndent })}},
		{pTextTransform, enumProp("text-transform", true, func(s *Style) *TextTransform { return &s.TextTransform }, map[string]TextTransform{
			"none": TransformNone, "capitalize": Capitalize, "uppercase": Uppercase, "lowercase": Lowercase,
		})},
		{pLetterSpacing, dimProp("letter-spacing", true, func(s *Style) *Dimension { return &s.LetterSpacing }, 0, kwNormal, false)},
		{pWordSpacing, dimProp("word-spacing", true, func(s *Style) *Dimension { return &s.WordSpacing }, 0, kwNormal, false)},
		{pWhiteSpace, enumProp("white-space", true, func(s *Style) *WhiteSpace { return &s.WhiteSpace }, map[string]WhiteSpace{
			"normal": WhiteSpaceNormal, "pre": Pre, "nowrap": Nowrap, "pre-wrap": PreWrap,
			"pre-line": PreLine, "break-spaces": BreakSpaces, "-moz-pre-wrap": PreWrap, "-o-pre-wrap": PreWrap,
		})},
		{pWordBreak, enumProp("word-break", true, func(s *Style) *WordBreak { return &s.WordBreak }, map[string]WordBreak{
			"normal": WordBreakNormal, "break-all": BreakAll, "keep-all": KeepAll, "auto-phrase": WordBreakNormal,
		})},
		{pOverflowWrap, enumProp("overflow-wrap", true, func(s *Style) *OverflowWrap { return &s.OverflowWrap }, map[string]OverflowWrap{
			"normal": OverflowWrapNormal, "break-word": BreakWord, "anywhere": Anywhere,
		})},
		{pLineBreak, enumProp("line-break", true, func(s *Style) *LineBreak { return &s.LineBreak }, map[string]LineBreak{
			"auto": LineBreakAuto, "loose": LineBreakLoose, "normal": LineBreakNormal, "strict": LineBreakStrict, "anywhere": LineBreakAnywhere,
		})},
		{pWordSpaceTransform, propDef{name: "word-space-transform", inherited: true, parse: parseWordSpaceTransform, compute: func(_ *styler, st *Style, v any) {
			if w, ok := v.(WordSpaceTransform); ok {
				st.WordSpaceTransform = w
			}
		}, copyFrom: field(func(s *Style) *WordSpaceTransform { return &s.WordSpaceTransform })}},
		{pTabSize, dimProp("tab-size", true, func(s *Style) *Dimension { return &s.TabSize }, nonNeg, kwNoKw, true)},
		{pVerticalAlign, propDef{name: "vertical-align", parse: parseVerticalAlign, compute: func(s *styler, st *Style, v any) {
			if va, ok := v.(valignSpec); ok {
				st.VerticalAlign = VerticalAlign{Kind: va.kind}
				if va.kind == AlignLength {
					st.VerticalAlign.Length = va.l.dim(s.lc, false)
				}
			}
		}, copyFrom: field(func(s *Style) *VerticalAlign { return &s.VerticalAlign })}},
		{pDirection, enumProp("direction", true, func(s *Style) *Direction { return &s.Direction }, map[string]Direction{"ltr": LTR, "rtl": RTL})},
		{pUnicodeBidi, enumProp("unicode-bidi", false, func(s *Style) *UnicodeBidi { return &s.UnicodeBidi }, map[string]UnicodeBidi{
			"normal": BidiNormal, "embed": Embed, "isolate": Isolate, "bidi-override": BidiOverride,
			"isolate-override": IsolateOverride, "plaintext": Plaintext, "-webkit-isolate": Isolate,
		})},
		{pListStyleType, propDef{name: "list-style-type", inherited: true, parse: parseListStyleType, compute: func(_ *styler, st *Style, v any) {
			switch t := v.(type) {
			case ListStyleType:
				st.ListStyleType, st.ListStyleString = t, ""
			case listString:
				st.ListStyleType, st.ListStyleString = ListString, string(t)
			}
		}, copyFrom: func(dst, src *Style) {
			dst.ListStyleType, dst.ListStyleString = src.ListStyleType, src.ListStyleString
		}}},
		{pListStylePosition, enumProp("list-style-position", true, func(s *Style) *ListStylePosition { return &s.ListStylePosition }, map[string]ListStylePosition{
			"outside": Outside, "inside": Inside,
		})},
		{pListStyleImage, propDef{name: "list-style-image", inherited: true, parse: parseImage, compute: func(_ *styler, st *Style, v any) {
			if u, ok := v.(string); ok {
				st.ListStyleImage = u
			}
		}, copyFrom: field(func(s *Style) *string { return &s.ListStyleImage })}},
		{pBorderCollapse, enumProp("border-collapse", true, func(s *Style) *BorderCollapse { return &s.BorderCollapse }, map[string]BorderCollapse{
			"separate": SeparateBorders, "collapse": CollapseBorders,
		})},
		{pBorderSpacing, propDef{name: "border-spacing", inherited: true, parse: parseBorderSpacing, compute: func(s *styler, st *Style, v any) {
			if p, ok := v.([2]length); ok {
				st.BorderSpacing = [2]float64{math.Max(0, p[0].points(s.lc)), math.Max(0, p[1].points(s.lc))}
			}
		}, copyFrom: field(func(s *Style) *[2]float64 { return &s.BorderSpacing })}},
		{pCaptionSide, enumProp("caption-side", true, func(s *Style) *CaptionSide { return &s.CaptionSide }, map[string]CaptionSide{
			"top": CaptionTop, "bottom": CaptionBottom, "block-start": CaptionTop, "block-end": CaptionBottom,
		})},
		{pEmptyCells, enumProp("empty-cells", true, func(s *Style) *EmptyCells { return &s.EmptyCells }, map[string]EmptyCells{"show": Show, "hide": Hide})},
		{pTableLayout, enumProp("table-layout", false, func(s *Style) *TableLayout { return &s.TableLayout }, map[string]TableLayout{"auto": TableAuto, "fixed": TableFixed})},
		{pBreakBefore, enumProp("break-before", false, func(s *Style) *Break { return &s.BreakBefore }, breakValues)},
		{pBreakAfter, enumProp("break-after", false, func(s *Style) *Break { return &s.BreakAfter }, breakValues)},
		{pBreakInside, enumProp("break-inside", false, func(s *Style) *BreakInside { return &s.BreakInside }, map[string]BreakInside{
			"auto": InsideAuto, "avoid": InsideAvoid, "avoid-page": InsideAvoidPage, "avoid-column": InsideAuto, "avoid-region": InsideAuto,
		})},
		{pOrphans, countProp("orphans", func(s *Style) *int { return &s.Orphans })},
		{pWidows, countProp("widows", func(s *Style) *int { return &s.Widows })},
	}
	for _, d := range defs {
		props[d.id] = d.def
	}
	margins := edgeProps("margin-", "", func(s *Style) *[4]Dimension { return &s.Margin }, allowPct, kwAuto)
	paddings := edgeProps("padding-", "", func(s *Style) *[4]Dimension { return &s.Padding }, allowPct|nonNeg, kwNoKw)
	sides := [4]string{"border-top", "border-right", "border-bottom", "border-left"}
	for i := 0; i < 4; i++ {
		props[pMarginTop+propID(i)] = margins[i]
		props[pPaddingTop+propID(i)] = paddings[i]
		st, w, c := borderProps(Side(i), sides[i])
		props[pBorderTopStyle+propID(i)] = st
		props[pBorderTopWidth+propID(i)] = w
		props[pBorderTopColor+propID(i)] = c
	}
	props[pOutlineStyle], props[pOutlineWidth], props[pOutlineColor], props[pOutlineOffset] = outlineProps()
	props[pBoxShadow] = propDef{name: "box-shadow", parse: parseBoxShadow, compute: computeShadows, copyFrom: copyShadows}
	props[pObjectFit] = enumProp("object-fit", false, func(s *Style) *ObjectFit { return &s.ObjectFit }, objectFits)
	props[pObjectPosition] = propDef{name: "object-position", parse: parseObjectPosition, compute: func(s *styler, st *Style, v any) {
		if p, ok := v.([2]length); ok {
			st.ObjectPosition = [2]Dimension{p[0].dim(s.lc, false), p[1].dim(s.lc, false)}
		}
	}, copyFrom: field(func(s *Style) *[2]Dimension { return &s.ObjectPosition })}
	props[pContain] = propDef{name: "contain", parse: parseContain, compute: func(_ *styler, st *Style, v any) {
		if c, ok := v.(Containment); ok {
			st.Contain = c
		}
	}, copyFrom: field(func(s *Style) *Containment { return &s.Contain })}
	props[pContent] = contentProp()
	props[pCounterReset] = counterProp("counter-reset", 0, func(s *Style) *[]CounterOp { return &s.CounterReset })
	props[pCounterIncrement] = counterProp("counter-increment", 1, func(s *Style) *[]CounterOp { return &s.CounterIncrement })
	props[pCounterSet] = counterProp("counter-set", 0, func(s *Style) *[]CounterOp { return &s.CounterSet })
	props[pQuotes] = quotesProp()
	corners := [4]string{"border-top-left-radius", "border-top-right-radius", "border-bottom-right-radius", "border-bottom-left-radius"}
	for i, n := range corners {
		props[pRadiusTopLeft+propID(i)] = radiusProp(Corner(i), n)
	}
	longhandByName = map[string]propID{}
	for i := range props {
		longhandByName[props[i].name] = propID(i)
	}
	for alias, id := range map[string]propID{
		"word-wrap": pOverflowWrap, "overflow-x": pOverflow, "overflow-y": pOverflow,
		"overflow-block": pOverflow, "overflow-inline": pOverflow,
		"margin-block-start": pMarginTop, "margin-block-end": pMarginBottom,
		"margin-inline-start": pMarginLeft, "margin-inline-end": pMarginRight,
		"padding-block-start": pPaddingTop, "padding-block-end": pPaddingBottom,
		"padding-inline-start": pPaddingLeft, "padding-inline-end": pPaddingRight,
		"inset-block-start": pTop, "inset-block-end": pBottom,
		"inset-inline-start": pLeft, "inset-inline-end": pRight,
		"inline-size": pWidth, "block-size": pHeight,
		"min-inline-size": pMinWidth, "min-block-size": pMinHeight,
		"max-inline-size": pMaxWidth, "max-block-size": pMaxHeight,
		"page-break-inside": pBreakInside,
	} {
		longhandByName[alias] = id
	}
	initShorthands()
}

// minSizeProp defines min-width or min-height: a non-negative length or
// percentage, auto and the sizing keywords computing to 0 (CSS Sizing 3,
// 4.5, for a box that is not a flex item).
func minSizeProp(name string, f func(*Style) *Dimension) propDef {
	return dimProp(name, false, f, allowPct|nonNeg, kwMinSizing, false)
}

// countProp defines orphans or widows: a positive integer (CSS
// Fragmentation 3, 4.4).
func countProp(name string, f func(*Style) *int) propDef {
	return propDef{
		name: name, inherited: true,
		parse: func(v []token, _ *declCtx) (any, bool) {
			t, ok := single(v)
			if !ok {
				return nil, false
			}
			n, ok := parseInteger(t)
			return n, ok && n >= 1
		},
		compute: func(_ *styler, st *Style, v any) {
			if n, ok := v.(int); ok {
				*f(st) = n
			}
		},
		copyFrom: field(f),
	}
}

var breakValues = map[string]Break{
	"auto": BreakAuto, "avoid": BreakAvoid, "avoid-page": BreakAvoidPage, "page": Page,
	"left": LeftPage, "right": RightPage, "recto": RightPage, "verso": LeftPage, "all": Page,
	"column": BreakAuto, "avoid-column": BreakAuto, "region": BreakAuto, "avoid-region": BreakAuto,
}

var boxEdges = map[string]BoxEdge{"border-box": BorderEdge, "padding-box": PaddingEdge, "content-box": ContentEdge}

func parseBgClip(v []token, c *declCtx) (any, bool) {
	k, ok := keyword(v)
	if !ok {
		return nil, false
	}
	if k == "text" || k == "-webkit-text" {
		c.warn("unsupported-css-value", "background-clip: text is not supported and computes to border-box")
		return BorderEdge, true
	}
	e, ok := boxEdges[k]
	return e, ok
}

// parseTextIndent parses text-indent (CSS Text 3, 8.1); the hanging and
// each-line keywords are accepted and ignored.
func parseTextIndent(v []token, _ *declCtx) (any, bool) {
	var l length
	found := false
	for _, t := range v {
		if t.isIdent("hanging") || t.isIdent("each-line") {
			continue
		}
		if found {
			return nil, false
		}
		var ok bool
		if l, ok = parseLength(t, allowPct); !ok {
			return nil, false
		}
		found = true
	}
	return l, found
}

// valignSpec is a specified vertical-align.
type valignSpec struct {
	kind AlignKind
	l    length
}

var valignKeywords = map[string]AlignKind{
	"baseline": Baseline, "sub": Sub, "super": Super, "top": AlignTop, "text-top": TextTop,
	"middle": Middle, "bottom": AlignBottom, "text-bottom": TextBottom,
}

func parseVerticalAlign(v []token, _ *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	if t.typ == tIdent {
		k, ok := valignKeywords[lower(t.value)]
		return valignSpec{kind: k}, ok
	}
	l, ok := parseLength(t, allowPct)
	return valignSpec{kind: AlignLength, l: l}, ok
}

func parseBorderSpacing(v []token, _ *declCtx) (any, bool) {
	if len(v) < 1 || len(v) > 2 {
		return nil, false
	}
	h, ok := parseLength(v[0], nonNeg)
	if !ok {
		return nil, false
	}
	out := [2]length{h, h}
	if len(v) == 2 {
		if out[1], ok = parseLength(v[1], nonNeg); !ok {
			return nil, false
		}
	}
	return out, true
}

// parseWordSpaceTransform parses word-space-transform (CSS Text 4, 4.3):
// none, or space or ideographic-space with or without auto-phrase, which
// litho ignores.
func parseWordSpaceTransform(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "none" {
		return WordSpaceNone, true
	}
	if len(v) == 0 || len(v) > 2 {
		return nil, false
	}
	w, phrase := WordSpaceNone, false
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		switch lower(t.value) {
		case "space", "ideographic-space":
			if w != WordSpaceNone {
				return nil, false
			}
			w = WordSpaceSpace
			if lower(t.value) == "ideographic-space" {
				w = WordSpaceIdeographic
			}
		case "auto-phrase":
			if phrase {
				return nil, false
			}
			phrase = true
		default:
			return nil, false
		}
	}
	return w, true
}

// parseDecorationLine parses text-decoration-line (CSS Text Decoration
// 3, 2.1); blink is accepted and draws nothing.
func parseDecorationLine(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "none" {
		return TextDecoration(0), true
	}
	var d TextDecoration
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		var bit TextDecoration
		switch lower(t.value) {
		case "underline":
			bit = Underline
		case "overline":
			bit = Overline
		case "line-through":
			bit = LineThrough
		case "blink":
			continue
		default:
			return nil, false
		}
		if d&bit != 0 {
			return nil, false
		}
		d |= bit
	}
	return d, len(v) > 0
}

var listStyleTypes = map[string]ListStyleType{
	"disc": Disc, "circle": Circle, "square": Square, "decimal": Decimal,
	"decimal-leading-zero": DecimalLeadingZero, "lower-roman": LowerRoman, "upper-roman": UpperRoman,
	"lower-alpha": LowerAlpha, "upper-alpha": UpperAlpha, "lower-latin": LowerAlpha,
	"upper-latin": UpperAlpha, "lower-greek": LowerGreek, "armenian": Armenian,
	"upper-armenian": Armenian, "georgian": Georgian, "none": ListNone,
	"disclosure-open": Disc, "disclosure-closed": Disc,
}

// listString is a specified list-style-type <string>.
type listString string

// parseListStyleType parses list-style-type (CSS Lists 3, 3.1). A counter
// style litho lacks computes to decimal, as CSS Counter Styles 3, 2 asks
// of an unknown name; a string is the marker as written.
func parseListStyleType(v []token, c *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tIdent:
		k := lower(t.value)
		if lt, ok := listStyleTypes[k]; ok {
			return lt, true
		}
		c.warn("unsupported-css-value", "list-style-type: %s is not supported and computes to decimal", k)
		return Decimal, true
	case tString:
		return listString(t.value), true
	case tFunction:
		if eqFold(t.value, "symbols") {
			c.warn("unsupported-css-value", "list-style-type: symbols() is not supported and computes to disc")
			return Disc, true
		}
	}
	return nil, false
}

// textAlignProp adds match-parent to text-align (CSS Text 3, 6.1): it
// computes to the parent's value, which inherit gives for every value but
// start and end.
func textAlignProp(def propDef) propDef {
	base := def.parse
	def.parse = func(v []token, c *declCtx) (any, bool) {
		if k, ok := keyword(v); ok && k == "match-parent" {
			return wideInherit, true
		}
		return base(v, c)
	}
	return def
}

// textAlignLastProp defines text-align-last (CSS Text 3, 6.3), whose
// initial value auto is not [TextAlign]'s zero value.
func textAlignLastProp() propDef {
	def := textAlignProp(enumProp("text-align-last", true, func(s *Style) *TextAlign { return &s.TextAlignLast }, map[string]TextAlign{
		"auto": AlignAuto, "start": AlignStart, "end": AlignEnd, "left": AlignLeft, "right": AlignRight,
		"center": AlignCenter, "justify": AlignJustify,
	}))
	def.initial = AlignAuto
	return def
}

// originProp defines background-origin. Its initial value is padding-box
// (CSS Backgrounds 3, 3.8), which [Initial] leaves at the zero BoxEdge,
// border-box: the cascade computes the specified initial value instead.
func originProp() propDef {
	def := enumProp("background-origin", false, func(s *Style) *BoxEdge { return &s.BackgroundOrigin }, boxEdges)
	def.initial = PaddingEdge
	return def
}
