package layout

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// shareCheckEvery is how many nodes [ShareStyles] visits between two
// checks of its context.
const shareCheckEvery = 1024

// ShareStyles makes the nodes of the tree under root that hold equal
// styles hold one: it replaces each Node.Style, a marker's and a
// footnote body's included, by the style of the table tb equal to it
// (see [css.Styles]), adopting the styles tb holds no equal of. A nil tb
// shares through a table of its own. Equal styles lay out alike, so the
// layout of the tree does not change, and a converter that builds one
// style a node, most of them alike, keeps the few distinct ones.
//
// The caller calls it on a finished tree and writes no node's style
// afterwards, since many nodes may share one style. It writes nothing
// else, and stops early, leaving the rest of the tree as it was, when ctx
// is done.
func ShareStyles(ctx context.Context, root *Node, tb *css.Styles) {
	if root == nil {
		return
	}
	if tb == nil {
		tb = css.NewStyles()
	}
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
// its boxes hold whose cost it accounted: charged to the conversion's
// budget, or charged by another stage.
type sharedStyles struct {
	table   *css.Styles
	anon    map[*css.Style]*css.Style
	markers map[markerKey]*css.Style
	symbols map[spaceKey]*css.Style
	charged map[*css.Style]struct{}
}

// holdStyle charges the conversion's budget for st, a style a box of the
// engine holds, once whatever the number of boxes holding it, unless
// [Options.ChargedStyle] reports that another stage charged it.
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
	if e.opts.ChargedStyle != nil && e.opts.ChargedStyle(st) {
		return
	}
	e.charge(limits.StyleCost, "computed styles")
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

// symbolMarkerOf returns the style of the inside symbol marker of a list
// item whose marker style is mst, in language lang: an inline replaced box
// of the bullet's size that ends where the item's content starts, made once
// a marker style and language and shared. Its width is the bullet's,
// Blink's (o+1)/2 pixels for a font ascent A rounded to whole pixels and o
// = A*2/3, followed by a margin of 1em and a pixel; its bottom margin puts
// the bullet's top 3*(A-o)/2 pixels below the top of the ascent, as
// [symbolRect] places an outside one. CSS Lists 3, 3.1 leaves the look of
// a marker to the user agent; this follows Chromium.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws an inside
//     disc, circle or square of a list in Liberation Serif at 10, 16, 20,
//     32, 50 and 100px as a 3, 5, 6, 10, 15 and 30 pixel bullet starting
//     at the item's content edge, and starts the item's text 14, 22, 27,
//     43, 66 and 131 pixels after that edge: the bullet, 1em and a pixel.
func (e *engine) symbolMarkerOf(mst *css.Style, lang string) *css.Style {
	key := spaceKey{st: mst, lang: e.familyLang(lang)}
	if st, ok := e.shared.symbols[key]; ok {
		return st
	}
	st := *mst
	gap := css.Right
	if mst.Direction == css.RTL {
		gap = css.Left
	}
	if t := mst.ListStyleType; t == css.DisclosureOpen || t == css.DisclosureClosed {
		// A disclosure triangle's square sits on the baseline, 0.4em
		// before the content ([engine.disclosureSize]).
		w := e.disclosureSize(mst)
		st.Width, st.Height = css.Pts(w), css.Pts(w)
		st.Margin[gap] = css.Pts(float64(math.Floor(float64(e.fontSize(mst)/css.Pt*0.4)*64) / 64 * css.Pt))
		st.Margin[css.Bottom] = css.Pts(0)
	} else {
		a := int(math.Round(min(max(e.faceMetrics(mst, lang).Ascent/css.Pt, 0), maxSymbolAscent))) // fits int: bounded
		o := a * 2 / 3
		w := (o + 1) / 2
		st.Width, st.Height = css.Pts(float64(w)*css.Pt), css.Pts(float64(w)*css.Pt)
		st.Margin[gap] = css.Pts(e.fontSize(mst) + css.Pt)
		st.Margin[css.Bottom] = css.Pts(float64(a-3*(a-o)/2-w) * css.Pt)
	}
	st.VerticalAlign = css.VerticalAlign{Kind: css.Baseline}
	shared := e.shareStyle(&st)
	if e.shared.symbols == nil {
		e.shared.symbols = map[spaceKey]*css.Style{}
	}
	e.shared.symbols[key] = shared
	return shared
}

// markerKey keys the marker styles of [engine.markerOf].
type markerKey struct {
	st       *css.Style
	cascaded bool
}

// markerOf returns the style of the marker drawn in style item
// ([markerStyle]), made once an item style and shared.
func (e *engine) markerOf(item *css.Style, cascaded bool) *css.Style {
	key := markerKey{item, cascaded}
	if st, ok := e.shared.markers[key]; ok {
		return st
	}
	st := e.shareStyle(markerStyle(item, cascaded))
	if e.shared.markers == nil {
		e.shared.markers = map[markerKey]*css.Style{}
	}
	e.shared.markers[key] = st
	return st
}
