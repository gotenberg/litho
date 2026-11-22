package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file resolves the sizing keywords of width, height and their
// minimums and maximums (CSS Sizing 3, 3.1 and 3.2; CSS Sizing 4, 3.1)
// when a box is laid out: min-content, max-content and fit-content widths
// come from the content's intrinsic widths (5.1), stretch fills the
// containing block less the margins. In the block axis min-content,
// max-content and fit-content behave as auto for height, and as the
// content height for min-height and max-height. The resolved values
// replace the box's style for the layout, its computed style kept for the
// intrinsic sizes, where the keywords take the content's own widths and
// stretch and fit-content behave as auto.

// sizeSrc returns the computed style of b: the one [engine.resolveSizing]
// replaced, or its style.
func (b *box) sizeSrc() *css.Style {
	if b.orig != nil {
		return b.orig
	}
	return b.st
}

// hasSizing reports a style with a sizing keyword on a size property, or
// a calc-size() on width, whose auto basis resolves here too.
func hasSizing(st *css.Style) bool {
	return st.Width.IsSizingKeyword() || st.MinWidth.IsSizingKeyword() || st.MaxWidth.IsSizingKeyword() ||
		st.Height.IsSizingKeyword() || st.MinHeight.IsSizingKeyword() || st.MaxHeight.IsSizingKeyword() ||
		st.Width.SizeCalc()
}

// contentKeyword reports the min-content, max-content or fit-content
// keyword, or fit-content().
func contentKeyword(d css.Dimension) bool {
	return d.Kind == css.MinContent || d.Kind == css.MaxContent || d.Kind == css.FitContent || d.Kind == css.FitContentOf
}

// fitLimit returns the argument of the fit-content() d as a content size
// against the reference ref: its length, less the padding and borders
// edges under border-box sizing, at least 0 (CSS Sizing 4, 3.2).
func fitLimit(d css.Dimension, ref, edges float64, borderBox bool) float64 {
	v := resolve(d.FitLimit(), ref)
	if borderBox {
		v -= edges
	}
	return max(v, 0)
}

// contentIntrinsic returns the min- and max-content widths of b's content
// box, whatever its own width properties say.
func (e *engine) contentIntrinsic(b *box) (float64, float64) {
	if b.kind == kindReplaced {
		if b.picture == nil {
			return 0, 0
		}
		w, _ := e.replacedSize(&box{st: autoWidth(b.sizeSrc()), picture: b.picture, kind: kindReplaced}, cblock{})
		return w, w
	}
	lo, hi := formatterFor(b).intrinsic(e, b)
	lo, hi = containedWidths(b, b.sizeSrc(), lo, hi)
	return lo, max(hi, lo)
}

// resolveSizing replaces the sizing keywords of b's size properties with
// the lengths they resolve to in the containing block cb, whose width
// less the box's margins, borders and padding is what stretch fills;
// availW and availH, when not negative, replace that room for an
// absolutely positioned box, whose insets reduce it. Its edges must be
// resolved.
func (e *engine) resolveSizing(b *box, cb cblock, availW, availH float64) {
	src := b.sizeSrc()
	b.minHContent, b.maxHContent = false, false
	if !hasSizing(src) {
		b.st = src
		b.orig = nil
		if r, _ := prefRatio(b); r > 0 {
			st := *src
			b.st = &st
			if e.applyRatio(b, &st, cb, availH) {
				b.orig = src
			} else {
				b.st = src
			}
		}
		return
	}
	b.orig = src
	st := *src
	he, ve := hEdges(b), vEdges(b)
	roomW := availW
	if roomW < 0 {
		roomW = cb.w - nonAuto(b.m[css.Left], src.Margin[css.Left].Kind == css.Auto) - nonAuto(b.m[css.Right], src.Margin[css.Right].Kind == css.Auto) - he
	}
	roomW = max(roomW, 0)
	measured := false
	var lo, hi float64
	r, content := prefRatio(b)
	ratioW, ratioOK := 0.0, false
	if r > 0 {
		if h, ok := specHeight(b, src.Height, cb); ok {
			ratioW, ratioOK = widthOfHeight(h, r, edgesFor(b, content)), true
		}
	}
	width := func(d css.Dimension, limit bool) css.Dimension {
		var v float64
		switch d.Kind { //nolint:exhaustive // the other kinds stay as they are
		case css.MinContent, css.MaxContent, css.FitContent, css.FitContentOf:
			if limit && ratioOK {
				// A box whose ratio and definite height give its width
				// takes that width as its min- and max-content sizes
				// (CSS Sizing 4, 4.1 and 4.2); the width property itself
				// goes to applyRatio, which adds the automatic minimum.
				v = ratioW
				break
			}
			if !measured {
				lo, hi = e.contentIntrinsic(b)
				measured = true
			}
			switch d.Kind { //nolint:exhaustive // the content keywords alone
			case css.MinContent:
				v = lo
			case css.MaxContent:
				v = hi
			case css.FitContentOf:
				// The argument stands for the available space (CSS Sizing
				// 4, 3.2).
				v = min(hi, max(lo, fitLimit(d, cb.w, he, src.BoxSizing == css.BorderBox && b.kind != kindInline)))
			default:
				v = min(hi, max(lo, roomW))
			}
		case css.Stretch:
			v = roomW
		case css.Auto:
			if !d.SizeCalc() {
				return d
			}
			v = e.autoBasisWidth(b, roomW)
		default:
			return d
		}
		borderBox := src.BoxSizing == css.BorderBox && b.kind != kindInline
		v = throughCalc(d, v, he, borderBox, cb.w)
		if borderBox {
			v += he
		}
		return css.Pts(v)
	}
	st.Width = width(src.Width, false)
	st.MinWidth = width(src.MinWidth, true)
	st.MaxWidth = width(src.MaxWidth, true)
	roomH, defH := availH, availH >= 0
	if !defH && cb.hDefinite {
		top, bottom := stretchMargins(b, src)
		roomH, defH = cb.h-top-bottom-ve, true
	}
	stretchH := func(d, fallback css.Dimension) css.Dimension {
		if !defH {
			return fallback
		}
		v := throughCalc(d, max(roomH, 0), ve, src.BoxSizing == css.BorderBox, heightRef(cb))
		if src.BoxSizing == css.BorderBox {
			v += ve
		}
		return css.Pts(v)
	}
	switch {
	case contentKeyword(src.Height):
		st.Height = css.Dimension{Kind: css.Auto}
	case src.Height.Kind == css.Stretch:
		st.Height = stretchH(src.Height, css.Dimension{Kind: css.Auto})
	}
	// A box whose ratio and definite width give its height takes that
	// height as its min- and max-content block sizes (CSS Sizing 4, 4.1).
	ratioH, ratioHOK := 0.0, false
	if r > 0 && src.Width.Kind != css.Auto {
		if w, ok := specWidth(b, st.Width, cb.w); ok {
			ratioH, ratioHOK = heightOfWidth(w, r, edgesFor(b, content)), true
			if src.BoxSizing == css.BorderBox {
				ratioH += ve
			}
		}
	}
	switch {
	case contentKeyword(src.MinHeight) && ratioHOK:
		st.MinHeight = css.Pts(ratioH)
	case contentKeyword(src.MinHeight):
		st.MinHeight, b.minHContent = css.Pts(0), true
	case src.MinHeight.Kind == css.Stretch:
		st.MinHeight = stretchH(src.MinHeight, css.Pts(0))
	}
	switch {
	case contentKeyword(src.MaxHeight) && ratioHOK:
		st.MaxHeight = css.Pts(ratioH)
	case contentKeyword(src.MaxHeight):
		st.MaxHeight, b.maxHContent = css.Dimension{Kind: css.None}, true
	case src.MaxHeight.Kind == css.Stretch:
		st.MaxHeight = stretchH(src.MaxHeight, css.Dimension{Kind: css.None})
	}
	b.st = &st
	e.applyRatio(b, &st, cb, availH)
}

// stretchMargins returns the block-start and block-end margins of b, of
// computed style src, that the stretch-fit height takes out of its
// containing block's: an auto margin counts as 0, and so does the margin
// of an in-flow block-level box on a side where its parent, no
// independent formatting context, has no border or padding: the margin
// would collapse with the parent's (CSS Sizing 3, 4.2).
//
// # Corpus evidence
//
//   - wpt: css-sizing/stretch/block-height-006, -009 and -010 (a 50px
//     bottom margin under height, min-height and max-height stretch in a
//     100px parent) draw a 100px square in Chromium 156.
func stretchMargins(b *box, src *css.Style) (float64, float64) {
	top := nonAuto(b.m[css.Top], src.Margin[css.Top].Kind == css.Auto)
	bottom := nonAuto(b.m[css.Bottom], src.Margin[css.Bottom].Kind == css.Auto)
	if b.kind != kindBlock || b.inline || b.abs || b.float != css.FloatNone || b.flexItem || b.gridItem || b.tpart != partNone {
		return top, bottom
	}
	p := b.parent
	for p != nil && p.anon {
		p = p.parent
	}
	if p == nil || p.establishesBFC() {
		return top, bottom
	}
	if p.bd[css.Top]+p.pd[css.Top] == 0 {
		top = 0
	}
	if p.bd[css.Bottom]+p.pd[css.Bottom] == 0 {
		bottom = 0
	}
	return top, bottom
}

// contentHeightBounds applies a min-height or max-height of min-content,
// max-content or fit-content to the content height h of b, whose content
// laid out takes content: in the block axis each is the content height
// (CSS Sizing 3, 3.2), through the calculation of a calc-size() over
// that keyword (CSS Sizing 4, 7.1), whose percentages resolve in cb.
func contentHeightBounds(b *box, h, content float64, cb cblock) float64 {
	src := b.sizeSrc()
	edges, borderBox, ref := vEdges(b), src.BoxSizing == css.BorderBox, heightRef(cb)
	if b.maxHContent {
		h = min(h, throughCalc(src.MaxHeight, content, edges, borderBox, ref))
	}
	if b.minHContent {
		h = max(h, throughCalc(src.MinHeight, content, edges, borderBox, ref))
	}
	return h
}

// keywordWidth applies the sizing keyword of the computed width of st to
// the min- and max-content widths lo and hi of a content box, whose
// content alone takes clo and chi (CSS Sizing 3, 5.2): min-content and
// max-content take that width of the content; fit-content and stretch
// behave as auto, the intrinsic contributions being made under a min- or
// max-content constraint. min-width and max-width then clamp it as they
// clamp any width (CSS 2.1, 10.4).
func keywordWidth(st *css.Style, lo, hi, clo, chi, edges float64) (float64, float64) {
	switch st.Width.Kind { //nolint:exhaustive // the other kinds are handled by the caller
	case css.MinContent:
		lo, hi = clo, clo
	case css.MaxContent:
		lo, hi = chi, chi
	case css.FitContentOf:
		// A length argument fits the content as the width does; a
		// percentage one makes the width a cyclic percentage, which
		// contributes as auto (CSS Sizing 3, 5.2.1).
		if !st.Width.FitLimit().HasPercent() {
			f := min(chi, max(clo, fitLimit(st.Width, 0, edges, st.BoxSizing == css.BorderBox)))
			lo, hi = f, f
		}
	}
	return lo, hi
}

// keywordIntrinsic applies the sizing keywords of the computed min-width
// and max-width of st to the min- and max-content widths lo and hi of a
// content box of padding and borders edges, whose content alone takes clo
// and chi (CSS Sizing 3, 5.2), each through the calculation of a
// calc-size() over it (CSS Sizing 4, 7.1).
func keywordIntrinsic(st *css.Style, lo, hi, clo, chi, edges float64) (float64, float64) {
	borderBox := st.BoxSizing == css.BorderBox
	through := func(d css.Dimension, v float64) float64 { return throughCalc(d, v, edges, borderBox, 0) }
	switch d := st.MaxWidth; d.Kind { //nolint:exhaustive // the other kinds are handled by the caller
	case css.MinContent:
		lo, hi = min(lo, through(d, clo)), min(hi, through(d, clo))
	case css.MaxContent:
		lo, hi = min(lo, through(d, chi)), min(hi, through(d, chi))
	case css.FitContentOf:
		// A percentage argument is a cyclic percentage, which a maximum
		// contributes as none (CSS Sizing 3, 5.2.1).
		if !d.FitLimit().HasPercent() {
			f := min(chi, max(clo, fitLimit(d, 0, edges, borderBox)))
			lo, hi = min(lo, f), min(hi, f)
		}
	}
	switch d := st.MinWidth; d.Kind { //nolint:exhaustive // the other kinds are handled by the caller
	case css.MinContent:
		lo, hi = max(lo, through(d, clo)), max(hi, through(d, clo))
	case css.MaxContent:
		lo, hi = max(lo, through(d, chi)), max(hi, through(d, chi))
	case css.FitContent:
		lo, hi = max(lo, through(d, clo)), max(hi, through(d, chi))
	case css.FitContentOf:
		// A percentage of a minimum resolves against zero here (CSS
		// Sizing 3, 5.2.1).
		f := min(chi, max(clo, fitLimit(d, 0, edges, borderBox)))
		lo, hi = max(lo, f), max(hi, f)
	}
	return lo, max(hi, lo)
}

// containedHeight returns the content height h of b, its content's,
// unless b is under size containment: then the box sizes as if empty, at
// its contain-intrinsic-height when one is set (CSS Contain 2, 3.2; CSS
// Sizing 4, 5.1). Its content is laid out and painted all the same.
func containedHeight(b *box, h float64) float64 {
	if b.kind == kindReplaced || b.st.Containment()&css.ContainSize == 0 {
		return h
	}
	if c := b.st.ContainIntrinsic; c != nil && c.Height >= 0 {
		return c.Height
	}
	return 0
}

// containedWidths returns the min- and max-content widths lo and hi of
// b's content, unless b is under size containment in the inline axis: then
// both are its contain-intrinsic-width, or 0 (CSS Contain 2, 3.2 and 3.3;
// CSS Sizing 4, 5.1).
func containedWidths(b *box, st *css.Style, lo, hi float64) (float64, float64) {
	if b.kind == kindReplaced || st.Containment()&(css.ContainSize|css.ContainInlineSize) == 0 {
		return lo, hi
	}
	if c := st.ContainIntrinsic; c != nil && c.Width >= 0 {
		return c.Width, c.Width
	}
	return 0, 0
}

// throughCalc applies the calc-size() of d, if any, to v, the used content
// size of its basis in a box of padding and borders edges in that axis:
// the calculation reads the size in the box that box-sizing names, and
// its percentages resolve against ref, 0 when indefinite (CSS Sizing 4,
// 7.1). A size never falls below 0 (CSS Values 4, 10.12).
func throughCalc(d css.Dimension, v, edges float64, borderBox bool, ref float64) float64 {
	if !d.SizeCalc() {
		return v
	}
	if borderBox {
		return max(d.OfSize(v+edges, ref)-edges, 0)
	}
	return max(d.OfSize(v, ref), 0)
}

// heightRef returns the height percentages resolve against in cb, 0 when
// it is not definite.
func heightRef(cb cblock) float64 {
	if cb.hDefinite {
		return cb.h
	}
	return 0
}

// usedHeight returns the content height of b, whose height h its own
// properties give and whose content takes content, in the containing
// block cb: an auto height of calc-size() is the calculation of the
// content height (CSS Sizing 4, 7.1), then min-height and max-height
// apply, their sizing keywords and calculations included.
func usedHeight(b *box, h, content float64, hSet bool, cb cblock) float64 {
	src := b.sizeSrc()
	if b.heightCalced {
		b.heightCalced = false
	} else if !hSet && src.Height.SizeCalc() && b.st.Height.Kind == css.Auto {
		h = throughCalc(src.Height, h, vEdges(b), src.BoxSizing == css.BorderBox, heightRef(cb))
	}
	return contentHeightBounds(b, constrainHeight(b, h, cb), content, cb)
}

// contentMinHeight returns the automatic minimum height of b, whose
// content takes content, in the containing block cb: the content height,
// through a calc-size() over auto on min-height (CSS Sizing 4, 4.3 and
// 7.1).
func contentMinHeight(b *box, content float64, cb cblock) float64 {
	return throughCalc(b.st.MinHeight, content, vEdges(b), b.st.BoxSizing == css.BorderBox, heightRef(cb))
}

// autoBasisWidth returns the content width an auto width gives b, for the
// basis of a calc-size(): the room of its containing block, roomW, for a
// block-level box in normal flow, which stretches (CSS 2.1, 10.3.3), and
// the fit-content width in that room for every other box, which shrinks
// to fit (CSS 2.1, 10.3.5, 10.3.7 and 10.3.9; CSS Flexbox 1, 9.2).
func (e *engine) autoBasisWidth(b *box, roomW float64) float64 {
	if b.kind == kindBlock && !b.inline && !b.abs && b.float == css.FloatNone && !b.flexItem && !b.gridItem && b.tpart == partNone {
		return roomW
	}
	lo, hi := e.contentIntrinsic(b)
	return min(hi, max(lo, roomW))
}
