package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file offsets stickily positioned boxes (CSS Position 3, 3.4). Paged
// output prints every scroll container at its initial scroll position, so
// a sticky box takes the offset that position gives it, once: the least
// that keeps its border box inside its sticky view rectangle, the
// scrollport of its nearest scroll container less its insets, without
// pushing its margin box out of its containing block. The viewport of a
// box is the area of the page its top is on: on paper each page area is
// the viewport of what it holds (CSS Paged Media 3, the page model), and
// a sticky box
// never leaves the page it is laid out on for the viewport's sake; inside
// a fixed box, the viewport is the page that box is fixed to.
//
// Chromium's print departs from this: it offsets a sticky box against a
// viewport of its own, which moves a bottom: 0 box from the second page to
// the first; its screen agrees with the clause.

// placeSticky sets the offset of every sticky box under b. viewport is
// the box whose overflow went to the viewport, nil for none.
func (e *engine) placeSticky(b, viewport *box) {
	for _, k := range b.kids {
		if k.st.Position == css.Sticky && k.kind != kindText && k.kind != kindBreak && k.kind != kindInline && !k.tpart.internal() {
			k.setSticky(e.stickyOffset(k, viewport))
		}
		e.placeSticky(k, viewport)
	}
}

// stickyOffset returns the offset of the sticky box b at the initial
// scroll position of its nearest scroll container, the viewport when it
// has none (CSS Position 3, 3.4). Where both insets of an axis apply and
// conflict, top wins, and left.
//
// An auto margin counts as 0 in the limit of the containing block, as
// Chromium counts it.
//
// # Corpus evidence
//
//   - wpt: css-position/sticky/position-sticky-margins-002 and -003 (an
//     auto margin pushing a bottom: 0 and a right: 0 sticky box past its
//     100px scroll container) draw a 100px green square in Chromium 156.
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) moves a
//     sticky box of top: 20px 50px down a 100px scroll container not at
//     all, and one of bottom: 10px 250px down it to 60px, on screen and in
//     print alike; on screen it moves a top: 50px box at 10px to 50px and
//     a bottom: 0 box at 1550px to the bottom of its 600px viewport, and
//     its print of a letter page draws the second at 384px on page 1.
func (e *engine) stickyOffset(b, viewport *box) (float64, float64) {
	var port [4]float64 // left, top, right, bottom of the scrollport
	sc := b.parent
	for sc != nil && (!scrollContainer(sc.st) || sc.kind != kindBlock) {
		sc = sc.parent
	}
	if sc == nil || sc == viewport || sc.parent == nil {
		// A fixed box is laid out once, on the page area it is fixed to,
		// whatever its content overflows into.
		at := b.y
		for a := b.parent; a != nil; a = a.parent {
			if a.st.Position == css.Fixed {
				at = a.y
				break
			}
		}
		pg := e.pageOf(at)
		top := e.pageTop(pg)
		port = [4]float64{0, top, e.areaW(pg), top + e.areaH(pg)}
	} else {
		port = [4]float64{
			sc.x + sc.bd[css.Left], sc.y + sc.bd[css.Top],
			sc.x + sc.borderW() - sc.bd[css.Right], sc.y + sc.borderH() - sc.bd[css.Bottom],
		}
	}
	pw, ph := port[2]-port[0], port[3]-port[1]
	cb := relContainer(b)
	if cb == nil {
		return 0, 0
	}
	cl, ct := cb.contentX(), cb.contentY()
	cr, cbot := cl+cb.w, ct+cb.h
	margin := func(s css.Side) float64 {
		if b.st.Margin[s].Kind == css.Auto {
			return 0
		}
		return b.m[s]
	}
	l, t := b.x, b.y
	r, bot := l+b.borderW(), t+b.borderH()
	axis := func(lo, hi float64, startInset, endInset css.Dimension, size, pLo, pHi, cLo, cHi, mLo, mHi float64) float64 {
		d := 0.0
		if endInset.Kind != css.Auto {
			if v := pHi - resolve(endInset, size); hi > v {
				d = min(max(v-hi, cLo-(lo-mLo)), 0)
			}
		}
		if startInset.Kind != css.Auto {
			if v := pLo + resolve(startInset, size); lo+d < v {
				d = max(min(v-lo, cHi-(hi+mHi)), d, 0)
			}
		}
		return d
	}
	dx := axis(l, r, b.st.Left, b.st.Right, pw, port[0], port[2], cl, cr, margin(css.Left), margin(css.Right))
	dy := axis(t, bot, b.st.Top, b.st.Bottom, ph, port[1], port[3], ct, cbot, margin(css.Top), margin(css.Bottom))
	return dx, dy
}
