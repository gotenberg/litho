package layout

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// checkPage reports a page box whose size or margins [Layout] cannot lay
// out on.
func checkPage(pb PageBox) error {
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	if !finite(pb.Width) || !finite(pb.Height) || pb.Width <= 0 || pb.Height <= 0 || pb.Width > maxPageSide || pb.Height > maxPageSide {
		return fmt.Errorf("%w: page of %v by %v points", ErrInvalidOptions, pb.Width, pb.Height)
	}
	if !finite(pb.Bleed) || pb.Bleed < 0 || pb.Width+2*pb.Bleed > maxPageSide || pb.Height+2*pb.Bleed > maxPageSide {
		return fmt.Errorf("%w: bleed of %v points on a page of %v by %v", ErrInvalidOptions, pb.Bleed, pb.Width, pb.Height)
	}
	for _, m := range pb.Margin {
		if !finite(m) || m < 0 {
			return fmt.Errorf("%w: page margin %v", ErrInvalidOptions, m)
		}
	}
	if w, h := areaOf(pb); w <= 0 || h <= 0 {
		return fmt.Errorf("%w: margins leave a page area of %v by %v points", ErrInvalidOptions, w, h)
	}
	return checkFrame(pb)
}

// areaOf returns the size of the page area of pb.
func areaOf(pb PageBox) (float64, float64) {
	return pb.Width - pb.Margin[css.Left] - pb.Margin[css.Right], pb.Height - pb.Margin[css.Top] - pb.Margin[css.Bottom]
}

// geometry is where the pages cut the flow. The flow is one column as wide
// as the first page's area; page k shows the slice from tops[k] to
// tops[k+1]. With every page alike (no [Options.PageAt]) the cuts fall
// every page area height and nothing is stored.
type geometry struct {
	e *engine
	// boxes are the page boxes resolved so far, tops the flow positions of
	// their tops followed by the bottom of the last: len(tops) is
	// len(boxes)+1. Both stay empty without PageAt.
	boxes []PageBox
	tops  []float64
	// infos are the pages the boxes were resolved for, by
	// [Options.PageFor].
	infos []PageInfo
}

// init sets up the geometry of e, whose first page box is first.
func (g *geometry) init(e *engine, first PageBox) {
	g.e = e
	if e.opts.PageAt == nil && e.opts.PageFor == nil && e.foot == nil {
		return
	}
	g.boxes = []PageBox{first}
	g.tops = []float64{0, e.usableH(0, first, 0)}
	g.infos = []PageInfo{e.infoOf(0)}
}

// varies reports pages that differ from one another.
func (g *geometry) varies() bool { return g.tops != nil }

// grow resolves one more page box, reporting false past the page cap or
// once the layout failed. A box out of range fails the layout.
func (g *geometry) grow() bool {
	e := g.e
	n := len(g.boxes)
	if n > e.maxPages || e.err != nil {
		return false
	}
	info := e.infoOf(n)
	pb := e.opts.Page
	switch {
	case e.opts.PageFor != nil:
		pb = e.opts.PageFor(info)
	case e.opts.PageAt != nil:
		pb = e.opts.PageAt(n)
	}
	if err := checkPage(pb); err != nil {
		e.fail(fmt.Errorf("page %d: %w", n+1, err))
		return false
	}
	g.boxes = append(g.boxes, pb)
	g.tops = append(g.tops, g.tops[n]+e.usableH(n, pb, g.tops[n]))
	g.infos = append(g.infos, info)
	return true
}

// resync drops the page boxes from the first one resolved for a page
// whose name or blankness has changed since: they resolve again as the
// layout reaches them. The pages before keep their boxes, so every flow
// position above the first dropped page's top keeps its page.
func (g *geometry) resync() {
	if g.e == nil || g.e.opts.PageFor == nil {
		return
	}
	for i := 1; i < len(g.infos); i++ {
		if g.e.infoOf(i) != g.infos[i] {
			g.boxes, g.infos, g.tops = g.boxes[:i], g.infos[:i], g.tops[:i+1]
			return
		}
	}
}

// isRight reports whether page p is a right page: the first page is a
// right page when the root's direction is left to right, a left page
// otherwise, and the sides alternate (CSS Page 3, 4.3.2).
func (e *engine) isRight(p int) bool {
	return (p%2 == 0) != e.rtlRoot
}

// box returns the page box of page p.
func (e *engine) box(p int) PageBox {
	g := &e.geom
	if !g.varies() {
		return e.opts.Page
	}
	for p >= len(g.boxes) && g.grow() {
	}
	return g.boxes[min(max(p, 0), len(g.boxes)-1)]
}

// areaW returns the page area width of page p.
func (e *engine) areaW(p int) float64 {
	if !e.geom.varies() {
		return e.pageW
	}
	w, _ := areaOf(e.box(p))
	return w
}

// areaH returns the page area height of page p.
func (e *engine) areaH(p int) float64 {
	if e.cols != nil {
		return e.cols.height(p)
	}
	if !e.geom.varies() {
		return e.pageH
	}
	_, h := areaOf(e.box(p))
	return h
}

// overflowRight returns the right edge of the scrollable overflow of the
// root box rb, from the left edge of the first page's area (CSS Overflow
// 3, 2.2): the border box of every box laid out, the line contents of
// every block container, without descending into a box that clips its
// overflow. The box whose overflow went to the viewport does not clip its
// own content (CSS Overflow 3, 3.3), so the page area's overflow counts it
// all. When the pages differ in width, what a page holds counts in
// proportion to that page's area width, scaled to the first page's: the
// content a wider named page holds is no overflow, and Chromium prints
// such a page without shrinking the document. The walk keeps its own
// stack: the tree is as deep as the document.
func (e *engine) overflowRight(rb *box) float64 {
	vp := viewportBox(rb)
	right := 0.0
	count := func(edge, y float64) {
		if e.geom.varies() {
			if w := e.areaW(e.pageOf(y)); w > 0 {
				edge = edge / w * e.pageW
			}
		}
		right = max(right, clamp(edge))
	}
	stack := []*box{rb}
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !b.laidOut {
			continue
		}
		if b.kind == kindBlock || b.kind == kindReplaced {
			count(b.x+b.borderW(), b.y)
		}
		if b.kind == kindBlock && b.parent != nil && b.st.Overflow == css.OverflowHidden && b != vp {
			// The box clips its content: its border box alone counts.
			continue
		}
		for _, l := range b.lines {
			for _, it := range l.items {
				count(l.x+it.x+it.w, l.y)
			}
			for _, f := range l.frags {
				count(l.x+f.x1, l.y)
			}
		}
		stack = append(stack, b.kids...)
	}
	return right
}

// fittedHeight returns the height of the content the first page shows,
// from the top of its page area, for [Options.FitFirstPage], at most the
// area's height and at least one point. A box that ends on the first page
// counts with its bottom margin, one the cut crosses for its descendants
// and lines that end on the page when part of its content starts on a
// later page, and fills the page otherwise, and so does a fragment that ends there,
// inside the clips of its groups: content a cut carries over takes no room
// on the page. The content of a fixed box paints on every page and takes
// no room of its own.
func (e *engine) fittedHeight(rb *box, d *ditem) float64 {
	areaH := e.areaH(0)
	end := areaH + eps
	bottom := 0.0
	stack := []*box{rb}
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !b.laidOut || b.fixed || b.kind == kindInline || b.kind == kindText || b.kind == kindBreak || b.y >= end-2*eps {
			continue
		}
		if y := b.y + b.borderH() + max(b.m[css.Bottom], 0); y <= end {
			bottom = max(bottom, y)
			continue
		}
		if b.mc != nil || !continues(b, end) {
			// The box itself reaches past the cut, or its columns do: it
			// fills the page.
			bottom = areaH
			continue
		}
		for _, l := range b.lines {
			if y := l.y + l.h; y <= end {
				bottom = max(bottom, y)
			}
		}
		stack = append(stack, b.kids...)
	}
	var walk func(it *ditem, lo, hi float64)
	walk = func(it *ditem, lo, hi float64) {
		for _, k := range it.kids {
			if k.group {
				if k.fixed {
					continue
				}
				klo, khi := lo, hi
				if k.clip != nil {
					if y0, y1, ok := pathSpan(k.clip.Ops); ok {
						klo, khi = max(lo, y0), min(hi, y1)
					}
				}
				if khi > klo {
					walk(k, klo, khi)
				}
				continue
			}
			if k.page > 0 || k.page < 0 && k.y1 > end {
				continue
			}
			bottom = max(bottom, min(k.y1, hi))
		}
	}
	walk(d, math.Inf(-1), math.Inf(1))
	return min(max(clamp(bottom), 1), areaH)
}

// assembledBox returns the page box page k is assembled with: its own,
// the first trimmed to its content under [Options.FitFirstPage], fitH
// being that content's height.
func (e *engine) assembledBox(k int, fitH float64) PageBox {
	pb := e.box(k)
	if k == 0 && e.opts.FitFirstPage {
		pb.Height = pb.Margin[css.Top] + fitH + pb.Margin[css.Bottom]
	}
	return pb
}

// continues reports whether a line or a box inside b starts at or below
// end, on a later page, through boxes the cut crosses: the cut falls
// between pieces of b's content rather than across a box of it.
func continues(b *box, end float64) bool {
	stack := []*box{b}
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, l := range c.lines {
			if l.y >= end-2*eps {
				return true
			}
		}
		for _, k := range c.kids {
			if !k.laidOut || k.kind == kindInline || k.kind == kindText || k.kind == kindBreak || k.fixed {
				continue
			}
			if k.y >= end-2*eps {
				return true
			}
			if k.y+k.borderH() > end {
				stack = append(stack, k)
			}
		}
	}
	return false
}
