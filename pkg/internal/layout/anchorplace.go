package layout

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// alignKind is how an anchored box is aligned in one physical axis of its
// inset-modified containing block (CSS Box Alignment 3, 6; CSS Anchor
// Positioning 1, 4): by the rules of CSS 2.1 (alignNormal), stretched,
// at the low edge (left or top), at the high edge, centred, or centred on
// the default anchor.
type alignKind uint8

const (
	alignNormal alignKind = iota
	alignStretch
	alignLow
	alignHigh
	alignCenter
	alignAnchorCenter
)

// anchorPlan is one position option resolved for a box: the style with
// its anchor functions substituted, the containing block, possibly the
// position-area's, the inset-modified containing block, and the
// alignment in each axis.
type anchorPlan struct {
	st *css.Style
	cb absCB
	// orig is the containing block before position-area, the one the box
	// shifts into when it overflows its inset-modified containing block;
	// an end edge at infinity is one that grows with the document.
	orig edges
	// imcb is the inset-modified containing block.
	imcb   edges
	ax, ay alignKind
	// over is, for the horizontal then the vertical axis, the overflow
	// position of the self-alignment, and startLow reports an axis whose
	// start is its low edge in the containing block's writing mode.
	over     [2]overflowPos
	startLow [2]bool
	// limit is the containing block before any scrollable area grows it,
	// which a safe alignment keeps the box inside.
	limit edges
	def   *box
	defR  edges
	// refsDefault reports a box that references its default anchor
	// (position-visibility: anchor-valid); missing one whose anchor
	// function found no anchor.
	refsDefault, missing bool
	// negative reports an inset-modified containing block of negative
	// size, which no position option takes (CSS Anchor Positioning 1,
	// 5.4.1).
	negative bool
	page     int
}

// physArea is a position-area resolved to the tracks it selects in each
// physical axis, bit [css.TrackStart] the left or top track.
type physArea struct{ x, y uint8 }

// reverseTracks swaps the start and end tracks of a track set.
func reverseTracks(t uint8) uint8 {
	out := t & css.TrackCenter
	if t&css.TrackStart != 0 {
		out |= css.TrackEnd
	}
	if t&css.TrackEnd != 0 {
		out |= css.TrackStart
	}
	return out
}

// spanAxis returns whether the span sp of a position-area lies on the
// horizontal axis and whether its tracks run from the low coordinate, in a
// containing block of axes cb for a box of axes self (CSS Anchor
// Positioning 1, 3.1.2).
func spanAxis(sp css.AreaSpan, cb, self flowAxes) (bool, bool) {
	axes := cb
	if sp.Ref == css.AreaSelf {
		axes = self
	}
	switch sp.Axis {
	case css.AreaX, css.AreaY:
		h := sp.Axis == css.AreaX
		if sp.Ref == css.AreaPhysical {
			return h, true
		}
		return h, axes.startLow(h)
	case css.AreaBlock:
		return axes.blockX, axes.blockLow
	case css.AreaInline:
	}
	return !axes.blockX, axes.inlineL
}

// resolveArea returns the physical tracks a position-area selects.
func resolveArea(p css.PositionArea, cb, self flowAxes) physArea {
	var out physArea
	for _, sp := range [2]css.AreaSpan{p.A, p.B} {
		h, low := spanAxis(sp, cb, self)
		t := sp.Tracks
		if !low {
			t = reverseTracks(t)
		}
		if h {
			out.x = t
		} else {
			out.y = t
		}
	}
	return out
}

// trackRange returns the low and high edges of the tracks t of an axis
// whose containing block spans [lo, hi] and whose anchor spans [a0, a1]:
// the position-area grid's lines are the start of the containing block or
// of the anchor, whichever comes first, the anchor's two edges, and the
// end of the containing block or of the anchor, whichever comes last (CSS
// Anchor Positioning 1, 3.1.1).
func trackRange(t uint8, lo, hi, a0, a1 float64) (float64, float64) {
	starts := [3]float64{min(lo, a0), a0, a1}
	ends := [3]float64{a0, a1, max(hi, a1)}
	from, to := math.Inf(1), math.Inf(-1)
	for i := range starts {
		if t&(1<<i) != 0 {
			from, to = min(from, starts[i]), max(to, ends[i])
		}
	}
	if from > to {
		return lo, hi
	}
	return from, to
}

// areaAlign returns the alignment position-area gives normal in an axis
// whose selected tracks are t (CSS Anchor Positioning 1, 4.1): centre for
// the centre track alone, anchor-center for all three, else toward the
// track left out, at the edge away from it.
func areaAlign(t uint8) alignKind {
	switch {
	case t == css.TrackCenter:
		return alignCenter
	case t == css.TrackAll:
		return alignAnchorCenter
	case t&css.TrackStart != 0:
		return alignHigh
	}
	return alignLow
}

// selfAlign returns the physical alignment of the self-alignment value a
// in the horizontal axis when horizontal, the containing block's axes cb
// and the box's self (CSS Box Alignment 3, 4.2 and 6.1), alignNormal for
// normal and auto. hasDefault reports a default anchor, without which
// anchor-center is centre.
func selfAlign(a css.BoxAlign, horizontal bool, cb, self flowAxes, hasDefault bool) alignKind {
	at := func(low bool) alignKind {
		if low {
			return alignLow
		}
		return alignHigh
	}
	switch a.Keyword() {
	case css.BoxStretch:
		return alignStretch
	case css.BoxCenter:
		return alignCenter
	case css.BoxStart, css.BoxFlexStart, css.BoxBaseline, css.BoxLastBaseline:
		return at(cb.startLow(horizontal))
	case css.BoxEnd, css.BoxFlexEnd:
		return at(!cb.startLow(horizontal))
	case css.BoxSelfStart:
		return at(self.startLow(horizontal))
	case css.BoxSelfEnd:
		return at(!self.startLow(horizontal))
	case css.BoxLeft:
		return alignLow
	case css.BoxRight:
		return alignHigh
	case css.BoxAnchorCenter:
		if hasDefault {
			return alignAnchorCenter
		}
		return alignCenter
	case css.BoxNormal, css.BoxAuto, css.BoxSpaceBetween, css.BoxSpaceAround, css.BoxSpaceEvenly, css.BoxSafe, css.BoxUnsafe:
	}
	return alignNormal
}

// scrollableCB returns the containing block a box with a default anchor
// takes in place of cb (CSS Anchor Positioning 1, 2.3): the initial
// containing block, which the root's scroller generates, grows to the
// document's height, the pages the flow takes.
func (e *engine) scrollableCB(cb absCB) absCB {
	if cb.box != nil || cb.page || cb.anchored || e.rootBox == nil {
		return cb
	}
	rb := e.rootBox
	cb.h = max(cb.h, rb.y+rb.borderH()+rb.m[css.Bottom]-cb.y)
	return cb
}

// planAnchored resolves the position option st of the absolutely
// positioned box b in its containing block cb.
func (e *engine) planAnchored(b *box, st *css.Style, cb absCB) *anchorPlan {
	a := st.AnchorPositioning()
	self := e.axesIn(st, e.frameOf(b))
	cbAxes := e.icbAxes()
	if cb.box != nil {
		cbAxes = e.boxAxes(cb.box)
	}
	s := &anchorScope{e: e, b: b, cb: cb, cbAxes: cbAxes, self: self, page: -1, rects: map[*box]edges{}}
	s.def = e.defaultAnchor(b, st, 0)
	if s.def != nil {
		cb = e.scrollableCB(cb)
		s.defR = s.rectOf(s.def)
	}
	plan := &anchorPlan{def: s.def, defR: s.defR, orig: edges{cb.x, cb.y, cb.x + cb.w, cb.y + cb.h}}
	if g := cb.box; g != nil && g.grid != nil {
		// The grid area a grid container's placement names is already an
		// adjustment of the containing block: the original one is the
		// container's padding box (CSS Position 3, 2.1.1).
		plan.orig = edges{g.x + g.bd[css.Left], g.y + g.bd[css.Top], g.x + g.bd[css.Left] + g.w + g.pd[css.Left] + g.pd[css.Right], g.y + g.bd[css.Top] + g.h + g.pd[css.Top] + g.pd[css.Bottom]}
	}
	var zero [2]bool
	var area physArea
	if !a.Area.None() {
		plan.refsDefault = true
		if s.def != nil {
			area = resolveArea(a.Area, cbAxes, self)
			x0, x1 := trackRange(area.x, cb.x, cb.x+cb.w, s.defR.x0, s.defR.x1)
			y0, y1 := trackRange(area.y, cb.y, cb.y+cb.h, s.defR.y0, s.defR.y1)
			cb.x, cb.y, cb.w, cb.h = x0, y0, x1-x0, y1-y0
			zero = [2]bool{true, true}
		}
	}
	inlineH := !cbAxes.blockX
	hAlign, vAlign := st.JustifySelf, st.AlignSelf
	if !inlineH {
		hAlign, vAlign = vAlign, hAlign
	}
	for i, al := range [2]css.BoxAlign{hAlign, vAlign} {
		if al.Keyword() == css.BoxAnchorCenter {
			plan.refsDefault = true
			if s.def != nil {
				zero[i] = true
			}
		}
	}
	s.cb = cb
	sub := s.substitute(st)
	plan.missing, plan.page = s.missing, s.page
	for _, d := range [...]css.Dimension{
		st.Top, st.Right, st.Bottom, st.Left, st.Width, st.Height, st.MinWidth,
		st.MinHeight, st.MaxWidth, st.MaxHeight, st.Margin[0], st.Margin[1], st.Margin[2], st.Margin[3],
	} {
		if d.Anchored() && d.UsesDefaultAnchor() {
			plan.refsDefault = true
		}
	}
	// The single auto inset of an axis, before position-area makes it 0,
	// turns the area's default alignment toward the other edge (4.1).
	single := [2]alignKind{}
	for i, pair := range [2][2]css.Dimension{{sub.Left, sub.Right}, {sub.Top, sub.Bottom}} {
		la, ha := pair[0].Kind == css.Auto, pair[1].Kind == css.Auto
		switch {
		case la && !ha:
			single[i] = alignHigh
		case ha && !la:
			single[i] = alignLow
		}
	}
	for i, z := range zero {
		if !z {
			continue
		}
		lo, hi := css.Left, css.Right
		if i == 1 {
			lo, hi = css.Top, css.Bottom
		}
		ins := [4]*css.Dimension{&sub.Top, &sub.Right, &sub.Bottom, &sub.Left}
		for _, side := range [2]css.Side{lo, hi} {
			if ins[side].Kind == css.Auto {
				*ins[side] = css.Pts(0)
			}
			if sub.Margin[side].Kind == css.Auto {
				sub.Margin[side] = css.Pts(0)
			}
		}
	}
	tracks := [2]uint8{area.x, area.y}
	for i, al := range [2]css.BoxAlign{hAlign, vAlign} {
		k := selfAlign(al, i == 0, cbAxes, self, s.def != nil)
		if k == alignNormal && tracks[i] != 0 {
			k = areaAlign(tracks[i])
			if single[i] != alignNormal {
				k = single[i]
			}
		}
		if k != alignNormal && !zero[i] {
			// An alignment the inset-modified containing block cannot
			// hold, an axis with an auto inset, is left to CSS 2.1.
			pair := [2]css.Dimension{sub.Left, sub.Right}
			if i == 1 {
				pair = [2]css.Dimension{sub.Top, sub.Bottom}
			}
			if pair[0].Kind == css.Auto || pair[1].Kind == css.Auto {
				k = alignNormal
			}
		}
		if i == 0 {
			plan.ax = k
		} else {
			plan.ay = k
		}
		plan.over[i], plan.startLow[i] = overflowOf(al), cbAxes.startLow(i == 0)
	}
	plan.st, plan.cb, plan.limit = sub, cb, plan.orig
	plan.imcb = e.insetModified(b, sub, cb)
	plan.negative = plan.imcb.x1 < plan.imcb.x0-eps || plan.imcb.y1 < plan.imcb.y0-eps
	if s.def != nil && cb.page && !s.def.fixed && e.rootBox != nil {
		// A fixed box whose default anchor scrolls with the document
		// shifts within the document's scrollable overflow area, the
		// viewport or the root's box where that is larger (CSS
		// Box Alignment 3, 4.4.1.2: its overflow limit rectangle).
		rb := e.rootBox
		w := rb.x + rb.borderW() + max(rb.m[css.Right], 0) - cb.x
		if e.rtlRoot {
			plan.orig.x0 = min(plan.orig.x0, plan.orig.x1-w)
		} else {
			plan.orig.x1 = max(plan.orig.x1, cb.x+w)
		}
		// A fixed box's coordinates are those of the page its anchor
		// starts on.
		plan.orig.y1 = max(plan.orig.y1, rb.y+rb.borderH()+max(rb.m[css.Bottom], 0)-e.pageTop(max(s.page, 0)))
	}
	if s.def != nil && cb.box == nil && !cb.page && !cb.anchored {
		// The scrollable containing block of the root grows with what
		// overflows its end edges, the box itself included: a box never
		// shifts back from them, its inline end and its bottom. Its options are still tried against the
		// page's width and the document's height.
		plan.orig.y1 = math.Inf(1)
		if e.rtlRoot {
			// A right-to-left root scrolls leftward.
			plan.orig.x0 = math.Inf(-1)
		} else {
			plan.orig.x1 = math.Inf(1)
		}
	}
	return plan
}

// insetModified returns the inset-modified containing block of a box
// styled st in cb (CSS Position 3, 4.1): an auto inset of an axis whose
// other inset is set stands at the containing block's edge, and two auto
// insets at the static position and the containing block's far edge.
func (e *engine) insetModified(b *box, st *css.Style, cb absCB) edges {
	l, lSet := insetOf(st.Left, cb.w)
	r, rSet := insetOf(st.Right, cb.w)
	t, tSet := insetOf(st.Top, cb.h)
	bo, bSet := insetOf(st.Bottom, cb.h)
	if !lSet && !rSet {
		if b.srtl {
			r = cb.x + cb.w - b.sx
		} else {
			l = b.sx - cb.x
		}
	}
	if !tSet && !bSet {
		t = b.sy - cb.y
	}
	return edges{cb.x + l, cb.y + t, cb.x + cb.w - r, cb.y + cb.h - bo}
}

// fitsPlan reports whether b, laid out by plan, has its margin box inside
// the plan's inset-modified containing block (CSS Anchor Positioning 1,
// 5.4.1).
func fitsPlan(b *box, plan *anchorPlan) bool {
	m := edges{b.x - b.m[css.Left], b.y - b.m[css.Top], b.x + b.borderW() + b.m[css.Right], b.y + b.borderH() + b.m[css.Bottom]}
	c := plan.imcb
	return m.x0 >= c.x0-eps && m.y0 >= c.y0-eps && m.x1 <= c.x1+eps && m.y1 <= c.y1+eps
}

// tryEntry is a position option: its style, and its entry, 0 for the
// fallback base styles and i for the i-th entry of position-try-fallbacks.
type tryEntry struct {
	st    *css.Style
	entry int
	// try is the entry's accepted @position-try properties, nil for the
	// base styles.
	try *css.TryStyle
}

// positionOptions returns the position options list of a box whose
// fallback base styles are base (CSS Anchor Positioning 1, 5.1): the base
// styles, then each entry of position-try-fallbacks that makes one, in the
// order position-try-order sorts them by the room each leaves (5.2), at
// most maxTryOptions entries.
func (e *engine) positionOptions(b *box, base *css.Style, cb absCB) []tryEntry {
	a := base.AnchorPositioning()
	out := make([]tryEntry, 1, 1+len(a.Fallbacks))
	out[0] = tryEntry{st: base}
	cbAxes := e.icbAxes()
	if cb.box != nil {
		cbAxes = e.boxAxes(cb.box)
	}
	for i, o := range a.Fallbacks {
		var t css.TryStyle
		switch {
		case !o.Area.None():
			t = base.TryBase()
			t.Area = o.Area
		case o.Rule != "" && o.Try == nil:
			continue
		case o.Try != nil:
			t = *o.Try
		default:
			t = base.TryBase()
		}
		for _, tt := range o.Tactics {
			applyTactic(&t, tt, cbAxes)
		}
		out = append(out, tryEntry{st: base.WithTry(&t), entry: i + 1, try: &t})
	}
	if a.Order == css.TryNormal || len(out) < 2 {
		return out
	}
	room := make(map[int]float64, len(out))
	for _, o := range out {
		p := e.planAnchored(b, o.st, cb)
		w, h := max(p.imcb.x1-p.imcb.x0, 0), max(p.imcb.y1-p.imcb.y0, 0)
		width := true
		switch a.Order {
		case css.TryMostHeight:
			width = false
		case css.TryMostBlockSize:
			width = cbAxes.blockX
		case css.TryMostInlineSize:
			width = !cbAxes.blockX
		case css.TryMostWidth, css.TryNormal:
		}
		room[o.entry] = h
		if width {
			room[o.entry] = w
		}
	}
	slices.SortStableFunc(out, func(x, y tryEntry) int {
		switch {
		case room[x.entry] > room[y.entry]:
			return -1
		case room[x.entry] < room[y.entry]:
			return 1
		}
		return 0
	})
	return out
}

// applyTactic executes the try tactic tt on t in a containing block of
// axes cb (CSS Anchor Positioning 1, 5.4.2): flip-block and flip-inline
// mirror the axis the containing block's writing mode names, flip-x and
// flip-y the physical axes, flip-start the diagonal from the block-start
// inline-start corner.
func applyTactic(t *css.TryStyle, tt css.TryTactic, cb flowAxes) {
	switch tt {
	case css.FlipX:
		flipAxis(t, true, cb)
	case css.FlipY:
		flipAxis(t, false, cb)
	case css.FlipBlock:
		flipAxis(t, cb.blockX, cb)
	case css.FlipInline:
		flipAxis(t, !cb.blockX, cb)
	case css.FlipStart:
		// The corner the start sides meet in: top-left or bottom-right
		// is the main diagonal, the others the other one, which is the
		// main one between two horizontal mirrors.
		hLow, vLow := cb.startLow(true), cb.startLow(false)
		if hLow == vLow {
			transpose(t)
			return
		}
		flipAxis(t, true, cb)
		transpose(t)
		flipAxis(t, true, cb)
	}
}

// flipAxis mirrors t in the horizontal axis when horizontal, the vertical
// one otherwise: the insets and margins of the axis swap, the anchor()
// functions of its insets name the mirrored side, a percentage p becoming
// 100% - p, its self-alignment turns, and position-area takes the
// mirrored tracks (CSS Anchor Positioning 1, 5.4.2).
func flipAxis(t *css.TryStyle, horizontal bool, cb flowAxes) {
	lo, hi := css.Top, css.Bottom
	if horizontal {
		lo, hi = css.Left, css.Right
	}
	t.Inset[lo], t.Inset[hi] = t.Inset[hi], t.Inset[lo]
	t.Margin[lo], t.Margin[hi] = t.Margin[hi], t.Margin[lo]
	mirror := func(f css.AnchorFn) css.AnchorFn {
		if f.Size {
			return f
		}
		switch f.Side {
		case css.AnchorTop:
			f.Side = css.AnchorBottom
		case css.AnchorBottom:
			f.Side = css.AnchorTop
		case css.AnchorLeft:
			f.Side = css.AnchorRight
		case css.AnchorRight:
			f.Side = css.AnchorLeft
		case css.AnchorStart:
			f.Side = css.AnchorEnd
		case css.AnchorEnd:
			f.Side = css.AnchorStart
		case css.AnchorSelfStart:
			f.Side = css.AnchorSelfEnd
		case css.AnchorSelfEnd:
			f.Side = css.AnchorSelfStart
		case css.AnchorPercent:
			f.Percent = 1 - f.Percent
		case css.AnchorInside, css.AnchorOutside:
		}
		return f
	}
	t.Inset[lo], t.Inset[hi] = t.Inset[lo].MapAnchors(mirror), t.Inset[hi].MapAnchors(mirror)
	al := &t.AlignSelf
	if horizontal == !cb.blockX {
		al = &t.JustifySelf
	}
	*al = mirrorAlign(*al)
	t.Area = mirrorArea(t.Area, horizontal, cb)
}

// mirrorAlign returns the self-alignment a takes when its axis is
// mirrored: the start and end positions swap.
func mirrorAlign(a css.BoxAlign) css.BoxAlign {
	safe := a & (css.BoxSafe | css.BoxUnsafe)
	k := a.Keyword()
	switch k {
	case css.BoxStart:
		k = css.BoxEnd
	case css.BoxEnd:
		k = css.BoxStart
	case css.BoxFlexStart:
		k = css.BoxFlexEnd
	case css.BoxFlexEnd:
		k = css.BoxFlexStart
	case css.BoxSelfStart:
		k = css.BoxSelfEnd
	case css.BoxSelfEnd:
		k = css.BoxSelfStart
	case css.BoxLeft:
		k = css.BoxRight
	case css.BoxRight:
		k = css.BoxLeft
	case css.BoxNormal, css.BoxAuto, css.BoxStretch, css.BoxBaseline, css.BoxLastBaseline, css.BoxCenter,
		css.BoxSpaceBetween, css.BoxSpaceAround, css.BoxSpaceEvenly, css.BoxAnchorCenter, css.BoxSafe, css.BoxUnsafe:
	}
	return k | safe
}

// mirrorArea returns p with the tracks of its span on the mirrored axis
// reversed.
func mirrorArea(p css.PositionArea, horizontal bool, cb flowAxes) css.PositionArea {
	if p.None() {
		return p
	}
	for _, sp := range [2]*css.AreaSpan{&p.A, &p.B} {
		// A self- span reads the box's own writing mode; litho takes the
		// containing block's to tell its axis, the two agreeing in a
		// document of one writing mode.
		if h, _ := spanAxis(*sp, cb, cb); h == horizontal {
			sp.Tracks = reverseTracks(sp.Tracks)
		}
	}
	return p
}

// transpose mirrors t across the main diagonal: top and left swap, bottom
// and right, the sizes of the two axes, the two self-alignment
// properties, the sides and sizes the anchor functions name, and the axes
// of position-area (CSS Anchor Positioning 1, 5.4.2).
func transpose(t *css.TryStyle) {
	t.Inset[css.Top], t.Inset[css.Left] = t.Inset[css.Left], t.Inset[css.Top]
	t.Inset[css.Bottom], t.Inset[css.Right] = t.Inset[css.Right], t.Inset[css.Bottom]
	t.Margin[css.Top], t.Margin[css.Left] = t.Margin[css.Left], t.Margin[css.Top]
	t.Margin[css.Bottom], t.Margin[css.Right] = t.Margin[css.Right], t.Margin[css.Bottom]
	t.Width, t.Height = t.Height, t.Width
	t.MinWidth, t.MinHeight = t.MinHeight, t.MinWidth
	t.MaxWidth, t.MaxHeight = t.MaxHeight, t.MaxWidth
	t.MinWidthAuto, t.MinHeightAuto = t.MinHeightAuto, t.MinWidthAuto
	ja, aa := t.JustifySelf, t.AlignSelf
	t.AlignSelf, t.JustifySelf = leftRightLess(ja), aa
	swap := func(f css.AnchorFn) css.AnchorFn {
		if f.Size {
			switch f.Dim {
			case css.AnchorWidth:
				f.Dim = css.AnchorHeight
			case css.AnchorHeight:
				f.Dim = css.AnchorWidth
			case css.AnchorBlock:
				f.Dim = css.AnchorInline
			case css.AnchorInline:
				f.Dim = css.AnchorBlock
			case css.AnchorSelfBlock:
				f.Dim = css.AnchorSelfInline
			case css.AnchorSelfInline:
				f.Dim = css.AnchorSelfBlock
			case css.AnchorDimAuto:
			}
			return f
		}
		switch f.Side {
		case css.AnchorTop:
			f.Side = css.AnchorLeft
		case css.AnchorLeft:
			f.Side = css.AnchorTop
		case css.AnchorBottom:
			f.Side = css.AnchorRight
		case css.AnchorRight:
			f.Side = css.AnchorBottom
		case css.AnchorInside, css.AnchorOutside, css.AnchorStart, css.AnchorEnd, css.AnchorSelfStart,
			css.AnchorSelfEnd, css.AnchorPercent:
		}
		return f
	}
	for i := range t.Inset {
		t.Inset[i] = t.Inset[i].MapAnchors(swap)
		t.Margin[i] = t.Margin[i].MapAnchors(swap)
	}
	for _, d := range []*css.Dimension{&t.Width, &t.Height, &t.MinWidth, &t.MinHeight, &t.MaxWidth, &t.MaxHeight} {
		*d = d.MapAnchors(swap)
	}
	if !t.Area.None() {
		for _, sp := range [2]*css.AreaSpan{&t.Area.A, &t.Area.B} {
			switch sp.Axis {
			case css.AreaX:
				sp.Axis = css.AreaY
			case css.AreaY:
				sp.Axis = css.AreaX
			case css.AreaBlock:
				sp.Axis = css.AreaInline
			case css.AreaInline:
				sp.Axis = css.AreaBlock
			}
		}
	}
}

// leftRightLess returns a justify-self value moved to align-self, which
// takes no left or right: they become start and end.
func leftRightLess(a css.BoxAlign) css.BoxAlign {
	switch a.Keyword() { //nolint:exhaustive // only left and right have no align-self form
	case css.BoxLeft:
		return css.BoxStart | a&(css.BoxSafe|css.BoxUnsafe)
	case css.BoxRight:
		return css.BoxEnd | a&(css.BoxSafe|css.BoxUnsafe)
	}
	return a
}

// layoutAnchored lays out the absolutely positioned box b, which uses
// anchor positioning, in its containing block cb: it tries the position
// options in order, laying the box out off the page cuts for each, takes
// the first whose margin box fits its inset-modified containing block,
// the base styles when none does, and lays the box out with it (CSS
// Anchor Positioning 1, 5.4.1). position-visibility then hides it when an
// anchor it needs is missing, invisible or clipped, or when it overflows
// (5.5).
func (e *engine) layoutAnchored(b *box, cb absCB) {
	base, ok := e.anchorBase[b]
	if !ok {
		// A style that holds the option an earlier layout chose tries
		// its options from its own base styles.
		base = b.st.FallbackBase()
		if e.anchorBase == nil {
			e.anchorBase = map[*box]*css.Style{}
		}
		e.anchorBase[b] = base
	}
	options := e.positionOptions(b, base, cb)
	// The option an earlier layout of the document took is the last
	// successful one: the box keeps it while it fits (5.4.1.1).
	last, kept := e.opts.PositionChoices[b.seq]
	if kept {
		kept = slices.ContainsFunc(options, func(o tryEntry) bool { return o.entry == last })
	}
	var chosen *anchorPlan
	entry := 0
	var try *css.TryStyle
	if len(options) > 1 {
		seen := e.seenPage
		e.noPaginate++
		if kept {
			i := slices.IndexFunc(options, func(o tryEntry) bool { return o.entry == last })
			options = append([]tryEntry{options[i]}, slices.Delete(slices.Clone(options), i, i+1)...)
		}
		for _, o := range options {
			plan := e.planAnchored(b, o.st, cb)
			if plan.negative {
				continue
			}
			e.layoutPlan(b, plan)
			if e.err != nil {
				break
			}
			if fitsPlan(b, plan) {
				chosen, entry, try = plan, o.entry, o.try
				break
			}
		}
		e.noPaginate--
		e.seenPage = seen
	}
	if chosen == nil {
		st := base
		if kept {
			// No option fits: the box keeps the one it had.
			st, entry, try = options[0].st, last, options[0].try
		}
		chosen = e.planAnchored(b, st, cb)
	}
	if e.choices == nil {
		e.choices = map[int]int{}
	}
	e.choices[b.seq] = entry
	if try != nil && b.node != nil {
		if e.options == nil {
			e.options = map[*Node]PositionOption{}
		}
		e.options[b.node] = PositionOption{Entry: entry, Try: try}
	}
	e.layoutPlan(b, chosen)
	e.holdStyle(b.st)
	if chosen.page >= 0 {
		b.anchorPage = chosen.page
	}
	vis := chosen.st.AnchorPositioning().Visibility
	switch {
	case vis.Has(css.VisibilityAlways):
	case vis.Has(css.VisibilityAnchorValid) && chosen.refsDefault && chosen.def == nil,
		vis.Has(css.VisibilityAnchorVisible) && chosen.def != nil && (chosen.def.hidden || e.clippedAnchor(chosen.def, b)),
		vis.Has(css.VisibilityNoOverflow) && !fitsPlan(b, chosen):
		hideTree(b, 0)
	}
}

// selfAligned reports an absolutely positioned box styled st that its
// self-alignment places in its inset-modified containing block (CSS
// Position 3, 4.3): an axis whose two insets are set and whose
// justify-self or align-self is a position, which the rules of CSS 2.1
// do not apply. normal, auto and stretch align as CSS 2.1 does, and an
// axis with an auto inset is aligned by its static position.
func selfAligned(st *css.Style) bool {
	positional := func(a css.BoxAlign) bool {
		switch a.Keyword() {
		case css.BoxNormal, css.BoxAuto, css.BoxStretch, css.BoxSpaceBetween, css.BoxSpaceAround, css.BoxSpaceEvenly:
			return false
		case css.BoxStart, css.BoxEnd, css.BoxCenter, css.BoxFlexStart, css.BoxFlexEnd, css.BoxSelfStart, css.BoxSelfEnd,
			css.BoxLeft, css.BoxRight, css.BoxBaseline, css.BoxLastBaseline, css.BoxAnchorCenter, css.BoxSafe, css.BoxUnsafe:
		}
		return true
	}
	set := func(d css.Dimension) bool { return d.Kind != css.Auto }
	return positional(st.JustifySelf) && set(st.Left) && set(st.Right) ||
		positional(st.AlignSelf) && set(st.Top) && set(st.Bottom)
}

// layoutPlan lays b out by plan: by the rules of CSS 2.1 in an axis
// aligned normally, else in the range of the inset-modified containing
// block its alignment gives it, shifted into the containing block before
// position-area when it overflows that range and fits there (CSS Anchor
// Positioning 1, 4.1, the default overflow alignment).
func (e *engine) layoutPlan(b *box, plan *anchorPlan) {
	st := *plan.st
	cb := plan.cb
	c := plan.imcb
	// sized is the inset-modified containing block, the room the box is
	// sized in (CSS Position 3, 4.1); ranges are where it aligns, which
	// anchor-center narrows to the widest range centred on the anchor.
	sized := [2][2]float64{{c.x0, c.x1}, {c.y0, c.y1}}
	for i := range sized {
		if math.IsInf(sized[i][1], 1) {
			sized[i][1] = [2]float64{cb.x + cb.w, cb.y + cb.h}[i]
		}
	}
	ranges := sized
	if plan.ax == alignAnchorCenter && plan.def != nil {
		ranges[0] = centred(sized[0][0], sized[0][1], float64((plan.defR.x0+plan.defR.x1)/2))
	}
	if plan.ay == alignAnchorCenter && plan.def != nil {
		ranges[1] = centred(sized[1][0], sized[1][1], float64((plan.defR.y0+plan.defR.y1)/2))
	}
	lay := cb
	if plan.ax != alignNormal {
		lay.x, lay.w = sized[0][0], max(sized[0][1]-sized[0][0], 0)
		st.Left, st.Right = alignedInsets(plan.ax)
	}
	if plan.ay != alignNormal {
		lay.y, lay.h = sized[1][0], max(sized[1][1]-sized[1][0], 0)
		st.Top, st.Bottom = alignedInsets(plan.ay)
	}
	if plan.ay == alignCenter || plan.ay == alignAnchorCenter {
		// The height comes from the content, measured off the page cuts.
		b.st, b.orig = &st, nil
		e.noPaginate++
		e.layoutAbsIn(b, lay)
		e.noPaginate--
		st.Top = css.Pts(float64(ranges[1][0]-lay.y) + float64((ranges[1][1]-ranges[1][0]-b.marginH())/2))
	}
	b.st, b.orig = &st, nil
	e.layoutAbsIn(b, lay)
	// The vertical shift lays the box out again, so it comes before the
	// horizontal moves.
	if plan.ay != alignNormal {
		y := b.y - b.m[css.Top]
		dy := plan.shift(1, y, b.marginH(), ranges[1], [2]float64{c.y0, c.y1})
		if dy != 0 {
			st.Top = css.Pts(y + dy - lay.y)
			st.Bottom = css.Dimension{Kind: css.Auto}
			e.layoutAbsIn(b, lay)
		}
	}
	if plan.ax == alignCenter || plan.ax == alignAnchorCenter {
		b.shift(float64(ranges[0][0]-lay.x)+float64((ranges[0][1]-ranges[0][0]-b.marginW())/2), 0)
	}
	if plan.ax != alignNormal {
		x := b.x - b.m[css.Left]
		dx := plan.shift(0, x, b.marginW(), ranges[0], [2]float64{c.x0, c.x1})
		if dx != 0 {
			b.shift(dx, 0)
		}
	}
}

// overflowPos is the overflow position of a self-alignment value (CSS
// Box Alignment 3, 4.4): none, safe, or unsafe; safe anchor-center apart,
// which the pinned @webref/css grammar does not hold and Chromium parses.
type overflowPos uint8

const (
	overflowDefault overflowPos = iota
	overflowSafe
	overflowUnsafe
	overflowSafeAnchorCenter
)

// overflowOf returns the overflow position of the self-alignment a.
func overflowOf(a css.BoxAlign) overflowPos {
	switch {
	case a.Safe() && a.Keyword() == css.BoxAnchorCenter:
		return overflowSafeAnchorCenter
	case a.Safe():
		return overflowSafe
	case a.Unsafe():
		return overflowUnsafe
	}
	return overflowDefault
}

// shift returns how far the margin box at p of size n, aligned in the
// range r of the inset-modified containing block whose span in that axis
// is imcb, moves in axis i (0 horizontal) by the overflow position of its
// self-alignment (CSS Box Alignment 3, 4.4 and 4.4.1.2):
//
//   - none: the default overflow alignment, into the containing block
//     before position-area, grown by the scrollable area ([intoRange]);
//   - unsafe: none, the alignment honoured whatever overflows;
//   - safe: into the overflow limit rectangle without the scrollable
//     area, the containing block as it is, and at its start when the box
//     is larger, the rule 4.4.1.2 gives an absolutely positioned box;
//   - safe anchor-center: Chromium's rule, which no clause states: the
//     box is kept from passing the inset-modified containing block's
//     start edge, and may pass its end.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws a 100px
//     box of safe anchor-center on an anchor 20px from its containing
//     block's right edge centred on the anchor, past that edge; of
//     anchor-center shifted back inside it; of unsafe anchor-center
//     centred.
//   - wpt: css/css-anchor-position/anchor-center-safe and -rtl: a box of
//     safe anchor-center that would pass the start edge stands on it.
func (p *anchorPlan) shift(i int, pos, n float64, r, imcb [2]float64) float64 {
	o := [2]float64{p.orig.x0, p.orig.x1}
	l := [2]float64{p.limit.x0, p.limit.x1}
	if i == 1 {
		o, l = [2]float64{p.orig.y0, p.orig.y1}, [2]float64{p.limit.y0, p.limit.y1}
	}
	switch p.over[i] {
	case overflowUnsafe:
		return 0
	case overflowSafeAnchorCenter:
		if p.startLow[i] && pos < imcb[0] {
			return imcb[0] - pos
		}
		if !p.startLow[i] && pos+n > imcb[1] {
			return imcb[1] - (pos + n)
		}
		return 0
	case overflowSafe:
		if pos >= r[0]-eps && pos+n <= r[1]+eps {
			return 0
		}
		if n > l[1]-l[0]+eps {
			if p.startLow[i] {
				return l[0] - pos
			}
			return l[1] - n - pos
		}
		return intoRange(pos, n, r[0], r[1], l[0], l[1])
	case overflowDefault:
	}
	return intoRange(pos, n, r[0], r[1], o[0], o[1])
}

// alignedInsets returns the insets that put a box aligned k in its range:
// at its low edge, its high edge, or its full span for stretch; a centred
// box is placed low and moved.
func alignedInsets(k alignKind) (css.Dimension, css.Dimension) {
	auto := css.Dimension{Kind: css.Auto}
	switch k {
	case alignHigh:
		return auto, css.Pts(0)
	case alignStretch:
		return css.Pts(0), css.Pts(0)
	case alignNormal, alignLow, alignCenter, alignAnchorCenter:
	}
	return css.Pts(0), auto
}

// centred returns the range of [lo, hi] centred on c, as wide as both
// sides of c allow, empty when c lies outside it.
func centred(lo, hi, c float64) [2]float64 {
	h := max(min(c-lo, hi-c), 0)
	return [2]float64{c - h, c + h}
}

// intoRange returns how far a margin box at p of size n, which a range
// [lo, hi] holds or not, moves to lie inside [o0, o1], the containing
// block before position-area: none when it fits its range or is larger
// than the containing block (CSS Box Alignment 3, 4.4, the default
// overflow alignment of an absolutely positioned box with a default
// anchor).
func intoRange(p, n, lo, hi, o0, o1 float64) float64 {
	if p >= lo-eps && p+n <= hi+eps || n > o1-o0+eps {
		return 0
	}
	switch {
	case p < o0:
		return o0 - p
	case p+n > o1:
		return o1 - (p + n)
	}
	return 0
}

// clippedAnchor reports whether the anchor a of p is clipped by
// intervening boxes (CSS Anchor Positioning 1, 5.5): a box between a and
// p's containing block clips its overflow and leaves nothing of a's
// border box inside. A box clips an absolutely positioned descendant
// only when it holds that descendant's containing block (CSS Overflow 3,
// 3): the boxes between an absolutely positioned box on the way and its
// containing block clip nothing of it. The anchor is where the relative
// offsets put it.
func (e *engine) clippedAnchor(a, p *box) bool {
	pcb := cbGenerator(p)
	r := boxRect(a)
	// above is the containing block the boxes on the way must reach before
	// one clips, set while the way runs inside an absolutely positioned
	// box's escape.
	var above *box
	escaping := a.abs
	if escaping {
		above = a.cbBox
	}
	prev := a
	for x, n := a.parent, 0; x != nil && x != pcb && n <= maxTreeDepth; x, n = x.parent, n+1 {
		// The anchor moves with the relative offsets below the clipping
		// box, which moves with those above it.
		dx, dy := ownOffset(prev)
		r = edges{r.x0 + dx, r.y0 + dy, r.x1 + dx, r.y1 + dy}
		prev = x
		if escaping && x == above {
			escaping = false
		}
		clips := !escaping
		switch {
		case x.abs && !escaping:
			escaping, above = true, x.cbBox
		case x.abs && above != nil && (x.cbBox == nil || strictlyIn(above, x.cbBox)):
			// A box escaping further up takes the way with it.
			above = x.cbBox
		}
		if !clips || x.kind == kindInline || x.anon || x.st.Overflow != css.OverflowHidden {
			continue
		}
		cx0, cy0 := x.x+x.bd[css.Left], x.y+x.bd[css.Top]
		cx1, cy1 := cx0+x.w+x.pd[css.Left]+x.pd[css.Right], cy0+x.h+x.pd[css.Top]+x.pd[css.Bottom]
		if r.x1 <= cx0 || r.x0 >= cx1 || r.y1 <= cy0 || r.y0 >= cy1 {
			return true
		}
	}
	return false
}

// hideTree hides b and everything inside it, which visibility: force-hidden
// does (CSS Anchor Positioning 1, 5.5).
func hideTree(b *box, depth int) {
	if depth > maxTreeDepth {
		return
	}
	b.hidden = true
	for _, k := range b.kids {
		hideTree(k, depth+1)
	}
}
