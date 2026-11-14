package formula

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// lctx is what the layout of an element receives from its parent: the
// form it has as an embellished operator and its stretch size constraint
// (MathML Core, 3.1.2 and 3.2.4.2).
type lctx struct {
	form form
	// stretch reports a stretch size constraint: inline, along the inline
	// axis to target; otherwise along the block axis to cover asc and
	// desc, an ink line-ascent and line-descent.
	stretch   bool
	inline    bool
	target    float64
	asc, desc float64
}

// constrained returns c without its stretch size constraint.
func (c lctx) constrained() lctx { return lctx{form: c.form} }

// boxCost is what one element's box and the items it holds are charged
// against the conversion's memory budget. Not measured to the byte: a box
// is about 130 bytes, its items 64 each, and an element holds a few.
const boxCost = 512

// layoutRoot lays the formula root out, styled st, as an mrow whose
// operators take their spaces (MathML Core, 2.1.1).
func (l *layouter) layoutRoot(root *Element, st *Style) *box {
	return l.layout(root, st, lctx{}, 0)
}

// layout lays e out, styled st, under the constraints of c, depth levels
// below the root, and returns its margin box.
func (l *layouter) layout(e *Element, st *Style, c lctx, depth int) *box {
	if !l.admit(depth) {
		return &box{}
	}
	var b *box
	switch e.Name {
	case "mi", "mn", "mtext", "ms", "annotation", "annotation-xml":
		b = l.token(e, st)
	case "mo":
		b = l.mo(e, st, c)
	case "mspace":
		b = l.mspace(e, st)
	case "mfrac":
		b = l.mfrac(e, st, c, depth)
	case "msqrt":
		b = l.msqrt(e, st, c, depth)
	case "mroot":
		b = l.mroot(e, st, c, depth)
	case "msub", "msup", "msubsup":
		b = l.scripts(e, st, c, depth)
	case "munder", "mover", "munderover":
		b = l.underOver(e, st, c, depth)
	case "mmultiscripts":
		b = l.multiscripts(e, st, c, depth)
	case "mpadded":
		b = l.mpadded(e, st, c, depth)
	case "mtable":
		b = l.mtable(e, st, depth)
	default:
		b = l.row(e, st, c, depth)
	}
	if st.RTL && !isToken(e.Name) && e.Name != "mo" {
		mirror(b)
	}
	return edges(b, st, centred(e.Name))
}

// mirror places the items of b, laid out from the left, from the right:
// the inline offset of an item in a right-to-left box is the box's inline
// size less the item's line-left offset and inline size (MathML Core,
// 3.1.1). Only the boxes of tokens hold glyph runs, and a token keeps the
// order its shaping gave its text.
func mirror(b *box) {
	for i := range b.items {
		it := &b.items[i]
		switch {
		case it.child != nil:
			it.x = b.w - it.x - it.child.w
		case it.rect != nil:
			it.x = b.w - it.x - it.rect.w
		}
	}
}

// centred reports the elements whose math content box sits in the
// middle of a content box CSS sizes, block-start edges aligned (MathML
// Core, 3.3.2.1, 3.3.2.2, 3.4.2.3 to 3.4.2.5); the others sit at its
// inline-start edge (3.1.2).
func centred(name string) bool {
	switch name {
	case "mfrac", "munder", "mover", "munderover":
		return true
	}
	return false
}

// admit counts one more element laid out depth levels below the root,
// checks the context and charges its box, and reports whether it is laid
// out: past [MaxDepth] or [MaxElements] it is left out with a warning.
func (l *layouter) admit(depth int) bool {
	if l.err != nil {
		return false
	}
	if depth > MaxDepth {
		l.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("formula content nested deeper than %d elements was left out", MaxDepth))
		return false
	}
	l.elements++
	if l.elements > MaxElements {
		l.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("formula elements past the first %d were left out", MaxElements))
		return false
	}
	if l.elements%256 == 0 {
		if err := l.ctx.Err(); err != nil {
			l.fail(err)
			return false
		}
	}
	if err := l.mem.Charge(boxCost, "formula boxes"); err != nil {
		l.fail(err)
		return false
	}
	return true
}

// inFlow returns the in-flow children of e with their indexes among its
// children: all of them but those past the first of semantics and
// maction, which the user agent stylesheet displays none (MathML Core,
// 3.6 and 3.7).
func inFlow(e *Element) (kids []*Element, index []int) {
	for i, k := range e.Children {
		if i > 0 && (e.Name == "semantics" || e.Name == "maction") {
			break
		}
		kids = append(kids, k)
		index = append(index, i)
	}
	return kids, index
}

// isGrouping reports the grouping elements of MathML Core, 3.1.3 and
// 3.2.4.1, unknown elements included (4.1), which lay their children out
// as an mrow does.
func isGrouping(name string) bool {
	switch name {
	case "mi", "mn", "mo", "mtext", "ms", "mspace", "mfrac", "msqrt", "mroot", "msub", "msup",
		"msubsup", "munder", "mover", "munderover", "mmultiscripts", "mpadded", "mtable", "mtr",
		"mtd", "annotation", "annotation-xml":
		return false
	}
	return true
}

// isScripted reports the scripted elements (MathML Core, 3.4).
func isScripted(name string) bool {
	switch name {
	case "msub", "msup", "msubsup", "munder", "mover", "munderover", "mmultiscripts":
		return true
	}
	return false
}

// core returns the core operator of e when e is an embellished operator
// (MathML Core, 3.2.4.1), nil when it is not one. Results are kept, so
// each element is looked at once.
func (l *layouter) core(e *Element, depth int) *Element {
	if c, ok := l.cores[e]; ok {
		return c
	}
	var c *Element
	switch {
	case depth > MaxDepth:
	case e.Name == "mo":
		c = e
	case isScripted(e.Name) || e.Name == "mfrac":
		if kids, _ := inFlow(e); len(kids) > 0 {
			c = l.core(kids[0], depth+1)
		}
	case isGrouping(e.Name) || e.Name == "mpadded":
		if k := l.embellishingChild(e, depth); k != nil {
			c = l.core(k, depth+1)
		}
	}
	if l.cores == nil {
		l.cores = map[*Element]*Element{}
	}
	l.cores[e] = c
	return c
}

// embellishingChild returns the in-flow child through which e, a
// grouping element or an mpadded, is an embellished operator: its one
// embellished operator child, the others space-like; nil when there is
// none.
func (l *layouter) embellishingChild(e *Element, depth int) *Element {
	kids, _ := inFlow(e)
	var found *Element
	for _, k := range kids {
		if l.spaceLike(k, depth+1) {
			continue
		}
		if found != nil || l.core(k, depth+1) == nil {
			return nil
		}
		found = k
	}
	return found
}

// spaceLike reports a space-like element (MathML Core, 3.2.5.1): an mtext
// or an mspace, or a grouping element or an mpadded whose in-flow
// children are all space-like. Results are kept.
func (l *layouter) spaceLike(e *Element, depth int) bool {
	if v, ok := l.spaces[e]; ok {
		return v
	}
	v := false
	switch {
	case depth > MaxDepth:
	case e.Name == "mtext" || e.Name == "mspace":
		v = true
	case isGrouping(e.Name) || e.Name == "mpadded":
		v = true
		kids, _ := inFlow(e)
		for _, k := range kids {
			if !l.spaceLike(k, depth+1) {
				v = false
				break
			}
		}
	}
	if l.spaces == nil {
		l.spaces = map[*Element]bool{}
	}
	l.spaces[e] = v
	return v
}
