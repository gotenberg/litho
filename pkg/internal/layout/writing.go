package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// orthoEligible reports a box that may root an orthogonal flow: a block
// container laid out by the flow formatter, a block, an inline block, a
// float, an absolutely positioned box or a table cell, not a table, a
// flex or grid container, a multi-column container or a list item, whose
// markers sit outside.
func orthoEligible(b *box) bool {
	if b.kind != kindBlock || b.flex != nil || b.grid != nil || isMulticol(b) || b.st.Display == css.ListItem {
		return false
	}
	return b.tpart == partNone || b.tpart == partCell
}

// wrapOrtho makes b, a box of a vertical writing mode in a horizontal
// flow, the root of an orthogonal flow (CSS Writing Modes 4, 7.3): its
// content moves into an anonymous block of the horizontal mode, which the
// layout turns a quarter turn into b's content box.
func (e *engine) wrapOrtho(b *box) {
	st := anonStyle(b.st)
	st.WritingMode = css.HorizontalTB
	in := &box{
		st: st, kind: kindBlock, anon: true, first: true, last: true, parent: b,
		lang: b.lang, elem: b.elem, link: b.link, depth: b.depth + 1,
	}
	e.boxes++
	in.kids = b.kids
	for _, k := range in.kids {
		k.parent = in
	}
	e.normalize(in)
	b.kids = []*box{in}
	b.inlineKids = false
	b.ortho = true
}

// orthoFormatter lays out the content of the root of an orthogonal flow:
// the anonymous block holding it is laid out horizontally, as wide as the
// root's content box is tall, then turned a quarter turn into the root's
// content box, whose height is that width (CSS Writing Modes 4, 7.3). The
// text reads top to bottom, lines stacking right to left, but in
// sideways-lr, where it reads bottom to top, lines stacking left to
// right. The content is monolithic: it does not break across pages.
//
// The inline size is the root's height when definite, else its table
// row's, else the content's max-content width, at most the page area's
// height (CSS Writing Modes 4, 7.3.1 leaves the fallback to the user
// agent).
type orthoFormatter struct{}

func (orthoFormatter) layout(e *engine, b *box, cb cblock, _ *bfc, cursor float64, _ *marginState) float64 {
	in := b.kids[0]
	l := e.orthoInline(b, cb)
	hc := e.layoutOrtho(b, in, cb, cursor, l)
	w := cb.w
	if w <= 0 {
		w = hc
	}
	in.st.TransformOrigin = [2]css.Dimension{css.Pts(0), css.Pts(0)}
	if b.st.WritingMode == css.SidewaysLR {
		in.st.Transform = []css.TransformFunc{
			{Kind: css.TransformTranslate, X: css.Pts(0), Y: css.Pts(l)},
			{Kind: css.TransformRotate, V: [6]float64{-math.Pi / 2}},
		}
	} else {
		in.st.Transform = []css.TransformFunc{
			{Kind: css.TransformTranslate, X: css.Pts(w), Y: css.Pts(0)},
			{Kind: css.TransformRotate, V: [6]float64{math.Pi / 2}},
		}
	}
	return cursor + l
}

// intrinsic returns the width the content takes turned: its height laid
// out at the inline size.
func (orthoFormatter) intrinsic(e *engine, b *box) (float64, float64) {
	in := b.kids[0]
	l := e.orthoInline(b, cblock{})
	hc := e.layoutOrtho(b, in, cblock{}, 0, l)
	return hc, hc
}

// layoutOrtho lays out in, the anonymous block of the orthogonal flow
// root b, l wide at cursor in the root's content box cb, off the page
// cuts, and returns its height.
func (e *engine) layoutOrtho(b, in *box, cb cblock, cursor, l float64) float64 {
	in.st.Width = css.Pts(l)
	in.st.Transform = nil
	e.noPaginate++
	defer func() { e.noPaginate-- }()
	kcb := cblock{x: cb.x, w: l}
	if cb.w == 0 && cb.x == 0 {
		kcb.x = b.contentX()
	}
	e.layoutBlock(in, kcb, &bfc{}, cursor, &marginState{kept: true}, breakNone)
	return in.borderH()
}

// orthoInline returns the inline size of the orthogonal flow root b laid
// out in cb (see [orthoFormatter]).
func (e *engine) orthoInline(b *box, cb cblock) float64 {
	if cb.hDefinite {
		return max(cb.h, 0)
	}
	if h, ok := specHeight(b, b.st.Height, cblock{}); ok {
		return h
	}
	if b.tpart == partCell && b.parent != nil {
		if h, ok := specHeight(b.parent, b.parent.st.Height, cblock{}); ok {
			return max(h-vEdges(b), 0)
		}
	}
	_, hi := formatterFor(b.kids[0]).intrinsic(e, b.kids[0])
	return min(hi, e.areaH(0))
}
