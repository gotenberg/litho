package layout

import "github.com/gotenberg/litho/pkg/internal/typeset"

// ditemCost is what one item of the display list holds besides its
// fragment: the [ditem], 80 bytes on a 64-bit platform, and its slot in
// its group's kids.
const ditemCost = 96

// groupFxCost is the effects of a group that has some, a [groupFx] of 112
// bytes, its transform and its clip's path aside.
const groupFxCost = 112

// matrixCost is a group's transform, a [typeset.Matrix] of 48 bytes.
const matrixCost = 48

// itemCost returns what the display list item d holds, its kids aside:
// the item, and a leaf's fragment or a group's effects.
func itemCost(d *ditem) int64 {
	if !d.group {
		return ditemCost + typeset.FragmentCost(d.frag)
	}
	n := int64(ditemCost)
	if fx := d.fx; fx != nil {
		n += groupFxCost
		if fx.clip != nil {
			n += typeset.FragmentCost(fx.clip)
		}
		if fx.transform != nil {
			n += matrixCost
		}
		if fx.mask != nil {
			n += typeset.ContentCost(fx.mask.Content)
		}
		if bd := fx.backdrop; bd != nil {
			n += backdropCost + typeset.FragmentCost(bd.edge)
		}
	}
	return n
}

// displayCost returns what the display list d holds, every item under it
// included.
func displayCost(d *ditem) int64 {
	if d == nil {
		return 0
	}
	var n int64
	stack := []*ditem{d}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n += itemCost(it)
		stack = append(stack, it.kids...)
	}
	return n
}

// chargeDrawn charges the conversion's budget n bytes of display list
// items as the painter makes them: a box that paints many items, the tiles
// of a repeated background, is refused as it paints them rather than once
// the list is done.
func (e *engine) chargeDrawn(n int64) {
	e.charge(n, "the display list")
	if e.err == nil {
		e.drawn += n
	}
}

// settleDrawn brings what the engine charged for its display list, the
// painted list d and the footnote areas, to what they hold
// ([displayCost]): the items charged as they were painted and the rest.
// The engine holds the charge until the layout drops it.
func (e *engine) settleDrawn(d *ditem) {
	total := displayCost(d)
	if f := e.foot; f != nil {
		for _, nd := range f.disp {
			total += displayCost(nd)
		}
	}
	switch {
	case total > e.drawn:
		e.chargeDrawn(total - e.drawn)
	case total < e.drawn:
		e.mem.Release(e.drawn - total)
		e.held -= e.drawn - total
		e.drawn = total
	}
}
