package html

import (
	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// positionOptions holds the position option a layout chose for each
// element with position options that took an entry of its fallbacks, by
// element: the accepted @position-try properties its computed values
// hold in the next cascade, which its descendants inherit (CSS Anchor
// Positioning 1, 5.4).
type positionOptions map[*dom.Node]layout.PositionOption

// option answers [css.ComputeOptions.PositionOption].
func (o positionOptions) option(n *dom.Node) *css.TryStyle { return o[n].Try }

// equal reports whether o and q hold an option for the same elements and
// each the same entry.
func (o positionOptions) equal(q positionOptions) bool {
	if len(o) != len(q) {
		return false
	}
	for n, t := range o {
		if u, ok := q[n]; !ok || u.Entry != t.Entry {
			return false
		}
	}
	return true
}

// positioned records node, the box of the element n, when n has position
// options and a style of the document inherits an accepted @position-try
// property from such an element: the option the layout chooses for it
// then changes computed values.
func (b *builder) positioned(node *layout.Node, n *dom.Node) {
	if !b.computed.InheritsPositionOptions() || node.Style.Anchor == nil || len(node.Style.Anchor.Fallbacks) == 0 {
		return
	}
	if b.anchored == nil {
		b.anchored = map[*layout.Node]*dom.Node{}
	}
	b.anchored[node] = n
}

// chosen returns the position options res chose, by element, for the
// elements whose options a cascade reads.
func (p *prepared) chosen(res *layout.Result) positionOptions {
	if len(p.b.anchored) == 0 {
		return nil
	}
	out := positionOptions{}
	for node, t := range res.PositionOptions {
		if n, ok := p.b.anchored[node]; ok {
			out[n] = t
		}
	}
	return out
}
