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
	if e.opts.BrowserRules {
		// The content of an orthogonal flow root is laid out in the
		// root's own block formatting context (CSS Writing Modes 4, 7.3):
		// no margin of it collapses through the anonymous block, whose
		// top-left corner the content turns about.
		st.Display = css.FlowRoot
	}
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

// turnKey names the copy of a style turned into an orthogonal flow of a
// writing mode.
type turnKey struct {
	st    *css.Style
	frame css.WritingMode
}

// turnedStyle returns st as a box inside an orthogonal flow of the
// writing mode frame holds it, under [Options.BrowserRules]: the flow is
// laid out horizontally and turned onto the page, so the physical sides
// and axes of st are turned the other way. The physical properties keep
// their physical meaning (CSS Writing Modes 4, 6): margin-left stays on
// the left of the page whatever the writing mode. The flow-relative
// properties were mapped to physical ones by the cascade, and text-align,
// float and clear, whose left and right mean line-left and line-right
// (6.4), need no turn. The Office converters write the sides of turned
// text as the text's own, and keep st. A copy is made once a style and
// charged as a computed style.
func (e *engine) turnedStyle(st *css.Style, frame css.WritingMode) *css.Style {
	if !e.opts.BrowserRules || st == nil {
		return st
	}
	if t, ok := e.turned[turnKey{st, frame}]; ok {
		return t
	}
	if e.turned == nil {
		e.turned = map[turnKey]*css.Style{}
	}
	t := *st
	from := turnSides(frame)
	for i := css.Top; i <= css.Left; i++ {
		t.Margin[i] = st.Margin[from[i]]
		t.Padding[i] = st.Padding[from[i]]
		t.Border[i] = st.Border[from[i]]
		// A corner of the flow lies between two of its sides, the corner
		// of the page between the sides they come from; its radii swap
		// axes.
		r := st.BorderRadius[cornerFrom(from, css.Corner(i))]
		t.BorderRadius[i] = css.Radius{H: r.V, V: r.H}
	}
	phys := [4]css.Dimension{st.Top, st.Right, st.Bottom, st.Left}
	t.Top, t.Right, t.Bottom, t.Left = phys[from[css.Top]], phys[from[css.Right]], phys[from[css.Bottom]], phys[from[css.Left]]
	t.Width, t.Height = st.Height, st.Width
	t.MinWidth, t.MinHeight = st.MinHeight, st.MinWidth
	t.MaxWidth, t.MaxHeight = st.MaxHeight, st.MaxWidth
	t.MinWidthAuto, t.MinHeightAuto = st.MinHeightAuto, st.MinWidthAuto
	if len(st.BoxShadow) > 0 {
		t.BoxShadow = make([]css.Shadow, len(st.BoxShadow))
		for i, sh := range st.BoxShadow {
			sh.X, sh.Y = turnOffset(frame, sh.X, sh.Y)
			t.BoxShadow[i] = sh
		}
	}
	e.turned[turnKey{st, frame}] = &t
	e.turned[turnKey{&t, frame}] = &t
	return &t
}

// turnSides returns, for each side of an orthogonal flow of the writing
// mode frame, the side of the page it lies on: the flow turned a quarter
// turn clockwise puts its top on the right of the page, its left on the
// top; turned counter-clockwise, in sideways-lr, its top on the left and
// its left on the bottom.
func turnSides(frame css.WritingMode) [4]css.Side {
	if frame == css.SidewaysLR {
		return [4]css.Side{css.Left, css.Top, css.Right, css.Bottom}
	}
	return [4]css.Side{css.Right, css.Bottom, css.Left, css.Top}
}

// cornerFrom returns the corner of the page the corner c of a flow whose
// sides lie on the page sides from lies on.
func cornerFrom(from [4]css.Side, c css.Corner) css.Corner {
	// The corners in order follow the sides: corner c lies between side c
	// (top, right, bottom, left) and the side before it.
	a, b := from[c], from[(c+3)%4]
	for k := css.TopLeft; k <= css.BottomLeft; k++ {
		if s, before := css.Side(k), css.Side((k+3)%4); s == a && before == b || s == b && before == a {
			return k
		}
	}
	return c
}

// turnOffset returns the page offset (x, y) in the coordinates of an
// orthogonal flow of the writing mode frame.
func turnOffset(frame css.WritingMode, x, y float64) (float64, float64) {
	if frame == css.SidewaysLR {
		return -y, x
	}
	return y, -x
}
