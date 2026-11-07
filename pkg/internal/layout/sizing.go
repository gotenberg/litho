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

// hasSizing reports a style with a sizing keyword on a size property.
func hasSizing(st *css.Style) bool {
	return st.Width.IsSizingKeyword() || st.MinWidth.IsSizingKeyword() || st.MaxWidth.IsSizingKeyword() ||
		st.Height.IsSizingKeyword() || st.MinHeight.IsSizingKeyword() || st.MaxHeight.IsSizingKeyword()
}

// contentKeyword reports the min-content, max-content or fit-content
// keyword.
func contentKeyword(d css.Dimension) bool {
	return d.Kind == css.MinContent || d.Kind == css.MaxContent || d.Kind == css.FitContent
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
	width := func(d css.Dimension) css.Dimension {
		var v float64
		switch d.Kind { //nolint:exhaustive // the other kinds stay as they are
		case css.MinContent, css.MaxContent, css.FitContent:
			if !measured {
				lo, hi = e.contentIntrinsic(b)
				measured = true
			}
			switch d.Kind { //nolint:exhaustive // the content keywords alone
			case css.MinContent:
				v = lo
			case css.MaxContent:
				v = hi
			default:
				v = min(hi, max(lo, roomW))
			}
		case css.Stretch:
			v = roomW
		default:
			return d
		}
		if src.BoxSizing == css.BorderBox && b.kind != kindInline {
			v += he
		}
		return css.Pts(v)
	}
	st.Width = width(src.Width)
	st.MinWidth = width(src.MinWidth)
	st.MaxWidth = width(src.MaxWidth)
	roomH, defH := availH, availH >= 0
	if !defH && cb.hDefinite {
		roomH, defH = cb.h-nonAuto(b.m[css.Top], src.Margin[css.Top].Kind == css.Auto)-nonAuto(b.m[css.Bottom], src.Margin[css.Bottom].Kind == css.Auto)-ve, true
	}
	stretchH := func(fallback css.Dimension) css.Dimension {
		if !defH {
			return fallback
		}
		v := max(roomH, 0)
		if src.BoxSizing == css.BorderBox {
			v += ve
		}
		return css.Pts(v)
	}
	switch {
	case contentKeyword(src.Height):
		st.Height = css.Dimension{Kind: css.Auto}
	case src.Height.Kind == css.Stretch:
		st.Height = stretchH(css.Dimension{Kind: css.Auto})
	}
	switch {
	case contentKeyword(src.MinHeight):
		st.MinHeight, b.minHContent = css.Pts(0), true
	case src.MinHeight.Kind == css.Stretch:
		st.MinHeight = stretchH(css.Pts(0))
	}
	switch {
	case contentKeyword(src.MaxHeight):
		st.MaxHeight, b.maxHContent = css.Dimension{Kind: css.None}, true
	case src.MaxHeight.Kind == css.Stretch:
		st.MaxHeight = stretchH(css.Dimension{Kind: css.None})
	}
	b.st = &st
}

// contentHeightBounds applies a min-height or max-height of min-content,
// max-content or fit-content to the content height h of b, whose content
// laid out takes content: in the block axis each is the content height
// (CSS Sizing 3, 3.2).
func contentHeightBounds(b *box, h, content float64) float64 {
	if b.maxHContent {
		h = min(h, content)
	}
	if b.minHContent {
		h = max(h, content)
	}
	return h
}

// keywordIntrinsic applies the sizing keywords of b's computed width,
// min-width and max-width to the min- and max-content widths lo and hi
// of its content box (CSS Sizing 3, 5.2): min-content and max-content
// take that width of the content; fit-content and stretch behave as
// auto, the intrinsic contributions being made under a min- or
// max-content constraint.
func keywordIntrinsic(st *css.Style, lo, hi, clo, chi float64) (float64, float64) {
	switch st.Width.Kind { //nolint:exhaustive // the other kinds are handled by the caller
	case css.MinContent:
		lo, hi = clo, clo
	case css.MaxContent:
		lo, hi = chi, chi
	}
	switch st.MaxWidth.Kind { //nolint:exhaustive // the other kinds are handled by the caller
	case css.MinContent:
		lo, hi = min(lo, clo), min(hi, clo)
	case css.MaxContent:
		lo, hi = min(lo, chi), min(hi, chi)
	}
	switch st.MinWidth.Kind { //nolint:exhaustive // the other kinds are handled by the caller
	case css.MinContent:
		lo, hi = max(lo, clo), max(hi, clo)
	case css.MaxContent:
		lo, hi = max(lo, chi), max(hi, chi)
	case css.FitContent:
		lo, hi = max(lo, clo), max(hi, chi)
	}
	return lo, max(hi, lo)
}
