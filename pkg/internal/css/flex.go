package css

// This file parses the properties of flexible box layout (CSS Flexbox 1,
// 5 and 7) and the box alignment properties that apply to flex containers
// and items (CSS Box Alignment 3, 5, 6 and 8).

// flexDirections are the flex-direction keywords (CSS Flexbox 1, 5.1).
var flexDirections = map[string]FlexDirection{
	"row": FlexRow, "row-reverse": FlexRowReverse, "column": FlexColumn, "column-reverse": FlexColumnReverse,
}

// flexWraps are the flex-wrap keywords (CSS Flexbox 1, 5.2).
var flexWraps = map[string]FlexWrap{"nowrap": NoWrap, "wrap": Wrap, "wrap-reverse": WrapReverse}

// alignSet is the set of keywords an alignment property takes.
type alignSet uint8

const (
	// alignContent is justify-content and align-content.
	alignContent alignSet = iota
	// alignItems is align-items.
	alignItems
	// alignSelf is align-self, align-items' keywords and auto.
	alignSelf
)

// contentPositions are the <content-position> keywords, and
// selfPositions the <self-position> ones (CSS Box Alignment 3, 4.3).
var (
	contentPositions = map[string]BoxAlign{
		"center": BoxCenter, "start": BoxStart, "end": BoxEnd, "flex-start": BoxFlexStart, "flex-end": BoxFlexEnd,
	}
	selfPositions = map[string]BoxAlign{
		"center": BoxCenter, "start": BoxStart, "end": BoxEnd, "flex-start": BoxFlexStart, "flex-end": BoxFlexEnd,
		"self-start": BoxSelfStart, "self-end": BoxSelfEnd,
	}
	contentDistributions = map[string]BoxAlign{
		"space-between": BoxSpaceBetween, "space-around": BoxSpaceAround, "space-evenly": BoxSpaceEvenly, "stretch": BoxStretch,
	}
)

// parseAlign parses a value of an alignment property taking the set (CSS
// Box Alignment 3, 5.1, 5.2, 6.1 and 6.2), with justify set for
// justify-content, which also takes left and right but no baseline.
func parseAlign(v []token, set alignSet, justify bool) (BoxAlign, bool) {
	if len(v) < 1 || len(v) > 2 {
		return 0, false
	}
	words := make([]string, len(v))
	for i, t := range v {
		if t.typ != tIdent {
			return 0, false
		}
		words[i] = lower(t.value)
	}
	if len(words) == 1 {
		switch w := words[0]; {
		case w == "normal":
			return BoxNormal, true
		case w == "auto":
			return BoxAuto, set == alignSelf
		case w == "baseline":
			return BoxBaseline, !justify
		case w == "stretch" && set != alignContent:
			return BoxStretch, true
		case (w == "left" || w == "right") && justify:
			if w == "left" {
				return BoxLeft, true
			}
			return BoxRight, true
		}
	}
	if len(words) == 2 && (words[0] == "first" || words[0] == "last") && words[1] == "baseline" {
		if justify {
			return 0, false
		}
		if words[0] == "last" {
			return BoxLastBaseline, true
		}
		return BoxBaseline, true
	}
	if len(words) == 1 && set == alignContent {
		if a, ok := contentDistributions[words[0]]; ok {
			return a, true
		}
	}
	// [ <overflow-position>? <position> ].
	safe := false
	pos := words[0]
	if len(words) == 2 {
		switch words[0] {
		case "safe":
			safe = true
		case "unsafe":
		default:
			return 0, false
		}
		pos = words[1]
	}
	positions := selfPositions
	if set == alignContent {
		positions = contentPositions
	}
	a, ok := positions[pos]
	if !ok && justify && (pos == "left" || pos == "right") {
		a, ok = BoxLeft, true
		if pos == "right" {
			a = BoxRight
		}
	}
	if !ok {
		return 0, false
	}
	if safe {
		a |= BoxSafe
	}
	return a, true
}

// alignProp defines an alignment longhand stored at f.
func alignProp(name string, f func(*Style) *BoxAlign, set alignSet, justify bool) propDef {
	return propDef{
		name: name,
		parse: func(v []token, _ *declCtx) (any, bool) {
			a, ok := parseAlign(v, set, justify)
			return a, ok
		},
		compute: func(_ *styler, st *Style, v any) {
			if a, ok := v.(BoxAlign); ok {
				*f(st) = a
			}
		},
		copyFrom: field(f),
	}
}

// factorProp defines flex-grow or flex-shrink: a non-negative number
// (CSS Flexbox 1, 7.3.1 and 7.3.2).
func factorProp(name string, f func(*Style) *float64) propDef {
	return propDef{
		name: name,
		parse: func(v []token, _ *declCtx) (any, bool) {
			n, ok := parseFactor(v)
			return n, ok
		},
		compute: func(_ *styler, st *Style, v any) {
			if n, ok := v.(float64); ok {
				*f(st) = n
			}
		},
		copyFrom: field(f),
	}
}

// parseFactor parses a non-negative <number>.
func parseFactor(v []token) (float64, bool) {
	t, ok := single(v)
	if !ok || (t.typ != tNumber && t.typ != tFunction) {
		return 0, false
	}
	n, ok := parseNumber(t)
	if !ok || n < 0 {
		return 0, false
	}
	return min(n, maxFlexFactor), true
}

// maxFlexFactor bounds a flex factor. Overflow guard, not spec-mandated:
// the layout sums the factors of a line, and a factor of a billion
// already takes all of a line's free space.
const maxFlexFactor = 1e9

// kwFlexBasis are the keywords of flex-basis (CSS Flexbox 1, 7.3.3): the
// intrinsic sizing keywords compute to content, the size of the content.
var kwFlexBasis = map[string]DimKind{
	"auto": Auto, "content": FlexContent,
	"min-content": FlexContent, "max-content": FlexContent, "fit-content": FlexContent,
}

// flexBasisProp defines flex-basis: auto, content, or a non-negative
// length or percentage.
func flexBasisProp() propDef {
	return dimProp("flex-basis", false, func(s *Style) *Dimension { return &s.FlexBasis }, allowPct|nonNeg, kwFlexBasis, false)
}

// gapProp defines row-gap or column-gap: normal, or a non-negative length
// or percentage (CSS Box Alignment 3, 8.1).
func gapProp(name string, f func(*Style) *Dimension) propDef {
	return dimProp(name, false, f, allowPct|nonNeg, kwNormal, false)
}

// orderProp defines order: an integer (CSS Flexbox 1, 5.4).
func orderProp() propDef {
	return propDef{
		name: "order",
		parse: func(v []token, _ *declCtx) (any, bool) {
			t, ok := single(v)
			if !ok {
				return nil, false
			}
			n, ok := parseInteger(t)
			return n, ok
		},
		compute: func(_ *styler, st *Style, v any) {
			if n, ok := v.(int); ok {
				st.Order = n
			}
		},
		copyFrom: field(func(s *Style) *int { return &s.Order }),
	}
}

// minAuto is the specified auto keyword of min-width or min-height.
type minAuto struct{}

// minSizeProp defines min-width or min-height stored at f, its auto flag
// at autoF: auto, the initial value, computes to Pts(0) with the flag set
// (CSS Sizing 3, 4.4; CSS Flexbox 1, 4.5); the intrinsic sizing keywords
// compute to 0.
func minSizeProp(name string, f func(*Style) *Dimension, autoF func(*Style) *bool) propDef {
	def := dimProp(name, false, f, allowPct|nonNeg, kwMinSizing, false)
	parse, compute := def.parse, def.compute
	def.parse = func(v []token, c *declCtx) (any, bool) {
		if k, ok := keyword(v); ok && k == "auto" {
			return minAuto{}, true
		}
		return parse(v, c)
	}
	def.compute = func(s *styler, st *Style, v any) {
		if _, ok := v.(minAuto); ok {
			*f(st) = Pts(0)
			*autoF(st) = true
			return
		}
		if _, ok := v.(dimSpec); ok {
			*autoF(st) = false
		}
		compute(s, st, v)
	}
	def.copyFrom = func(dst, src *Style) {
		*f(dst) = *f(src)
		*autoF(dst) = *autoF(src)
	}
	return def
}

// flexProps returns the longhands of flexible box layout and alignment.
func flexProps() map[propID]propDef {
	return map[propID]propDef{
		pFlexDirection:  enumProp("flex-direction", false, func(s *Style) *FlexDirection { return &s.FlexDirection }, flexDirections),
		pFlexWrap:       enumProp("flex-wrap", false, func(s *Style) *FlexWrap { return &s.FlexWrap }, flexWraps),
		pOrder:          orderProp(),
		pFlexGrow:       factorProp("flex-grow", func(s *Style) *float64 { return &s.FlexGrow }),
		pFlexShrink:     factorProp("flex-shrink", func(s *Style) *float64 { return &s.FlexShrink }),
		pFlexBasis:      flexBasisProp(),
		pJustifyContent: alignProp("justify-content", func(s *Style) *BoxAlign { return &s.JustifyContent }, alignContent, true),
		pAlignContent:   alignProp("align-content", func(s *Style) *BoxAlign { return &s.AlignContent }, alignContent, false),
		pAlignItems:     alignProp("align-items", func(s *Style) *BoxAlign { return &s.AlignItems }, alignItems, false),
		pAlignSelf:      alignProp("align-self", func(s *Style) *BoxAlign { return &s.AlignSelf }, alignSelf, false),
		pRowGap:         gapProp("row-gap", func(s *Style) *Dimension { return &s.RowGap }),
		pColumnGap:      gapProp("column-gap", func(s *Style) *Dimension { return &s.ColumnGap }),
	}
}

// flexAliases are the legacy and prefixed names of the flex longhands.
var flexAliases = map[string]propID{
	"grid-row-gap": pRowGap, "grid-column-gap": pColumnGap,
	"-webkit-flex-direction": pFlexDirection, "-webkit-flex-wrap": pFlexWrap, "-webkit-order": pOrder,
	"-webkit-flex-grow": pFlexGrow, "-webkit-flex-shrink": pFlexShrink, "-webkit-flex-basis": pFlexBasis,
	"-webkit-justify-content": pJustifyContent, "-webkit-align-content": pAlignContent,
	"-webkit-align-items": pAlignItems, "-webkit-align-self": pAlignSelf,
}

// expandFlex expands the flex shorthand (CSS Flexbox 1, 7.1): none |
// [ <'flex-grow'> <'flex-shrink'>? || <'flex-basis'> ]. none is 0 0 auto,
// auto is 1 1 auto; a flex-shrink left out is 1 and a flex basis left out
// 0%, as Chromium computes it (a lone number n is n 1 0%).
func expandFlex(v []token, c *declCtx) ([]entry, bool) {
	if k, ok := keyword(v); ok {
		switch k {
		case "none":
			return flexEntries(0, 0, dimSpec{kind: Auto}), true
		case "auto":
			return flexEntries(1, 1, dimSpec{kind: Auto}), true
		}
	}
	if len(v) < 1 || len(v) > 3 {
		return nil, false
	}
	grow, shrink := 1.0, 1.0
	var basis any = dimSpec{kind: Points, l: length{hasPct: true}}
	i := 0
	factors := func() bool {
		n, ok := parseFactor(v[i : i+1])
		if !ok {
			return false
		}
		grow, i = n, i+1
		if i < len(v) {
			if n, ok := parseFactor(v[i : i+1]); ok {
				shrink, i = n, i+1
			}
		}
		return true
	}
	parseBasis := func() bool {
		b, ok := props[pFlexBasis].parse(v[i:i+1], c)
		if ok {
			basis, i = b, i+1
		}
		return ok
	}
	if factors() {
		if i < len(v) && !parseBasis() {
			return nil, false
		}
	} else {
		if !parseBasis() {
			return nil, false
		}
		if i < len(v) && !factors() {
			return nil, false
		}
	}
	if i != len(v) {
		return nil, false
	}
	return []entry{{pFlexGrow, grow}, {pFlexShrink, shrink}, {pFlexBasis, basis}}, true
}

// flexEntries returns the longhands of flex: grow shrink basis.
func flexEntries(grow, shrink float64, basis dimSpec) []entry {
	return []entry{{pFlexGrow, grow}, {pFlexShrink, shrink}, {pFlexBasis, basis}}
}

// expandFlexFlow expands flex-flow (CSS Flexbox 1, 5.3): flex-direction
// || flex-wrap.
func expandFlexFlow(v []token, _ *declCtx) ([]entry, bool) {
	if len(v) < 1 || len(v) > 2 {
		return nil, false
	}
	var dir, wrap any = wideInitial, wideInitial
	var hasDir, hasWrap bool
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		k := lower(t.value)
		if d, ok := flexDirections[k]; ok && !hasDir {
			dir, hasDir = d, true
			continue
		}
		if w, ok := flexWraps[k]; ok && !hasWrap {
			wrap, hasWrap = w, true
			continue
		}
		return nil, false
	}
	return []entry{{pFlexDirection, dir}, {pFlexWrap, wrap}}, true
}

// expandGap expands gap (CSS Box Alignment 3, 8.3): row-gap then
// column-gap, one value for both.
func expandGap(v []token, c *declCtx) ([]entry, bool) {
	if len(v) < 1 || len(v) > 2 {
		return nil, false
	}
	row, ok := props[pRowGap].parse(v[:1], c)
	if !ok {
		return nil, false
	}
	col := row
	if len(v) == 2 {
		if col, ok = props[pColumnGap].parse(v[1:], c); !ok {
			return nil, false
		}
	}
	return []entry{{pRowGap, row}, {pColumnGap, col}}, true
}

// placeShorthand expands place-content, place-items or place-self (CSS
// Box Alignment 3, 5.3, 6.3 and 6.4): the align value, then the justify
// one, which defaults to the align one. justify is the justify longhand,
// numProps for place-items and place-self, whose justify half applies to
// no flex layout and is checked for its shape alone.
func placeShorthand(align, justify propID) shorthandDef {
	ids := []propID{align}
	if justify != numProps {
		ids = append(ids, justify)
	}
	return shorthandDef{
		longhands: ids,
		expand: func(v []token, c *declCtx) ([]entry, bool) {
			for split := 1; split <= min(len(v), 2); split++ {
				a, ok := props[align].parse(v[:split], c)
				if !ok {
					continue
				}
				rest := v[split:]
				if len(rest) > 2 {
					return nil, false
				}
				out := make([]entry, 0, 2)
				out = append(out, entry{align, a})
				if justify == numProps {
					for _, t := range rest {
						if t.typ != tIdent {
							return nil, false
						}
					}
					return out, true
				}
				if len(rest) == 0 {
					rest = v[:split]
				}
				j, ok := props[justify].parse(rest, c)
				if !ok {
					if x, _ := a.(BoxAlign); x != BoxBaseline && x != BoxLastBaseline || len(rest) != split {
						return nil, false
					}
					j = BoxStart
				}
				return append(out, entry{justify, j}), true
			}
			return nil, false
		},
	}
}

// flexShorthands returns the shorthands of flexible box layout and
// alignment.
func flexShorthands() map[string]shorthandDef {
	flex := shorthandDef{longhands: []propID{pFlexGrow, pFlexShrink, pFlexBasis}, expand: expandFlex}
	flow := shorthandDef{longhands: []propID{pFlexDirection, pFlexWrap}, expand: expandFlexFlow}
	gap := shorthandDef{longhands: []propID{pRowGap, pColumnGap}, expand: expandGap}
	return map[string]shorthandDef{
		"flex": flex, "-webkit-flex": flex,
		"flex-flow": flow, "-webkit-flex-flow": flow,
		"gap": gap, "grid-gap": gap,
		"place-content": placeShorthand(pAlignContent, pJustifyContent),
		"place-items":   placeShorthand(pAlignItems, numProps),
		"place-self":    placeShorthand(pAlignSelf, numProps),
	}
}

// blockify returns the block-level equivalent of an inline-level display
// value (CSS Display 3, 2.7): what a float, an absolutely positioned box,
// the root and a flex item take. The internal table boxes become blocks.
func blockify(d Display) (Display, bool) {
	switch d { //nolint:exhaustive // the other display values are already block-level
	case InlineTable:
		return Table, true
	case Inline, InlineBlock:
		return Block, true
	case InlineListItem:
		return ListItem, true
	case InlineFlex:
		return Flex, true
	case TableRowGroup, TableColumn, TableColumnGroup,
		TableHeaderGroup, TableFooterGroup, TableRow, TableCell, TableCaption:
		return Block, false
	}
	return d, false
}

// isFlexContainer reports a flex container's display value.
func isFlexContainer(d Display) bool { return d == Flex || d == InlineFlex }
