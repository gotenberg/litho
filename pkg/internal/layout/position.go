package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file lays out the absolutely positioned boxes (CSS 2.1, 9.6, 10.3.7,
// 10.3.8, 10.6.4 and 10.6.5). The flow records the static position of each
// one where it would have sat; once the flow is laid out, every box's
// containing block has its final geometry, and the positioned boxes are
// laid out in tree order, each in the coordinates of the tall column, so
// an absolutely positioned box fragments across the page cuts like the
// flow does (CSS Fragmentation 3, 5). A fixed box is laid out once on the
// first page, without page cuts, and painted on every page (CSS 2.1,
// 9.6.1).

// absCB is the containing block of an absolutely positioned box: its
// padding edges in flow coordinates.
type absCB struct {
	x, y, w, h float64
	// box is the box that establishes it, nil for the initial containing
	// block or the page.
	box *box
	// page reports the page area of a fixed box.
	page bool
	// anchored reports the page box of a page-anchored box, the page
	// anchorPage.
	anchored   bool
	anchorPage int
}

// setStatic records the static position of the absolutely positioned box
// a: the start edge x, at the right when rtl, and the top y of its
// hypothetical box.
func setStatic(a *box, x, y float64, rtl bool) {
	a.sx, a.sy, a.srtl, a.staticSet = x, y, rtl, true
}

// staticBlock records the static position of the absolutely positioned
// box a met in the block flow of the containing block cb at y: where its
// margin box would have started had it been in flow (CSS 2.1, 10.3.7 and
// 10.6.4).
func staticBlock(a *box, cb cblock, y float64) {
	rtl := cb.rtl(a)
	x := cb.x
	if rtl {
		x = cb.x + cb.w
	}
	setStatic(a, x, y, rtl)
}

// staticLine records the static position of the absolutely positioned
// boxes anchored in the characters [start, end) of p, which make the line
// l, or at its end when it ends the paragraph. A box that was inline-level
// sits where its inline box would have; a block-level one starts below the
// line when content precedes it on the line, at its top otherwise (CSS
// Position 3, 4.1).
func (s *inlineState) staticLine(p *ifcPara, start, end int, l *lineBox) {
	for _, a := range p.abs {
		if s.placed[a.box] || (a.pos >= end && end < p.n) {
			continue
		}
		s.placed[a.box] = true
		rtl := s.root.st.Direction == css.RTL
		if a.box.st.OuterInline {
			setStatic(a.box, s.xAt(p, l, a.pos), l.y, false)
			continue
		}
		x := s.cb.x
		if rtl {
			x = s.cb.x + s.cb.w
		}
		y := l.y
		if a.pos > start {
			y = l.y + l.h
		}
		setStatic(a.box, x, y, rtl)
	}
}

// staticRest records the static position of the absolutely positioned
// boxes of p no line placed, at y: those of an empty paragraph.
func (s *inlineState) staticRest(p *ifcPara, y float64, mc *marginState, pending bool) {
	if pending {
		y += mc.strut.sum()
	}
	for _, a := range p.abs {
		if s.placed[a.box] {
			continue
		}
		s.placed[a.box] = true
		staticBlock(a.box, s.cb, y)
	}
}

// xAt returns the flow x of the character position pos of p on the line l.
func (s *inlineState) xAt(p *ifcPara, l *lineBox, pos int) float64 {
	if len(l.items) == 0 {
		return l.x
	}
	best := l.items[0].x
	for _, it := range l.items {
		if pos < it.start {
			continue
		}
		if pos >= it.end {
			best = max(best, it.x+it.w)
			continue
		}
		x := it.x
		if it.text != nil && p.p != nil {
			for j := it.start; j < pos && j < p.p.Len(); j++ {
				x += p.p.Advance(j)
			}
		}
		return l.x + x
	}
	return l.x + best
}

// layoutPositioned lays out the absolutely positioned boxes of the tree
// rooted at b, in tree order, so a box's containing block, an ancestor, is
// laid out before it.
func (e *engine) layoutPositioned(b *box) {
	for _, k := range b.kids {
		if e.err != nil {
			return
		}
		if k.abs {
			if !k.staticSet {
				k.laidOut = false
				continue
			}
			e.layoutAbs(k)
		}
		if k.mc != nil {
			e.layoutPositionedInColumns(k)
			continue
		}
		e.layoutPositioned(k)
	}
}

// containerOf returns the containing block of the absolutely positioned box
// b (CSS 2.1, 10.1): the padding box of its nearest positioned ancestor, or
// one with a transform (CSS Transforms 1, 6), the initial containing block
// when it has none; for a fixed box the page area, unless a transformed
// ancestor holds it.
func (e *engine) containerOf(b *box) absCB {
	if b.st.PageAnchor && b.st.Position == css.Absolute {
		return e.pageCB(b)
	}
	fixed := b.st.Position == css.Fixed
	for a := b.parent; a != nil; a = a.parent {
		if a.tpart.internal() && a.tpart != partCell {
			continue
		}
		holds := len(a.st.Transform) > 0 && a.kind == kindBlock
		if !fixed && a.st.Position != css.Static {
			holds = true
		}
		if !holds {
			continue
		}
		if a.kind == kindInline {
			return inlineCB(a)
		}
		pad := absCB{
			x: a.x + a.bd[css.Left], y: a.y + a.bd[css.Top],
			w: a.w + a.pd[css.Left] + a.pd[css.Right], h: a.h + a.pd[css.Top] + a.pd[css.Bottom],
			box: a,
		}
		if a.grid != nil {
			// A box whose containing block a grid container generates
			// takes the grid area its placement names (CSS Grid 1, 9).
			return gridAbsCB(a, b, pad)
		}
		return pad
	}
	return absCB{w: e.pageW, h: e.pageH, page: fixed}
}

// inlineCB returns the containing block an inline box a establishes (CSS
// 2.1, 10.1, 4): from the top-left of the padding box of its first line
// fragment to the bottom-right of its last one's.
func inlineCB(a *box) absCB {
	var host *box
	for h := a.parent; h != nil; h = h.parent {
		if h.kind != kindInline {
			host = h
			break
		}
	}
	cb := absCB{x: a.anchorX, y: a.anchorY, box: a}
	if host == nil {
		return cb
	}
	var first, last *inlineFrag
	var fl, ll *lineBox
	for _, l := range host.lines {
		for _, f := range l.frags {
			if f.box != a {
				continue
			}
			if first == nil {
				first, fl = f, l
			}
			last, ll = f, l
		}
	}
	if first == nil {
		return cb
	}
	x0 := fl.x + first.x0 + a.bd[css.Left]
	y0 := fl.y + first.top + a.bd[css.Top]
	x1 := ll.x + last.x1 - a.bd[css.Right]
	y1 := ll.y + last.bottom - a.bd[css.Bottom]
	cb.x, cb.y = x0, y0
	cb.w, cb.h = max(x1-x0, 0), max(y1-y0, 0)
	return cb
}

// insetOf returns the used value of the offset property d against ref,
// false for auto.
func insetOf(d css.Dimension, ref float64) (float64, bool) {
	if !d.IsLength() {
		return 0, false
	}
	return resolve(d, ref), true
}

// layoutAbs lays out the absolutely positioned box b in its containing
// block.
func (e *engine) layoutAbs(b *box) {
	b.laidOut = true
	cb := e.containerOf(b)
	b.cbBox = cb.box
	b.fixed = cb.page
	if cb.anchored {
		b.pageAnchored, b.anchorPage = true, cb.anchorPage
		// The box lies on its page whole, its margins included: no page
		// cut runs through it.
		e.noPaginate++
		defer func() { e.noPaginate-- }()
	}
	// The static position is where the box would have been painted: it
	// moves with the relatively positioned boxes between it and its
	// containing block, which move none of the containing block itself.
	for a := b.parent; a != nil && a != cb.box; {
		ox, oy := offset(a)
		b.sx += ox
		b.sy += oy
		if a.abs {
			a = a.cbBox
		} else {
			a = a.parent
		}
	}
	if !cb.anchored && !cb.page {
		// A box inside a box that lies on one page whole lies there too.
		for a := b.parent; a != nil; a = a.parent {
			if a.pageAnchored {
				b.pageAnchored, b.anchorPage = true, a.anchorPage
				e.noPaginate++
				defer func() { e.noPaginate-- }()
				break
			}
		}
	}
	if b.st.PageClamp && !b.pageAnchored && !cb.anchored && !cb.page && b.st.Position == css.Absolute {
		// The box lies on its anchor's page whole: no page cut runs
		// through it.
		e.noPaginate++
		defer func() { e.noPaginate-- }()
		defer e.clampToPage(b, e.pageOf(b.sy))
	}
	if b.fixed {
		// A fixed box sits on the page its static position falls on, then
		// on every page (CSS 2.1, 9.6.1).
		b.sy -= e.pageTop(e.pageOf(b.sy))
		e.noPaginate++
		defer func() { e.noPaginate-- }()
	}
	resolveEdges(b, cb.w)
	ccb := cblock{x: cb.x, w: cb.w, h: cb.h, hDefinite: true}
	if hasSizing(b.sizeSrc()) {
		aw, ah := absRoom(b, cb)
		e.resolveSizing(b, ccb, aw, ah)
	}
	var rw, rh float64
	if b.kind == kindReplaced {
		rw, rh = e.replacedSize(b, ccb)
	}
	left := e.absWidths(b, cb, rw)
	b.x = cb.x + left + b.m[css.Left]
	_, lSet := insetOf(b.st.Left, cb.w)
	_, rSet := insetOf(b.st.Right, cb.w)
	if !lSet && !rSet {
		b.x += flexStaticShift(b, true)
	}
	if b.kind == kindReplaced {
		b.h = rh
	}
	e.absHeights(b, cb, ccb)
	_, tSet := insetOf(b.st.Top, cb.h)
	_, bSet := insetOf(b.st.Bottom, cb.h)
	if !tSet && !bSet {
		b.shift(0, flexStaticShift(b, false))
	}
}

// absWidths sets the content width and horizontal margins of the
// absolutely positioned box b in cb and returns the left offset of its
// margin box from cb's left edge (CSS 2.1, 10.3.7; 10.3.8 for a replaced
// box of used width rw).
func (e *engine) absWidths(b *box, cb absCB, rw float64) float64 {
	st := b.st
	w, wSet := specWidth(b, st.Width, cb.w)
	switch {
	case b.kind == kindReplaced:
		w, wSet = rw, true
	case b.tpart == partWrapper:
		w, wSet = e.wrapperWidth(b, cb.w, cb.w), true
	}
	left, width := e.solveH(b, cb, w, !wSet)
	if b.kind != kindReplaced {
		if c := constrainWidth(b, width, cb.w); math.Abs(c-width) > eps {
			resolveEdges(b, cb.w)
			left, width = e.solveH(b, cb, c, false)
		}
	}
	b.w = width
	return left
}

// solveH solves the horizontal constraint of an absolutely positioned box
// for a content width w, auto when wAuto, setting its auto margins, and
// returns its margin box's left offset and its width.
func (e *engine) solveH(b *box, cb absCB, w float64, wAuto bool) (float64, float64) {
	st := b.st
	edges := hEdges(b)
	mlAuto, mrAuto := st.Margin[css.Left].Kind == css.Auto, st.Margin[css.Right].Kind == css.Auto
	l, lSet := insetOf(st.Left, cb.w)
	r, rSet := insetOf(st.Right, cb.w)
	if !lSet && !rSet {
		// The start side takes the static position.
		if b.srtl {
			r, rSet = cb.x+cb.w-b.sx, true
		} else {
			l, lSet = b.sx-cb.x, true
		}
	}
	ml, mr := b.m[css.Left], b.m[css.Right]
	rtl := cb.box != nil && cb.box.st.Direction == css.RTL || cb.box == nil && b.srtl
	switch {
	case wAuto && !lSet:
		w = e.shrinkToFit(b, cb.w-r)
		l = cb.w - r - mr - w - edges - ml
	case wAuto && !rSet:
		w = e.shrinkToFit(b, cb.w-l)
	case wAuto:
		w = max(cb.w-l-r-ml-mr-edges, 0)
	case !lSet:
		l = cb.w - r - mr - w - edges - ml
	case !rSet:
	default:
		rest := cb.w - l - r - w - edges
		switch {
		case mlAuto && mrAuto:
			ml = rest / 2
			if ml < 0 {
				ml = 0
				if rtl {
					ml = rest
				}
			}
			mr = rest - ml
		case mlAuto:
			ml = rest - mr
		case mrAuto:
			mr = rest - ml
		case rtl:
			// Over-constrained: left is ignored in a right-to-left
			// containing block, right otherwise.
			l = cb.w - r - mr - w - edges - ml
		}
	}
	b.m[css.Left], b.m[css.Right] = ml, mr
	return clamp(l), max(clamp(w), 0)
}

// absHeights sets the content height and vertical margins of the
// absolutely positioned box b in cb, its width and x set, and lays its
// content out where it lands (CSS 2.1, 10.6.4; 10.6.5 for a replaced
// box).
func (e *engine) absHeights(b *box, cb absCB, ccb cblock) {
	st := b.st
	edges := vEdges(b)
	t, tSet := insetOf(st.Top, cb.h)
	bo, bSet := insetOf(st.Bottom, cb.h)
	if !tSet && !bSet {
		t, tSet = b.sy-cb.y, true
	}
	mtAuto, mbAuto := st.Margin[css.Top].Kind == css.Auto, st.Margin[css.Bottom].Kind == css.Auto
	h, hSet := specHeight(b, st.Height, ccb)
	if b.kind == kindReplaced {
		h, hSet = b.h, true
	}
	if k := b.sizeSrc().Height.Kind; !hSet && tSet && bSet && (k == css.MinContent || k == css.MaxContent || k == css.FitContent) {
		// An intrinsic height keyword does not stretch between the insets:
		// a block's min-content and max-content heights are its content's
		// height, which fit-content keeps over the room the insets leave
		// (CSS Sizing 3, 3.2 and 5.1), the auto margins sharing what is
		// left, negative or not.
		e.noPaginate++
		b.y = cb.y
		e.absContent(b, ccb, 0, false)
		e.noPaginate--
		h, hSet = constrainHeight(b, b.h, ccb), true
	}
	if !hSet && tSet && bSet {
		h, hSet = max(cb.h-t-bo-b.m[css.Top]-b.m[css.Bottom]-edges, 0), true
		h = constrainHeight(b, h, ccb)
	} else if hSet && b.kind != kindReplaced {
		h = constrainHeight(b, h, ccb)
	}
	if hSet && tSet && bSet {
		rest := cb.h - t - bo - h - edges
		mt, mb := b.m[css.Top], b.m[css.Bottom]
		switch {
		case mtAuto && mbAuto:
			mt = rest / 2
			mb = rest - mt
		case mtAuto:
			mt = rest - mb
		case mbAuto:
			mb = rest - mt
		}
		b.m[css.Top], b.m[css.Bottom] = mt, mb
	}
	place := func(top float64) {
		b.y = cb.y + top + b.m[css.Top]
		if b.kind == kindReplaced {
			b.h = h
			if e.paginating() {
				b.y = e.fit(b.y, b.borderH())
			}
			return
		}
		e.absContent(b, ccb, h, hSet)
	}
	if tSet {
		place(t)
		return
	}
	// Bottom alone: the height comes from the content, measured off the
	// page cuts, then the box is laid out where it lands.
	if !hSet {
		e.noPaginate++
		b.y = cb.y
		e.absContent(b, ccb, 0, false)
		e.noPaginate--
		h, hSet = b.h, true
	}
	place(cb.h - bo - b.m[css.Bottom] - edges - h - b.m[css.Top])
}

// absContent lays out the content of the absolutely positioned box b, its
// position and width set, at the height h when hSet, else at the height of
// its content; b establishes a block formatting context (CSS 2.1, 9.4.1).
func (e *engine) absContent(b *box, ccb cblock, h float64, hSet bool) {
	own := &bfc{}
	mc := &marginState{}
	kcb := wrapperCB(b, cblock{x: b.contentX(), w: b.w, h: h, hDefinite: hSet}, ccb)
	saved := e.enterClamp(b, ccb, b.contentY())
	end := formatterFor(b).layout(e, b, kcb, own, b.contentY(), mc)
	e.leaveClamp(saved)
	end += mc.strut.sum()
	mc.commit(end)
	end = max(end, own.bottom(), b.contentY())
	ch := end - b.contentY()
	content := ch
	if hSet {
		ch = h
	}
	b.h = contentHeightBounds(b, constrainHeight(b, ch, ccb), content)
	e.placeMarker(b)
	e.reach(b.y + b.borderH())
}

// transformOf returns the matrix the transform of b maps flow coordinates
// with, about its origin in the border box r (CSS Transforms 1, 6 and 13),
// and false for a box without a transform or with a singular one, which
// paints nothing.
func transformOf(b *box, r compose.Rect) (compose.Matrix, bool, bool) {
	if !b.transformed() {
		return compose.Identity, false, true
	}
	ox := r.X + resolve(b.st.TransformOrigin[0], r.Width)
	oy := r.Y + resolve(b.st.TransformOrigin[1], r.Height)
	m := compose.Matrix{A: 1, D: 1, E: ox, F: oy}
	for _, f := range b.st.Transform {
		m = mul(m, funcMatrix(f, r))
	}
	m = mul(m, compose.Matrix{A: 1, D: 1, E: -ox, F: -oy})
	for _, v := range []float64{m.A, m.B, m.C, m.D, m.E, m.F} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > maxLength {
			return compose.Identity, true, false
		}
	}
	if math.Abs(m.A*m.D-m.B*m.C) < 1e-12 {
		return compose.Identity, true, false
	}
	return m, true, true
}

// funcMatrix returns the matrix of one transform function on a border box
// r (CSS Transforms 1, 13.1).
func funcMatrix(f css.TransformFunc, r compose.Rect) compose.Matrix {
	v := f.V
	for i := range v {
		v[i] = clamp(v[i])
	}
	switch f.Kind {
	case css.TransformMatrix:
		return compose.Matrix{A: v[0], B: v[1], C: v[2], D: v[3], E: v[4], F: v[5]}
	case css.TransformTranslate:
		return compose.Matrix{A: 1, D: 1, E: resolve(f.X, r.Width), F: resolve(f.Y, r.Height)}
	case css.TransformScale:
		return compose.Matrix{A: v[0], D: v[1]}
	case css.TransformRotate:
		sin, cos := math.Sincos(v[0])
		return compose.Matrix{A: cos, B: sin, C: -sin, D: cos}
	case css.TransformSkew:
		return compose.Matrix{A: 1, B: math.Tan(v[1]), C: math.Tan(v[0]), D: 1}
	}
	return compose.Identity
}

// mul returns the matrix that applies b, then a.
func mul(a, b compose.Matrix) compose.Matrix {
	return compose.Matrix{
		A: a.A*b.A + a.C*b.B,
		B: a.B*b.A + a.D*b.B,
		C: a.A*b.C + a.C*b.D,
		D: a.B*b.C + a.D*b.D,
		E: a.A*b.E + a.C*b.F + a.E,
		F: a.B*b.E + a.D*b.F + a.F,
	}
}

// absRoom returns the width and height the stretch keyword fills for the
// absolutely positioned box b in cb: the inset-modified containing block,
// an auto inset standing at the static position (CSS Sizing 4, 3.1; CSS
// Position 3, 4.1), less the box's margins, borders and padding.
func absRoom(b *box, cb absCB) (float64, float64) {
	st := b.sizeSrc()
	l, lSet := insetOf(st.Left, cb.w)
	r, rSet := insetOf(st.Right, cb.w)
	if !lSet && !b.srtl {
		l = b.sx - cb.x
	}
	if !rSet && b.srtl {
		r = cb.x + cb.w - b.sx
	}
	t, tSet := insetOf(st.Top, cb.h)
	bo, _ := insetOf(st.Bottom, cb.h)
	if !tSet {
		t = b.sy - cb.y
	}
	w := cb.w - l - r - hEdges(b) - nonAuto(b.m[css.Left], st.Margin[css.Left].Kind == css.Auto) - nonAuto(b.m[css.Right], st.Margin[css.Right].Kind == css.Auto)
	h := cb.h - t - bo - vEdges(b) - nonAuto(b.m[css.Top], st.Margin[css.Top].Kind == css.Auto) - nonAuto(b.m[css.Bottom], st.Margin[css.Bottom].Kind == css.Auto)
	return max(w, 0), max(h, 0)
}

// pageCB returns the containing block of the page-anchored box b: the
// page box, margins included, of the page its css.Style.AnchorPage names,
// else of the page its static position falls on, in flow coordinates,
// where the page area's top-left corner is the flow's left edge at the
// page's top. A page past the page cap is the last one the cap allows,
// which the page count then refuses.
func (e *engine) pageCB(b *box) absCB {
	p := e.pageOf(b.sy)
	if n := b.st.AnchorPage; n > 0 {
		p = min(n, e.maxPages+1) - 1
	}
	pb := e.box(p)
	return absCB{
		x: -pb.Margin[css.Left], y: e.pageTop(p) - pb.Margin[css.Top],
		w: pb.Width, h: pb.Height,
		anchored: true, anchorPage: p,
	}
}

// clampToPage moves the absolutely positioned box b, laid out, whole onto
// the page box of page p (css.Style.PageClamp): down to its top edge when
// b starts above it, else up until b's bottom edge meets its bottom edge,
// the top edge winning; b then paints on page p alone.
func (e *engine) clampToPage(b *box, p int) {
	if e.err != nil {
		return
	}
	pb := e.box(p)
	top := e.pageTop(p) - pb.Margin[css.Top]
	ny := max(min(b.y, top+pb.Height-b.borderH()), top)
	b.shift(0, ny-b.y)
	b.pageAnchored, b.anchorPage = true, p
}
