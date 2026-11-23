package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// pageAlignShift returns how far page k's slice of the flow moves down by its
// page box's [PageBox.AlignContent]: that fraction of the room between
// the bottom of the content the page shows and the end of its slice. A
// page whose content runs on to the next page has no room.
func (e *engine) pageAlignShift(k int) float64 {
	if e.aligned == nil || k < 0 {
		return 0
	}
	pb := e.box(k)
	f := pb.AlignContent
	if !(f > 0) || pb.SpreadContent {
		return 0
	}
	if v, ok := e.shifts[k]; ok {
		return v
	}
	top := e.pageTop(k)
	end := top + e.sliceH(k)
	room := max(end-e.contentBottom(e.aligned, top, end), 0)
	v := float64(min(f, 1) * room)
	if e.shifts == nil {
		e.shifts = map[int]float64{}
	}
	e.shifts[k] = v
	return v
}

// contentBottom returns the bottom of the boxes of the flow rooted at rb
// over [top, end), the slice of a page: the border box of each box that
// ends in it, end for one that runs past it, top for none. A box that
// starts above the slice or runs past it is read through its children,
// the bottom margin of a box that ends in it counting too,
// the boxes it holds in the slice; floats and positioned boxes count
// where they lie, fixed ones not.
func (e *engine) contentBottom(rb *box, top, end float64) float64 {
	bottom := top
	stack := []*box{rb}
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !b.laidOut || b.fixed || b.kind == kindInline || b.kind == kindText || b.kind == kindBreak {
			continue
		}
		y0, y1 := b.y, b.y+b.borderH()
		if y1 <= top+eps || y0 >= end-eps || math.IsNaN(y0) {
			continue
		}
		if y0 >= top-eps && y1 <= end+eps {
			// The space after the last box counts as its content, as a
			// word processor counts a paragraph's space after.
			bottom = max(bottom, min(y1+max(b.m[css.Bottom], 0), end))
			if len(b.lines) == 0 {
				// The last box's margin may collapse through its parent.
				stack = append(stack, b.kids...)
			}
			continue
		}
		if len(b.kids) == 0 || len(b.lines) > 0 {
			// A box of lines the cut crosses fills the page.
			bottom = max(bottom, min(y1, end))
			continue
		}
		stack = append(stack, b.kids...)
	}
	return bottom
}

// maxSpreadBlocks bounds the blocks [engine.spreadPages] collects.
// Anti-DoS guard, not spec-mandated: a page holds a few dozen blocks, and
// a document past the bound keeps its pages' content at the top.
const maxSpreadBlocks = 1 << 16

// spreadPages moves the blocks of the flow rooted at rb down on each page
// whose box asks [PageBox.SpreadContent]: block i of the page's n by i
// times the room its content leaves over n-1. A block is a box of lines,
// a table, a replaced box or an empty box; a box holding blocks is read
// through them.
func (e *engine) spreadPages(rb *box) {
	if !e.paginating() || e.cols != nil || !e.spreads() {
		return
	}
	type unit struct {
		b    *box
		page int
	}
	var units []unit
	// cut marks the pages a block runs past the edge of.
	cut := map[int]bool{}
	spread := map[int]bool{}
	stack := []*box{rb}
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !b.laidOut || b.fixed || b.abs || b.float != css.FloatNone || b.inline || b.kind == kindInline || b.kind == kindText || b.kind == kindBreak || math.IsNaN(b.y) {
			continue
		}
		block := len(b.lines) > 0 || b.kind == kindReplaced || b.tpart == partWrapper || len(b.kids) == 0
		if !block {
			for i := len(b.kids) - 1; i >= 0; i-- {
				stack = append(stack, b.kids[i])
			}
			continue
		}
		y0, y1 := b.y, b.y+b.borderH()
		if n := len(b.lines); n > 0 {
			// A paragraph pushed to the next page whole keeps its box's
			// top where it was: its lines say where it lies.
			y0, y1 = b.lines[0].y, b.lines[n-1].y+b.lines[n-1].h
		}
		// pageOf counts a position eps above a page's top on that page.
		p, q := e.pageOf(y0), e.pageOf(y1-2*eps)
		if y1-y0 <= eps {
			q = p
		}
		if p != q {
			for k := p; k <= q && k-p < maxSpreadBlocks; k++ {
				cut[k] = true
			}
			continue
		}
		pb, ok := spread[p]
		if !ok {
			pb = e.box(p).SpreadContent
			spread[p] = pb
		}
		if !pb {
			continue
		}
		if len(units) >= maxSpreadBlocks {
			return
		}
		units = append(units, unit{b: b, page: p})
	}
	for i := 0; i < len(units); {
		j := i + 1
		for j < len(units) && units[j].page == units[i].page {
			j++
		}
		if k := units[i].page; !cut[k] {
			bs := make([]*box, 0, j-i)
			for _, u := range units[i:j] {
				bs = append(bs, u.b)
			}
			e.spreadPage(k, bs, rb)
		}
		i = j
	}
}

// spreads reports whether a page box laid out asks
// [PageBox.SpreadContent].
func (e *engine) spreads() bool {
	if !e.geom.varies() {
		return e.opts.Page.SpreadContent
	}
	for _, pb := range e.geom.boxes {
		if pb.SpreadContent {
			return true
		}
	}
	return false
}

// spreadPage spreads the blocks bs of page k, in flow order, down the
// room the flow rooted at rb leaves on it. Blocks that sit side by side
// leave the page as it is.
func (e *engine) spreadPage(k int, bs []*box, rb *box) {
	if len(bs) < 2 {
		return
	}
	top := e.pageTop(k)
	for i := 1; i < len(bs); i++ {
		if max(bs[i].y, top) < bs[i-1].y+bs[i-1].borderH()-eps {
			return
		}
	}
	end := top + e.sliceH(k)
	room := max(end-e.contentBottom(rb, top, end), 0)
	gap := room / float64(len(bs)-1)
	for i, b := range bs {
		b.shift(0, float64(gap*float64(i)))
	}
}
