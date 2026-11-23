package css

// legacyBox holds the properties of the legacy flexible box, display:
// -webkit-box and -webkit-inline-box, which the Compatibility Standard
// names and Chromium lays out as a flex container of its own rules: the
// container's -webkit-box-pack, -webkit-box-align and
// -webkit-box-direction, an item's -webkit-box-flex and
// -webkit-box-ordinal-group. -webkit-box-orient is
// [LineClamp]'s, since it also decides the legacy line clamp. None is
// inherited.
type legacyBox struct {
	pack    legacyPack
	align   legacyAlign
	reverse bool
	// flex is the item's -webkit-box-flex, which may be negative.
	flex float64
	// group is the item's -webkit-box-ordinal-group, 0 for its initial
	// value 1.
	group int
}

// legacyPack is -webkit-box-pack.
type legacyPack uint8

// The values of -webkit-box-pack.
const (
	packStart legacyPack = iota
	packEnd
	packCenter
	packJustify
)

// legacyAlign is -webkit-box-align.
type legacyAlign uint8

// The values of -webkit-box-align.
const (
	alignStretch legacyAlign = iota
	alignStart
	alignEnd
	alignCenter
	alignBaseline
)

// legacyBoxProps returns the longhands of the legacy flexible box. Their
// grammars are Chromium's (CSS.supports): pack start, end, center or
// justify; align start, end, center, baseline or stretch; direction
// normal or reverse; flex any number; an ordinal group a positive
// integer.
func legacyBoxProps() map[propID]propDef {
	return map[propID]propDef{
		pWebkitBoxPack: enumProp("-webkit-box-pack", false, func(s *Style) *legacyPack { return &s.legacyBox.pack }, map[string]legacyPack{
			"start": packStart, "end": packEnd, "center": packCenter, "justify": packJustify,
		}),
		pWebkitBoxAlign: enumProp("-webkit-box-align", false, func(s *Style) *legacyAlign { return &s.legacyBox.align }, map[string]legacyAlign{
			"stretch": alignStretch, "start": alignStart, "end": alignEnd, "center": alignCenter, "baseline": alignBaseline,
		}),
		pWebkitBoxDirection: {
			name: "-webkit-box-direction",
			parse: func(v []token, _ *declCtx) (any, bool) {
				switch k, _ := keyword(v); k {
				case "normal":
					return false, true
				case "reverse":
					return true, true
				}
				return nil, false
			},
			compute: func(_ *styler, st *Style, v any) {
				if b, ok := v.(bool); ok {
					st.legacyBox.reverse = b
				}
			},
			copyFrom: field(func(s *Style) *bool { return &s.legacyBox.reverse }),
		},
		pWebkitBoxFlex: {
			name: "-webkit-box-flex",
			parse: func(v []token, _ *declCtx) (any, bool) {
				t, ok := single(v)
				if !ok || (t.typ != tNumber && t.typ != tFunction) {
					return nil, false
				}
				n, ok := parseNumber(t)
				if !ok {
					return nil, false
				}
				return max(min(n, maxFlexFactor), -maxFlexFactor), true
			},
			compute: func(_ *styler, st *Style, v any) {
				if n, ok := v.(float64); ok {
					st.legacyBox.flex = n
				}
			},
			copyFrom: field(func(s *Style) *float64 { return &s.legacyBox.flex }),
		},
		pWebkitBoxOrdinalGroup: {
			name: "-webkit-box-ordinal-group",
			parse: func(v []token, _ *declCtx) (any, bool) {
				t, ok := single(v)
				if !ok {
					return nil, false
				}
				n, ok := parseInteger(t)
				if !ok || n < 1 {
					return nil, false
				}
				return n, true
			},
			compute: func(_ *styler, st *Style, v any) {
				if n, ok := v.(int); ok {
					st.legacyBox.group = n
				}
			},
			copyFrom: field(func(s *Style) *int { return &s.legacyBox.group }),
		},
	}
}

// fixupLegacyBox lays a display: -webkit-box or -webkit-inline-box
// container out as Chromium does: a flex container, except when it is
// vertical and clamps its lines (-webkit-line-clamp), which stays the
// block container the clamp applies to. Its main axis follows
// -webkit-box-orient and -webkit-box-direction, it never wraps, and
// -webkit-box-pack and -webkit-box-align stand for justify-content and
// align-items, whose own values it ignores, as it ignores flex-direction
// and flex-wrap.
//
// # Corpus evidence
//
//   - probe, html: on 49 containers of Chromium 156, a -webkit-box lays
//     its items out as display: flex with flex-wrap: nowrap, pack start,
//     end, center and justify as justify-content flex-start, flex-end,
//     center and space-between, align as align-items, orient vertical as
//     flex-direction: column, direction reverse as the reversed
//     direction; a -webkit-box-direction is not inherited; author
//     justify-content, align-items, flex-direction and flex-wrap change
//     nothing, gap applies.
func fixupLegacyBox(st *Style) {
	c := &st.LineClamp
	if !c.webkitBox || c.webkitVertical && c.webkitLines > 0 {
		return
	}
	switch st.Display { //nolint:exhaustive // a legacy box computes to a block or an inline block
	case Block:
		st.Display = Flex
	case InlineBlock:
		st.Display = InlineFlex
	default:
		return
	}
	st.flexOrGrid = false
	st.FlexWrap = NoWrap
	switch {
	case c.webkitVertical && st.legacyBox.reverse:
		st.FlexDirection = FlexColumnReverse
	case c.webkitVertical:
		st.FlexDirection = FlexColumn
	case st.legacyBox.reverse:
		st.FlexDirection = FlexRowReverse
	default:
		st.FlexDirection = FlexRow
	}
	st.JustifyContent = [...]BoxAlign{packStart: BoxFlexStart, packEnd: BoxFlexEnd, packCenter: BoxCenter, packJustify: BoxSpaceBetween}[st.legacyBox.pack]
	st.AlignItems = [...]BoxAlign{alignStretch: BoxStretch, alignStart: BoxFlexStart, alignEnd: BoxFlexEnd, alignCenter: BoxCenter, alignBaseline: BoxBaseline}[st.legacyBox.align]
	st.AlignContent = BoxNormal
}

// legacyBoxItem gives the item st of a legacy flexible box parent its
// flex factors and order as Chromium does: -webkit-box-flex grows and
// shrinks it alike, a negative factor as 0; -webkit-box-ordinal-group
// orders it; flex-grow, flex-shrink, order and align-self change nothing,
// flex-basis applies. An auto flex basis of an item of auto width in a
// horizontal box is its fit-content width, the box's width the room it
// fits, and no automatic minimum size holds the item.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 lays out an item of -webkit-box-flex 0
//     wider than the box unshrunk, a text item of auto width at its
//     fit-content width, an item with a long word and -webkit-box-flex 1
//     shrunk below the word, items of -webkit-box-ordinal-group 2, 1 and
//     none in the order 1, none, 2, and ignores flex: 1 but its 0 basis,
//     order and align-self on the items.
func legacyBoxItem(st, parent *Style) {
	if !parent.LineClamp.webkitBox || parent.Display != Flex && parent.Display != InlineFlex {
		return
	}
	f := max(st.legacyBox.flex, 0)
	st.FlexGrow, st.FlexShrink = f, f
	st.Order = max(st.legacyBox.group, 1)
	st.AlignSelf = BoxAuto
	st.MinWidthAuto, st.MinHeightAuto = false, false
	if st.FlexBasis.Kind == Auto && !parent.LineClamp.webkitVertical && st.Width.Kind == Auto {
		st.FlexBasis = Dimension{Kind: FitContent}
	}
}
