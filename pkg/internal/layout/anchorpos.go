package layout

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// This file lays out the boxes CSS Anchor Positioning 1 places: an
// absolutely positioned box that names a default anchor, a position-area,
// anchor() or anchor-size() functions, anchor-center or position options.
//
// One step owns the resolution, [engine.layoutAnchored], called by
// [engine.layoutAbs] once the box's containing block is known. Positioned
// boxes are laid out after the flow, in tree order ([engine.layoutPositioned]),
// so every in-flow box is laid out by then, and so is every positioned box
// before this one in tree order: the boxes the module's acceptable anchor
// element conditions admit (2.2) are exactly the ones whose geometry is
// final. The step finds the anchors, resolves the functions to lengths and
// position-area to a containing block, applies the self-alignment the
// module defines, and tries the position options in order; the box is
// then laid out by the rules of CSS 2.1 that [engine.layoutAbsIn] follows.
//
// Across pages, the flow is laid out as one tall column the page cuts
// slice: an absolutely positioned box whose containing block is in that
// flow sees its anchor unfragmented in it and fragments like the flow
// (2, "If the anchor box is fragmented"), so it follows its anchor to the
// page the anchor is on, earlier or later than its own static position. A
// fixed box, whose containing block is each page's area, sees its anchor
// where it lies on its page, the bounding box of the anchor's fragments
// when it spans pages, and is drawn from that page on.

// anchorIndex holds the anchors of a layout: the boxes that name each
// anchor name, in tree order, and the principal box of each node.
type anchorIndex struct {
	byName    map[string][]*box
	principal map[*Node]*box
}

// anchorsIndexed returns the anchors of the layout, indexed on first use:
// the positioned boxes are laid out after every box is generated.
func (e *engine) anchorsIndexed() *anchorIndex {
	if e.anchorIdx != nil {
		return e.anchorIdx
	}
	x := &anchorIndex{byName: map[string][]*box{}, principal: map[*Node]*box{}}
	var walk func(b *box, depth int)
	walk = func(b *box, depth int) {
		if depth > maxTreeDepth {
			return
		}
		if b.node != nil && !b.anon && b.kind != kindText && b.kind != kindBreak {
			if _, seen := x.principal[b.node]; !seen {
				x.principal[b.node] = b
				if a := b.st.Anchor; a != nil {
					for _, n := range a.Names {
						x.byName[n] = append(x.byName[n], b)
					}
				}
			}
		}
		for _, k := range b.kids {
			walk(k, depth+1)
		}
	}
	if e.rootBox != nil {
		walk(e.rootBox, 0)
	}
	e.anchorIdx = x
	return x
}

// anchoredBox reports an absolutely positioned box whose style uses anchor
// positioning: a default anchor other than none or normal, a
// position-area, position options, a position-visibility that can hide
// it, anchor-center, or an anchor function in an inset, sizing or margin
// property.
func (e *engine) anchoredBox(b *box) bool {
	if _, ok := e.anchorBase[b]; ok {
		return true
	}
	st := b.st
	if a := st.Anchor; a != nil {
		if a.Default.Kind != css.AnchorNormal && a.Default.Kind != css.AnchorNone || !a.Area.None() ||
			len(a.Fallbacks) > 0 || a.Visibility.Has(css.VisibilityNoOverflow) {
			return true
		}
	}
	if st.AlignSelf == css.BoxAnchorCenter || st.JustifySelf == css.BoxAnchorCenter {
		return true
	}
	for _, d := range [...]css.Dimension{
		st.Top, st.Right, st.Bottom, st.Left, st.Width, st.Height, st.MinWidth,
		st.MinHeight, st.MaxWidth, st.MaxHeight, st.Margin[0], st.Margin[1], st.Margin[2], st.Margin[3],
	} {
		if d.Anchored() {
			return true
		}
	}
	return false
}

// cbGenerator returns the box whose containing block holds b, nil for the
// initial containing block or a page: for an absolutely positioned box the
// one [holderOf] finds, for any other its nearest ancestor that is a block
// container (CSS 2.1, 10.1).
func cbGenerator(b *box) *box {
	if b.abs {
		return holderOf(b)
	}
	for a := b.parent; a != nil; a = a.parent {
		if !a.anon && a.kind != kindInline {
			return a
		}
	}
	return nil
}

// strictlyIn reports whether d lies inside a, a itself excluded.
func strictlyIn(d, a *box) bool {
	for x, n := d.parent, 0; x != nil && n <= maxTreeDepth; x, n = x.parent, n+1 {
		if x == a {
			return true
		}
	}
	return false
}

// acceptableAnchor reports whether a, which names name, is an acceptable
// anchor element for the absolutely positioned box p (CSS Anchor
// Positioning 1, 2.2): in scope for p, and laid out strictly before p,
// that is a and p have the same containing block and a is not absolutely
// positioned or comes earlier in tree order, or the box generating a's
// containing block is acceptable for p. The initial containing block and
// a page area count as one containing block, the viewport's: the probes
// show a fixed box anchored to in-flow content.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 places a fixed
//     box at its in-flow anchor, on screen and in print.
func acceptableAnchor(a, p *box, name string) bool {
	if a == p || strictlyIn(a, p) || name != "" && !inScope(a, p, name) {
		return false
	}
	pcb := cbGenerator(p)
	for x, n := a, 0; x != nil && n <= maxTreeDepth; n++ {
		g := cbGenerator(x)
		if g == pcb && (!x.abs || x.seq < p.seq) {
			return true
		}
		x = g
	}
	return false
}

// scopes reports whether b's anchor-scope limits name to its subtree (CSS
// Anchor Positioning 1, 2.1.1).
func scopes(b *box, name string) bool {
	a := b.st.Anchor
	return a != nil && (a.ScopeAll || slices.Contains(a.ScopeNames, name))
}

// inScope reports whether the anchor a's name is in scope for p (CSS
// Anchor Positioning 1, 2.1.1): the nearest box scoping name around a, a
// itself included, and the nearest around p, p excluded, must be one.
func inScope(a, p *box, name string) bool {
	var sa, sp *box
	for x, n := a, 0; x != nil && n <= maxTreeDepth; x, n = x.parent, n+1 {
		if scopes(x, name) {
			sa = x
			break
		}
	}
	for x, n := p.parent, 0; x != nil && n <= maxTreeDepth; x, n = x.parent, n+1 {
		if scopes(x, name) {
			sp = x
			break
		}
	}
	return sa == sp
}

// targetAnchor returns the target anchor element of p for name (CSS
// Anchor Positioning 1, 2.2): the nearest ancestor of p that is an
// acceptable anchor of that name, else the last such box in tree order,
// nil for none.
func (e *engine) targetAnchor(p *box, name string) *box {
	var ancestor, last *box
	for _, a := range e.anchorsIndexed().byName[name] {
		if !acceptableAnchor(a, p, name) {
			continue
		}
		if strictlyIn(p, a) {
			if ancestor == nil || strictlyIn(a, ancestor) {
				ancestor = a
			}
			continue
		}
		last = a
	}
	if ancestor != nil {
		return ancestor
	}
	return last
}

// originating returns the box of the element a ::before or ::after box b
// belongs to, nil for another box.
func (e *engine) originating(b *box) *box {
	if b.node == nil || !b.node.Generated {
		return nil
	}
	for a := b.parent; a != nil; a = a.parent {
		if a.node != nil && !a.anon {
			return e.anchorsIndexed().principal[a.node]
		}
	}
	return nil
}

// defaultAnchor returns the default anchor element of p styled st (CSS
// Anchor Positioning 1, 2.3): normal is auto when position-area is set,
// none otherwise; auto takes the implicit anchor element, which for a
// pseudo-element is its originating element (2.3.1) and for any other
// element none, no host language feature of a static page defining one;
// match-parent takes what the parent's position-anchor names, when it is
// acceptable for p. The parent's value is read for p: the acceptable
// anchor conditions are those of a box positioned where p is, the parent
// itself maybe being in flow.
func (e *engine) defaultAnchor(p *box, st *css.Style, depth int) *box {
	a := st.AnchorPositioning()
	kind := a.Default.Kind
	if kind == css.AnchorNormal {
		kind = css.AnchorNone
		if !a.Area.None() {
			kind = css.AnchorAuto
		}
	}
	switch kind {
	case css.AnchorAuto:
		if o := e.originating(p); o != nil && acceptableAnchor(o, p, "") {
			return o
		}
	case css.AnchorNamed:
		return e.targetAnchor(p, a.Default.Name)
	case css.AnchorMatchParent:
		parent := e.originating(p)
		if parent == nil {
			for x := p.parent; x != nil; x = x.parent {
				if x.node != nil && !x.anon {
					parent = x
					break
				}
			}
		}
		if parent == nil || depth > maxTreeDepth {
			return nil
		}
		pa := parent.st.AnchorPositioning()
		if pa.Default.Kind == css.AnchorAuto || pa.Default.Kind == css.AnchorNormal && !pa.Area.None() {
			// The parent's implicit anchor.
			if o := e.originating(parent); o != nil && acceptableAnchor(o, p, "") {
				return o
			}
			return nil
		}
		if pa.Default.Kind == css.AnchorMatchParent {
			if d := e.defaultAnchor(parent, parent.st, depth+1); d != nil && acceptableAnchor(d, p, "") {
				return d
			}
			return nil
		}
		if d := e.defaultAnchor(p, parent.st, depth+1); d != nil && acceptableAnchor(d, p, "") {
			return d
		}
	case css.AnchorNormal, css.AnchorNone:
	}
	return nil
}

// edges is an axis-aligned rectangle by its edges.
type edges struct{ x0, y0, x1, y1 float64 }

func (r edges) union(o edges) edges {
	return edges{min(r.x0, o.x0), min(r.y0, o.y0), max(r.x1, o.x1), max(r.y1, o.y1)}
}

// boxRect returns the border box of a in flow coordinates: the bounding
// box of its fragments for an inline box, which its lines hold.
func boxRect(a *box) edges {
	if a.kind != kindInline {
		return edges{a.x, a.y, a.x + a.borderW(), a.y + a.borderH()}
	}
	host := a.parent
	for host != nil && host.kind == kindInline {
		host = host.parent
	}
	for host != nil && host.anon && host.parent != nil {
		host = host.parent
	}
	r := edges{a.anchorX, a.anchorY, a.anchorX, a.anchorY}
	found := false
	piece := func(b *box) bool { return b == a || b.kind == kindInline && a.node != nil && b.node == a.node }
	var scan func(h *box, depth int)
	scan = func(h *box, depth int) {
		if depth > maxTreeDepth {
			return
		}
		for _, l := range h.lines {
			for _, f := range l.frags {
				if !piece(f.box) {
					continue
				}
				fr := edges{l.x + f.x0, l.y + f.top, l.x + f.x1, l.y + f.bottom}
				if !found {
					r, found = fr, true
				} else {
					r = r.union(fr)
				}
			}
		}
		for _, k := range h.kids {
			if k.anon && k.kind == kindBlock || k.kind == kindInline {
				scan(k, depth+1)
			}
		}
	}
	if host != nil {
		scan(host, 0)
	}
	return r
}

// anchorRect returns the anchor box of a for the absolutely positioned
// box p, whose containing block's box is pcb (CSS Anchor Positioning 1,
// 2): a's border box with the relative offsets and transforms of a and of
// its ancestors up to pcb applied, the axis-aligned bounding box of what
// they make of it, and through each multi-column container it crosses the
// bounding box of its pieces in the columns. A fixed p, whose containing
// block is a page's area, sees the anchor on its page: page is the page
// the anchor starts on, -1 for p not fixed.
func (e *engine) anchorRect(a, p, pcb *box) (edges, int) {
	r := boxRect(a)
	for x, n := a, 0; x != nil && x != pcb && n <= maxTreeDepth; x, n = x.parent, n+1 {
		if x.transformed() {
			if m, _, ok := transformOf(x, typeset.Rect{X: x.x, Y: x.y, Width: x.borderW(), Height: x.borderH()}); ok {
				r = mapRect(m, r)
			}
		}
		dx, dy := ownOffset(x)
		r = edges{r.x0 + dx, r.y0 + dy, r.x1 + dx, r.y1 + dy}
		if c := x.parent; c != nil && c != pcb && c.mc != nil {
			r = columnRect(c, x, r)
		}
	}
	if !p.fixed {
		return r, -1
	}
	first, last := e.pageOf(r.y0+eps), e.pageOf(max(r.y1-eps, r.y0+eps))
	top := e.pageTop(first)
	if last == first {
		return edges{r.x0, r.y0 - top, r.x1, r.y1 - top}, first
	}
	// Fragments on several pages: their bounding box on the page.
	return edges{r.x0, min(r.y0-top, 0), r.x1, e.pageH}, first
}

// mapRect returns the bounding box of r mapped by m.
func mapRect(m typeset.Matrix, r edges) edges {
	out := edges{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, pt := range [4][2]float64{{r.x0, r.y0}, {r.x1, r.y0}, {r.x0, r.y1}, {r.x1, r.y1}} {
		x := float64(m.A*pt[0]) + float64(m.C*pt[1]) + m.E
		y := float64(m.B*pt[0]) + float64(m.D*pt[1]) + m.F
		out = out.union(edges{x, y, x, y})
	}
	return out
}

// columnRect returns the bounding box of the pieces the columns of the
// multi-column container c cut r, a rectangle of c's column flow inside
// its child k, into, each moved to its column.
func columnRect(c, k *box, r edges) edges {
	s := setOf(c, k)
	if s == nil || len(s.frags) == 0 {
		return r
	}
	i0, i1 := s.fragOf(c, r.y0+eps), s.fragOf(c, max(r.y1-eps, r.y0+eps))
	out := r
	found := false
	for i := i0; i <= i1 && i-i0 <= maxColumns; i++ {
		y0, y1 := s.band(c, i)
		if i == 0 {
			y0 = math.Inf(-1)
		}
		if i == len(s.frags)-1 {
			y1 = math.Inf(1)
		}
		lo, hi := max(r.y0, y0), min(r.y1, y1)
		if hi < lo || hi == lo && r.y1 > r.y0 {
			continue
		}
		dx, dy := c.mc.shiftOf(s, i)
		piece := edges{r.x0 + dx, lo + dy, r.x1 + dx, hi + dy}
		if !found {
			out, found = piece, true
		} else {
			out = out.union(piece)
		}
	}
	return out
}

// flowAxes is how a writing mode and a direction lay their axes on the
// page: whether the block axis is horizontal, and whether the block and
// inline axes start at their low coordinate (left or top).
type flowAxes struct {
	blockX            bool
	blockLow, inlineL bool
}

// axesOf returns the axes of the style st (CSS Writing Modes 4, 2 and 3).
func flowAxesOf(st *css.Style) flowAxes {
	rtl := st != nil && st.Direction == css.RTL
	wm := css.HorizontalTB
	if st != nil {
		wm = st.WritingMode
	}
	switch wm {
	case css.VerticalRL, css.SidewaysRL:
		return flowAxes{blockX: true, blockLow: false, inlineL: !rtl}
	case css.VerticalLR:
		return flowAxes{blockX: true, blockLow: true, inlineL: !rtl}
	case css.SidewaysLR:
		// Its lines run bottom to top.
		return flowAxes{blockX: true, blockLow: true, inlineL: rtl}
	case css.HorizontalTB:
	}
	return flowAxes{blockLow: true, inlineL: !rtl}
}

// startLow reports whether the axis horizontal (true) or vertical (false)
// of the axes w starts at its low coordinate.
func (w flowAxes) startLow(horizontal bool) bool {
	if horizontal == w.blockX {
		return w.blockLow
	}
	return w.inlineL
}

// anchorScope is what the anchor functions of one box resolve against: the
// box, its containing block, the writing modes of both, its default
// anchor, and the pages of the anchors met, for a fixed box.
type anchorScope struct {
	e      *engine
	b      *box
	cb     absCB
	cbAxes flowAxes
	self   flowAxes
	def    *box
	defR   edges
	// page is the last page an anchor of a fixed box starts on, -1 for
	// none; missing reports a function whose anchor was not found.
	page    int
	missing bool
	// rects caches the anchor box of each anchor.
	rects map[*box]edges
}

// rectOf returns the anchor box of a for the scope's box.
func (s *anchorScope) rectOf(a *box) edges {
	if r, ok := s.rects[a]; ok {
		return r
	}
	r, page := s.e.anchorRect(a, s.b, s.cb.box)
	if page > s.page {
		s.page = page
	}
	s.rects[a] = r
	return r
}

// anchorOf returns the target anchor element of a function naming name,
// the default anchor for "".
func (s *anchorScope) anchorOf(name string) *box {
	if name == "" {
		return s.def
	}
	return s.e.targetAnchor(s.b, name)
}

// edge returns the coordinate of the side f names of its anchor box r, in
// the inset property side, horizontal or vertical, and false for a side
// of the other axis (CSS Anchor Positioning 1, 3.2).
func (s *anchorScope) edge(f css.AnchorFn, side css.Side, r edges) (float64, bool) {
	horizontal := side == css.Left || side == css.Right
	lo, hi := r.y0, r.y1
	if horizontal {
		lo, hi = r.x0, r.x1
	}
	low := side == css.Left || side == css.Top
	switch f.Side {
	case css.AnchorInside:
		if low {
			return lo, true
		}
		return hi, true
	case css.AnchorOutside:
		if low {
			return hi, true
		}
		return lo, true
	case css.AnchorTop, css.AnchorBottom:
		if horizontal {
			return 0, false
		}
		if f.Side == css.AnchorTop {
			return lo, true
		}
		return hi, true
	case css.AnchorLeft, css.AnchorRight:
		if !horizontal {
			return 0, false
		}
		if f.Side == css.AnchorLeft {
			return lo, true
		}
		return hi, true
	case css.AnchorStart, css.AnchorEnd, css.AnchorSelfStart, css.AnchorSelfEnd, css.AnchorPercent:
	}
	axes := s.cbAxes
	if f.Side == css.AnchorSelfStart || f.Side == css.AnchorSelfEnd {
		axes = s.self
	}
	start, end := lo, hi
	if !axes.startLow(horizontal) {
		start, end = hi, lo
	}
	switch f.Side { //nolint:exhaustive // the physical sides, inside and outside returned above; a percentage lies between start and end
	case css.AnchorStart, css.AnchorSelfStart:
		return start, true
	case css.AnchorEnd, css.AnchorSelfEnd:
		return end, true
	}
	return start + float64(f.Percent*(end-start)), true
}

// size returns the length anchor-size() f measures on r, in a property of
// the horizontal axis when horizontal (CSS Anchor Positioning 1, 4.1).
func (s *anchorScope) size(f css.AnchorFn, horizontal bool, r edges) float64 {
	w := horizontal
	switch f.Dim {
	case css.AnchorWidth:
		w = true
	case css.AnchorHeight:
		w = false
	case css.AnchorBlock:
		w = s.cbAxes.blockX
	case css.AnchorInline:
		w = !s.cbAxes.blockX
	case css.AnchorSelfBlock:
		w = s.self.blockX
	case css.AnchorSelfInline:
		w = !s.self.blockX
	case css.AnchorDimAuto:
	}
	if w {
		return r.x1 - r.x0
	}
	return r.y1 - r.y0
}

// resolver returns the function [css.Dimension.ResolveAnchors] takes for a
// property: the inset of side when inset is set, else a sizing or margin
// property of the horizontal axis when horizontal. An anchor() resolves to the
// inset that puts the property's edge of the inset-modified containing
// block on the anchor's edge (CSS Anchor Positioning 1, 3.2); anchor-size()
// to the anchor's size (4.1).
func (s *anchorScope) resolver(sd css.Side, inset, horizontal bool) func(css.AnchorFn) (float64, bool) {
	return func(f css.AnchorFn) (float64, bool) {
		a := s.anchorOf(f.Name)
		if a == nil {
			s.missing = true
			return 0, false
		}
		r := s.rectOf(a)
		if f.Size {
			return s.size(f, horizontal, r), true
		}
		if !inset {
			return 0, false
		}
		v, ok := s.edge(f, sd, r)
		if !ok {
			return 0, false
		}
		switch sd {
		case css.Top:
			return v - s.cb.y, true
		case css.Bottom:
			return s.cb.y + s.cb.h - v, true
		case css.Left:
			return v - s.cb.x, true
		case css.Right:
		}
		return s.cb.x + s.cb.w - v, true
	}
}

// substitute returns a copy of st with its anchor functions resolved in
// scope s, each declaration that resolves to nothing taking its initial
// value (CSS Anchor Positioning 1, 3.2.1 and 4.1.1: invalid at
// computed-value time).
func (s *anchorScope) substitute(st *css.Style) *css.Style {
	c := *st
	init := css.InitialValue()
	insets := [4]*css.Dimension{&c.Top, &c.Right, &c.Bottom, &c.Left}
	inits := [4]css.Dimension{init.Top, init.Right, init.Bottom, init.Left}
	for i, d := range insets {
		sd := [4]css.Side{css.Top, css.Right, css.Bottom, css.Left}[i]
		if r, ok := d.ResolveAnchors(s.resolver(sd, true, sd == css.Left || sd == css.Right)); ok {
			*d = r
		} else {
			*d = inits[i]
		}
	}
	for i := range c.Margin {
		h := i == int(css.Left) || i == int(css.Right)
		if r, ok := c.Margin[i].ResolveAnchors(s.resolver(css.Top, false, h)); ok {
			c.Margin[i] = r
		} else {
			c.Margin[i] = init.Margin[i]
		}
	}
	sizes := [...]struct {
		d    *css.Dimension
		init css.Dimension
		h    bool
		auto *bool
	}{
		{&c.Width, init.Width, true, nil},
		{&c.Height, init.Height, false, nil},
		{&c.MinWidth, init.MinWidth, true, &c.MinWidthAuto},
		{&c.MinHeight, init.MinHeight, false, &c.MinHeightAuto},
		{&c.MaxWidth, init.MaxWidth, true, nil},
		{&c.MaxHeight, init.MaxHeight, false, nil},
	}
	for _, z := range sizes {
		if r, ok := z.d.ResolveAnchors(s.resolver(css.Top, false, z.h)); ok {
			*z.d = r
		} else {
			*z.d = z.init
			if z.auto != nil {
				*z.auto = true
			}
		}
	}
	return &c
}

// stOrNil returns the style of b, nil for no box.
func (b *box) stOrNil() *css.Style {
	if b == nil {
		return nil
	}
	return b.st
}
