package layout

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
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
func (p *painter) pageFragments(r compose.Rect) []compose.Rect {
	e := p.e
	y0, y1 := r.Y-p.dy, r.Y+r.Height-p.dy
	first, last := e.pageOf(y0+eps), e.pageOf(y1-eps)
	if last <= first || last-first > maxCloneFragments {
		return []compose.Rect{r}
	}
	out := make([]compose.Rect, 0, last-first+1)
	for k := first; k <= last; k++ {
		a, z := max(y0, e.pageTop(k)), min(y1, e.pageTop(k+1))
		if z > a+eps {
			out = append(out, compose.Rect{X: r.X, Y: a + p.dy, Width: r.Width, Height: z - a})
		}
	}
	return out
}

// maxCloneFragments bounds the page fragments one cloned box paints:
// 10,000. Anti-DoS guard, not spec-mandated: a box past it paints as one
// sliced box, and a paragraph crossing ten thousand pages is no document
// a reader prints.
const maxCloneFragments = 10_000
