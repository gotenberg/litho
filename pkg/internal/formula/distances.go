package formula

import (
	mt "github.com/gotenberg/litho/pkg/internal/typography/mathtable"
)

// Distances lays a formula whose first available font has no OpenType
// MATH table out by the box of each glyph's font and by fixed distances,
// in ems of the font size, in place of the fallback constants of MathML
// Core, 5.1, which read the font's ink: [Style.Distances] sets it for the
// formulas of a source whose producer lays them out so. Under it the ink
// line-ascent and line-descent of a token other than an operator are its
// font's ascent and descent, as if each glyph filled its line, where an
// operator keeps its glyph's ink; the gaps of limits, under
// and over scripts and stretched stacks are 0, the scripts' boxes
// touching their base's; and the fields below place what remains. A
// field of 0 keeps the rule of MathML Core.
type Distances struct {
	// SuperscriptRaise is the height of a superscript's baseline above
	// its base's, for a base no taller than its font's ascent; a taller
	// base raises it by the height it reaches past that ascent.
	SuperscriptRaise float64
	// SubscriptDrop is the depth of a subscript's baseline below its
	// base's, for a base no deeper than its font's descent; a deeper base
	// drops it by the depth it reaches past that descent.
	SubscriptDrop float64
	// LargeOperatorScale draws a large operator in display style at this
	// many times its font size, its glyph's middle kept where it was.
	LargeOperatorScale float64
	// BracketExcess scales a stretchy operator whose font gives it no
	// construction along the block axis vertically, keeping its width,
	// so that its ink covers the target sizes and this fraction of them
	// more, centred on them.
	BracketExcess float64
	// ElementSpacing is the space between two adjacent children of a row
	// neither of which is an embellished operator.
	ElementSpacing float64
	// LimitGap is the space between a large operator's ink and the boxes
	// of its limits.
	LimitGap float64
	// OperatorSpacing is the space after a large operator or an element of
	// under and over scripts in a row, in place of the operator's rspace
	// and of ElementSpacing.
	OperatorSpacing float64
}

// distanceConstant returns the value c takes at size under d: the limit
// gap for the gaps between a large operator and its limits, 0 for the
// other gaps of limits, under and over scripts and stretched stacks,
// which the font boxes of the scripts and the base keep apart; ok is false
// for the constants that keep their fallback.
func distanceConstant(d *Distances, c mt.Constant, size float64) (v float64, ok bool) {
	switch c { //nolint:exhaustive // the other constants keep the fallbacks of 5.1
	case mt.UpperLimitGapMin, mt.LowerLimitGapMin:
		return float64(d.LimitGap * size), true
	case mt.UpperLimitBaselineRiseMin, mt.LowerLimitBaselineDropMin,
		mt.UnderbarVerticalGap, mt.OverbarVerticalGap, mt.UnderbarExtraDescender, mt.OverbarExtraAscender,
		mt.StretchStackGapAboveMin, mt.StretchStackGapBelowMin, mt.StretchStackTopShiftUp, mt.StretchStackBottomShiftDown:
		return 0, true
	}
	return 0, false
}

// fontBox returns the ascent and descent of the font of f at size, from
// its horizontal header; ok is false when the font records none.
func (f *face) fontBox(size float64) (asc, desc float64, ok bool) {
	if f == nil || f.glyphs == nil {
		return 0, 0, false
	}
	e, ok := f.glyphs.FontHExtents()
	if !ok || e.Ascender <= 0 {
		return 0, 0, false
	}
	return f.units(float64(e.Ascender), size), -f.units(float64(e.Descender), size), true
}

// fontBoxed gives b, the box of a token of f at size moved down by
// delta, its font's ascent and descent as its ink and line metrics, when
// f lays out by distances.
func (f *face) fontBoxed(b *box, size, delta float64) {
	if f == nil || f.distances == nil {
		return
	}
	if asc, desc, ok := f.fontBox(size); ok {
		b.asc, b.desc = asc-delta, desc+delta
		b.inkAsc, b.inkDesc = b.asc, b.desc
	}
}

// scriptShifts returns SuperShift and SubShift of scripts on base under
// the distances of f at size: the raise and drop, grown by what the base
// reaches past its font's ascent and descent; ok is false when f does not
// lay out by distances or they set neither.
func (f *face) scriptShifts(size float64, base *box) (sup, sub float64, ok bool) {
	if f == nil || f.distances == nil || f.distances.SuperscriptRaise == 0 && f.distances.SubscriptDrop == 0 {
		return 0, 0, false
	}
	asc, desc, _ := f.fontBox(size)
	d := f.distances
	sup = float64(d.SuperscriptRaise*size) + max(0, base.inkAsc-asc)
	sub = float64(d.SubscriptDrop*size) + max(0, base.inkDesc-desc)
	return sup, sub, true
}
