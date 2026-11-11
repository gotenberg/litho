package layout

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// shareCheckEvery is how many nodes [ShareStyles] visits between two
// checks of its context.
const shareCheckEvery = 1024

// ShareStyles makes the nodes of the tree under root that hold equal
// styles hold one: it replaces each Node.Style, a marker's and a
// footnote body's included, by the first style of the tree equal to it
// (see [css.Styles]). Equal styles lay out alike, so the layout of the
// tree does not change, and a converter that builds one style a node, most
// of them alike, keeps the few distinct ones.
//
// The caller calls it on a finished tree and writes no node's style
// afterwards: a style can now be shared by many nodes. It writes nothing
// else, and stops early, leaving the rest of the tree as it was, when ctx
// is done.
func ShareStyles(ctx context.Context, root *Node) {
	if root == nil {
		return
	}
	tb := css.NewStyles()
	done := map[*css.Style]*css.Style{}
	stack := []*Node{root}
	for visited := 1; len(stack) > 0; visited++ {
		if visited%shareCheckEvery == 0 && ctx.Err() != nil {
			return
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if st := n.Style; st != nil {
			s, ok := done[st]
			if !ok {
				s = tb.Share(st)
				done[st] = s
			}
			n.Style = s
		}
		stack = append(stack, n.Children...)
		if n.Marker != nil {
			stack = append(stack, n.Marker)
		}
		if n.Footnote != nil {
			stack = append(stack, n.Footnote)
		}
	}
}

// sharedStyles is what an engine knows of the styles its boxes hold: the
// table sharing the styles it makes for boxes of its own, the anonymous
// block and marker styles it made from each parent style, and the styles
// it charged the conversion's budget for.
type sharedStyles struct {
	table   *css.Styles
	anon    map[*css.Style]*css.Style
	markers map[*css.Style]*css.Style
	charged map[*css.Style]struct{}
}

// holdStyle charges the conversion's budget for st, a style a box of the
// engine holds, once whatever the number of boxes holding it.
func (e *engine) holdStyle(st *css.Style) {
	if st == nil {
		return
	}
	if _, ok := e.shared.charged[st]; ok {
		return
	}
	if e.shared.charged == nil {
		e.shared.charged = map[*css.Style]struct{}{}
	}
	e.shared.charged[st] = struct{}{}
	e.charge(styleCost, "computed styles")
}

// shareStyle returns the style the engine holds equal to st, a style it
// made for a box of its own and writes no more, charged once.
func (e *engine) shareStyle(st *css.Style) *css.Style {
	if e.shared.table == nil {
		e.shared.table = css.NewStyles()
	}
	st = e.shared.table.Share(st)
	e.holdStyle(st)
	return st
}

// anonOf returns the style of an anonymous block inside a box of style
// parent ([anonStyle]), made once a parent style and shared. The engine
// writes it no more: a box whose style it writes takes [anonStyle].
func (e *engine) anonOf(parent *css.Style) *css.Style {
	if st, ok := e.shared.anon[parent]; ok {
		return st
	}
	st := e.shareStyle(anonStyle(parent))
	if e.shared.anon == nil {
		e.shared.anon = map[*css.Style]*css.Style{}
	}
	e.shared.anon[parent] = st
	return st
}

// markerOf returns the style of the marker of a list item of style item
// ([markerStyle]), made once an item style and shared.
func (e *engine) markerOf(item *css.Style) *css.Style {
	if st, ok := e.shared.markers[item]; ok {
		return st
	}
	st := e.shareStyle(markerStyle(item))
	if e.shared.markers == nil {
		e.shared.markers = map[*css.Style]*css.Style{}
	}
	e.shared.markers[item] = st
	return st
}
