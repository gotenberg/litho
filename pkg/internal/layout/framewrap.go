package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file wraps the flow around the page-anchored and page-clamped boxes
// whose style asks for it (css.Office.FrameWrap), as Writer wraps its text
// around a frame. Such a box is laid out after the flow, where its anchor
// landed, yet the text before its anchor on its page wraps around it too:
// the layout runs again with the rectangles the boxes took, seeded into
// the root's formatting context as exclusions, until the boxes land where
// the exclusions were or the passes run out.

// maxWrapPasses bounds the layouts a document with wrapped frames takes:
// the first finds where the boxes land, each later one lays the flow out
// around them. A frame whose anchor the wrapping moves to another page
// moves its frame, which the next pass follows. Anti-DoS guard, not
// spec-mandated: three passes settle every document whose frames do not
// chase their own anchors from page to page, and a fourth bounds those.
const maxWrapPasses = 4

// textMin is the least room, in points, on a side of a box of
// css.FrameWrapDynamic that takes text: 2 cm, Writer's TEXT_MIN of 1134
// twips (sw/source/core/text/txtfly.cxx).
const textMin = 1134.0 / 20

// wrapSeed is the rectangle a wrapped box took on its page: its border
// box grown by its wrap gap, relative to the page area's top-left corner,
// cut to the page area.
type wrapSeed struct {
	page           int
	mode           css.FrameWrap
	x0, y0, x1, y1 float64
}

// collectWraps returns the rectangles the wrapped boxes of the tree rooted
// at b took, in tree order.
func (e *engine) collectWraps(b *box, out []wrapSeed) []wrapSeed {
	for _, k := range b.kids {
		if k.abs && k.laidOut && k.pageAnchored && k.st.FrameWrap != css.FrameWrapThrough && len(out) < maxFloats {
			g := k.st.WrapGap
			top := e.pageTop(k.anchorPage)
			s := wrapSeed{
				page: k.anchorPage, mode: k.st.FrameWrap,
				x0: k.x - k.bd[css.Left] - k.pd[css.Left] - g[css.Left],
				y0: k.y - g[css.Top] - top,
				x1: k.x - k.bd[css.Left] - k.pd[css.Left] + k.borderW() + g[css.Right],
				y1: k.y + k.borderH() + g[css.Bottom] - top,
			}
			// A box taller than its page area wraps the text of that
			// area alone.
			s.y0 = max(s.y0, 0)
			s.y1 = min(s.y1, e.areaH(k.anchorPage))
			if s.y1 > s.y0 && s.x1 > s.x0 {
				out = append(out, s)
			}
		}
		out = e.collectWraps(k, out)
	}
	return out
}

// seedWraps returns the exclusions of the seeds in flow coordinates, the
// floats the root's formatting context starts with.
func (e *engine) seedWraps(seeds []wrapSeed) []floatRect {
	out := make([]floatRect, 0, len(seeds))
	for _, s := range seeds {
		top := e.pageTop(s.page)
		out = append(out, floatRect{wrap: s.mode, x0: s.x0, y0: top + s.y0, x1: s.x1, y1: top + s.y1})
	}
	return out
}

// wrapBand narrows the band [l, r) of a line beside the exclusion f as its
// wrap mode says: the text keeps to one side of it, the side the mode
// names or the one with more room, or leaves the band.
func (f floatRect) wrapBand(l, r float64) (float64, float64) {
	if f.x1 <= l || f.x0 >= r {
		return l, r
	}
	left, right := f.x0-l, r-f.x1
	switch f.wrap {
	case css.FrameWrapLeft:
		return l, f.x0
	case css.FrameWrapRight:
		return f.x1, r
	case css.FrameWrapDynamic:
		if max(left, right) < textMin {
			return l, l
		}
	case css.FrameWrapNone:
		return l, l
	}
	if left >= right {
		return l, f.x0
	}
	return f.x1, r
}

// sameWraps reports whether the rectangles a and b agree to a hundredth of
// a point: the boxes landed where the pass kept clear for them.
func sameWraps(a, b []wrapSeed) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.page != y.page || x.mode != y.mode || !within(x.x0, y.x0) || !within(x.y0, y.y0) || !within(x.x1, y.x1) || !within(x.y1, y.y1) {
			return false
		}
	}
	return true
}

// within reports whether a and b are a hundredth of a point apart or less.
func within(a, b float64) bool { return math.Abs(a-b) <= 0.01 }
