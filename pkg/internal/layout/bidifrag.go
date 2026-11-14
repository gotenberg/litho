package layout

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// visualEdges places the fragments and edges of the inline boxes of a line
// that bidi reordering puts out of logical order. Reordering can split an
// inline box into several fragments on one line; each fragment is a border
// box of its own, and the margins, borders and padding of the box's edges
// go in visual order: its left edge on its left-most fragment and its right
// edge on its right-most one, whichever of the two is its start (CSS Writing
// Modes 3, 2.4.5).
//
// # Corpus evidence
//
//   - probe, html: "abc <span>xyz אבג</span> דהו end" with a bordered span:
//     Chromium 156 draws two border boxes, "xyz " with the left border and
//     "אבג" with the right one, the outer Hebrew word between them.
type visualEdges struct {
	// members lists, per inline box, the visual indexes of the line's spans
	// it holds, ascending; runs gives the fragment each of them falls in.
	members map[*box][]int
	runs    map[*box][]int
	// leftAt and rightAt list the boxes whose left-most or right-most span
	// is the span of that index, in the order their edges are met from
	// left to right: outermost first on the left, innermost first on the
	// right.
	leftAt, rightAt map[int][]*box
}

// reorderedLine reports whether the spans of a line, in visual order, are
// out of logical order: a line of right-to-left or mixed text.
func reorderedLine(spans []text.Span) bool {
	for i := 1; i < len(spans); i++ {
		if spans[i].Start < spans[i-1].Start {
			return true
		}
	}
	return false
}

// newVisualEdges finds the fragments of the inline boxes of the line of
// characters [start, end) of p whose spans, in visual order, are spans.
func (s *inlineState) newVisualEdges(p *ifcPara, spans []text.Span, start, end int) *visualEdges {
	v := &visualEdges{members: map[*box][]int{}, runs: map[*box][]int{}, leftAt: map[int][]*box{}, rightAt: map[int][]*box{}}
	add := func(b *box, i int) {
		m := v.members[b]
		if len(m) > 0 && m[len(m)-1] == i {
			return
		}
		v.members[b] = append(m, i)
	}
	for i, sp := range spans {
		b := p.infos[sp.Run].box
		if b != nil && b.kind == kindInline {
			add(b, i)
		}
		for a := s.parentInline(b); a != nil; a = s.parentInline(a) {
			add(a, i)
		}
	}
	// An empty inline box with no edges holds no span: it sits where its
	// position falls, at the start of the span after it or the end of the
	// span before it.
	attach := func(b *box, pos int) {
		if _, ok := v.members[b]; ok || b.kind != kindInline {
			return
		}
		for i, sp := range spans {
			if sp.Start == pos || sp.End == pos {
				v.members[b] = []int{i}
				return
			}
		}
	}
	for pos := start; pos <= end; pos++ {
		for _, b := range p.openAt[pos] {
			attach(b, pos)
		}
		for _, b := range p.closeAt[pos] {
			attach(b, pos)
		}
	}
	for b, m := range v.members {
		r := make([]int, len(m))
		for k := 1; k < len(m); k++ {
			r[k] = r[k-1]
			if m[k] != m[k-1]+1 {
				r[k]++
			}
		}
		v.runs[b] = r
		v.leftAt[m[0]] = append(v.leftAt[m[0]], b)
		v.rightAt[m[len(m)-1]] = append(v.rightAt[m[len(m)-1]], b)
	}
	// The collector numbers boxes in document order, a box before its
	// descendants.
	order := s.d.order
	for _, l := range v.leftAt {
		slices.SortFunc(l, func(a, b *box) int { return order[a] - order[b] })
	}
	for _, l := range v.rightAt {
		slices.SortFunc(l, func(a, b *box) int { return order[b] - order[a] })
	}
	return v
}

// run returns the fragment of box b that the span of visual index i falls
// in, 0 when b holds no such span.
func (v *visualEdges) run(b *box, i int) int {
	k := slices.Index(v.members[b], i)
	if k < 0 {
		return 0
	}
	return v.runs[b][k]
}

// lastRun returns the index of the right-most fragment of box b.
func (v *visualEdges) lastRun(b *box) int {
	r := v.runs[b]
	if len(r) == 0 {
		return 0
	}
	return r[len(r)-1]
}

// edges advances x past the left edges of the boxes whose left-most span
// is span i when left, else past the right edges of those whose right-most
// span it is. A left-to-right box's left edge is its start, drawn on the
// line its first fragment opens on; a right-to-left box's left edge is its
// end, drawn on the line its last fragment closes on.
func (s *inlineState) edges(p *ifcPara, v *visualEdges, i int, left bool, start, end int, x *float64, widen func(*box, float64, float64)) {
	list := v.rightAt[i]
	if left {
		list = v.leftAt[i]
	}
	for _, b := range list {
		opens, closes := p.opensIn(b, start, end), p.closesIn(b, start, end)
		var draws bool
		if (b.st.Direction == css.RTL) == left {
			draws = b.last && closes
		} else {
			draws = b.first && opens
		}
		if draws {
			s.edge(b, left, x, widen)
		} else {
			widen(b, *x, *x)
		}
	}
}
