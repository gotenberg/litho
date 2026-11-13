package html

import (
	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// firstLines gives the layout the styles of the first formatted lines
// (layout.FirstLineStyler) from the cascade: the ::first-line of an
// element, and the style of an inline element, a ::before or ::after, or
// a first letter on such a line, which inherits from the ::first-line
// (CSS Pseudo 4, 2.1.3). It knows the nodes the builder made inside an
// element with a ::first-line alone.
type firstLines struct {
	computed *css.Computed
	// roots maps the node of each block container with a ::first-line to
	// its element, nodes each inline node inside one to what styles it.
	roots map[*layout.Node]*dom.Node
	nodes map[*layout.Node]lineRef
	// depth counts the elements with a ::first-line the builder is inside.
	depth int
}

// lineRef is what styles a node on a first line: the element n, one of
// its pseudo-elements, or its ::first-letter.
type lineRef struct {
	n      *dom.Node
	which  css.PseudoElement
	pseudo bool
}

// Pseudo returns the ::first-line style of the block container node n,
// inheriting from parent.
func (f *firstLines) Pseudo(n *layout.Node, parent *css.Style) *css.Style {
	d, ok := f.roots[n]
	if !ok {
		return nil
	}
	return f.computed.FirstLine(d, parent)
}

// Restyle returns the style of n on a first line, where it inherits from
// parent.
func (f *firstLines) Restyle(n *layout.Node, parent *css.Style) *css.Style {
	r, ok := f.nodes[n]
	if !ok {
		return nil
	}
	if !r.pseudo {
		return f.computed.Restyle(r.n, parent)
	}
	if r.which == css.PseudoFirstLetter {
		ps := f.computed.FirstLetter(r.n, parent)
		if ps == nil {
			return nil
		}
		return letterStyle(ps)
	}
	return f.computed.RestylePseudo(r.n, r.which, parent)
}

// enterFirstLine records node, the box of the element n of style st, when
// n has a ::first-line, and reports whether it did: the caller calls
// [builder.leaveFirstLine] once n's content is built.
func (b *builder) enterFirstLine(node *layout.Node, n *dom.Node, st *css.Style) bool {
	if !takesFirstLetter(st.Display) || b.computed.Pseudo(n, css.PseudoFirstLine) == nil {
		return false
	}
	if b.lines == nil {
		b.lines = &firstLines{computed: b.computed, roots: map[*layout.Node]*dom.Node{}, nodes: map[*layout.Node]lineRef{}}
	}
	b.lines.roots[node] = n
	b.lines.depth++
	return true
}

// leaveFirstLine ends what [builder.enterFirstLine] began.
func (b *builder) leaveFirstLine() { b.lines.depth-- }

// onFirstLine records node, the box of the element n or of its
// pseudo-element which when pseudo, as one that may sit on a first line.
func (b *builder) onFirstLine(node *layout.Node, n *dom.Node, which css.PseudoElement, pseudo bool) {
	if b.lines == nil || b.lines.depth == 0 || node == nil {
		return
	}
	b.lines.nodes[node] = lineRef{n: n, which: which, pseudo: pseudo}
}
