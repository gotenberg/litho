package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// This file sizes a non-replaced box that has a preferred aspect ratio
// (CSS Sizing 4, 4.1 and 4.2): when one of its sizes is automatic, that
// size, the ratio-dependent one, follows the other through the ratio, as
// a replaced element's does. A definite height gives the width; else the
// width, auto or not, gives the height. A width that is itself automatic
// takes the minimum and maximum heights through the ratio, as CSS 2.1,
// 10.4 constrains a replaced element both of whose sizes are auto. The
// height a ratio gives grows to the content's when min-height is auto
// and the box is not a scroll container (4.3).

// prefRatio returns the preferred aspect ratio of the non-replaced box b,
// width over height, 0 for none, and whether it relates the content box
// rather than the border box (CSS Sizing 4, 4.1). Inline boxes and the
// internal boxes of a table have none: aspect-ratio does not apply to
// them. A table's ratio is its table box's.
func prefRatio(b *box) (float64, bool) {
	if b.kind != kindBlock || b.tpart > partTable || b.ruby != nil {
		return 0, false
	}
	st := b.sizeSrc()
	r := st.AspectRatio.Ratio()
	if !numeric.Positive(r) {
		return 0, false
	}
	return r, st.AspectRatio.Auto() || st.BoxSizing != css.BorderBox
}

// ratioEdges are the horizontal and vertical padding and borders of a box
// whose ratio relates its border box, both 0 when the ratio relates its
// content box.
type ratioEdges struct{ h, v float64 }

// edgesFor returns the ratioEdges of b, its edges resolved, for a ratio
// that relates its content box when content is set.
func edgesFor(b *box, content bool) ratioEdges {
	if content {
		return ratioEdges{}
	}
	return ratioEdges{h: hEdges(b), v: vEdges(b)}
}

// widthOfHeight returns the content width that the ratio r gives the
// content height h of a box of edges ed.
func widthOfHeight(h, r float64, ed ratioEdges) float64 {
	return max(float64((h+ed.v)*r)-ed.h, 0)
}

// heightOfWidth returns the content height that the ratio r gives the
// content width w of a box of edges ed.
func heightOfWidth(w, r float64, ed ratioEdges) float64 {
	return max((w+ed.h)/r-ed.v, 0)
}

// borderBoxed returns the content width w as the width property of b
// gives it: its border-box width under box-sizing: border-box.
func borderBoxed(b *box, w float64, st *css.Style) float64 {
	if st.BoxSizing == css.BorderBox {
		return w + hEdges(b)
	}
	return w
}

// applyRatio rewrites the sizes in st, the used style of the box b in the
// containing block cb, by b's preferred aspect ratio, and returns whether
// it changed any (CSS Sizing 4, 4.2). A width of auto, min-content,
// max-content or fit-content is automatic here, as Chromium sizes it: such
// a width over a definite height
// becomes the width the ratio gives, at least the content's min-content
// width when min-width is auto and b is not a scroll container (4.3); an
// auto width over an auto height takes the height's definite minimum and
// maximum through the ratio, within its own. availH, when not negative, is
// the height an absolutely positioned box's insets leave it, definite when
// both its top and bottom are set. The edges of b must be resolved and
// b.st must be st.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) draws a block
//     of aspect-ratio 1 and max-height 50px 50 wide, an inline block of
//     aspect-ratio 2, max-height 20px and wider text 40 wide, one of
//     min-width 100px 100 by 50, and passes the WPT tests
//     block-aspect-ratio-021 and -032 (width: max-content and
//     min-content with a ratio and a height).
func (e *engine) applyRatio(b *box, st *css.Style, cb cblock, availH float64) bool {
	r, content := prefRatio(b)
	if k := b.sizeSrc().Width.Kind; r == 0 || k != css.Auto && !contentKeyword(b.sizeSrc().Width) {
		return false
	}
	ed := edgesFor(b, content)
	maxW, maxSet := specWidth(b, st.MaxWidth, cb.w)
	minW, _ := specWidth(b, st.MinWidth, cb.w)
	h, ok := specHeight(b, st.Height, cb)
	if !ok && st.Height.Kind == css.Auto && availH >= 0 && b.abs && st.Top.Kind != css.Auto && st.Bottom.Kind != css.Auto &&
		(st.Left.Kind == css.Auto || st.Right.Kind == css.Auto) {
		// The insets of an absolutely positioned box set both its top
		// and bottom edges: its height is definite (CSS Position 3, 4.3).
		// When they set its left and right edges too, the width stretches
		// and the ratio gives the height, as the inline axis comes first.
		h, ok = availH, true
	}
	if ok {
		w := widthOfHeight(constrainHeight(b, h, cb), r, ed)
		if st.MinWidthAuto && st.Overflow == css.OverflowVisible {
			lo, _ := e.contentIntrinsic(b)
			if maxSet {
				lo = min(lo, maxW)
			}
			w = max(w, lo)
		}
		st.Width = css.Pts(borderBoxed(b, w, st))
		return true
	}
	changed := false
	if mx, ok := specHeight(b, st.MaxHeight, cb); ok {
		w := max(widthOfHeight(mx, r, ed), minW)
		if !maxSet || maxW > w {
			st.MaxWidth, changed = css.Pts(borderBoxed(b, w, st)), true
		}
	}
	if mn, ok := specHeight(b, st.MinHeight, cb); ok && mn > 0 {
		w := widthOfHeight(mn, r, ed)
		if maxSet {
			w = min(w, maxW)
		}
		if minW < w {
			st.MinWidth, changed = css.Pts(borderBoxed(b, w, st)), true
		}
	}
	return changed
}

// ratioHeight returns the content height that the preferred aspect ratio
// of b gives its content width b.w, and false when b has none. A caller
// asks only for a box whose height is auto or a percentage of an
// indefinite height, which behaves as auto (CSS 2.1, 10.5).
func ratioHeight(b *box) (float64, bool) {
	r, content := prefRatio(b)
	if r == 0 {
		return 0, false
	}
	return heightOfWidth(b.w, r, edgesFor(b, content)), true
}

// contentMinimum reports whether the automatic minimum height of b, whose
// height its ratio gives, is its content's: min-height is auto and b is
// not a scroll container (CSS Sizing 4, 4.3). overflow: clip computes to
// hidden, and so takes no content minimum either.
func contentMinimum(b *box) bool {
	return b.st.MinHeightAuto && b.st.Overflow == css.OverflowVisible
}

// ratioIntrinsic applies the preferred aspect ratio of b to the min- and
// max-content widths lo and hi of its content box (CSS Sizing 4, 4.2),
// its computed style st and its padding and borders pd and bd: a fixed
// height gives both, and an auto width keeps within the fixed minimum
// and maximum heights through the ratio. A percentage height counts as
// auto here, and so does a percentage width: a percentage of the size
// being measured is cyclic and behaves as auto for the contribution of a
// non-replaced box (CSS Sizing 3, 5.2.1).
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) sizes an
//     inline block, a min-content grid and a float holding a block of
//     height 100px, width 50% and aspect-ratio 1 (2 for the float) 100,
//     100 and 200 wide.
func ratioIntrinsic(b *box, st *css.Style, pd, bd [4]float64, lo, hi float64) (float64, float64) {
	r, content := prefRatio(b)
	if r == 0 || st.Width.Kind != css.Auto && st.Width.Kind != css.Percent && !contentKeyword(st.Width) {
		return lo, hi
	}
	ve := pd[css.Top] + pd[css.Bottom] + bd[css.Top] + bd[css.Bottom]
	var ed ratioEdges
	if !content {
		ed = ratioEdges{h: pd[css.Left] + pd[css.Right] + bd[css.Left] + bd[css.Right], v: ve}
	}
	// height turns a fixed height-like value into a content height.
	height := func(d css.Dimension) float64 {
		v := d.Value
		if st.BoxSizing == css.BorderBox {
			v -= ve
		}
		return max(v, 0)
	}
	mn, mx := 0.0, -1.0
	if st.MinHeight.Kind == css.Points {
		mn = height(st.MinHeight)
	}
	if st.MaxHeight.Kind == css.Points {
		mx = max(height(st.MaxHeight), mn)
	}
	if st.Height.Kind == css.Points {
		h := max(height(st.Height), mn)
		if mx >= 0 {
			h = min(h, mx)
		}
		w := widthOfHeight(h, r, ed)
		if st.MinWidthAuto && st.Overflow == css.OverflowVisible {
			// The automatic minimum width is the content's min-content
			// width (CSS Sizing 4, 4.3).
			w = max(w, lo)
		}
		return w, w
	}
	if mx >= 0 {
		w := widthOfHeight(mx, r, ed)
		lo, hi = min(lo, w), min(hi, w)
	}
	if mn > 0 {
		w := widthOfHeight(mn, r, ed)
		lo, hi = max(lo, w), max(hi, w)
	}
	return lo, hi
}

// replacedRatio returns the aspect ratio that sizes the replaced box b,
// width over height, 0 for none, and the edges it relates: a <ratio> of
// aspect-ratio, relating the box box-sizing names; else the picture's
// natural ratio; else the ratio of auto && <ratio>, relating the content
// box (CSS Sizing 4, 4.1). The edges of b must be resolved.
func replacedRatio(b *box) (float64, ratioEdges) {
	a := b.st.AspectRatio
	if r := a.Ratio(); r > 0 && !a.Auto() {
		return r, edgesFor(b, b.st.BoxSizing != css.BorderBox)
	}
	if r := pictureRatio(b.picture); r > 0 {
		return r, ratioEdges{}
	}
	return a.Ratio(), ratioEdges{}
}

// wrapsColumns reports a multi-line column flex container.
func wrapsColumns(b *box) bool {
	return b.flex != nil && (b.st.FlexDirection == css.FlexColumn || b.st.FlexDirection == css.FlexColumnReverse) && b.st.FlexWrap != css.NoWrap
}
