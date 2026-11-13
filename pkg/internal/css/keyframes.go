package css

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// keyframesRule is an @keyframes rule (CSS Animations 1, 3): its name and
// its keyframe blocks, in order, each at one or more offsets.
type keyframesRule struct {
	name   string
	blocks []keyframeBlock
	cond   *mediaCond
	// ids and custom are the longhands and the custom properties the
	// keyframes set, as animated returns them.
	ids    []propID
	custom []string
}

// keyframeBlock is one <keyframe-block>: the offsets its selectors name,
// from 0 to 1, its declarations, and the easing and composite operation
// its animation-timing-function and animation-composition set, nil for
// the animation's own.
type keyframeBlock struct {
	offsets     []float64
	decls       []Declaration
	easing      *Easing
	composition *Composition
}

// maxKeyframeOffsets caps the offsets of one @keyframes rule: 1000.
// Anti-DoS guard, not spec-mandated: each animated property walks the
// offsets once per element, and real rules name a handful.
const maxKeyframeOffsets = 1000

// notAnimatable holds the longhands whose definitions say they are not
// animatable (Web Animations 1, 5.2), which a keyframe does not set:
// the animation, transition and timeline longhands but view-timeline-inset,
// direction, unicode-bidi and writing-mode (CSS Writing Modes 4, 2.1, 2.2
// and 3.1) and contain (CSS Containment 2, 3). Immutable lookup table.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 animates none of
//     direction, writing-mode, contain, will-change, the transition and
//     animation durations in a keyframe; it computes a keyframe's
//     unicode-bidi at every progress, which litho does not follow.
var notAnimatable = func() [numProps]bool {
	var on [numProps]bool
	for _, id := range []propID{
		pDirection, pUnicodeBidi, pWritingMode, pContain,
		pAnimationName, pAnimationDuration, pAnimationTimingFunction, pAnimationIterationCount,
		pAnimationDirection, pAnimationFillMode, pAnimationPlayState, pAnimationDelay,
		pAnimationComposition, pAnimationTimeline, pAnimationRangeStart, pAnimationRangeEnd,
		pScrollTimelineName, pViewTimelineName, pTransitionProperty, pTransitionDuration,
		pTransitionTimingFunction, pTransitionDelay, pTransitionBehavior, pScrollTimelineAxis,
		pViewTimelineAxis, pTimelineScope,
	} {
		on[id] = true
	}
	return on
}()

// silentInKeyframes holds the longhands a keyframe ignores without a
// warning: the animation longhands, which CSS Animations 1, 3 ignores in a
// keyframe block, and the longhands litho parses and holds no value of,
// whose animation would change nothing it prints. Immutable lookup table.
var silentInKeyframes = func() [numProps]bool {
	var on [numProps]bool
	for _, id := range []propID{
		pAnimationName, pAnimationDuration, pAnimationTimingFunction, pAnimationIterationCount,
		pAnimationDirection, pAnimationFillMode, pAnimationPlayState, pAnimationDelay,
		pAnimationComposition, pAnimationTimeline, pAnimationRangeStart, pAnimationRangeEnd,
		pTransitionProperty, pTransitionDuration, pTransitionTimingFunction, pTransitionDelay,
		pTransitionBehavior, pScrollTimelineAxis, pViewTimelineAxis, pViewTimelineInset, pTimelineScope,
	} {
		on[id] = true
	}
	return on
}()

// parseKeyframesRule parses the prelude and block of an @keyframes rule
// (CSS Animations 1, 3): a <keyframes-name>, then keyframe blocks. A
// block whose selector list holds an invalid selector is ignored, and so
// is a declaration marked !important. In a block, animation-timing-function
// sets the easing to the next keyframe (3.1), animation-composition the
// composite operation (CSS Animations 2, 4.10); the other animation,
// transition and timeline properties, and those not animatable, are
// ignored.
func parseKeyframesRule(prelude, block []token, c *declCtx) (*keyframesRule, bool) {
	pre := significant(prelude)
	if len(pre) != 1 {
		return nil, false
	}
	name, ok := parseKeyframesName(pre[0])
	if !ok {
		return nil, false
	}
	r := &keyframesRule{name: name}
	count := 0
	newListParser(block).rules(false, func(it item) bool {
		if it.kind != itemQualified {
			return true
		}
		offsets, ok := keyframeSelectors(it.prelude, c)
		if !ok || count+len(offsets) > maxKeyframeOffsets {
			return true
		}
		count += len(offsets)
		kb := keyframeBlock{offsets: offsets}
		newListParser(it.block).declarations(func(d item) bool {
			if d.kind != itemDeclaration || d.important {
				return true
			}
			switch n := lower(d.name); n {
			case "animation-timing-function":
				if e, ok := parseEasingItem(significant(d.value)); ok {
					kb.easing = &e
				}
				return true
			case "animation-composition":
				if k, ok := keywordItem(compositionKeywords)(significant(d.value)); ok {
					kb.composition = &k
				}
				return true
			}
			decl := c.declare(d.name, d.value, false)
			if !decl.declares() {
				return true
			}
			kept := decl.entries[:0:0]
			for _, e := range decl.entries {
				if silentInKeyframes[e.id] {
					continue
				}
				if notAnimatable[e.id] {
					c.warn(warn.CSSUnsupported, warn.Approximated, "%q is not animatable: the keyframe's declaration was ignored", props[e.id].name)
					continue
				}
				kept = append(kept, e)
			}
			if len(decl.entries) > 0 && len(kept) == 0 {
				return true
			}
			decl.entries = kept
			kb.decls = append(kb.decls, decl)
			return true
		})
		r.blocks = append(r.blocks, kb)
		return true
	})
	r.ids, r.custom = r.animated()
	return r, true
}

// keyframeSelectors parses a <keyframe-selector> list (CSS Animations 1,
// 3): from, to and percentages from 0% to 100%, as offsets from 0 to 1.
// It fails on any other selector. A named timeline range selector
// (Scroll-driven Animations 1, Appendix A) parses and is ignored with a warning:
// litho holds no view timeline whose ranges it would resolve against.
func keyframeSelectors(prelude []token, c *declCtx) ([]float64, bool) {
	var out []float64
	for _, part := range splitCommas(trimWS(prelude)) {
		sel := significant(part)
		switch {
		case len(sel) == 1 && sel[0].isIdent("from"):
			out = append(out, 0)
		case len(sel) == 1 && sel[0].isIdent("to"):
			out = append(out, 1)
		case len(sel) == 1 && sel[0].typ == tPercentage && sel[0].num >= 0 && sel[0].num <= 100:
			out = append(out, sel[0].num/100)
		case len(sel) == 2 && sel[0].typ == tIdent && rangeNames[lower(sel[0].value)] != RangeNone && sel[1].typ == tPercentage:
			c.warn(warn.CSSUnsupported, warn.Approximated, "a keyframe at a named timeline range offset was ignored")
			return nil, false
		default:
			return nil, false
		}
	}
	return out, len(out) > 0
}

// keyframe is the value one keyframe gives one property: its offset, the
// declaration that sets it, nil for the underlying value of an implicit
// keyframe, and the easing to the next keyframe, nil for the
// animation's.
type keyframe struct {
	offset float64
	decl   *Declaration
	easing *Easing
}

// propertyKeyframes returns the keyframes of the longhand id, or of the
// custom property name when it is not empty, sorted by offset (CSS
// Animations 1, 3): at each offset the last declaration in the rule wins,
// and the 0% and 100% keyframes the rule leaves out take the underlying
// value.
func (r *keyframesRule) propertyKeyframes(id propID, name string) []keyframe {
	var out []keyframe
	for bi := range r.blocks {
		b := &r.blocks[bi]
		var d *Declaration
		for di := range b.decls {
			x := &b.decls[di]
			switch {
			case name != "":
				if x.isCustom && x.Property == name {
					d = x
				}
			case x.isCustom:
			case len(x.pending) > 0:
				if slices.Contains(longhandsOf(x.Property), id) {
					d = x
				}
			default:
				for _, e := range x.entries {
					if e.id == id {
						d = x
					}
				}
			}
		}
		if d == nil {
			continue
		}
		for _, o := range b.offsets {
			i := slices.IndexFunc(out, func(k keyframe) bool { return k.offset == o })
			if i < 0 {
				out = append(out, keyframe{offset: o})
				i = len(out) - 1
			}
			out[i].decl, out[i].easing = d, b.easing
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.SortStableFunc(out, func(a, b keyframe) int {
		switch {
		case a.offset < b.offset:
			return -1
		case a.offset > b.offset:
			return 1
		}
		return 0
	})
	if out[0].offset != 0 {
		out = slices.Insert(out, 0, keyframe{offset: 0})
	}
	if out[len(out)-1].offset != 1 {
		out = append(out, keyframe{offset: 1})
	}
	return out
}

// animated returns the longhands and the custom properties the rule's
// keyframes set, each once, in the order of [propID] and of first
// appearance.
func (r *keyframesRule) animated() ([]propID, []string) {
	var set [numProps]bool
	var custom []string
	for _, b := range r.blocks {
		for di := range b.decls {
			d := &b.decls[di]
			switch {
			case d.isCustom:
				if !slices.Contains(custom, d.Property) {
					custom = append(custom, d.Property)
				}
			case len(d.pending) > 0:
				for _, id := range longhandsOf(d.Property) {
					set[id] = !notAnimatable[id] && !silentInKeyframes[id]
				}
			default:
				for _, e := range d.entries {
					set[e.id] = true
				}
			}
		}
	}
	var ids []propID
	for id := range numProps {
		if set[id] {
			ids = append(ids, id)
		}
	}
	return ids, custom
}

// keyframesAt holds the @keyframes rules a resolver applies, by name:
// the last rule of a name in document order whose media condition
// matches (CSS Animations 1, 3).
type keyframesAt map[string]*keyframesRule

// discreteAnimation holds the longhands whose definitions animate them in
// discrete steps (Web Animations 1, 5.2: "Animation type: discrete"),
// which flip at the midpoint without a warning; a longhand neither in it
// nor interpolated by litho flips too, with one. Immutable lookup table.
var discreteAnimation = func() [numProps]bool {
	var on [numProps]bool
	for _, id := range []propID{
		pPosition, pClear, pVisibility, pOverflow, pBoxSizing, pBorderTopStyle, pBorderRightStyle,
		pBorderBottomStyle, pBorderLeftStyle, pBgImage, pBgRepeat, pFontFamily, pFontVariant, pTextAlign,
		pTextDecoration, pTextAlignLast, pTextDecorationStyle, pTextTransform, pWhiteSpace, pWordBreak,
		pOverflowWrap, pLineBreak, pWordSpaceTransform, pListStyleType, pListStylePosition,
		pListStyleImage, pBorderCollapse, pCaptionSide, pEmptyCells, pTableLayout, pBreakBefore,
		pBreakAfter, pBreakInside, pObjectFit, pContent, pQuotes, pFlexDirection, pFlexWrap,
		pJustifyContent, pAlignContent, pAlignItems, pAlignSelf, pPage, pBorderImageSource,
		pBorderImageRepeat, pMaskBorderSource, pMaskBorderRepeat, pBgAttachment, pGridTemplateAreas,
		pGridAutoFlow, pGridRowStart, pGridRowEnd, pGridColumnStart, pGridColumnEnd, pJustifyItems,
		pJustifySelf, pBlockEllipsis, pContinue, pTextJustify, pTextAutospace, pFontSynthesisWeight,
		pFontSynthesisStyle, pTextSpacingTrim, pStringSet, pFootnoteDisplay, pFootnotePolicy, pBookmarkLabel, pBookmarkState,
		pColumnRuleStyle, pColumnSpan, pColumnFill, pColumnWrap, pBoxDecorationBreak, pFontKerning,
		pTransformBox, pTextDecorationSkipInk, pTextUnderlinePosition, pHyphens, pHyphenateCharacter,
		pTextWrapMode, pTextWrapStyle, pRubyPosition, pTextEmphasisStyle, pTextEmphasisPosition,
		pFontFeatureSettings, pFontVariantLigatures, pFontVariantNumeric, pFontVariantEastAsian,
		pFontVariantPosition, pFontVariantAlternates, pFontVariantEmoji, pFontLanguageOverride,
		pFontSynthesisSmallCaps, pMixBlendMode, pBgBlendMode, pIsolation, pMaskImage, pMaskMode,
		pMaskRepeat, pMaskOrigin, pMaskClip, pMaskComposite, pMaskType, pBaselineSource, pTextBoxTrim,
		pTextBoxEdge, pPrintColorAdjust, pFontOpticalSizing, pMarginTrim,
	} {
		on[id] = true
	}
	return on
}()
