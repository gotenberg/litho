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
	f := e.box(k).AlignContent
	if !(f > 0) {
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
