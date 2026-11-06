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
	for _, m := range pb.Margin {
		if !finite(m) || m < 0 {
			return fmt.Errorf("%w: page margin %v", ErrInvalidOptions, m)
		}
	}
	if w, h := areaOf(pb); w <= 0 || h <= 0 {
		return fmt.Errorf("%w: margins leave a page area of %v by %v points", ErrInvalidOptions, w, h)
	}
	return nil
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
}

// init sets up the geometry of e, whose first page box is first.
func (g *geometry) init(e *engine, first PageBox) {
	g.e = e
	if e.opts.PageAt == nil {
		return
	}
	_, h := areaOf(first)
	g.boxes = []PageBox{first}
	g.tops = []float64{0, h}
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
	pb := e.opts.PageAt(n)
	if err := checkPage(pb); err != nil {
		e.fail(fmt.Errorf("page %d: %w", n+1, err))
		return false
	}
	_, h := areaOf(pb)
	g.boxes = append(g.boxes, pb)
	g.tops = append(g.tops, g.tops[n]+h)
	return true
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

// areaH returns the page area height of page p.
func (e *engine) areaH(p int) float64 {
	if !e.geom.varies() {
		return e.pageH
	}
	_, h := areaOf(e.box(p))
	return h
}
