package layout

import (
	"fmt"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// cblock is a containing block: the content box edges a box resolves its
// widths, percentages and floats against (CSS 2.1, 10.1).
type cblock struct {
	x, w float64
	// h is the height percentages resolve against when definite.
	h         float64
	hDefinite bool
}

// strut is a set of adjoining margins being collapsed (CSS 2.1, 8.3.1):
// the largest positive one and the most negative one.
type strut struct {
	pos, neg float64
}

// add returns the strut with margin m adjoined.
func (s strut) add(m float64) strut {
	if m > 0 {
		s.pos = max(s.pos, m)
	} else {
		s.neg = min(s.neg, m)
	}
	return s
}

// sum returns the collapsed margin.
func (s strut) sum() float64 { return s.pos + s.neg }

// pastMargin returns where content lands under the margins s collapsed at
// cursor. Margins adjoining an unforced break truncate (CSS Fragmentation
// 3, 5.2): a positive margin that would carry the content across a page
// cut breaks there and leaves nothing on the next page, and one that
// starts at the top of a page the content before it filled is dropped.
// kept reports margins that start at a page top without an unforced break
// before them, after a forced break or at a box's content edge: they stay.
func (e *engine) pastMargin(cursor float64, s strut, kept bool) float64 {
	y := cursor + s.sum()
	if !e.paginating() || s.sum() <= 0 {
		return y
	}
	if !kept && cursor > e.flowStart()+eps && e.atPageTop(cursor) {
		return cursor
	}
	if e.pageOf(y) > e.pageOf(cursor) {
		y = e.nextPage(cursor)
	}
	return y
}

// marginState carries the margins collapsing through the block flow and
// the boxes whose top waits on them: a box whose top margin collapses with
// its first child's has its border top where that child's content lands.
type marginState struct {
	strut   strut
	waiting []*box
	// absorb drops the top margins adjoining the top of a box placed by
	// clearance: they collapsed into the margin the clearance replaced.
	absorb bool
	// kept reports margins pending after a forced break or at a box's
	// content edge: they do not truncate at a page top (CSS Fragmentation
	// 3, 5.2).
	kept bool
	// floats are the floats placed while the margins were pending.
	floats []pendingFloat
}

// pendingFloat is a float placed at y while margins were pending: idx is
// its rectangle in bc, -1 for none, and owner the innermost box waiting on
// the margins it sits in.
type pendingFloat struct {
	box   *box
	bc    *bfc
	idx   int
	y     float64
	owner *box
}

// collapsedThrough hands the floats of b, an empty box whose margins
// collapse through and which no longer waits, to the box still waiting
// around it, or lets them keep their place when none does.
func (mc *marginState) collapsedThrough(b *box) {
	kept := mc.floats[:0]
	for _, f := range mc.floats {
		if f.owner == b {
			if len(mc.waiting) == 0 {
				continue
			}
			f.owner = mc.waiting[len(mc.waiting)-1]
		}
		kept = append(kept, f)
	}
	mc.floats = kept
}

// clone returns a copy of mc that shares no slice with it.
func (mc *marginState) clone() marginState {
	c := *mc
	c.waiting = slices.Clone(mc.waiting)
	c.floats = slices.Clone(mc.floats)
	return c
}

// floatPlaced records the float f placed at y in bc, its rectangle at
// index idx of bc's floats, when a box's top waits on the pending margins:
// the float then belongs inside that box. A float placed between resolved
// boxes keeps its place.
func (mc *marginState) floatPlaced(f *box, bc *bfc, idx int, y float64) {
	if len(mc.waiting) == 0 {
		return
	}
	mc.floats = append(mc.floats, pendingFloat{box: f, bc: bc, idx: idx, y: y, owner: mc.waiting[len(mc.waiting)-1]})
}

// floatTop returns where the first float placed under the pending margins
// sits, false when none is.
func (mc *marginState) floatTop() (float64, bool) {
	if len(mc.floats) == 0 {
		return 0, false
	}
	return mc.floats[0].y, true
}

// commit resolves the pending margins: content lands at y, and so does the
// top of every box waiting on it.
//
// A float placed while a box's top waited on the margins sits at that
// box's top, wherever the margins resolve (CSS 2.1, 8.3.1 and 9.5.1, as
// browsers place floats once the position of their formatting context's
// content is known): it moves down with them.
func (mc *marginState) commit(y float64) {
	mc.commitFloats(y)
	for _, b := range mc.waiting {
		b.y = y
	}
	mc.waiting = nil
	mc.strut = strut{}
	mc.absorb = false
	mc.kept = false
}

// commitFloats moves the floats placed under the pending margins to y,
// where the margins resolve, and forgets them; the margins stay pending.
func (mc *marginState) commitFloats(y float64) {
	if len(mc.waiting) > 0 {
		for _, f := range mc.floats {
			dy := y - f.y
			if math.Abs(dy) <= eps {
				continue
			}
			f.box.shift(0, dy)
			if f.idx >= 0 && f.idx < len(f.bc.floats) {
				f.bc.floats[f.idx].y0 += dy
				f.bc.floats[f.idx].y1 += dy
			}
		}
	}
	mc.floats = nil
}

// leadingMargins returns the top margins of the first in-flow descendants
// of b that adjoin its top margin (CSS 2.1, 8.3.1): they collapse with it,
// so they take part in the hypothetical position clearance is computed
// from (9.5.2). The walk stops at a top border or padding, at a box that
// establishes a block formatting context, and at inline content.
func leadingMargins(b *box, cbw float64) strut {
	var s strut
	for depth := 0; depth < maxTreeDepth; depth++ {
		pd, bd, _ := edgesOf(b, cbw)
		if pd[css.Top] != 0 || bd[css.Top] != 0 || b.inlineKids || (depth > 0 && b.establishesBFC()) {
			break
		}
		var first *box
		for _, k := range b.kids {
			if k.float == css.FloatNone && !k.inline {
				first = k
				break
			}
		}
		if first == nil {
			break
		}
		_, _, m := edgesOf(first, cbw)
		s = s.add(m[css.Top])
		b = first
	}
	return s
}

// breakMode is how a box starts with respect to the page cuts.
type breakMode uint8

const (
	// breakNone places the box where it falls.
	breakNone breakMode = iota
	// breakForced starts the box on the next page, keeping its top margin
	// (CSS Fragmentation 3, 5.2).
	breakForced
	// breakPush starts the box on the next page with its top margin
	// truncated, as an unforced break does, to keep it whole.
	breakPush
	// breakLeft and breakRight are forced breaks to the next left or right
	// page, leaving the page between blank when the next one is the wrong
	// side (CSS Fragmentation 3, 3.1; CSS Page 3, 4.3).
	breakLeft
	breakRight
)

// formatter lays out the content of a block container. Block flow and
// inline flow share [flowFormatter]; a table formatter implements the same
// interface and joins [formatterFor].
type formatter interface {
	// layout lays out the content of b, whose border box position and
	// content width are set, starting at cursor with the margins pending
	// in mc, and returns the cursor after the content, the margins of its
	// last child still pending in mc.
	layout(e *engine, b *box, cb cblock, bc *bfc, cursor float64, mc *marginState) float64
	// intrinsic returns the min- and max-content widths of b's content
	// box (CSS Sizing 3, 5.1).
	intrinsic(e *engine, b *box) (float64, float64)
}

// formatterFor returns the formatter of the block container b: a table
// wrapper lays out its captions and table (CSS 2.1, 17.4), every other
// container, a cell or a caption included, lays out as flow.
func formatterFor(b *box) formatter {
	if b.tpart == partWrapper {
		return tableFormatter{}
	}
	if b.flex != nil {
		return flexFormatter{}
	}
	if b.grid != nil {
		return gridFormatter{}
	}
	if isMulticol(b) {
		return multicolFormatter{}
	}
	return flowFormatter{}
}

// flowFormatter lays out block flow (CSS 2.1, 9.4.1) and inline flow (CSS
// 2.1, 9.4.2).
type flowFormatter struct{}

func (flowFormatter) layout(e *engine, b *box, cb cblock, bc *bfc, cursor float64, mc *marginState) float64 {
	if b.inlineKids {
		return e.layoutInline(b, cb, bc, cursor, mc)
	}
	return e.layoutBlockKids(b, cb, bc, cursor, mc)
}

func (flowFormatter) intrinsic(e *engine, b *box) (float64, float64) {
	if b.inlineKids {
		return e.inlineIntrinsic(b)
	}
	lo, hi := 0.0, 0.0
	// Consecutive floats sit side by side in max-content layout until one
	// clears them; a box that establishes a formatting context sits beside
	// them, any other in-flow box below (CSS 2.1, 9.5).
	floats := 0.0
	for _, k := range b.kids {
		if e.err != nil {
			break
		}
		kl, kh := e.contribution(k)
		lo = max(lo, kl)
		switch {
		case k.float != css.FloatNone:
			if k.st.Clear != css.ClearNone {
				floats = 0
			}
			floats += kh
			hi = max(hi, floats)
		case k.st.Clear == css.ClearNone && k.establishesBFC():
			hi = max(hi, floats+kh)
		default:
			floats = 0
			hi = max(hi, kh)
		}
	}
	return lo, hi
}

// resolveEdges sets the used padding, border widths and margins of b
// against the containing block width cbw: percentages of margins and
// padding refer to it (CSS 2.1, 8.3 and 8.4). An auto margin is 0 here.
func resolveEdges(b *box, cbw float64) {
	b.pd, b.bd, b.m = edgesOf(b, cbw)
	if b.kind == kindInline {
		// Vertical margins of an inline box have no effect (CSS 2.1, 10.6.1).
		b.m[css.Top], b.m[css.Bottom] = 0, 0
	}
}

// hEdges returns the horizontal padding and borders of b.
func hEdges(b *box) float64 {
	return b.pd[css.Left] + b.pd[css.Right] + b.bd[css.Left] + b.bd[css.Right]
}

// vEdges returns the vertical padding and borders of b.
func vEdges(b *box) float64 {
	return b.pd[css.Top] + b.pd[css.Bottom] + b.bd[css.Top] + b.bd[css.Bottom]
}

// specWidth returns the content width a width-like value d asks for
// against cbw, honouring box-sizing (CSS UI 3, 4.1), and false for auto or
// none.
func specWidth(b *box, d css.Dimension, cbw float64) (float64, bool) {
	if d.Kind != css.Points && d.Kind != css.Percent {
		return 0, false
	}
	w := resolve(d, cbw)
	if b.st.BoxSizing == css.BorderBox && b.kind != kindInline {
		w -= hEdges(b)
	}
	return max(w, 0), true
}

// specHeight returns the content height a height-like value d asks for,
// false for auto, none, or a percentage of an indefinite height (CSS 2.1,
// 10.5).
func specHeight(b *box, d css.Dimension, cb cblock) (float64, bool) {
	if d.Kind != css.Points && (d.Kind != css.Percent || !cb.hDefinite) {
		return 0, false
	}
	h := resolve(d, cb.h)
	if b.st.BoxSizing == css.BorderBox {
		h -= vEdges(b)
	}
	return max(h, 0), true
}

// constrainWidth applies min-width and max-width to the content width w
// (CSS 2.1, 10.4).
func constrainWidth(b *box, w, cbw float64) float64 {
	if mx, ok := specWidth(b, b.st.MaxWidth, cbw); ok {
		w = min(w, mx)
	}
	if mn, ok := specWidth(b, b.st.MinWidth, cbw); ok {
		w = max(w, mn)
	}
	return max(w, 0)
}

// constrainHeight applies min-height and max-height to the content height
// h (CSS 2.1, 10.7).
func constrainHeight(b *box, h float64, cb cblock) float64 {
	if mx, ok := specHeight(b, b.st.MaxHeight, cb); ok {
		h = min(h, mx)
	}
	if mn, ok := specHeight(b, b.st.MinHeight, cb); ok {
		h = max(h, mn)
	}
	return max(h, 0)
}

// blockWidths sets the content width and horizontal margins of a block-level
// non-replaced box in normal flow, in a containing block of width cbw and
// direction rtl (CSS 2.1, 10.3.3), then applies min-width and max-width
// (10.4). w, when set, replaces an auto width: the shrink-to-fit width of a
// box beside floats, or a replaced box's used width.
func blockWidths(b *box, cbw float64, rtl bool, w float64, wSet bool) {
	st := b.st
	edges := hEdges(b)
	solve := func(width float64, auto bool) {
		ml, mr := b.m[css.Left], b.m[css.Right]
		mlAuto, mrAuto := st.Margin[css.Left].Kind == css.Auto, st.Margin[css.Right].Kind == css.Auto
		if auto {
			if mlAuto {
				ml = 0
			}
			if mrAuto {
				mr = 0
			}
			width = max(cbw-ml-mr-edges, 0)
		} else {
			if width+edges+nonAuto(ml, mlAuto)+nonAuto(mr, mrAuto) > cbw {
				if mlAuto {
					ml = 0
				}
				if mrAuto {
					mr = 0
				}
				mlAuto, mrAuto = false, false
			}
			rest := cbw - width - edges
			switch {
			case mlAuto && mrAuto:
				ml = rest / 2
				mr = rest - ml
			case mlAuto:
				ml = rest - mr
			case mrAuto:
				mr = rest - ml
			case rtl:
				// Over-constrained: the start margin wins (10.3.3).
				ml = rest - mr
			default:
				mr = rest - ml
			}
		}
		b.w, b.m[css.Left], b.m[css.Right] = width, ml, mr
	}
	width, set := specWidth(b, st.Width, cbw)
	if wSet && !set {
		width, set = w, true
		if b.kind != kindReplaced && b.tpart != partWrapper {
			// A shrink-to-fit width stands in for auto: auto margins still
			// count as 0 (10.3.5).
			if st.Margin[css.Left].Kind == css.Auto {
				b.m[css.Left] = 0
			}
			if st.Margin[css.Right].Kind == css.Auto {
				b.m[css.Right] = 0
			}
			solve(width, false)
			if st.Margin[css.Left].Kind == css.Auto || st.Margin[css.Right].Kind == css.Auto {
				b.m[css.Left], b.m[css.Right] = resolve(st.Margin[css.Left], cbw), resolve(st.Margin[css.Right], cbw)
			}
			b.w = constrainWidth(b, width, cbw)
			return
		}
	}
	solve(width, !set)
	if c := constrainWidth(b, b.w, cbw); math.Abs(c-b.w) > eps {
		solve(c, false)
	}
}

// nonAuto returns m, or 0 for an auto margin.
func nonAuto(m float64, auto bool) float64 {
	if auto {
		return 0
	}
	return m
}

// shrinkToFit returns the shrink-to-fit content width of b in a space of
// avail points, its margins, borders and padding taken off (CSS 2.1,
// 10.3.5): min(max(min-content, available), max-content).
func (e *engine) shrinkToFit(b *box, avail float64) float64 {
	lo, hi := e.intrinsic(b)
	room := avail - hEdges(b) - b.m[css.Left] - b.m[css.Right]
	return max(min(max(lo, room), hi), 0)
}

// layoutRoot lays out the root box in the initial containing block, the
// page area (CSS 2.1, 10.1).
func (e *engine) layoutRoot(rb *box) {
	cb := cblock{x: 0, w: e.pageW, h: e.pageH, hDefinite: true}
	bc := &bfc{}
	mc := &marginState{}
	e.layoutBlock(rb, cb, bc, 0, mc, breakNone)
}

// layoutBlock lays out the in-flow block-level box b whose margin top
// would sit at cursor before the margins pending in mc collapse, and
// returns the cursor after its border box, its bottom margin pending in
// mc.
func (e *engine) layoutBlock(b *box, cb cblock, bc *bfc, cursor float64, mc *marginState, mode breakMode) float64 {
	if e.err != nil {
		return cursor
	}
	b.laidOut = true
	resolveEdges(b, cb.w)
	e.resolveSizing(b, cb, -1, -1)
	if mc.absorb {
		b.m[css.Top] = 0
	}
	mt, mb := b.m[css.Top], b.m[css.Bottom]
	if mode != breakNone && e.paginating() {
		// Margins before a break are truncated; after a forced break the
		// top margin stays, after an unforced one it is truncated too (CSS
		// Fragmentation 3, 5.2).
		y := cursor
		if !e.atPageTop(y) {
			y = e.nextPage(y)
		}
		blank := false
		if right := e.isRight(e.pageOf(y)); e.cols == nil && (mode == breakLeft && right || mode == breakRight && !right) {
			y = e.nextPage(y)
			blank = true
		}
		if mode != breakPush {
			// The page a forced break starts takes the start page value of
			// the box after it, and the page it skips is blank (CSS Page 3,
			// 4.1 and 4.2.3); the page boxes from there on may change.
			p := e.pageOf(y)
			e.mark(p, pageStart(b), blank, b)
			y = e.pageTop(p)
		}
		mc.commit(y)
		mc.kept = mode != breakPush
		cursor = y
		if mode == breakPush {
			mt = 0
			b.m[css.Top] = 0
		}
		e.reach(y)
	}
	cleared := false
	clearY := math.Inf(-1)
	if b.st.Clear != css.ClearNone {
		clearY = bc.clearTo(b.st.Clear)
		hyp := mc.strut.add(mt)
		if b.kind != kindReplaced && !b.establishesBFC() {
			lead := leadingMargins(b, cb.w)
			hyp = hyp.add(lead.pos).add(lead.neg)
		}
		if clearY > cursor+hyp.sum()+eps {
			mc.commit(cursor + mc.strut.sum())
			cleared = true
		}
	}
	if b.kind == kindReplaced || b.establishesBFC() {
		top := e.pastMargin(cursor, mc.strut.add(mt), mc.kept)
		switch {
		case cleared:
			top = clearY
		default:
			if fy, ok := mc.floatTop(); ok && math.Abs(top-fy) > eps && bc.narrowed(fy, 0, cb.x, cb.x+cb.w) {
				// A float placed under the pending margins sits at fy: a
				// box that must not overlap it and does not fit beside
				// it is separated from it as clearance would, its margin
				// truncated, rather than moving the float with a
				// collapsed margin (CSS 2.1, 9.5 and 9.5.2, as browsers
				// resolve it).
				mc.commit(fy)
				top = fy
			}
		}
		mc.commitFloats(top)
		e.layoutBFCRoot(b, cb, bc, top)
		mc.commit(b.y)
		mc.strut = strut{}.add(mb)
		return b.y + b.borderH()
	}
	blockWidths(b, cb.w, cb.rtl(b), 0, false)
	b.x = cb.x + b.m[css.Left]
	collapseTop := !cleared && b.bd[css.Top] == 0 && b.pd[css.Top] == 0
	inner := mc
	start := cursor
	switch {
	case cleared:
		b.y = clearY
		inner = &marginState{absorb: b.bd[css.Top] == 0 && b.pd[css.Top] == 0, kept: true}
		start = b.contentY()
	case collapseTop:
		mc.strut = mc.strut.add(mt)
		mc.waiting = append(mc.waiting, b)
		b.y = e.pastMargin(cursor, mc.strut, mc.kept)
	default:
		top := e.pastMargin(cursor, mc.strut.add(mt), mc.kept)
		mc.commit(top)
		b.y = top
		inner = &marginState{kept: true}
		start = b.contentY()
	}
	specH, hSet := specHeight(b, b.st.Height, cb)
	kcb := wrapperCB(b, cblock{x: b.contentX(), w: b.w, h: specH, hDefinite: hSet}, cb)
	end := formatterFor(b).layout(e, b, kcb, bc, start, inner)
	// The last child's bottom margin adjoins b's when b's height is auto
	// (CSS 2.1, 8.3.1); a min-height that then makes b taller cuts that
	// margin off instead of moving it into b, as browsers do.
	collapseBottom := b.bd[css.Bottom] == 0 && b.pd[css.Bottom] == 0 && !hSet
	if i := slices.Index(mc.waiting, b); i >= 0 {
		// No content resolved the margins: b is empty.
		if collapseBottom && b.st.MinHeight.Value <= 0 {
			mc.waiting = slices.Delete(mc.waiting, i, i+1)
			mc.collapsedThrough(b)
			b.y = cursor + mc.strut.sum()
			b.h = 0
			mc.strut = mc.strut.add(mb)
			return cursor
		}
		mc.commit(e.pastMargin(cursor, mc.strut, mc.kept))
		end = b.contentY()
	}
	trailing := inner.strut
	contentBottom := end
	if !collapseBottom {
		contentBottom = e.pastMargin(end, trailing, false)
		if contentBottom < b.contentY() {
			contentBottom = b.contentY()
		}
		inner.commit(contentBottom)
	}
	h := contentBottom - b.contentY()
	content := h
	if hSet {
		h = specH
	}
	b.h = contentHeightBounds(b, constrainHeight(b, max(h, 0), cb), max(content, 0))
	e.placeMarker(b)
	bottom := b.y + b.borderH()
	if collapseBottom && math.Abs(b.h-h) < eps {
		mc.strut = trailing.add(mb)
		return bottom
	}
	mc.strut = strut{}.add(mb)
	e.reach(bottom)
	return bottom
}

// rtl reports whether the containing block of b lays out right to left:
// b's own direction, which it inherits.
func (cblock) rtl(b *box) bool {
	if b.parent != nil {
		return b.parent.st.Direction == css.RTL
	}
	return b.st.Direction == css.RTL
}

// layoutBFCRoot lays out a block-level box that establishes a block
// formatting context, or a replaced box, in flow with its border top at
// top: its border box must not overlap the floats of the formatting
// context it sits in, so it narrows beside them or moves down past them
// (CSS 2.1, 9.5).
func (e *engine) layoutBFCRoot(b *box, cb cblock, bc *bfc, top float64) {
	left, right := cb.x, cb.x+cb.w
	rtl := cb.rtl(b)
	l, r := bc.band(top, 0, left, right)
	for pass := range maxFitPasses {
		resolveEdges(b, cb.w)
		e.resolveSizing(b, cblock{w: r - l, h: cb.h, hDefinite: cb.hDefinite}, -1, -1)
		if b.kind == kindReplaced {
			w, h := e.replacedSize(b, cb)
			blockWidths(b, r-l, rtl, w, true)
			b.w, b.h = w, h
			b.x = l + b.m[css.Left]
			b.y = e.fit(top, b.borderH())
		} else {
			// A table's width comes from its columns, its auto margins
			// centre it (CSS 2.1, 17.4).
			w, wSet := 0.0, false
			if b.tpart == partWrapper {
				w, wSet = e.wrapperWidth(b, cb.w, r-l), true
			}
			blockWidths(b, r-l, rtl, w, wSet)
			b.x = l + b.m[css.Left]
			b.y = top
			top = e.layoutContextIn(b, cb, top)
		}
		h := b.borderH()
		if !bc.narrowed(top, h, left, right) {
			return
		}
		l2, r2 := bc.band(top, h, left, right)
		if b.x >= l2-eps && b.x+b.borderW() <= r2+eps || pass == maxFitPasses-1 {
			return
		}
		if l2 > l+eps || r2 < r-eps {
			// Laid out in the space beside the floats at its top, the box
			// grows down beside a float that narrows it further: try the
			// narrower space at the same top before moving down, as a box
			// whose width depends on it may then fit.
			if !e.relayout(top) {
				return
			}
			l, r = l2, r2
			continue
		}
		next := bc.below(top, h)
		if math.IsInf(next, 1) || !e.relayout(top) {
			return
		}
		top = next
		l, r = bc.band(top, 0, left, right)
	}
}

// layoutOwnContext lays out the content of a box that establishes a block
// formatting context, its width and position set: its floats stay inside
// and count toward its auto height (CSS 2.1, 10.6.7).
func (e *engine) layoutOwnContext(b *box, cb cblock) {
	specH, hSet := specHeight(b, b.st.Height, cb)
	own := &bfc{}
	mc := &marginState{kept: true}
	kcb := wrapperCB(b, cblock{x: b.contentX(), w: b.w, h: specH, hDefinite: hSet}, cb)
	saved := e.enterClamp(b, cb, b.contentY())
	end := formatterFor(b).layout(e, b, kcb, own, b.contentY(), mc)
	e.leaveClamp(saved)
	end = e.pastMargin(end, mc.strut, false)
	mc.commit(end)
	end = max(end, own.bottom(), b.contentY())
	h := end - b.contentY()
	content := h
	if hSet {
		h = specH
	}
	b.h = contentHeightBounds(b, constrainHeight(b, h, cb), content)
	e.placeMarker(b)
	e.reach(b.y + b.borderH())
}

// layoutBlockKids lays out the block-level children of b in flow, honouring
// forced breaks and the avoid values of break-before, break-after and
// break-inside (CSS Fragmentation 3, 3.1 and 3.2) by laying a box out again
// on the next page when the first pass breaks where it asks not to.
func (e *engine) layoutBlockKids(b *box, cb cblock, bc *bfc, cursor float64, mc *marginState) float64 {
	type snapshot struct {
		cursor   float64
		mc       marginState
		floats   int
		warnings int
		clamp    *clampState
	}
	take := func(c float64) snapshot {
		return snapshot{cursor: c, mc: mc.clone(), floats: len(bc.floats), warnings: len(e.warnings), clamp: e.clampSnapshot()}
	}
	restore := func(s snapshot) float64 {
		e.clampRestore(s.clamp)
		*mc = s.mc.clone()
		bc.floats = bc.floats[:s.floats]
		e.warnings = e.warnings[:s.warnings]
		return s.cursor
	}
	// On pages whose areas differ in width, a box whose width follows the
	// page area's lays each child out at the width of the page the child
	// starts on (CSS Page 3, 4.1: content resumes on a page box of the
	// named type): cb, which is as wide as the page b starts on, grown by
	// how much wider the child's page is.
	adapt := e.paginating() && e.cols == nil && e.geom.varies() && followsPages(b)
	base := 0.0
	if adapt {
		base = e.widthDelta(e.pageOf(b.y))
	}
	cbFor := func(p int) cblock {
		if !adapt {
			return cb
		}
		c := cb
		c.w = max(cb.w+e.widthDelta(p)-base, 0)
		return c
	}
	var prev *box
	var prevSnap snapshot
	// filled reports an in-flow child with a border box before the one
	// being laid out.
	filled := false
	for _, k := range b.kids {
		if e.err != nil {
			return cursor
		}
		if e.clamp != nil && e.clamp.done && !k.abs {
			// A line clamp container discards what follows its last line
			// (CSS Overflow 4, 6): the box is not laid out, nor painted.
			e.clampDiscards()
			k.laidOut = false
			continue
		}
		if k.float != css.FloatNone {
			top := cursor + mc.strut.sum()
			mc.floatPlaced(k, bc, e.layoutFloat(k, cb, bc, top), top)
			continue
		}
		if k.abs {
			staticBlock(k, cb, cursor+mc.strut.sum())
			continue
		}
		snap := take(cursor)
		mode := breakNone
		if prev != nil && (e.forces(prev.st.BreakAfter) || e.forces(k.st.BreakBefore)) {
			mode = forcedMode(prev.st.BreakAfter, k.st.BreakBefore)
		} else if prev == nil && e.forces(k.st.BreakBefore) && cursor+mc.strut.sum() > e.flowStart()+eps {
			mode = forcedMode(css.BreakAuto, k.st.BreakBefore)
		}
		if mode == breakNone && prev != nil && e.cols == nil && namesDiffer(prev, k) {
			// Siblings whose page values differ meet at a forced break
			// (CSS Page 3, 4.1).
			mode = breakForced
		}
		start := e.startPage(cursor+mc.strut.sum(), mode)
		kcb := cbFor(start)
		cursor = e.layoutBlock(k, kcb, bc, cursor, mc, mode)
		if e.clampBlock(k) {
			cursor = restore(snap)
			e.clampCut(k)
			continue
		}
		if adapt && cbFor(e.pageOf(k.y)).w != kcb.w && e.relayout(k.y) {
			// k landed on a page of another width than the one it was laid
			// out for, or the page it starts took its own page name and
			// another box with it: lay it out again at that page's.
			cursor = e.layoutBlock(k, cbFor(e.pageOf(k.y)), bc, restore(snap), mc, mode)
		}
		if e.paginating() && mode == breakNone {
			switch {
			case prev != nil && avoidsBetween(prev, k) && avoidsInside(k) && e.straddles(k) && !e.atPageTop(prev.y) &&
				prev.borderH()+k.borderH() <= e.areaH(e.pageOf(prev.y)):
				// k may break neither inside nor before: the break moves
				// before prev, which goes to the next page with it (CSS
				// Fragmentation 3, 4.4, rule 1 before rule 2).
				if e.relayout(k.y) {
					cursor = e.layoutBlock(prev, cbFor(e.pageOf(prev.y)+1), bc, restore(prevSnap), mc, breakPush)
					cursor = e.layoutBlock(k, cbFor(e.pageOf(cursor)), bc, cursor, mc, breakNone)
				}
			case avoidsInside(k) && e.straddles(k) && !e.atPageTop(k.y) && (k.borderH() <= e.areaH(e.pageOf(k.y)+1) || filled):
				// The break before k violates no rule, the one inside it
				// does: k moves to the next page. One taller than a page
				// breaks inside there anyway, so it moves only past a
				// sibling with content, a class A break point, rather than
				// leave its page empty (CSS Fragmentation 3, 4.3 and 4.4).
				if e.relayout(k.y) {
					cursor = e.layoutBlock(k, cbFor(e.pageOf(k.y)+1), bc, restore(snap), mc, breakPush)
				}
			case prev != nil && avoidsBetween(prev, k) && e.pageOf(prev.y+prev.borderH()-1e-3) < e.pageOf(firstContent(k)) &&
				!e.atPageTop(prev.y) && prev.borderH()+k.borderH() <= e.areaH(e.pageOf(prev.y)):
				if e.relayout(k.y) {
					cursor = e.layoutBlock(prev, cbFor(e.pageOf(prev.y)+1), bc, restore(prevSnap), mc, breakPush)
					cursor = e.layoutBlock(k, cbFor(e.pageOf(cursor)), bc, cursor, mc, breakNone)
				}
			case !e.atPageTop(k.y) && e.pageOf(firstContent(k)) > e.pageOf(k.y) && hasContent(k):
				// k's content starts on a later page than its top: only its
				// border, padding or the margins of its first child sit on
				// the page before. Breaking there leaves an empty slice, so
				// k moves to the page its content starts on, as Chromium
				// breaks before a container rather than after its top edge
				// (CSS Fragmentation 3, 4.1, class C, and 4.4).
				if e.relayout(k.y) {
					cursor = e.layoutBlock(k, cbFor(e.pageOf(k.y)+1), bc, restore(snap), mc, breakPush)
				}
			}
		}
		if !k.anon || k.borderH() > 0 || len(k.lines) > 0 {
			// An anonymous block of collapsed white space alone is no
			// sibling a break between boxes looks at.
			prev, prevSnap = k, snap
		}
		filled = filled || k.borderH() > 0
	}
	return cursor
}

// isForced reports a break value that forces a page break (CSS
// Fragmentation 3, 3.1).
func isForced(v css.Break) bool {
	return v == css.Page || v == css.LeftPage || v == css.RightPage
}

// forcedMode returns the break mode of a forced break between a box whose
// break-after is after and the next one, whose break-before is before: the
// later box's value wins when both force a break.
func forcedMode(after, before css.Break) breakMode {
	v := after
	if isForced(before) {
		v = before
	}
	switch v { //nolint:exhaustive // the other forced value is a plain page break
	case css.LeftPage:
		return breakLeft
	case css.RightPage:
		return breakRight
	}
	return breakForced
}

// avoidsInside reports a box that avoids page or column breaks inside it.
func avoidsInside(b *box) bool {
	v := b.st.BreakInside
	return v == css.InsideAvoid || v == css.InsideAvoidPage || v == css.InsideAvoidColumn
}

// avoidsBetween reports siblings a and b between which a page break is to
// be avoided.
func avoidsBetween(a, b *box) bool {
	av := func(v css.Break) bool {
		return v == css.BreakAvoid || v == css.BreakAvoidPage || v == css.BreakAvoidColumn
	}
	return av(a.st.BreakAfter) || av(b.st.BreakBefore)
}

// straddles reports whether b's border box crosses a page cut.
func (e *engine) straddles(b *box) bool {
	return e.pageOf(b.y) != e.pageOf(b.y+b.borderH()-1e-3)
}

// firstContent returns the top of b's first line box, or of its content
// box when it has none.
func firstContent(b *box) float64 {
	if len(b.lines) > 0 {
		return b.lines[0].y
	}
	for _, k := range b.kids {
		if !k.inline && k.float == css.FloatNone && !k.abs && k.laidOut {
			return firstContent(k)
		}
	}
	return b.contentY()
}

// hasContent reports a box with a line box or an in-flow block-level child
// laid out, the content [firstContent] finds.
func hasContent(b *box) bool {
	if len(b.lines) > 0 {
		return true
	}
	for _, k := range b.kids {
		if !k.inline && k.float == css.FloatNone && k.laidOut {
			return true
		}
	}
	return false
}

// layoutFloat lays the float f out and places it no higher than minY in
// the containing block cb (CSS 2.1, 9.5.1), and returns the index of its
// rectangle in bc, -1 for none. It is laid out where it lands, so its
// content honours the page cuts.
func (e *engine) layoutFloat(f *box, cb cblock, bc *bfc, minY float64) int {
	if e.err != nil {
		return -1
	}
	f.laidOut = true
	resolveEdges(f, cb.w)
	e.resolveSizing(f, cb, -1, -1)
	if f.st.Clear != css.ClearNone {
		// A float's top outer edge goes below the floats it clears (CSS
		// 2.1, 9.5.2: clear on a floating box adds rule 10 to 9.5.1).
		minY = max(minY, bc.clearTo(f.st.Clear))
	}
	place := func(x, y float64) {
		if f.kind == kindReplaced {
			w, h := e.replacedSize(f, cb)
			f.w, f.h = w, h
			f.x, f.y = x+f.m[css.Left], y+f.m[css.Top]
			return
		}
		switch w, ok := specWidth(f, f.st.Width, cb.w); {
		case f.tpart == partWrapper:
			f.w = e.wrapperWidth(f, cb.w, cb.w)
		case ok:
			f.w = constrainWidth(f, w, cb.w)
		default:
			f.w = constrainWidth(f, e.shrinkToFit(f, cb.w), cb.w)
		}
		f.x, f.y = x+f.m[css.Left], y+f.m[css.Top]
		e.layoutOwnContext(f, cb)
	}
	place(cb.x, minY)
	x, y := bc.place(f.float, f.marginW(), f.marginH(), minY, cb.x, cb.x+cb.w)
	if dy := y + f.m[css.Top] - f.y; dy != 0 || x+f.m[css.Left] != f.x {
		if dy != 0 && e.paginating() && e.pageOf(f.y) != e.pageOf(y) && e.relayout(y) {
			place(x, y)
		} else {
			f.shift(x+f.m[css.Left]-f.x, dy)
		}
	}
	if len(bc.floats) >= maxFloats {
		e.warnOnce(codeTooManyFloats, fmt.Sprintf("more than %d floats in one formatting context: later ones do not push content aside", maxFloats), f.y)
		return -1
	}
	bc.floats = append(bc.floats, floatRect{
		side: f.float, x0: f.x - f.m[css.Left], y0: f.y - f.m[css.Top],
		x1: f.x - f.m[css.Left] + f.marginW(), y1: f.y - f.m[css.Top] + f.marginH(),
	})
	return len(bc.floats) - 1
}

// layoutAtomic lays the inline block or inline replaced box a out in a
// containing block of width cbw, its margin box at the origin, as one
// monolithic piece (CSS Fragmentation 3, 4.1); the line box moves it into
// place.
func (e *engine) layoutAtomic(a *box, cbw float64) {
	a.laidOut = true
	resolveEdges(a, cbw)
	e.resolveSizing(a, cblock{w: cbw}, -1, -1)
	cb := cblock{w: cbw}
	if a.kind == kindReplaced {
		a.w, a.h = e.replacedSize(a, cb)
		a.x, a.y = a.m[css.Left], a.m[css.Top]
		return
	}
	switch w, ok := specWidth(a, a.st.Width, cbw); {
	case a.tpart == partWrapper:
		a.w = e.wrapperWidth(a, cbw, cbw)
	case ok:
		a.w = constrainWidth(a, w, cbw)
	default:
		a.w = constrainWidth(a, e.shrinkToFit(a, cbw), cbw)
	}
	a.x, a.y = a.m[css.Left], a.m[css.Top]
	e.noPaginate++
	e.layoutOwnContext(a, cb)
	e.noPaginate--
	a.hasBaseline = false
	switch td := a.tdata; {
	case td != nil:
		// An inline table's baseline is its first row's (CSS 2.1, 10.8.1).
		if td.hasBaseline {
			a.baseline, a.hasBaseline = td.table.y+td.baseline-a.y, true
		}
	case a.flex != nil:
		// An inline flex container's baseline is its first main-axis
		// baseline (CSS Flexbox 1, 8.5).
		if y, ok := flexBaseline(a, false); ok {
			a.baseline, a.hasBaseline = y-a.y, true
		}
	case a.grid != nil:
		// An inline grid container's baseline is its first one (CSS
		// Grid 1, 10.7).
		if y, ok := gridBaseline(a, false); ok {
			a.baseline, a.hasBaseline = y-a.y, true
		}
	case a.st.Overflow == css.OverflowVisible:
		if y, ok := lastBaseline(a); ok {
			a.baseline, a.hasBaseline = y-a.y, true
		}
	}
}

// lastBaseline returns the baseline of the last line box in flow inside b
// (CSS 2.1, 10.8.1, for inline-block).
func lastBaseline(b *box) (float64, bool) {
	if b.flex != nil {
		return flexBaseline(b, true)
	}
	if b.grid != nil {
		return gridBaseline(b, true)
	}
	if len(b.lines) > 0 {
		l := b.lines[len(b.lines)-1]
		return l.y + l.baseline, true
	}
	for i := len(b.kids) - 1; i >= 0; i-- {
		k := b.kids[i]
		if k.inline || k.float != css.FloatNone || k.abs || k.kind != kindBlock || !k.laidOut {
			continue
		}
		if y, ok := lastBaseline(k); ok {
			return y, true
		}
	}
	return 0, false
}

// firstBaseline returns the baseline of the first line box in flow inside
// b, for the outside marker of a list item.
func firstBaseline(b *box) (float64, bool) {
	if b.flex != nil {
		return flexBaseline(b, false)
	}
	if b.grid != nil {
		return gridBaseline(b, false)
	}
	if len(b.lines) > 0 {
		l := b.lines[0]
		return l.y + l.baseline, true
	}
	for _, k := range b.kids {
		if k.inline || k.float != css.FloatNone || k.abs || k.kind != kindBlock || !k.laidOut {
			continue
		}
		if y, ok := firstBaseline(k); ok {
			return y, true
		}
	}
	return 0, false
}
