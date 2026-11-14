package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// clones reports a box whose fragments each get their own decorations at
// column breaks: box-decoration-break clone, or its Office variant that
// clones at page breaks too.
func clones(st *css.Style) bool {
	return st.BoxDecorationBreak == css.DecorationClone || st.BoxDecorationBreak == css.DecorationCloneAll
}

// clonedPosition returns where a line h tall whose top would be at top
// lands in a block container that clones its decorations at page breaks
// ([css.DecorationCloneAll]): on the page when it fits there with the
// container's bottom padding and border below it, else on the next page,
// below the container's top border and padding, which the fragment there
// draws again, as Writer lays a bordered paragraph out across pages.
func (s *inlineState) clonedPosition(top, h float64) float64 {
	e, b := s.e, s.root
	bottom := b.pd[css.Bottom] + b.bd[css.Bottom]
	y := e.fit(top, h+bottom)
	if e.atPageTop(y) && e.pageOf(y) > e.pageOf(b.y+eps) {
		y = e.fit(y+b.pd[css.Top]+b.bd[css.Top], h+bottom)
	}
	return y
}

// pageFragments returns the border boxes of a block whose border box is r,
// in flow coordinates offset by the painter's, one per page it crosses,
// each with all its edges ([css.DecorationCloneAll]).
func (p *painter) pageFragments(r typeset.Rect) []typeset.Rect {
	e := p.e
	y0, y1 := r.Y-p.dy, r.Y+r.Height-p.dy
	first, last := e.pageOf(y0+eps), e.pageOf(y1-eps)
	if last <= first || last-first > maxCloneFragments {
		return []typeset.Rect{r}
	}
	out := make([]typeset.Rect, 0, last-first+1)
	for k := first; k <= last; k++ {
		a, z := max(y0, e.pageTop(k)), min(y1, e.pageTop(k+1))
		if z > a+eps {
			out = append(out, typeset.Rect{X: r.X, Y: a + p.dy, Width: r.Width, Height: z - a})
		}
	}
	return out
}

// maxCloneFragments bounds the page fragments one cloned box paints:
// 10,000. Anti-DoS guard, not spec-mandated: a box past it paints as one
// sliced box, and a paragraph crossing ten thousand pages is no document
// a reader prints.
const maxCloneFragments = 10_000

// cloneClip is the room the cloned edges of a box laid out in the column
// set cols take: bottom at the end of each column from first on, top at
// the start of each column after first.
type cloneClip struct {
	cols        *colCtx
	first       int
	top, bottom float64
}

// enterClone makes the content of the block b, whose border box starts at
// y, leave room in each column it crosses for b's bottom padding and
// border at the column's end, and start below b's top border and padding
// in each column after its first: under box-decoration-break: clone each
// fragment holds them whole (CSS Fragmentation 3, 5.4). The margins are
// not cloned: they are truncated at the break (5.2), as Chromium lays the
// box out. It reports whether it made room, which [engine.leaveClone]
// gives back. A box of a specified height grows instead
// ([engine.cloneEdges]); pages slice the box, as Chromium prints it.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) lays a cloned
//     box of 10px margins, 10px padding and 5px borders holding 100px in
//     100px columns as fragments of 100 and 70px, the first below its top
//     margin, the second at its column's top; one of 180px content and no
//     margins as fragments of 100, 100 and 70px.
func (e *engine) enterClone(b *box, y float64, hSet bool) bool {
	c := e.cols
	if hSet || !clones(b.st) || !e.paginating() || c == nil {
		return false
	}
	top, bottom := b.bd[css.Top]+b.pd[css.Top], b.bd[css.Bottom]+b.pd[css.Bottom]
	if top <= 0 && bottom <= 0 {
		return false
	}
	first := c.of(y + eps)
	e.clips = append(e.clips, cloneClip{cols: c, first: first, top: top, bottom: bottom})
	for k := first; k < len(c.frags); k++ {
		f := &c.frags[k]
		f.tail += bottom
		if k > first {
			f.lead += top
		}
	}
	c.reflow(first)
	return true
}

// leaveClone gives back the room [engine.enterClone] made, entered, for
// the block whose content ends at y: the column y lies in keeps its lead
// and ends at its full height, where the block's own bottom edges follow
// its content; the columns after it lose both.
func (e *engine) leaveClone(entered bool, y float64) {
	if !entered {
		return
	}
	cl := e.clips[len(e.clips)-1]
	e.clips = e.clips[:len(e.clips)-1]
	c := cl.cols
	if len(c.frags) == 0 {
		return
	}
	last := max(c.index(y-eps), cl.first)
	for k := last; k < len(c.frags); k++ {
		f := &c.frags[k]
		f.tail = max(f.tail-cl.bottom, 0)
		if k > last {
			f.lead = max(f.lead-cl.top, 0)
		}
	}
	c.reflow(last)
}

// cloned returns the column f with the room the cloned edges of the boxes
// being laid out in c take in it.
func (c *colCtx) cloned(f colFrag) colFrag {
	i := len(c.frags)
	for _, cl := range c.e.clips {
		if cl.cols != c {
			continue
		}
		f.tail += cl.bottom
		if i > cl.first {
			f.lead += cl.top
		}
	}
	return f
}

// reflow moves the slices of the columns after column i to follow the
// slice before each, once the room of the cloned edges changed.
func (c *colCtx) reflow(i int) {
	for k := max(i, 0) + 1; k < len(c.frags); k++ {
		c.frags[k].vtop = c.frags[k-1].end()
	}
}
