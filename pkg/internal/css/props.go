package css

import (
	"maps"
	"math"
)

// propID names a longhand. The cascade computes longhands in this order:
// font-size first, since em units resolve against it, then color, since
// currentColor resolves to it.
type propID uint16

const (
	pFontSize propID = iota
	pColor
	pDisplay
	pPosition
	pFloat
	pClear
	pVisibility
	pOverflowX
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
	pFlexDirection
	pFlexWrap
	pOrder
	pFlexGrow
	pFlexShrink
	pFlexBasis
	pJustifyContent
	pAlignContent
	pAlignItems
	pAlignSelf
	pRowGap
	pColumnGap
	pPage
	pBorderImageSource
	pBorderImageSlice
	pBorderImageWidth
	pBorderImageOutset
	pBorderImageRepeat
	pMaskBorderSource
	pMaskBorderSlice
	pMaskBorderWidth
	pMaskBorderOutset
	pMaskBorderRepeat
	pBgAttachment
	pGridTemplateRows
	pGridTemplateColumns
	pGridTemplateAreas
	pGridAutoRows
	pGridAutoColumns
	pGridAutoFlow
	pGridRowStart
	pGridRowEnd
	pGridColumnStart
	pGridColumnEnd
	pJustifyItems
	pJustifySelf
	pTextOverflow
	pMaxLines
	pBlockEllipsis
	pContinue
	pWebkitLineClamp
	pWebkitBoxOrient
	pTextJustify
	pTextAutospace
	pFontStretch
	pFontSynthesisWeight
	pFontSynthesisStyle
	pTextSpacingTrim
	pTextAutospaceSize
	pTextShadow
	pStringSet
	pFootnoteDisplay
	pFootnotePolicy
	pBookmarkLevel
	pBookmarkLabel
	pBookmarkState
	pColumnCount
	pColumnWidth
	pColumnRuleStyle
	pColumnRuleWidth
	pColumnRuleColor
	pColumnSpan
	pColumnFill
	pColumnHeight
	pColumnWrap
	pBoxDecorationBreak
	pFontKerning
	pWritingMode
	pOverflowClipMargin
	pTranslate
	pRotate
	pScale
	pTransformBox
	pTextDecorationSkipInk
	pTextUnderlinePosition
	pHyphens
	pHyphenateCharacter
	pTextWrapMode
	pTextWrapStyle
	pRubyPosition
	pRubyAlign
	pRubyOverhang
	pTextEmphasisStyle
	pTextEmphasisColor
	pTextEmphasisPosition
	pFontFeatureSettings
	pFontVariantLigatures
	pFontVariantNumeric
	pFontVariantEastAsian
	pFontVariantPosition
	pFontVariantAlternates
	pFontVariantEmoji
	pFontLanguageOverride
	pFontSynthesisSmallCaps
	pFontSizeAdjust
	pInitialLetter
	pMixBlendMode
	pBgBlendMode
	pIsolation
	pClipPath
	pClip
	pMaskImage
	pMaskMode
	pMaskRepeat
	pMaskPosition
	pMaskSize
	pMaskOrigin
	pMaskClip
	pMaskComposite
	pMaskType
	pAspectRatio
	pContainIntrinsicWidth
	pContainIntrinsicHeight
	pBgPositionY
	pAnimationName
	pAnimationDuration
	pAnimationTimingFunction
	pAnimationIterationCount
	pAnimationDirection
	pAnimationFillMode
	pAnimationPlayState
	pAnimationDelay
	pAnimationComposition
	pAnimationTimeline
	pAnimationRangeStart
	pAnimationRangeEnd
	pScrollTimelineName
	pViewTimelineName
	pTransitionProperty
	pTransitionDuration
	pTransitionTimingFunction
	pTransitionDelay
	pTransitionBehavior
	pScrollTimelineAxis
	pViewTimelineAxis
	pViewTimelineInset
	pTimelineScope
	pShapeOutside
	pShapeMargin
	pShapeImageThreshold
	pBaselineSource
	pTextBoxTrim
	pTextBoxEdge
	pObjectViewBox
	pPrintColorAdjust
	pContentVisibility
	pFilter
	pBackdropFilter
	pFontVariationSettings
	pFontOpticalSizing
	pMarginTrim
	pFontPalette
	pTextOrientation
	pTextCombineUpright
	pMathStyle
	pMathShift
	pMathDepth
	pContainerType
	pContainerName
	pColorScheme
	pWillChange
	pAnchorName
	pAnchorScope
	pPositionAnchor
	pPositionArea
	pPositionTryFallbacks
	pPositionTryOrder
	pPositionVisibility
	pTextStrokeWidth
	pTextStrokeColor
	pTextFillColor
	pPaintOrder
	pOffsetPath
	pOffsetDistance
	pOffsetRotate
	pOffsetAnchor
	pOffsetPosition
	pFieldSizing
	pAppearance
	pAccentColor
	pResize
	pPerspective
	pPerspectiveOrigin
	pTransformStyle
	pBackfaceVisibility
	pZoom
	pOverflowY
	pWebkitBoxPack
	pWebkitBoxAlign
	pWebkitBoxDirection
	pWebkitBoxFlex
	pWebkitBoxOrdinalGroup
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
	// interp interpolates the longhand's computed values, nil for a
	// longhand litho animates in discrete steps.
	interp interpFn
}

// styler computes one element's style.
type styler struct {
	parent *Style
	lc     lenCtx
	// root is the root element's style, nil while computing the root.
	root *Style
	// body is the style of the document's body element, nil before it is
	// computed or when there is none.
	body *Style
	// prefersDark reports a dark preferred colour scheme, the medium's
	// prefers-color-scheme (CSS Color Adjustment 1, 2.1).
	prefersDark bool
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
	// size is the calculation of a calc-size() over the keyword basis
	// kind, nil otherwise.
	size *calcNode
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
	if d.size != nil {
		sizes := s.lc.unitSizes()
		return Dimension{Kind: d.kind, expr: d.size.resolveUnits(&sizes)}
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
				*f(st) = c.resolve(st.Color, st.DarkScheme)
				if c.dependsOnCurrent() {
					st.currentColor |= slot
					st.setColorExpr(slot, c.expr)
				} else {
					st.currentColor &^= slot
					st.setColorExpr(slot, nil)
				}
			}
		},
		copyFrom: func(dst, src *Style) {
			*f(dst) = *f(src)
			dst.currentColor = dst.currentColor&^slot | src.currentColor&slot
			if src.currentColor&slot == 0 {
				dst.setColorExpr(slot, nil)
				return
			}
			v := src.slotValue(slot)
			dst.setColorExpr(slot, v.expr)
			*f(dst) = v.resolve(dst.Color, dst.DarkScheme)
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
	// kwSizing are auto and the sizing keywords of width and height (CSS
	// Sizing 3, 3.1 and CSS Sizing 4, 3.1, with the prefixed spellings
	// Chromium accepts), kwMinSizing those of min-width and min-height,
	// kwMaxSizing those of max-width and max-height.
	kwSizing = withSizing(map[string]DimKind{"auto": Auto})
	// kwMinSizing maps auto to Points: [minSizeProp] parses auto itself.
	kwMinSizing = withSizing(map[string]DimKind{"auto": Points})
	kwMaxSizing = withSizing(map[string]DimKind{"none": None})
)

// sizeProp defines width, height, max-width or max-height: the keywords
// kws, a non-negative length or percentage, or a calc-size() over one of
// them.
func sizeProp(name string, f func(*Style) *Dimension, kws map[string]DimKind) propDef {
	return withCalcSize(dimProp(name, false, f, allowPct|nonNeg|allowMixed, kws, false), kws)
}

// withCalcSize returns def with calc-size() parsed (CSS Sizing 4, 7), its
// keyword bases those of bases but none.
func withCalcSize(def propDef, bases map[string]DimKind) propDef {
	parse := def.parse
	def.parse = func(v []token, c *declCtx) (any, bool) {
		if t, ok := single(v); ok && isCalcSize(&t) {
			d, ok := parseCalcSize(&t, bases)
			return d, ok
		}
		return parse(v, c)
	}
	return def
}

var displays = map[string]Display{
	"inline": Inline, "block": Block, "inline-block": InlineBlock, "list-item": ListItem,
	"table": Table, "inline-table": InlineTable, "table-row-group": TableRowGroup,
	"table-header-group": TableHeaderGroup, "table-footer-group": TableFooterGroup,
	"table-row": TableRow, "table-column-group": TableColumnGroup, "table-column": TableColumn,
	"table-cell": TableCell, "table-caption": TableCaption, "none": DisplayNone,
	"run-in": Block, "flow-root": FlowRoot, "ruby": Ruby, "ruby-text": RubyText, "contents": Contents,
	"flex": Flex, "inline-flex": InlineFlex, "-webkit-flex": Flex, "-ms-flexbox": Flex,
	"grid": Grid, "inline-grid": InlineGrid, "-ms-grid": Grid, "-ms-inline-grid": InlineGrid,
	"-webkit-inline-flex": InlineFlex, "-ms-inline-flexbox": InlineFlex,
	// math alone is inline math (MathML Core, 4.1), which computes to
	// inline flow on an element outside MathML.
	"math": Inline,
	// flow alone is block flow: an inner display type without an outer
	// one is block-level (CSS Display 3, 2.1).
	"flow": Block,
}

// legacyBoxDisplays are the displays of the legacy flexible box, block-
// and inline-level, which [fixupLegacyBox] lays out as flex containers
// once their orientation and line clamp are known.
var legacyBoxDisplays = map[string]Display{
	"-webkit-box": Block, "-webkit-inline-box": InlineBlock,
}

// flowRootListItem is the specified display: flow-root list-item, a
// block-level list item that establishes a block formatting context, or,
// inline, display: inline flow-root list-item, an inline-level one.
type flowRootListItem struct{ inline bool }

// parseDisplay parses display (CSS Display 3, 2), the one- and two-keyword
// forms.
func parseDisplay(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok {
		if d, ok := displays[k]; ok {
			return d, true
		}
		if d, ok := legacyBoxDisplays[k]; ok {
			return laidAs{d: d, webkitBox: true}, true
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
		// flow-root inside. A list item of flow-root inside establishes a
		// block formatting context (2.3): a block-level one is a block
		// container, an inline-level one an inline block.
		for w := range words {
			if w != "list-item" && w != "inline" && w != "block" && w != "flow" && w != "flow-root" {
				return nil, false
			}
		}
		if inline && words["block"] || words["flow"] && words["flow-root"] {
			return nil, false
		}
		if words["flow-root"] {
			return flowRootListItem{inline: inline}, true
		}
		if inline {
			return InlineListItem, true
		}
		return ListItem, true
	}
	// Otherwise the value is one outer and one inner display type.
	if len(words) != 2 || !inline && !words["block"] {
		return nil, false
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
	case words["flow-root"]:
		if inline {
			return InlineBlock, true
		}
		return FlowRoot, true
	case words["flex"]:
		if inline {
			return InlineFlex, true
		}
		return Flex, true
	case words["grid"]:
		if inline {
			return InlineGrid, true
		}
		return Grid, true
	case words["math"]:
		// MathML Core, 4.1: block math and inline math compute to block
		// flow and inline flow on an element outside MathML; the HTML
		// converter lays a MathML element out as math whatever its
		// display but none.
		if inline {
			return Inline, true
		}
		return Block, true
	case words["ruby"]:
		// CSS Ruby 1, 2.1: block ruby is a block box holding a ruby
		// container.
		if inline {
			return Ruby, true
		}
		return BlockRuby, true
	}
	return nil, false
}

func parseOpacity(v []token, _ *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	n, ok := numberOrPercent(t)
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
		return ptLength(px * Pt), ok
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
		initial:  ptLength(borderWidthKeywords["medium"] * Pt),
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
	h, ok := parseLength(v[0], allowPct|nonNeg|allowMixed)
	if !ok {
		return nil, false
	}
	r := radiusSpec{h: h, v: h}
	if len(v) == 2 {
		if r.v, ok = parseLength(v[1], allowPct|nonNeg|allowMixed); !ok {
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

// canvasText is the initial value of color, CanvasText (CSS Color 4,
// 3.1), black under a light used colour scheme and white under a dark
// one (CSS Color Adjustment 1, 2.2).
var canvasText = colorValue{expr: &token{typ: tIdent, value: "canvastext"}, schemeOnly: true}

func init() {
	defs := []struct {
		id  propID
		def propDef
	}{
		{pFontSize, propDef{name: "font-size", inherited: true, parse: parseFontSize, compute: computeFontSize, copyFrom: copyFontSize}},
		{pColor, propDef{
			name: "color", inherited: true,
			parse: func(v []token, c *declCtx) (any, bool) {
				if k, ok := keyword(v); ok && k == "-internal-quirk-inherit" {
					return quirkInherit{}, c.userAgent
				}
				t, ok := single(v)
				if !ok {
					return nil, false
				}
				col, ok := parseColor(t)
				return col, ok
			},
			compute: func(s *styler, st *Style, v any) {
				if _, ok := v.(quirkInherit); ok {
					if s.body == nil {
						v = canvasText
					} else {
						st.Color = s.body.Color
						return
					}
				}
				if c, ok := v.(colorValue); ok {
					// CSS Color 4, 4.4: currentColor on color itself
					// is inherit.
					st.Color = c.resolve(s.parent.Color, st.DarkScheme)
				}
			},
			copyFrom: field(func(s *Style) *Color { return &s.Color }),
			initial:  canvasText,
		}},
		{pDisplay, propDef{name: "display", parse: parseDisplay, compute: func(_ *styler, st *Style, v any) {
			st.ListItemRoot = false
			switch d := v.(type) {
			case Display:
				st.Display, st.flexOrGrid, st.LineClamp.webkitBox = d, false, false
			case laidAs:
				st.Display, st.flexOrGrid, st.LineClamp.webkitBox = d.d, true, d.webkitBox
			case flowRootListItem:
				st.Display, st.flexOrGrid, st.LineClamp.webkitBox, st.ListItemRoot = ListItem, false, false, true
				if d.inline {
					st.Display = InlineBlock
				}
			}
		}, copyFrom: func(dst, src *Style) {
			dst.Display, dst.flexOrGrid, dst.LineClamp.webkitBox, dst.ListItemRoot = src.Display, src.flexOrGrid, src.LineClamp.webkitBox, src.ListItemRoot
		}}},
		{pPosition, enumProp("position", false, func(s *Style) *Position { return &s.Position }, map[string]Position{
			"static": Static, "relative": Relative, "absolute": Absolute, "fixed": Fixed, "sticky": Sticky, "-webkit-sticky": Sticky,
		})},
		{pFloat, flowRelative(enumProp("float", false, func(s *Style) *Float { return &s.Float }, map[string]Float{
			"none": FloatNone, "left": FloatLeft, "right": FloatRight,
		}), func(st *Style, end, rtl bool) {
			st.Float = FloatLeft
			if end != rtl {
				st.Float = FloatRight
			}
		})},
		{pClear, flowRelative(enumProp("clear", false, func(s *Style) *Clear { return &s.Clear }, map[string]Clear{
			"none": ClearNone, "left": ClearLeft, "right": ClearRight, "both": ClearBoth,
		}), func(st *Style, end, rtl bool) {
			st.Clear = ClearLeft
			if end != rtl {
				st.Clear = ClearRight
			}
		})},
		{pVisibility, enumProp("visibility", true, func(s *Style) *Visibility { return &s.Visibility }, map[string]Visibility{
			"visible": Visible, "hidden": Hidden, "collapse": VisibilityCollapse,
		})},
		{pOverflowX, overflowAxisProp("overflow-x", func(s *Style) *overflowAxis { return &s.overflowX })},
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
		{pTranslate, translateProp()},
		{pRotate, rotateProp()},
		{pScale, scaleProp()},
		// CSS Transforms 1, 8: a CSS box takes fill-box as content-box and
		// stroke-box and view-box as border-box.
		{pTransformBox, enumProp("transform-box", false, func(s *Style) *TransformBox { return &s.TransformBox }, map[string]TransformBox{
			"view-box": TransformBorderBox, "border-box": TransformBorderBox, "stroke-box": TransformBorderBox,
			"content-box": TransformContentBox, "fill-box": TransformContentBox,
		})},
		{pWidth, withFitContent(sizeProp("width", func(s *Style) *Dimension { return &s.Width }, kwSizing), func(s *Style) *Dimension { return &s.Width }, nil)},
		{pHeight, withFitContent(sizeProp("height", func(s *Style) *Dimension { return &s.Height }, kwSizing), func(s *Style) *Dimension { return &s.Height }, nil)},
		{pMinWidth, withFitContent(minSizeProp("min-width", func(s *Style) *Dimension { return &s.MinWidth }, func(s *Style) *bool { return &s.MinWidthAuto }), func(s *Style) *Dimension { return &s.MinWidth }, func(s *Style) *bool { return &s.MinWidthAuto })},
		{pMinHeight, withFitContent(minSizeProp("min-height", func(s *Style) *Dimension { return &s.MinHeight }, func(s *Style) *bool { return &s.MinHeightAuto }), func(s *Style) *Dimension { return &s.MinHeight }, func(s *Style) *bool { return &s.MinHeightAuto })},
		{pMaxWidth, withFitContent(sizeProp("max-width", func(s *Style) *Dimension { return &s.MaxWidth }, kwMaxSizing), func(s *Style) *Dimension { return &s.MaxWidth }, nil)},
		{pMaxHeight, withFitContent(sizeProp("max-height", func(s *Style) *Dimension { return &s.MaxHeight }, kwMaxSizing), func(s *Style) *Dimension { return &s.MaxHeight }, nil)},
		{pTop, dimProp("top", false, func(s *Style) *Dimension { return &s.Top }, allowPct|allowMixed, kwAuto, false)},
		{pRight, dimProp("right", false, func(s *Style) *Dimension { return &s.Right }, allowPct|allowMixed, kwAuto, false)},
		{pBottom, dimProp("bottom", false, func(s *Style) *Dimension { return &s.Bottom }, allowPct|allowMixed, kwAuto, false)},
		{pLeft, dimProp("left", false, func(s *Style) *Dimension { return &s.Left }, allowPct|allowMixed, kwAuto, false)},
		{pBgColor, colorProp("background-color", func(s *Style) *Color { return &s.BackgroundColor }, slotBackground, nil)},
		{pBgImage, bgImageProp()},
		{pBgRepeat, bgListProp("background-repeat", parseBgRepeat, func(s *Style) *[2]Repeat { return &s.BackgroundRepeat },
			func(r *BackgroundLists) *[][2]Repeat { return &r.Repeats }, same[[2]Repeat])},
		{pBgPosition, bgPositionAxis(0)},
		{pBgSize, bgListProp("background-size", parseBgSize, func(s *Style) *BackgroundSize { return &s.BackgroundSize },
			func(r *BackgroundLists) *[]BackgroundSize { return &r.Sizes },
			func(s *styler, b bgSizeSpec) BackgroundSize {
				return BackgroundSize{Kind: b.kind, Width: s.computeDim(b.w, true), Height: s.computeDim(b.h, true)}
			})},
		{pBgOrigin, originProp()},
		{pBgClip, bgListProp("background-clip", parseBgClip, func(s *Style) *BoxEdge { return &s.BackgroundClip },
			func(r *BackgroundLists) *[]BoxEdge { return &r.Clips }, same[BoxEdge])},
		{pFontFamily, propDef{name: "font-family", inherited: true, parse: parseFontFamily, compute: func(_ *styler, st *Style, v any) {
			if f, ok := v.([]string); ok {
				st.FontFamilies = f
			}
		}, copyFrom: field(func(s *Style) *[]string { return &s.FontFamilies })}},
		{pFontWeight, propDef{name: "font-weight", inherited: true, parse: parseFontWeight, compute: computeFontWeight, copyFrom: field(func(s *Style) *int { return &s.FontWeight })}},
		{pFontStyle, propDef{name: "font-style", inherited: true, parse: parseFontStyle, compute: computeFontStyle, copyFrom: func(dst, src *Style) {
			dst.FontStyle, dst.FontObliqueAngle = src.FontStyle, src.FontObliqueAngle
		}}},
		{pFontVariant, enumProp("font-variant-caps", true, func(s *Style) *FontVariant { return &s.FontVariant }, capsKeywords)},
		{pLineHeight, propDef{name: "line-height", inherited: true, parse: parseLineHeight, compute: computeLineHeight, copyFrom: field(func(s *Style) *Dimension { return &s.LineHeight })}},
		{pTextAlign, thCenterProp(textAlignProp(enumProp("text-align", true, func(s *Style) *TextAlign { return &s.TextAlign }, map[string]TextAlign{
			"start": AlignStart, "end": AlignEnd, "left": AlignLeft, "right": AlignRight,
			"center": AlignCenter, "justify": AlignJustify, "justify-all": AlignJustifyAll,
			"-webkit-left": AlignLeft, "-webkit-right": AlignRight,
			"-webkit-center": AlignCenter, "-moz-left": AlignLeft, "-moz-right": AlignRight,
			"-moz-center": AlignCenter, "-webkit-auto": AlignStart,
		})))},
		{pTextDecoration, propDef{name: "text-decoration-line", parse: parseDecorationLine, compute: func(_ *styler, st *Style, v any) {
			if d, ok := v.(TextDecoration); ok {
				st.TextDecoration = d
			}
		}, copyFrom: field(func(s *Style) *TextDecoration { return &s.TextDecoration })}},
		{pTextAlignLast, textAlignLastProp()},
		{pTextDecorationColor, colorProp("text-decoration-color", func(s *Style) *Color { return &s.TextDecorationColor }, slotTextDecoration, colorValue{current: true})},
		{pTextDecorationStyle, enumProp("text-decoration-style", false, func(s *Style) *DecorationStyle { return &s.TextDecorationStyle }, decorationStyles)},
		{pTextDecorationThickness, dimProp("text-decoration-thickness", false, func(s *Style) *Dimension { return &s.TextDecorationThickness }, allowPct|allowMixed, kwThickness, false)},
		{pTextDecorationSkipInk, enumProp("text-decoration-skip-ink", true, func(s *Style) *SkipInk { return &s.TextDecorationSkipInk }, map[string]SkipInk{
			"auto": SkipInkAuto, "none": SkipInkNone, "all": SkipInkAll,
		})},
		{pTextUnderlinePosition, propDef{name: "text-underline-position", inherited: true, parse: parseUnderlinePosition, compute: func(_ *styler, st *Style, v any) {
			if u, ok := v.(UnderlinePosition); ok {
				st.TextUnderlinePosition = u
			}
		}, copyFrom: field(func(s *Style) *UnderlinePosition { return &s.TextUnderlinePosition })}},
		{pTextUnderlineOffset, dimProp("text-underline-offset", true, func(s *Style) *Dimension { return &s.TextUnderlineOffset }, allowPct|allowMixed, kwAuto, false)},
		{pTextIndent, propDef{name: "text-indent", inherited: true, parse: parseTextIndent, compute: func(s *styler, st *Style, v any) {
			if t, ok := v.(textIndent); ok {
				st.TextIndent = t.l.dim(s.lc, false)
				st.TextIndentHanging, st.TextIndentEachLine = t.hanging, t.eachLine
			}
		}, copyFrom: func(dst, src *Style) {
			dst.TextIndent, dst.TextIndentHanging, dst.TextIndentEachLine = src.TextIndent, src.TextIndentHanging, src.TextIndentEachLine
		}}},
		{pTextTransform, enumProp("text-transform", true, func(s *Style) *TextTransform { return &s.TextTransform }, map[string]TextTransform{
			"none": TransformNone, "capitalize": Capitalize, "uppercase": Uppercase, "lowercase": Lowercase,
			"math-auto": MathAuto,
		})},
		// letter-spacing and word-spacing take a <length-percentage>, a
		// percentage of the element's font size that stays a percentage
		// in the computed value (CSS Text 4, letter-spacing and word-spacing).
		{pLetterSpacing, dimProp("letter-spacing", true, func(s *Style) *Dimension { return &s.LetterSpacing }, allowPct|allowMixed, kwNormal, false)},
		{pWordSpacing, dimProp("word-spacing", true, func(s *Style) *Dimension { return &s.WordSpacing }, allowPct|allowMixed, kwNormal, false)},
		{pWhiteSpace, whiteSpaceCollapseProp()},
		{pWordBreak, enumProp("word-break", true, func(s *Style) *WordBreak { return &s.WordBreak }, map[string]WordBreak{
			"normal": WordBreakNormal, "break-all": BreakAll, "keep-all": KeepAll, "auto-phrase": KeepAll, "manual": WordBreakManual,
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
			case listName:
				st.ListStyleType, st.ListStyleString = ListNamed, string(t)
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
			"top": CaptionTop, "bottom": CaptionBottom,
		})},
		{pEmptyCells, enumProp("empty-cells", true, func(s *Style) *EmptyCells { return &s.EmptyCells }, map[string]EmptyCells{"show": Show, "hide": Hide})},
		{pTableLayout, enumProp("table-layout", false, func(s *Style) *TableLayout { return &s.TableLayout }, map[string]TableLayout{"auto": TableAuto, "fixed": TableFixed})},
		{pBreakBefore, enumProp("break-before", false, func(s *Style) *Break { return &s.BreakBefore }, breakValues)},
		{pBreakAfter, enumProp("break-after", false, func(s *Style) *Break { return &s.BreakAfter }, breakValues)},
		{pBreakInside, enumProp("break-inside", false, func(s *Style) *BreakInside { return &s.BreakInside }, map[string]BreakInside{
			"auto": InsideAuto, "avoid": InsideAvoid, "avoid-page": InsideAvoidPage, "avoid-column": InsideAvoidColumn, "avoid-region": InsideAuto,
		})},
		{pOrphans, countProp("orphans", func(s *Style) *int { return &s.Orphans })},
		{pWidows, countProp("widows", func(s *Style) *int { return &s.Widows })},
	}
	for _, d := range defs {
		props[d.id] = d.def
	}
	margins := edgeProps("margin-", "", func(s *Style) *[4]Dimension { return &s.Margin }, allowPct|allowMixed, kwAuto)
	paddings := edgeProps("padding-", "", func(s *Style) *[4]Dimension { return &s.Padding }, allowPct|nonNeg|allowMixed, kwNoKw)
	sides := [4]string{"border-top", "border-right", "border-bottom", "border-left"}
	for i := range 4 {
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
	props[pPage] = pageProp()
	props[pCounterReset] = counterProp("counter-reset", 0, func(s *Style) *[]CounterOp { return &s.CounterReset })
	props[pCounterIncrement] = counterProp("counter-increment", 1, func(s *Style) *[]CounterOp { return &s.CounterIncrement })
	props[pCounterSet] = counterProp("counter-set", 0, func(s *Style) *[]CounterOp { return &s.CounterSet })
	props[pQuotes] = quotesProp()
	corners := [4]string{"border-top-left-radius", "border-top-right-radius", "border-bottom-right-radius", "border-bottom-left-radius"}
	for i, n := range corners {
		props[pRadiusTopLeft+propID(i)] = radiusProp(Corner(i), n)
	}
	for id, def := range legacyBoxProps() {
		props[id] = def
	}
	for id, def := range flexProps() {
		props[id] = def
	}
	props[pBgAttachment] = attachmentProp()
	for i, def := range borderImageProps("border-image", func(s *Style) *BorderImage { return &s.BorderImage }, false) {
		props[pBorderImageSource+propID(i)] = def
	}
	for i, def := range borderImageProps("mask-border", func(s *Style) *BorderImage { return &s.MaskBorder }, true) {
		props[pMaskBorderSource+propID(i)] = def
	}
	for id, def := range gridProps() {
		props[id] = def
	}
	for id, def := range textOverflowProps() {
		props[id] = def
	}
	for id, def := range textFontProps() {
		props[id] = def
	}
	for id, def := range fontFeatureProps() {
		props[id] = def
	}
	for id, def := range fontVariationProps() {
		props[id] = def
	}
	for id, def := range textProps() {
		props[id] = def
	}
	for id, def := range rubyProps() {
		props[id] = def
	}
	for id, def := range mathProps() {
		props[id] = def
	}
	for id, def := range emphasisProps() {
		props[id] = def
	}
	for id, def := range initialLetterProps() {
		props[id] = def
	}
	for id, def := range aspectRatioProps() {
		props[id] = def
	}
	for id, def := range containIntrinsicProps() {
		props[id] = def
	}
	props[pBgPositionY] = bgPositionAxis(1)
	gcpmProps()
	for id, def := range overflowClipProps() {
		props[id] = def
	}
	for id, def := range multicolProps() {
		props[id] = def
	}
	for id, def := range compositingProps() {
		props[id] = def
	}
	props[pClipPath] = clipPathProp()
	props[pObjectViewBox] = objectViewBoxProp()
	props[pPrintColorAdjust] = printColorAdjustProp()
	props[pContentVisibility] = contentVisibilityProp()
	props[pMarginTrim] = marginTrimProp()
	for id, def := range textBoxProps() {
		props[id] = def
	}
	for id, def := range baselineProps() {
		props[id] = def
	}
	for id, def := range shapeOutsideProps() {
		props[id] = def
	}
	props[pClip] = clipProp()
	for id, def := range maskProps() {
		props[id] = def
	}
	for id, def := range animationProps() {
		props[id] = def
	}
	for id, def := range motionParsedProps() {
		props[id] = def
	}
	for id, f := range interpolators() {
		props[id].interp = f
	}
	for id, def := range filterProps() {
		props[id] = def
	}
	for id, def := range containerProps() {
		props[id] = def
	}
	props[pColorScheme] = colorSchemeProp()
	props[pWillChange] = willChangeProp()
	for id, def := range anchorProps() {
		props[id] = def
	}
	for id, def := range glyphPaintProps() {
		props[id] = def
	}
	for id, def := range motionProps() {
		props[id] = def
	}
	for id, def := range space3DProps() {
		props[id] = def
	}
	// zoom (CSS Viewport 1, 4): the cascade computes the effective zoom
	// before every length (computeInto), so the longhand's own compute
	// does nothing more.
	props[pZoom] = propDef{
		name: "zoom", parse: parseZoom, compute: func(*styler, *Style, any) {},
		copyFrom: field(func(s *Style) *float64 { return &s.Zoom }),
	}
	// field-sizing: fixed | content (CSS Form Control Styling 1, 7.1).
	props[pFieldSizing] = enumProp("field-sizing", false, func(s *Style) *FieldSizing { return &s.FieldSizing }, map[string]FieldSizing{
		"fixed": FieldFixed, "content": FieldContent,
	})
	// appearance: none | auto | <compat-auto> | textfield |
	// menulist-button (CSS Basic User Interface 4, 7.2).
	props[pAppearance] = enumProp("appearance", false, func(s *Style) *Appearance { return &s.Appearance }, appearanceKeywords)
	props[pAccentColor] = accentColorProp()
	// resize: none | both | horizontal | vertical | block | inline (CSS
	// Basic User Interface 4, 5.1), and Chromium's auto.
	props[pResize] = enumProp("resize", false, func(s *Style) *Resize { return &s.Resize }, map[string]Resize{
		"none": ResizeNone, "both": ResizeBoth, "horizontal": ResizeHorizontal, "vertical": ResizeVertical,
		"block": ResizeBlock, "inline": ResizeInline, "auto": ResizeAuto,
	})
	props[pOverflowY] = overflowAxisProp("overflow-y", func(s *Style) *overflowAxis { return &s.overflowY })
	anchorLengths()
	longhandByName = map[string]propID{}
	for i := range props {
		longhandByName[props[i].name] = propID(i)
	}
	maps.Copy(longhandByName, map[string]propID{
		"overflow-inline": pOverflowX, "overflow-block": pOverflowY,
		"margin-block-start": pMarginTop, "margin-block-end": pMarginBottom,
		"margin-inline-start": pMarginLeft, "margin-inline-end": pMarginRight,
		"padding-block-start": pPaddingTop, "padding-block-end": pPaddingBottom,
		"padding-inline-start": pPaddingLeft, "padding-inline-end": pPaddingRight,
		"inset-block-start": pTop, "inset-block-end": pBottom,
		"inset-inline-start": pLeft, "inset-inline-end": pRight,
		"inline-size": pWidth, "block-size": pHeight,
		"min-inline-size": pMinWidth, "min-block-size": pMinHeight,
		"max-inline-size": pMaxWidth, "max-block-size": pMaxHeight,
		"contain-intrinsic-inline-size": pContainIntrinsicWidth, "contain-intrinsic-block-size": pContainIntrinsicHeight,
		"-webkit-box-decoration-break": pBoxDecorationBreak,
		"-webkit-print-color-adjust":   pPrintColorAdjust,
	})
	maps.Copy(longhandByName, logicalAliases())
	maps.Copy(longhandByName, logicalRadii)
	initShorthands()
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
	"column": Column, "avoid-column": BreakAvoidColumn, "region": BreakAuto, "avoid-region": BreakAuto,
}

var boxEdges = map[string]BoxEdge{"border-box": BorderEdge, "padding-box": PaddingEdge, "content-box": ContentEdge}

// parseBgClip parses background-clip, a box or text a layer.
func parseBgClip(v []token, _ *declCtx) (any, bool) { return bgList(v, bgClipValue) }

// bgClipValue parses one layer of background-clip: a <visual-box> or
// text (CSS Backgrounds 4, 2.1).
func bgClipValue(v []token) (BoxEdge, bool) {
	k, ok := keyword(v)
	if !ok {
		return 0, false
	}
	if k == "text" || k == "-webkit-text" {
		return TextEdge, true
	}
	e, ok := boxEdges[k]
	return e, ok
}

// parseTextIndent parses text-indent (CSS Text 3, 8.1); the hanging and
// each-line keywords are accepted and ignored.
// parseTextIndent parses text-indent (CSS Text 3, 8.1):
// [ <length-percentage> ] && hanging? && each-line?.
func parseTextIndent(v []token, _ *declCtx) (any, bool) {
	var t textIndent
	found := false
	for _, tok := range v {
		switch {
		case tok.isIdent("hanging") && !t.hanging:
			t.hanging = true
			continue
		case tok.isIdent("each-line") && !t.eachLine:
			t.eachLine = true
			continue
		}
		if found {
			return nil, false
		}
		var ok bool
		if t.l, ok = parseLength(tok, allowPct|allowMixed); !ok {
			return nil, false
		}
		found = true
	}
	return t, found
}

// textIndent is a specified text-indent (CSS Text 3, 8.1): a length or
// percentage, hanging and each-line.
type textIndent struct {
	l                 length
	hanging, eachLine bool
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
// litho ignores. auto-phrase alone is invalid: the grammar is none |
// [ space | ideographic-space ] && auto-phrase?.
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
	return w, w != WordSpaceNone
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
	"disclosure-open": DisclosureOpen, "disclosure-closed": DisclosureClosed,
}

// listString is a specified list-style-type <string>.
type listString string

// listName is a specified list-style-type counter style name that
// [ListStyleType] does not cover.
type listName string

// parseListStyleType parses list-style-type (CSS Lists 3, 3.1): none, a
// counter style name, symbols() or a string. A name litho draws itself
// computes to its [ListStyleType]; any other is kept by name, and a name
// no style defines draws as decimal (CSS Counter Styles 3, 2).
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
		if cssWideKeywords[k] {
			return nil, false
		}
		return listName(counterStyleName(t.value)), true
	case tString:
		return listString(t.value), true
	case tFunction:
		if eqFold(t.value, "symbols") {
			if name, ok := parseSymbolsFunction(t, c); ok {
				return listName(name), true
			}
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

// quirkInherit is the color value of the table rule of ua-quirks.css,
// -internal-quirk-inherit: the colour of the document's body element, the
// initial colour when there is none (Quirks Mode Standard, 3.12), as
// Chromium computes it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 computes the
//     colour of a table inside font color=red, or a green div, as the
//     body's text=blue in quirks mode, and keeps color: inherit.
type quirkInherit struct{}

// thCenter is the text-align value of the th rule of ua.css,
// -internal-th-center: center when the parent's computed text-align is
// its initial value, start, the parent's value otherwise, as the HTML
// Standard asks of the user agent sheet in prose (15.3.8) and Chromium
// computes it (its -internal-center).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 computes a th's
//     text-align as left, center, right and -webkit-left under rows of
//     those values, right under a table in a right-aligned block.
type thCenter struct{}

// thCenterProp adds -internal-th-center to the text-align property def,
// a value of the user agent sheet alone.
func thCenterProp(def propDef) propDef {
	parse, compute := def.parse, def.compute
	def.parse = func(v []token, c *declCtx) (any, bool) {
		if k, ok := keyword(v); ok && k == "-internal-th-center" {
			if !c.userAgent {
				return nil, false
			}
			return thCenter{}, true
		}
		return parse(v, c)
	}
	def.compute = func(s *styler, st *Style, v any) {
		if _, ok := v.(thCenter); ok {
			st.TextAlign = AlignCenter
			if s.parent != nil && s.parent.TextAlign != AlignStart {
				st.TextAlign = s.parent.TextAlign
			}
			return
		}
		compute(s, st, v)
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
// (CSS Backgrounds 3, 3.8), not the zero BoxEdge, border-box: the initial
// keyword computes to it.
func originProp() propDef {
	def := bgListProp("background-origin", func(v []token, _ *declCtx) (any, bool) {
		return bgList(v, func(p []token) (BoxEdge, bool) {
			k, ok := keyword(p)
			e, known := boxEdges[k]
			return e, ok && known
		})
	}, func(s *Style) *BoxEdge { return &s.BackgroundOrigin }, func(r *BackgroundLists) *[]BoxEdge { return &r.Origins }, same[BoxEdge])
	def.initial = []BoxEdge{PaddingEdge}
	return def
}
