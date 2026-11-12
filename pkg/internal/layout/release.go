package layout

import "github.com/gotenberg/litho/pkg/internal/css"

// releasePolicy is when a layout releases the shaped text of its inline
// formatting contexts.
type releasePolicy uint8

const (
	// releaseFinal releases a block's shaped text once block flow will
	// not lay it out again: the policy of every layout.
	releaseFinal releasePolicy = iota
	// releaseNever keeps every shaped paragraph to the end of the layout.
	releaseNever
	// releaseEagerly releases the shaped text of every inline formatting
	// context as soon as it is laid out, wherever it lies, so that every
	// later layout of it starts from text shaped again.
	releaseEagerly
)

// A shaped paragraph is the largest thing a layout holds while it runs:
// its characters, their advances and break opportunities, and every glyph
// the shaper made of them, some 50 bytes a character. The lines laid out
// from it hold their own glyph runs, so once a block's lines are final the
// paragraphs of its inline content serve no one. Block flow knows when
// that is: [engine.layoutBlockKids] lays a child out again only while it
// is the child being placed or the one before it, so a child is final
// once its next sibling is placed, provided the block itself is never laid
// out again. A block is settled when nothing it, its siblings and its
// ancestors declare lays it out again: see [engine.settles].
//
// A layout that comes back to a released paragraph shapes it again
// ([engine.ifcOf]), and gets the paragraph it released: shaping is a pure
// function of the paragraph's runs, its direction and the font index, the
// runs stay with the inline content, and every layout of a line sets the
// advances of the tabs and atomic inlines it reads ([engine.prepareInline],
// [engine.breakLineIn]). A release is therefore never wrong, only wasted
// when a layout the flags cannot foresee comes back: a block whose content
// starts a page below its top moves to that page whole.

// settles reports whether k, the in-flow child of the settled block b laid
// out after prev and before next (either nil for none) in b's formatting
// context bc, is settled itself: block flow lays it out once, and so
// releases the shaped paragraphs of each of its children once the next
// one is placed. A box whose layout is retried as a whole is not: a table,
// a flex or grid container, a multi-column container, an orthogonal flow,
// a link of a chain, a box breaks avoid inside or around, a formatting
// context root fitted beside floats.
func (e *engine) settles(b, prev, k, next *box, bc *bfc) bool {
	switch {
	case !b.settled, k.kind != kindBlock, k.inline, k.abs, k.float != css.FloatNone,
		k.tpart != partNone, k.flex != nil, k.grid != nil, isMulticol(k), k.ortho, k.monoCol, k.chain != nil:
		return false
	case avoidsInside(k), prev != nil && avoidsBetween(prev, k), next != nil && avoidsBetween(k, next):
		return false
	}
	return !k.establishesBFC() || len(bc.floats) == 0
}

// releasing reports whether the children of b, laid out in block flow,
// release their shaped paragraphs as they become final: b is settled, and
// no column set, line clamp or monolithic layout lays them out again.
func (e *engine) releasing(b *box) bool {
	switch e.opts.release {
	case releaseNever:
		return false
	case releaseFinal, releaseEagerly:
	}
	return b.settled && e.cols == nil && e.clamp == nil && e.paginating()
}

// nextInFlow returns the first in-flow block-level box of kids after index
// i, nil for none: the sibling [engine.layoutBlockKids] places after
// kids[i].
func nextInFlow(kids []*box, i int) *box {
	for _, k := range kids[i+1:] {
		if !k.inline && k.float == css.FloatNone && !k.abs {
			return k
		}
	}
	return nil
}

// releaseShaped releases the shaped paragraphs of b and of its
// descendants but the absolutely positioned ones and theirs, which their
// containing block lays out later: their lines are final. It gives back
// what [engine.shapePara] charged the budget for them.
func (e *engine) releaseShaped(b *box) {
	stack := []*box{b}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if x.released {
			continue
		}
		x.released = true
		if d := x.ifc; d != nil && !d.released {
			d.released = true
			for _, p := range d.paras {
				// The position indexes of the inline boxes serve line
				// building alone, and are built again with the text.
				p.openAt, p.closeAt, p.openPos, p.closePos = nil, nil, nil, nil
				if p.p == nil {
					continue
				}
				n := int64(p.n) * paraRuneCost
				e.mem.Release(n)
				e.held -= n
				p.p = nil
			}
		}
		for _, k := range x.kids {
			if !k.abs {
				stack = append(stack, k)
			}
		}
	}
}

// reshape shapes again the paragraphs of b's released inline content d, for
// a layout that comes back to it.
func (e *engine) reshape(b *box, d *ifcData) {
	d.released = false
	for a := b; a != nil && a.released; a = a.parent {
		// The box's ancestors hold shaped text again, which their own
		// release gives back.
		a.released = false
	}
	e.reshaped++
	for _, p := range d.paras {
		if e.err != nil {
			return
		}
		p.index()
		e.shapePara(b, p)
	}
}
