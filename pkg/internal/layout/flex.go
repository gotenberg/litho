package layout

import (
	"fmt"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file lays out flex containers (CSS Flexbox 1). Box generation makes
// every in-flow child of a flex container a flex item, a run of text an
// anonymous one (section 4), and sorts them in order-modified document
// order (5.4). The flex formatter then runs the layout algorithm of
// section 9 in the container's logical axes: the flex base and
// hypothetical main sizes with the automatic minimum size (9.2, 4.5),
// the lines (9.3), the flexible lengths (9.7), the cross sizes with
// stretching and baseline alignment (9.4), the main-axis alignment with
// auto margins and justify-content (9.5, 8.1, 8.2), the cross-axis
// alignment with align-self and align-content (9.6, 8.3, 8.4), and maps
// the result onto the physical axes of a horizontal writing mode, the only
// one litho lays out. An item is measured off the page cuts, then laid
// out where it lands, so its content fragments across pages: a row
// container breaks between its lines and inside its items, the items of a
// line starting on the same page; a column container breaks between its
// items and inside them, like block flow (CSS Fragmentation 3).

// flexData is the flex layout state of a flex container.
type flexData struct {
	// first and last are the container's first and last main-axis
	// baselines below its border box top (CSS Flexbox 1, 8.5), valid
	// when hasFirst and hasLast.
	first, last       float64
	hasFirst, hasLast bool
	// measured caches the measurements of the items, and current the
	// measurement each item's present layout was made at.
	measured map[measureKey]itemMeasure
	current  map[*box]measureKey
}

// measureKey names the layout of an item at a content width w, a height
// h when hDef, in a containing block of width cbw and height cbh.
type measureKey struct {
	item     *box
	w, h     float64
	hDef     bool
	cbw, cbh float64
	cbhDef   bool
}

// itemMeasure is an item laid out off the page cuts: its content height,
// and its first and last baselines below its border box top.
type itemMeasure struct {
	h                 float64
	first, last       float64
	hasFirst, hasLast bool
}

// flexAxes are a flex container's axes in a horizontal writing mode (CSS
// Flexbox 1, 2 and 5.1): row reports a horizontal main axis; mainRev
// reports a main-start at the physical right or bottom, crossRev a
// cross-start at the bottom or right.
type flexAxes struct {
	row, wrap         bool
	mainRev, crossRev bool
	rtl               bool
}

// axesOf returns the axes of a flex container of style st.
func axesOf(st *css.Style) flexAxes {
	rtl := st.Direction == css.RTL
	a := flexAxes{rtl: rtl, wrap: st.FlexWrap != css.NoWrap}
	switch st.FlexDirection {
	case css.FlexRow:
		a.row, a.mainRev = true, rtl
	case css.FlexRowReverse:
		a.row, a.mainRev = true, !rtl
	case css.FlexColumn:
	case css.FlexColumnReverse:
		a.mainRev = true
	}
	rev := st.FlexWrap == css.WrapReverse
	if a.row {
		a.crossRev = rev
	} else {
		a.crossRev = rtl != rev
	}
	return a
}

// sides returns the physical sides of the main-start, main-end,
// cross-start and cross-end edges.
func (a flexAxes) sides() (ms, me, cs, ce css.Side) {
	ms, me, cs, ce = css.Left, css.Right, css.Top, css.Bottom
	if !a.row {
		ms, me, cs, ce = css.Top, css.Bottom, css.Left, css.Right
	}
	if a.mainRev {
		ms, me = me, ms
	}
	if a.crossRev {
		cs, ce = ce, cs
	}
	return ms, me, cs, ce
}

// mainStartIsStart reports whether the main-start edge is the start edge
// of the container's writing mode, which the start and end keywords
// name (CSS Box Alignment 3, 4.3); crossStartIsStart says the same of
// the cross axis.
func (a flexAxes) mainStartIsStart() bool {
	if a.row {
		return a.mainRev == a.rtl
	}
	return !a.mainRev
}

func (a flexAxes) crossStartIsStart() bool {
	if a.row {
		return !a.crossRev
	}
	return a.crossRev == a.rtl
}

// fitem is the layout state of one flex item.
type fitem struct {
	b *box
	// mm and cm are the used main-start, main-end, cross-start and
	// cross-end margins; mAuto and cAuto report the auto ones.
	mm, cm       [2]float64
	mAuto, cAuto [2]bool
	// mEdges and cEdges are the padding and borders along each axis.
	mEdges, cEdges float64
	// base, hypo, minM and maxM are the flex base size, the hypothetical
	// main size and the main size bounds of the content box (CSS Flexbox
	// 1, 9.2); target is the main size the flexible lengths resolve to
	// (9.7).
	base, hypo, minM, maxM float64
	target                 float64
	frozen                 bool
	grow, shrink           float64
	// cross is the content box cross size; crossSet reports one the style
	// fixes, stretch one stretched to its line (9.4, 11).
	cross    float64
	crossSet bool
	stretch  bool
	// stretchTo is the cross size a replaced item of a row is stretched
	// to when its single line has a definite cross size, which its ratio
	// transfers to its main size (CSS Flexbox 1, 9.2, 3B and 9.8); -1
	// for none.
	stretchTo float64
	// align is the item's used align-self.
	align css.BoxAlign
	// measured is the content height the item measured at, for a row
	// item, and its baselines below its border box top.
	m    itemMeasure
	hasM bool
	// mpos and cpos are the offsets of the margin box from the line's
	// main-start and cross-start edges.
	mpos, cpos float64
}

// outerMain returns the item's outer main size at its target size.
func (it *fitem) outerMain() float64 { return it.target + it.mEdges + it.mm[0] + it.mm[1] }

// outerHypo returns the item's outer hypothetical main size, its auto
// margins as 0 (CSS Flexbox 1, 9.3).
func (it *fitem) outerHypo() float64 { return it.hypo + it.mEdges + it.mm[0] + it.mm[1] }

// outerBase returns the item's outer flex base size.
func (it *fitem) outerBase() float64 { return it.base + it.mEdges + it.mm[0] + it.mm[1] }

// outerCross returns the item's outer cross size.
func (it *fitem) outerCross() float64 { return it.cross + it.cEdges + it.cm[0] + it.cm[1] }

// baseline reports an item aligned on its first or last baseline: last
// with last set.
func (it *fitem) baseline() (bool, bool) {
	if it.cAuto[0] || it.cAuto[1] {
		return false, false
	}
	switch it.align.Keyword() { //nolint:exhaustive // the other values align otherwise
	case css.BoxBaseline:
		return true, false
	case css.BoxLastBaseline:
		return true, true
	}
	return false, false
}

// ascent returns the distance from the item's outer cross-start edge, its
// margin top, to the baseline it aligns on in a row container: its first
// or last one, or its border box bottom when it has none (CSS Box
// Alignment 3, 9.1).
func (it *fitem) ascent(last bool) float64 {
	k := it.b
	top := k.m[css.Top]
	switch {
	case !last && it.m.hasFirst:
		return top + it.m.first
	case last && it.m.hasLast:
		return top + it.m.last
	}
	return top + it.cross + vEdges(k)
}

// flexLine is one flex line (CSS Flexbox 1, 6).
type flexLine struct {
	items []*fitem
	// cross is the line's cross size, pos its offset from the container's
	// cross-start content edge.
	cross, pos float64
	// asc and desc are the largest first-baseline ascent and descent of
	// its baseline-aligned items, lasc and ldesc those of its last
	// baseline-aligned ones.
	asc, desc, lasc, ldesc float64
}

// flexFormatter lays out the content of a flex container.
type flexFormatter struct{}

func (flexFormatter) layout(e *engine, b *box, cb cblock, _ *bfc, cursor float64, _ *marginState) float64 {
	return e.layoutFlex(b, cb, cursor)
}

// intrinsic returns the min- and max-content widths of a flex container
// (CSS Flexbox 1, 9.9.1): a row lays its items side by side, so its
// max-content width sums their max-content contributions, and so does its
// min-content width unless it wraps; a column stacks them, so it takes
// the largest. The gaps between items count in a row.
func (flexFormatter) intrinsic(e *engine, b *box) (float64, float64) {
	ax := axesOf(b.st)
	gap := 0.0
	if b.st.ColumnGap.Kind == css.Points {
		gap = max(clamp(b.st.ColumnGap.Value), 0)
	}
	lo, hi := 0.0, 0.0
	n := 0
	for _, k := range b.kids {
		if !k.flexItem || e.err != nil {
			continue
		}
		kl, kh := e.contribution(k)
		if ax.row {
			if ax.wrap {
				lo = max(lo, kl)
			} else {
				lo += kl
			}
			hi += kh
		} else {
			lo, hi = max(lo, kl), max(hi, kh)
		}
		n++
	}
	if ax.row && n > 1 {
		hi += gap * float64(n-1)
		if !ax.wrap {
			lo += gap * float64(n-1)
		}
	}
	return lo, max(hi, lo)
}

// heightBounds returns the min-height and max-height of b that a flex
// layout honours before the container's own height is set: lengths; a
// percentage applies once the container's height is.
func heightBounds(b *box) (float64, float64) {
	lo, hi := 0.0, math.Inf(1)
	if b.st.MinHeight.Kind == css.Points {
		lo, _ = specHeight(b, b.st.MinHeight, cblock{})
	}
	if b.st.MaxHeight.Kind == css.Points {
		hi, _ = specHeight(b, b.st.MaxHeight, cblock{})
		hi = max(hi, lo)
	}
	return lo, hi
}

// gapOf returns the used value of a row-gap or column-gap d against the
// content box size ref, 0 for normal or a percentage of an indefinite
// size (CSS Box Alignment 3, 8.1).
func gapOf(d css.Dimension, ref float64, definite bool) float64 {
	if d.Kind == css.Percent && !definite {
		return 0
	}
	return max(resolve(d, ref), 0)
}

// usedAlign returns the used align-self of an item of style st in a
// container of style c: auto takes the container's align-items, and
// normal behaves as stretch (CSS Box Alignment 3, 6.1).
func usedAlign(st, c *css.Style) css.BoxAlign {
	a := st.AlignSelf
	if a.Keyword() == css.BoxAuto {
		a = c.AlignItems
	}
	if a.Keyword() == css.BoxNormal || a.Keyword() == css.BoxAuto {
		return css.BoxStretch
	}
	return a
}

// layoutFlex lays out the flex container b's content in its content box
// cb, from top down, and returns the bottom of its content (CSS Flexbox
// 1, 9).
func (e *engine) layoutFlex(b *box, cb cblock, top float64) float64 {
	fd := b.flex
	fd.hasFirst, fd.hasLast = false, false
	if fd.measured == nil {
		fd.measured = map[measureKey]itemMeasure{}
		fd.current = map[*box]measureKey{}
	}
	ax := axesOf(b.st)
	ms, me, cs, ce := ax.sides()
	minH, maxH := heightBounds(b)
	clampH := func(h float64) float64 { return max(min(h, maxH), minH) }
	pcb := cblock{x: cb.x, w: cb.w, h: cb.h, hDefinite: cb.hDefinite}
	if pcb.hDefinite {
		pcb.h = clampH(pcb.h)
	}
	colGap := gapOf(b.st.ColumnGap, cb.w, true)
	rowGap := gapOf(b.st.RowGap, pcb.h, pcb.hDefinite)
	mainGap, crossGap := colGap, rowGap
	innerMain, mainDef := cb.w, true
	innerCross, crossDef := pcb.h, pcb.hDefinite
	if !ax.row {
		mainGap, crossGap = rowGap, colGap
		innerMain, mainDef = pcb.h, pcb.hDefinite
		innerCross, crossDef = cb.w, true
	}

	var items []*fitem
	for _, k := range b.kids {
		if e.err != nil {
			return top
		}
		if k.abs {
			// The static position of an absolutely positioned child is
			// the content box's corner, aligned once its size is known
			// (CSS Flexbox 1, 4.1; see flexStaticShift).
			setStatic(k, cb.x, top, false)
			continue
		}
		if !k.flexItem {
			continue
		}
		k.laidOut = true
		resolveEdges(k, cb.w)
		e.resolveSizing(k, pcb, -1, -1)
		st := k.st
		it := &fitem{b: k, grow: st.FlexGrow, shrink: st.FlexShrink, align: usedAlign(st, b.st)}
		it.mm = [2]float64{k.m[ms], k.m[me]}
		it.cm = [2]float64{k.m[cs], k.m[ce]}
		it.mAuto = [2]bool{st.Margin[ms].Kind == css.Auto, st.Margin[me].Kind == css.Auto}
		it.cAuto = [2]bool{st.Margin[cs].Kind == css.Auto, st.Margin[ce].Kind == css.Auto}
		it.mEdges, it.cEdges = hEdges(k), vEdges(k)
		if !ax.row {
			it.mEdges, it.cEdges = it.cEdges, it.mEdges
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		h := 0.0
		if ax.row && crossDef || !ax.row && mainDef {
			h = pcb.h
		}
		return top + clampH(h)
	}
	single := !ax.wrap
	for _, it := range items {
		it.stretchTo = -1
		k := it.b
		if ax.row && single && crossDef && k.kind == kindReplaced && it.align.Keyword() == css.BoxStretch &&
			k.st.Height.Kind == css.Auto && !it.cAuto[0] && !it.cAuto[1] {
			it.stretchTo = constrainHeight(k, max(innerCross-it.cm[0]-it.cm[1]-it.cEdges, 0), pcb)
		}
		if !ax.row {
			e.columnCross(it, cb, single)
		}
		e.baseSize(fd, it, ax, cb, pcb, innerMain, mainDef, top)
	}
	if e.err != nil {
		return top
	}

	// Lines (CSS Flexbox 1, 9.3), at the main size available: an
	// indefinite column height wraps at its max-height alone.
	avail := innerMain
	if !mainDef {
		avail = maxH
	}
	lines := collectLines(items, avail, mainGap, ax.wrap)
	if !mainDef {
		// The main size of an auto-height column is its content's,
		// within its min-height and max-height (9.3, as Chromium
		// resolves it).
		widest := 0.0
		for _, ln := range lines {
			s := 0.0
			for _, it := range ln.items {
				s += it.outerHypo()
			}
			widest = max(widest, s+mainGap*float64(len(ln.items)-1))
		}
		innerMain = clampH(widest)
	}
	for _, ln := range lines {
		resolveFlexible(ln.items, innerMain, mainGap)
	}

	// Cross sizes (9.4).
	for _, it := range items {
		if ax.row {
			e.rowCross(fd, it, single && crossDef, innerCross, pcb, top)
		}
	}
	for _, ln := range lines {
		lineCross(ln, ax.row)
		switch {
		case single && crossDef:
			ln.cross = innerCross
		case single && ax.row:
			ln.cross = clampH(ln.cross)
		}
	}
	total := crossGap * float64(len(lines)-1)
	for _, ln := range lines {
		total += ln.cross
	}
	if !crossDef {
		innerCross = clampH(total)
	}
	free := innerCross - total
	ac := b.st.AlignContent.Keyword()
	if ax.wrap && free > 0 && (ac == css.BoxNormal || ac == css.BoxStretch) {
		for _, ln := range lines {
			ln.cross += free / float64(len(lines))
		}
		free = 0
	}
	off, between := 0.0, 0.0
	if ax.wrap {
		off, between = distribute(b.st.AlignContent, free, len(lines), ax.crossStartIsStart(), false, ax.mainRev)
	}
	pos := off
	for _, ln := range lines {
		ln.pos = pos
		pos += ln.cross + crossGap + between
		for _, it := range ln.items {
			if it.stretch {
				it.cross = max(ln.cross-it.cm[0]-it.cm[1]-it.cEdges, 0)
				if ax.row {
					it.cross = constrainHeight(it.b, it.cross, pcb)
				} else {
					it.cross = constrainWidth(it.b, it.cross, cb.w)
				}
			}
		}
		alignMain(ln, b.st.JustifyContent, innerMain, mainGap, ax)
		alignCross(ln, ax)
	}

	// Write the used margins back, then place the items.
	for _, it := range items {
		k := it.b
		k.m[ms], k.m[me], k.m[cs], k.m[ce] = it.mm[0], it.mm[1], it.cm[0], it.cm[1]
	}
	place := placement{e: e, fd: fd, ax: ax, cb: cb, pcb: pcb, top: top, innerMain: innerMain, innerCross: innerCross}
	var end float64
	if ax.row {
		end = place.rows(lines)
	} else {
		end = place.columns(lines)
	}
	e.flexBaselines(b, lines, ax)
	return end
}

// columnCross sets the cross size, the width, of the item it of a column
// container of content box cb, which its main size depends on: its width,
// stretched to the container in a single-line one, else fitted to the
// content (CSS Flexbox 1, 9.4, 7 and 11).
func (e *engine) columnCross(it *fitem, cb cblock, single bool) {
	k := it.b
	canStretch := it.align.Keyword() == css.BoxStretch && !it.cAuto[0] && !it.cAuto[1] && k.st.Width.Kind == css.Auto
	if k.tpart == partWrapper && k.tdata != nil {
		// A table's width is its table box's (CSS 2.1, 17.4).
		if t := k.tdata.table; t.st.Width.Kind != css.Auto {
			it.cross, it.crossSet = e.wrapperWidth(k, cb.w, cb.w), true
			return
		}
	}
	room := cb.w - it.cm[0] - it.cm[1] - it.cEdges
	switch w, ok := specWidth(k, k.st.Width, cb.w); {
	case ok:
		if k.kind == kindReplaced {
			w, _ = e.replacedSize(k, cb)
		}
		it.cross, it.crossSet = constrainWidth(k, w, cb.w), true
	case canStretch:
		it.stretch = true
		if single {
			it.cross = constrainWidth(k, max(room, 0), cb.w)
		} else {
			it.cross = e.fitWidth(k, cb)
		}
	default:
		it.cross = e.fitWidth(k, cb)
	}
}

// fitWidth returns the fit-content width of the item k in the content
// box cb: a table's width, a replaced box's used width, or the
// shrink-to-fit width of a block (CSS Sizing 3, 5.2).
func (e *engine) fitWidth(k *box, cb cblock) float64 {
	switch {
	case k.kind == kindReplaced:
		w, _ := e.replacedSize(k, cb)
		return w
	case k.tpart == partWrapper:
		return e.wrapperWidth(k, cb.w, cb.w)
	}
	return constrainWidth(k, e.shrinkToFit(k, cb.w), cb.w)
}

// ratioOf returns the intrinsic ratio, width over height, of a replaced
// box, 0 for none.
func ratioOf(k *box) float64 {
	if k.kind == kindReplaced && k.picture != nil && k.picture.Width > 0 && k.picture.Height > 0 {
		return k.picture.Width / k.picture.Height
	}
	return 0
}

// baseSize sets the flex base size, the main size bounds and the
// hypothetical main size of the item it (CSS Flexbox 1, 9.2, 3 and 4.5).
func (e *engine) baseSize(fd *flexData, it *fitem, ax flexAxes, cb, pcb cblock, innerMain float64, mainDef bool, top float64) {
	k := it.b
	st := k.st
	mcb := cblock{x: cb.x, w: cb.w, h: innerMain, hDefinite: mainDef}
	// A table's sizes are its table box's: the wrapper, the flex item,
	// takes its margins and flex properties alone (CSS 2.1, 17.4). The
	// wrapper's content box is the table's border box.
	sized, sst, extra := k, st, 0.0
	if k.tpart == partWrapper && k.tdata != nil {
		t := k.tdata.table
		sized, sst = t, t.st
		pd, bd, _ := edgesOf(t, cb.w)
		if ax.row {
			extra = pd[css.Left] + pd[css.Right] + bd[css.Left] + bd[css.Right]
		} else {
			extra = pd[css.Top] + pd[css.Bottom] + bd[css.Top] + bd[css.Bottom]
		}
	}
	mainSpec := func(d css.Dimension, ref cblock) (float64, bool) {
		var v float64
		var ok bool
		if ax.row {
			v, ok = specWidth(sized, d, ref.w)
		} else {
			v, ok = specHeight(sized, d, ref)
		}
		if ok && sized != k && sized.st.BoxSizing != css.BorderBox {
			v += extra
		}
		return v, ok
	}
	content := -1.0
	contentSize := func() float64 {
		if content < 0 {
			content = e.contentMain(fd, it, ax, cb, pcb, top)
		}
		return content
	}
	if sized != k && !ax.row {
		// A table's content height is its rows', its height property
		// aside, which is a specified size (CSS Flexbox 1, 4.5), as
		// Chromium measures a table flex item of a column.
		contentSize = func() float64 {
			if content < 0 {
				saved := sized.st
				st := *saved
				st.Height, st.MinHeight = css.Dimension{Kind: css.Auto}, css.Pts(0)
				sized.st = &st
				content = e.contentMain(fd, it, ax, cb, pcb, top)
				sized.st = saved
			}
			return content
		}
	}
	base, ok := 0.0, false
	switch st.FlexBasis.Kind {
	case css.Points, css.Percent:
		base, ok = mainSpec(st.FlexBasis, mcb)
		if !ok {
			// A percentage of an indefinite main size acts as auto, as
			// Chromium resolves it.
			base, ok = mainSpec(mainProp(sst, ax), pcb)
		}
	case css.Auto:
		base, ok = mainSpec(mainProp(sst, ax), pcb)
	case css.FlexContent, css.None, css.Normal, css.Number, css.MinContent, css.MaxContent, css.FitContent, css.Stretch:
	}
	if !ok {
		base = contentSize()
	}
	it.base = max(base, 0)
	it.minM, it.maxM = 0, math.Inf(1)
	if mx, ok := mainSpec(maxProp(sst, ax), pcb); ok {
		it.maxM = mx
	}
	switch {
	case !minAuto(sst, ax):
		if mn, ok := mainSpec(minProp(sst, ax), pcb); ok {
			it.minM = mn
		}
	case st.Overflow == css.OverflowVisible:
		// The automatic minimum size (4.5): the content size
		// suggestion, capped by a specified size suggestion and by the
		// maximum main size.
		auto := e.minContentMain(fd, it, ax, cb, pcb, top, contentSize)
		if spec, ok := mainSpec(mainProp(sst, ax), pcb); ok {
			auto = min(auto, spec)
		}
		it.minM = min(auto, it.maxM)
	}
	if k.tpart == partWrapper && ax.row {
		// A table is never narrower than its min-content width (CSS
		// 2.1, 17.5.2), as Chromium sizes table flex items.
		lo, _ := e.intrinsic(k)
		it.minM = max(it.minM, lo)
		it.maxM = max(it.maxM, it.minM)
	}
	it.hypo = max(min(it.base, it.maxM), it.minM)
	it.target = it.hypo
}

// mainProp, minProp and maxProp return the size properties of the main
// axis, and minAuto whether its minimum is auto.
func mainProp(st *css.Style, ax flexAxes) css.Dimension {
	if ax.row {
		return st.Width
	}
	return st.Height
}

func minProp(st *css.Style, ax flexAxes) css.Dimension {
	if ax.row {
		return st.MinWidth
	}
	return st.MinHeight
}

func maxProp(st *css.Style, ax flexAxes) css.Dimension {
	if ax.row {
		return st.MaxWidth
	}
	return st.MaxHeight
}

func minAuto(st *css.Style, ax flexAxes) bool {
	if ax.row {
		return st.MinWidthAuto
	}
	return st.MinHeightAuto
}

// replacedMain returns the height of the replaced item k at the width w
// in a column: its specified height, else the height its ratio gives.
func (e *engine) replacedMain(k *box, w float64, pcb cblock) float64 {
	if h, ok := specHeight(k, k.st.Height, pcb); ok {
		return constrainHeight(k, h, pcb)
	}
	if r := ratioOf(k); r > 0 {
		return constrainHeight(k, w/r, pcb)
	}
	_, h := e.replacedSize(k, pcb)
	return h
}

// contentMain returns the max-content main size of the item it (CSS
// Flexbox 1, 9.2, 3E): in a row its max-content width, or the width its
// ratio gives a replaced box of definite height; in a column the height
// of its content laid out at its width.
func (e *engine) contentMain(fd *flexData, it *fitem, ax flexAxes, cb, pcb cblock, top float64) float64 {
	k := it.b
	if !ax.row {
		if k.kind == kindReplaced {
			return e.replacedMain(k, it.cross, pcb)
		}
		return e.measureItem(fd, it, it.cross, 0, false, pcb, cb.x, top).h
	}
	if k.kind == kindReplaced {
		if h, ok := specHeight(k, k.st.Height, pcb); ok {
			if r := ratioOf(k); r > 0 {
				return constrainWidth(k, constrainHeight(k, h, pcb)*r, cb.w)
			}
		}
		if r := ratioOf(k); r > 0 && it.stretchTo >= 0 && k.st.Width.Kind == css.Auto {
			return constrainWidth(k, it.stretchTo*r, cb.w)
		}
		w, _ := e.replacedSize(k, pcb)
		return w
	}
	_, hi := e.contentWidths(k)
	return hi
}

// contentWidths returns the min- and max-content widths of the content of
// the non-replaced item k, whatever its own width says, within its
// max-width: the content size suggestion and the content flex basis
// ignore the item's preferred width (CSS Flexbox 1, 4.5 and 9.2).
func (e *engine) contentWidths(k *box) (float64, float64) {
	if k.st.Width.Kind != css.Points {
		return e.intrinsic(k)
	}
	lo, hi := formatterFor(k).intrinsic(e, k)
	if mx, ok := specWidth(k, k.st.MaxWidth, 0); ok && k.st.MaxWidth.Kind == css.Points {
		lo, hi = min(lo, mx), min(hi, mx)
	}
	return lo, max(hi, lo)
}

// minContentMain returns the content size suggestion of the item it (CSS
// Flexbox 1, 4.5): its min-content width in a row, its content height in
// a column, a replaced box's used size.
func (e *engine) minContentMain(fd *flexData, it *fitem, ax flexAxes, cb, pcb cblock, top float64, content func() float64) float64 {
	k := it.b
	if !ax.row || k.kind == kindReplaced {
		if !ax.row && k.kind != kindReplaced && (k.tpart != partWrapper || k.tdata == nil) {
			return e.measureItem(fd, it, it.cross, 0, false, pcb, cb.x, top).h
		}
		return content()
	}
	lo, _ := e.contentWidths(k)
	return lo
}

// collectLines sorts the items into flex lines of avail points at most
// (CSS Flexbox 1, 9.3, 5): one line unless wrap, else as many items on
// each as fit, at least one.
func collectLines(items []*fitem, avail, gap float64, wrap bool) []*flexLine {
	if !wrap {
		return []*flexLine{{items: items}}
	}
	var out []*flexLine
	var cur *flexLine
	sum := 0.0
	for _, it := range items {
		o := it.outerHypo()
		if cur != nil && sum+gap+o > avail+eps {
			cur = nil
		}
		if cur == nil {
			cur = &flexLine{}
			out = append(out, cur)
			cur.items = append(cur.items, it)
			sum = o
			continue
		}
		cur.items = append(cur.items, it)
		sum += gap + o
	}
	return out
}

// resolveFlexible resolves the flexible lengths of the items of one line
// of inner main size main (CSS Flexbox 1, 9.7).
func resolveFlexible(items []*fitem, main, gap float64) {
	gaps := gap * float64(len(items)-1)
	sum := gaps
	for _, it := range items {
		sum += it.outerHypo()
	}
	grow := sum < main
	// Size the inflexible items (step 2).
	for _, it := range items {
		it.frozen = false
		it.target = it.hypo
		f := it.shrink
		if grow {
			f = it.grow
		}
		if f == 0 || grow && it.base > it.hypo || !grow && it.base < it.hypo {
			it.frozen = true
		}
	}
	space := func() float64 {
		s := main - gaps
		for _, it := range items {
			if it.frozen {
				s -= it.outerMain()
			} else {
				s -= it.outerBase()
			}
		}
		return s
	}
	initial := space()
	for range len(items) + 1 {
		var unfrozen []*fitem
		factors := 0.0
		for _, it := range items {
			if !it.frozen {
				unfrozen = append(unfrozen, it)
				if grow {
					factors += it.grow
				} else {
					factors += it.shrink
				}
			}
		}
		if len(unfrozen) == 0 {
			break
		}
		remaining := space()
		if factors < 1 {
			if v := initial * factors; math.Abs(v) < math.Abs(remaining) {
				remaining = v
			}
		}
		if remaining != 0 && factors > 0 {
			if grow {
				for _, it := range unfrozen {
					it.target = it.base + remaining*it.grow/factors
				}
			} else {
				scaled := 0.0
				for _, it := range unfrozen {
					scaled += it.shrink * it.base
				}
				for _, it := range unfrozen {
					if scaled > 0 {
						it.target = it.base + remaining*it.shrink*it.base/scaled
					} else {
						it.target = it.base
					}
				}
			}
		} else {
			for _, it := range unfrozen {
				it.target = it.base
			}
		}
		// Fix min and max violations (step 4d and 4e).
		total := 0.0
		viol := make([]float64, len(unfrozen))
		for i, it := range unfrozen {
			c := max(min(it.target, it.maxM), it.minM, 0)
			viol[i] = c - it.target
			total += viol[i]
			it.target = c
		}
		for i, it := range unfrozen {
			switch {
			case math.Abs(total) <= eps:
				it.frozen = true
			case total > 0 && viol[i] > 0, total < 0 && viol[i] < 0:
				it.frozen = true
			}
		}
	}
	for _, it := range items {
		it.frozen = true
	}
}

// rowCross sets the hypothetical cross size, the height, of the item it
// of a row container, laid out off the page cuts at its main size (CSS
// Flexbox 1, 9.4, 7), and its baselines; definite reports a single line
// of definite cross size cross.
func (e *engine) rowCross(fd *flexData, it *fitem, definite bool, cross float64, pcb cblock, top float64) {
	k := it.b
	st := k.st
	h, hSet := specHeight(k, st.Height, pcb)
	it.stretch = it.align.Keyword() == css.BoxStretch && !hSet && !it.cAuto[0] && !it.cAuto[1]
	if k.kind == kindReplaced {
		switch r := ratioOf(k); {
		case hSet:
			it.cross, it.crossSet = constrainHeight(k, h, pcb), true
		case it.stretch && definite:
			it.cross = constrainHeight(k, max(cross-it.cm[0]-it.cm[1]-it.cEdges, 0), pcb)
		case r > 0:
			it.cross = constrainHeight(k, it.target/r, pcb)
		default:
			_, it.cross = e.replacedSize(k, pcb)
		}
		it.m = itemMeasure{h: it.cross}
		return
	}
	if hSet {
		h = constrainHeight(k, h, pcb)
		it.m = e.measureItem(fd, it, it.target, h, true, pcb, pcb.x, top)
		it.cross, it.crossSet = h, true
	} else {
		it.m = e.measureItem(fd, it, it.target, 0, false, pcb, pcb.x, top)
		it.cross = constrainHeight(k, it.m.h, pcb)
	}
	it.hasM = true
}

// lineCross sets the cross size of the line ln from its items (CSS
// Flexbox 1, 9.4, 8): the largest outer cross size, and in a row the
// ascent and descent of its baseline-aligned items.
func lineCross(ln *flexLine, row bool) {
	ln.cross, ln.asc, ln.desc, ln.lasc, ln.ldesc = 0, 0, 0, 0, 0
	for _, it := range ln.items {
		o := it.outerCross()
		if b, last := it.baseline(); b && row {
			a := it.ascent(last)
			if last {
				ln.lasc, ln.ldesc = max(ln.lasc, a), max(ln.ldesc, o-a)
			} else {
				ln.asc, ln.desc = max(ln.asc, a), max(ln.desc, o-a)
			}
			continue
		}
		ln.cross = max(ln.cross, o)
	}
	ln.cross = max(ln.cross, ln.asc+ln.desc, ln.lasc+ln.ldesc)
}

// distribute returns the offset of the first box and the extra space
// between boxes that the alignment a gives n boxes in free space (CSS Box
// Alignment 3, 4.3 and 5.1; CSS Flexbox 1, 8.2 and 8.4). startIsStart
// reports a flex-start edge that is the start edge; row reports the
// container's row axis for left and right, which mainRev places.
func distribute(a css.BoxAlign, free float64, n int, startIsStart, row, mainRev bool) (float64, float64) {
	if n <= 0 {
		return 0, 0
	}
	end := func() float64 { return free }
	kw := a.Keyword()
	if a.Safe() && free < 0 {
		kw = css.BoxStart
	}
	switch kw { //nolint:exhaustive // the other values align at flex-start
	case css.BoxFlexEnd:
		return end(), 0
	case css.BoxCenter:
		return free / 2, 0
	case css.BoxStart:
		if !startIsStart {
			return end(), 0
		}
	case css.BoxEnd:
		if startIsStart {
			return end(), 0
		}
	case css.BoxLeft, css.BoxRight:
		if row && (kw == css.BoxLeft) == mainRev {
			return end(), 0
		}
		if !row && !startIsStart {
			return end(), 0
		}
	case css.BoxSpaceBetween:
		if free > 0 && n > 1 {
			return 0, free / float64(n-1)
		}
	case css.BoxSpaceAround:
		if free < 0 || n == 1 {
			return free / 2, 0
		}
		return free / float64(n) / 2, free / float64(n)
	case css.BoxSpaceEvenly:
		if free < 0 || n == 1 {
			return free / 2, 0
		}
		return free / float64(n+1), free / float64(n+1)
	}
	return 0, 0
}

// alignMain places the items of the line ln along the main axis of inner
// size main (CSS Flexbox 1, 9.5): auto margins take the positive free
// space, else justify-content distributes it.
func alignMain(ln *flexLine, jc css.BoxAlign, main, gap float64, ax flexAxes) {
	used := gap * float64(len(ln.items)-1)
	autos := 0
	for _, it := range ln.items {
		used += it.outerMain()
		for _, a := range it.mAuto {
			if a {
				autos++
			}
		}
	}
	free := main - used
	off, between := 0.0, 0.0
	if free > 0 && autos > 0 {
		each := free / float64(autos)
		for _, it := range ln.items {
			for i, a := range it.mAuto {
				if a {
					it.mm[i] += each
				}
			}
		}
	} else {
		off, between = distribute(jc, free, len(ln.items), ax.mainStartIsStart(), ax.row, ax.mainRev)
	}
	pos := off
	for _, it := range ln.items {
		it.mpos = pos
		pos += it.outerMain() + gap + between
	}
}

// alignCross places each item of the line ln in the line's cross size
// (CSS Flexbox 1, 9.6): auto margins take the free space, else
// align-self places the item.
func alignCross(ln *flexLine, ax flexAxes) {
	for _, it := range ln.items {
		free := ln.cross - it.outerCross()
		if it.cAuto[0] || it.cAuto[1] {
			it.cpos = 0
			if free <= 0 {
				continue
			}
			switch {
			case it.cAuto[0] && it.cAuto[1]:
				it.cm[0] += free / 2
				it.cm[1] += free / 2
				it.cpos = 0
			case it.cAuto[0]:
				it.cm[0] += free
			default:
				it.cm[1] += free
			}
			continue
		}
		kw := it.align.Keyword()
		if it.align.Safe() && free < 0 {
			kw = css.BoxStart
		}
		switch kw { //nolint:exhaustive // the other values align at cross-start
		case css.BoxFlexEnd:
			it.cpos = free
		case css.BoxCenter:
			it.cpos = free / 2
		case css.BoxStart, css.BoxEnd:
			it.cpos = 0
			if (kw == css.BoxStart) != ax.crossStartIsStart() {
				it.cpos = free
			}
		case css.BoxSelfStart, css.BoxSelfEnd:
			// The item's own start: its direction's in a column's
			// inline cross axis, the block start in a row.
			atStart := ax.crossStartIsStart()
			if !ax.row {
				atStart = ax.crossRev == (it.b.st.Direction == css.RTL)
			}
			it.cpos = 0
			if (kw == css.BoxSelfStart) != atStart {
				it.cpos = free
			}
		case css.BoxBaseline, css.BoxLastBaseline:
			it.cpos = 0
			if b, last := it.baseline(); b && ax.row {
				if last {
					it.cpos = ln.cross - ln.ldesc - it.ascent(true)
				} else {
					it.cpos = ln.asc - it.ascent(false)
				}
				if ax.crossRev {
					// The offset is from the physical top; cross-start
					// is the bottom.
					it.cpos = free - it.cpos
				}
			}
		default:
			it.cpos = 0
		}
	}
}

// placement maps the logical positions of a flex layout onto the flow
// and lays the items out where they land.
type placement struct {
	e                     *engine
	fd                    *flexData
	ax                    flexAxes
	cb, pcb               cblock
	top                   float64
	innerMain, innerCross float64
}

// mainPhys returns the offset from the content box's left or top edge of
// a span of the given size at offset pos from the main-start edge; crossPhys
// does the same along the cross axis.
func (p *placement) mainPhys(pos, size float64) float64 {
	if p.ax.mainRev {
		return p.innerMain - pos - size
	}
	return pos
}

func (p *placement) crossPhys(pos, size float64) float64 {
	if p.ax.crossRev {
		return p.innerCross - pos - size
	}
	return pos
}

// inLine returns the physical offset of the item it from its line's
// top or left edge.
func (p *placement) inLine(it *fitem, ln *flexLine) float64 {
	if p.ax.crossRev {
		return ln.cross - it.cpos - it.outerCross()
	}
	return it.cpos
}

// dependsOnHeight reports an item whose content takes its height as a
// reference: a flex container, a table, or a box with a child whose
// height is a percentage. Another one stretches without a new layout.
func dependsOnHeight(k *box) bool {
	if k.flex != nil || k.grid != nil || k.tpart == partWrapper {
		return true
	}
	for _, c := range k.kids {
		if c.anon {
			if dependsOnHeight(c) {
				return true
			}
			continue
		}
		if c.st.Height.Kind == css.Percent || c.st.MinHeight.Kind == css.Percent || c.st.MaxHeight.Kind == css.Percent ||
			c.abs && (c.st.Top.Kind == css.Percent || c.st.Bottom.Kind == css.Percent || c.st.Height.Kind == css.Percent) {
			return true
		}
	}
	return false
}

// key returns the measure key of the item k at width w, height h when
// hDef.
func (p *placement) key(k *box, w, h float64, hDef bool) measureKey {
	if !hDef {
		h = 0
	}
	return measureKey{item: k, w: w, h: h, hDef: hDef, cbw: p.pcb.w, cbh: p.pcb.h, cbhDef: p.pcb.hDefinite}
}

// put lays the item k out with its border box at (x, y), its content
// width w and its height h when hDef, and returns its content height. A
// layout made off the page cuts at the same sizes moves into place when
// the item lands on one page, or the layout does not paginate.
func (p *placement) put(k *box, x, y, w, h float64, hDef bool) float64 {
	e := p.e
	if k.kind == kindReplaced {
		k.laidOut = true
		k.x, k.y, k.w, k.h = x, y, w, h
		return h
	}
	key := p.key(k, w, h, hDef)
	if cur, ok := p.fd.current[k]; ok && cur == key && k.laidOut {
		mh := k.h
		if !e.paginating() || e.pageOf(y) == e.pageOf(y+mh+vEdges(k)-metricSlack) {
			k.shift(x-k.x, y-k.y)
			if m, ok := p.fd.measured[key]; ok {
				return m.h
			}
			return k.h
		}
	}
	h0 := e.layoutItemAt(k, x, y, w, h, hDef, p.pcb)
	if e.paginating() {
		// A layout across the page cuts holds their struts: it moves
		// nowhere else.
		delete(p.fd.current, k)
	} else {
		p.fd.current[k] = key
	}
	return h0
}

// rows lays out the lines of a row container top down and returns the
// bottom of its content. A line moves to the next page when a forced
// break precedes it or when one of its items cannot start on the page
// the line starts on; the struts its items' content takes at page cuts
// make the line taller, and push the lines below.
func (p *placement) rows(lines []*flexLine) float64 {
	e := p.e
	order := slices.Clone(lines)
	if p.ax.crossRev {
		slices.Reverse(order)
	}
	shift := 0.0
	var prev *flexLine
	for _, ln := range order {
		if e.err != nil {
			break
		}
		lineTop := p.top + p.crossPhys(ln.pos, ln.cross) + shift
		if e.paginating() && prev != nil && breakBetweenLines(prev, ln) && !e.atPageTop(lineTop) {
			y := e.nextPage(lineTop)
			shift += y - lineTop
			lineTop = y
		}
		grow := p.rowLine(ln, lineTop)
		if e.paginating() && !e.atPageTop(lineTop) && p.pushLine(ln, lineTop) && e.relayout(lineTop) {
			y := e.nextPage(lineTop)
			shift += y - lineTop
			lineTop = y
			grow = p.rowLine(ln, lineTop)
		}
		shift += grow
		prev = ln
	}
	return p.top + p.innerCross + shift
}

// breakBetweenLines reports a forced break between the line a and the
// line b after it: break-after on an item of a or break-before on one of
// b (CSS Flexbox 1, 10).
func breakBetweenLines(a, b *flexLine) bool {
	for _, it := range a.items {
		if isForced(it.b.st.BreakAfter) {
			return true
		}
	}
	for _, it := range b.items {
		if isForced(it.b.st.BreakBefore) {
			return true
		}
	}
	return false
}

// rowLine lays out the items of the line ln at lineTop and returns how
// much the page cuts made it grow: an item of auto height takes the
// struts its content takes, a stretched one the line's growth.
func (p *placement) rowLine(ln *flexLine, lineTop float64) float64 {
	e := p.e
	grow := 0.0
	own := make([]float64, len(ln.items))
	for i, it := range ln.items {
		if e.err != nil {
			return grow
		}
		k := it.b
		x := p.cb.x + p.mainPhys(it.mpos, it.outerMain()) + k.m[css.Left]
		y := lineTop + p.inLine(it, ln) + k.m[css.Top]
		switch {
		case it.crossSet:
			p.put(k, x, y, it.target, it.cross, true)
		case it.stretch && (dependsOnHeight(k) || k.kind == kindReplaced):
			p.put(k, x, y, it.target, it.cross, true)
		default:
			got := p.put(k, x, y, it.target, 0, false)
			if it.cross+eps >= it.m.h && got > it.m.h+eps {
				own[i] = got - it.m.h
				grow = max(grow, own[i])
			}
		}
	}
	for i, it := range ln.items {
		it.b.h = it.cross + own[i]
		if it.stretch {
			it.b.h = it.cross + grow
		}
	}
	return grow
}

// pushLine reports a line laid out at lineTop that must move to the next
// page: one of its items is monolithic or avoids breaks inside, crosses a
// page cut and fits a page, or its content starts on a later page than
// the line, as Chromium starts the items of a line on the same page.
func (p *placement) pushLine(ln *flexLine, lineTop float64) bool {
	e := p.e
	page := e.pageOf(lineTop)
	for _, it := range ln.items {
		k := it.b
		if (k.kind == kindReplaced || avoidsInside(k)) && e.straddles(k) && k.borderH() <= e.areaH(page+1) {
			return true
		}
		if k.kind != kindReplaced && hasContent(k) && e.pageOf(firstContent(k)) > page {
			return true
		}
	}
	return false
}

// columns lays out each line of a column container top down and returns
// the bottom of its content: an item moves to the next page after a
// forced break, or when it avoids breaks inside, crosses a cut and fits a
// page; the struts its content takes push the items below.
func (p *placement) columns(lines []*flexLine) float64 {
	e := p.e
	bottom := p.top + p.innerMain
	for _, ln := range lines {
		order := slices.Clone(ln.items)
		if p.ax.mainRev {
			slices.Reverse(order)
		}
		shift := 0.0
		var prev *fitem
		for _, it := range order {
			if e.err != nil {
				return bottom
			}
			k := it.b
			x := p.cb.x + p.crossPhys(ln.pos, ln.cross) + p.inLine(it, ln) + k.m[css.Left]
			y := p.top + p.mainPhys(it.mpos, it.outerMain()) + shift + k.m[css.Top]
			if e.paginating() && prev != nil && (isForced(prev.b.st.BreakAfter) || isForced(k.st.BreakBefore)) {
				if m := y - k.m[css.Top]; !e.atPageTop(m) {
					ny := e.nextPage(m)
					shift += ny - m
					y += ny - m
				}
			}
			grow := p.columnItem(it, x, y)
			if e.paginating() && !e.atPageTop(y-k.m[css.Top]) && p.pushItem(k) && e.relayout(y) {
				m := y - k.m[css.Top]
				ny := e.nextPage(m)
				shift += ny - m
				y += ny - m
				grow = p.columnItem(it, x, y)
			}
			shift += grow
			prev = it
		}
		bottom = max(bottom, p.top+p.innerMain+shift)
	}
	return bottom
}

// columnItem lays out the item it of a column container at (x, y) and
// returns how much the page cuts made it grow.
func (p *placement) columnItem(it *fitem, x, y float64) float64 {
	k := it.b
	if k.tpart == partWrapper && k.tdata != nil {
		// A table takes its main size from the flex layout, its height
		// property aside, as Chromium sizes table flex items: the table
		// box, the wrapper's content, gets the item's main size.
		t := k.tdata.table
		pd, bd, _ := edgesOf(t, p.cb.w)
		h := it.target
		if t.st.BoxSizing != css.BorderBox {
			h -= pd[css.Top] + pd[css.Bottom] + bd[css.Top] + bd[css.Bottom]
		}
		st := *t.st
		st.Height = css.Pts(max(h, 0))
		st.MinHeight, st.MaxHeight = css.Pts(0), css.Dimension{Kind: css.None}
		t.st = &st
	}
	if k.kind == kindReplaced || dependsOnHeight(k) {
		p.put(k, x, y, it.cross, it.target, true)
		k.h = it.target
		if k.tpart == partWrapper && k.tdata != nil && len(k.kids) == 1 {
			// A table without rows takes no height of its own: as the
			// wrapper's sole content it fills the item's main size.
			if t := k.tdata.table; t.laidOut && t.borderH() < k.h {
				t.h = max(k.h-vEdges(t), 0)
			}
		}
		return 0
	}
	got := p.put(k, x, y, it.cross, 0, false)
	ref := got
	if m, ok := p.fd.measured[p.key(k, it.cross, 0, false)]; ok {
		ref = m.h
	}
	grow := max(got-ref, 0)
	k.h = it.target + grow
	return grow
}

// pushItem reports an item of a column that must move to the next page:
// it is monolithic or avoids breaks inside, crosses a page cut and fits a
// page, or its content starts on a later page than its top.
func (p *placement) pushItem(k *box) bool {
	e := p.e
	page := e.pageOf(k.y)
	if (k.kind == kindReplaced || avoidsInside(k)) && e.straddles(k) && k.borderH() <= e.areaH(page+1) {
		return true
	}
	return k.kind != kindReplaced && hasContent(k) && e.pageOf(firstContent(k)) > page
}

// measureItem returns the measurement of the item it laid out off the
// page cuts at content width w and height h when hDef, in the containing
// block pcb, at (x, y): cached, so an item is measured once per size.
func (e *engine) measureItem(fd *flexData, it *fitem, w, h float64, hDef bool, pcb cblock, x, y float64) itemMeasure {
	k := it.b
	if !hDef {
		h = 0
	}
	key := measureKey{item: k, w: w, h: h, hDef: hDef, cbw: pcb.w, cbh: pcb.h, cbhDef: pcb.hDefinite}
	if m, ok := fd.measured[key]; ok {
		return m
	}
	if e.flexPasses >= maxFlexPasses {
		e.warnOnce(codeRelayoutBudget, fmt.Sprintf("more than %d flex item layouts: later items keep their last layout", maxFlexPasses), y)
		return itemMeasure{h: k.h}
	}
	e.flexPasses++
	e.noPaginate++
	ch := e.layoutItemAt(k, x, y, w, h, hDef, pcb)
	e.noPaginate--
	m := itemMeasure{h: ch}
	if v, ok := firstBaseline(k); ok {
		m.first, m.hasFirst = v-k.y, true
	}
	if v, ok := lastBaseline(k); ok {
		m.last, m.hasLast = v-k.y, true
	}
	fd.measured[key] = m
	fd.current[k] = key
	return m
}

// layoutItemAt lays out the content of the flex item k with its border
// box at (x, y), its content width w and its height h when hDef, in the
// flex container's content box pcb, and returns the height of its
// content. An item establishes an independent formatting context (CSS
// Flexbox 1, 4): its floats stay inside and its margins do not collapse
// with its content's.
func (e *engine) layoutItemAt(k *box, x, y, w, h float64, hDef bool, pcb cblock) float64 {
	k.laidOut = true
	k.x, k.y, k.w = x, y, w
	if k.kind == kindReplaced {
		k.h = h
		return h
	}
	own := &bfc{}
	mc := &marginState{kept: true}
	kcb := wrapperCB(k, cblock{x: k.contentX(), w: w, h: h, hDefinite: hDef}, pcb)
	saved := e.enterClamp(k, pcb, k.contentY())
	end := formatterFor(k).layout(e, k, kcb, own, k.contentY(), mc)
	e.leaveClamp(saved)
	end = e.pastMargin(end, mc.strut, false)
	mc.commit(end)
	end = max(end, own.bottom(), k.contentY())
	content := end - k.contentY()
	k.h = content
	if hDef {
		k.h = h
	}
	e.placeMarker(k)
	e.reach(k.y + k.borderH())
	return content
}

// flexBaselines sets the first and last main-axis baselines of the flex
// container b (CSS Flexbox 1, 8.5): in a row, the shared baseline of the
// first line's baseline-aligned items, else the baseline of its
// startmost item, the last line's alike; in a column, the first item's
// first baseline and the last item's last one. An item without a
// baseline lends its border box's bottom edge.
func (e *engine) flexBaselines(b *box, lines []*flexLine, ax flexAxes) {
	fd := b.flex
	if len(lines) == 0 || len(lines[0].items) == 0 {
		return
	}
	of := func(it *fitem, last bool) float64 {
		k := it.b
		if last {
			if v, ok := lastBaseline(k); ok {
				return v
			}
		} else if v, ok := firstBaseline(k); ok {
			return v
		}
		return k.y + k.borderH()
	}
	pick := func(ln *flexLine, last bool) float64 {
		if ax.row {
			for _, it := range ln.items {
				if bl, l := it.baseline(); bl && l == last {
					return of(it, last)
				}
			}
		}
		it := ln.items[0]
		if last {
			it = ln.items[len(ln.items)-1]
		}
		return of(it, last)
	}
	fd.first, fd.hasFirst = pick(lines[0], false)-b.y, true
	lastLine := lines[len(lines)-1]
	if len(lastLine.items) > 0 {
		fd.last, fd.hasLast = pick(lastLine, true)-b.y, true
	}
}

// flexBaseline returns the first or last baseline of the flex container
// b in flow coordinates.
func flexBaseline(b *box, last bool) (float64, bool) {
	fd := b.flex
	if last {
		return b.y + fd.last, fd.hasLast
	}
	return b.y + fd.first, fd.hasFirst
}

// flexStaticShift returns how far the absolutely positioned child b of a
// flex container moves from the content box corner its static position
// records, horizontally or vertically, once its margin box is sized: it
// sits where it would as the container's sole flex item, placed by
// justify-content along the main axis and by its align-self along the
// cross axis (CSS Flexbox 1, 4.1).
func flexStaticShift(b *box, horizontal bool) float64 {
	c := b.parent
	if c != nil && c.grid != nil && b.staticSet {
		return gridStaticShift(b, horizontal)
	}
	if c == nil || c.flex == nil || !b.staticSet {
		return 0
	}
	ax := axesOf(c.st)
	size, space := b.marginW(), c.w
	if !horizontal {
		size, space = b.marginH(), c.h
	}
	free := space - size
	var off float64
	var rev bool
	if horizontal == ax.row {
		off, _ = distribute(c.st.JustifyContent, free, 1, ax.mainStartIsStart(), ax.row, ax.mainRev)
		rev = ax.mainRev
	} else {
		a := usedAlign(b.st, c.st)
		switch kw := a.Keyword(); kw { //nolint:exhaustive // the other values align at cross-start
		case css.BoxFlexEnd:
			off = free
		case css.BoxCenter:
			off = free / 2
		case css.BoxStart, css.BoxSelfStart:
			if !ax.crossStartIsStart() {
				off = free
			}
		case css.BoxEnd, css.BoxSelfEnd:
			if ax.crossStartIsStart() {
				off = free
			}
		}
		if a.Safe() && free < 0 {
			off = 0
		}
		rev = ax.crossRev
	}
	if rev {
		return free - off
	}
	return off
}

// maxFlexPasses bounds the layouts of flex items made to measure them in
// one Layout call: past it, an item keeps its last layout as its
// measurement, with a warning. Anti-DoS guard, not spec-mandated: an
// item is measured once or twice per container layout, and nested flex
// containers measured at every level would otherwise multiply the work
// per level; a long document of flex rows measures some tens of
// thousands of items.
const maxFlexPasses = 1 << 20
