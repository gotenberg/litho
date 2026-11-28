package layout

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// cblock is a containing block: the content box edges a box resolves its
// widths, percentages and floats against (CSS 2.1, 10.1).
type cblock struct {
	x, w float64
	// h is the height percentages resolve against when definite.
	h         float64
	hDefinite bool
	// avail is, for the content box of an orthogonal flow root, the
	// definite height of the root's own containing block, its available
	// inline space (CSS Writing Modes 4, 7.3.1); availSet reports one.
	avail    float64
	availSet bool
	// shrinkScrollers reports a table cell measured before its height
	// is known, whose percentage-height scroll containers then take
	// their min-height alone ([engine.layoutCell]).
	shrinkScrollers bool
	// quirk holds the heights of the quirks of a document in quirks
	// mode, nil in any other mode or where none applies.
	quirk *quirkHeight
}

// quirkHeight is what a containing block holds for the quirks of a
// document in quirks mode.
type quirkHeight struct {
	// h is the height a percentage height resolves against when the
	// containing block's own is not definite: the height of the nearest
	// ancestor that is no block container of auto height (Quirks Mode
	// Standard, 3.5).
	h float64
	// fillEnd, when fill is set, is where the content box of an html
	// element filling the viewport ends, the initial containing block's
	// bottom for the html element itself ([engine.fillHeight]).
	fillEnd float64
	fill    bool
	// below is how far the containing blocks reach under fillEnd: the
	// bottom margins, borders and paddings of the filled boxes around it.
	below float64
}

// strut is a set of adjoining margins being collapsed (CSS 2.1, 8.3.1):
// the largest positive one and the most negative one.
type strut struct {
	pos, neg float64
	// quirky is the largest positive quirky margin of the set, which
	// quirkStart leaves out: the set adjoins the start of a quirky
	// container's content (HTML Standard, 15.3.3, as Blink's margin strut
	// keeps it apart).
	quirky     float64
	quirkStart bool
	// cleared reports that the set holds the top margin of a box with
	// clearance, which collapses through it: the set then does not
	// collapse with its parent's bottom margin (CSS 2.1, 8.3.1).
	cleared bool
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

// addFor returns the strut with margin m adjoined, a quirky margin when
// quirky.
func (s strut) addFor(m float64, quirky bool) strut {
	if quirky && m > 0 {
		s.quirky = max(s.quirky, m)
		return s
	}
	return s.add(m)
}

// sum returns the collapsed margin.
func (s strut) sum() float64 {
	if s.quirkStart {
		return s.pos + s.neg
	}
	return max(s.pos, s.quirky) + s.neg
}

// fillHeight returns the content height the html or body element b fills
// in quirks mode, its height auto, in the containing block cb (Quirks Mode
// Standard, 3.6 and 3.7): the html element's border box reaches the
// bottom of the initial containing block less its bottom margin, the
// body's the bottom of the html element's content box less its own; its
// top is where its collapsed top margin put it. A body positioned
// absolutely, floated or inline-level fills nothing; nor does a page sized
// to its content, which has no viewport to fill.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints a short
//     quirks mode document on a 3 inch page with its html border box
//     278 px tall under margins of 5 px, and its body border box 250 px
//     tall under 10 px margins and the html's 4 px border, a body whose
//     first child's 40 px margin collapses through its top 248 px tall;
//     with <!DOCTYPE html> they are as tall as their content.
func (e *engine) fillHeight(b *box, cb cblock, hSet bool) (float64, bool) {
	if e.opts.Mode != css.Quirks || hSet || cb.quirk == nil || !cb.quirk.fill || b.node == nil || b.anon || b.node.Quirks&FillsViewport == 0 ||
		b.abs || b.float != css.FloatNone || b.inline || e.opts.FitFirstPage || b.st.WritingMode != css.HorizontalTB {
		return 0, false
	}
	return max(cb.quirk.fillEnd-b.y-b.m[css.Bottom]-vEdges(b), 0), true
}

// fillTo returns the content height of the html or body element b, laid
// out to its content height, once it fills the content height fill
// ([engine.fillHeight]) in the containing block cb. The height a fill adds
// never overflows the page it is on: it is at most the remainder of the
// flow area of the page b's content ends on, the footnote area, the note
// area and the bands' room left out, less what b and the filled boxes
// around it keep under it, and nothing on a page b's content fills or runs
// past. So the fill never makes a page of its own, as Chromium's never
// does on the one page a viewport is.
func (e *engine) fillTo(b *box, cb cblock, fill float64) float64 {
	if fill <= b.h {
		return b.h
	}
	if e.paginating() && e.cols == nil {
		end := b.contentY() + b.h
		// pageOf reads a position within eps of a page's top as on that
		// page: a content that ends on a page's last line is on it.
		p := max(e.pageOf(b.contentY()), e.pageOf(end-2*eps))
		room := e.pageTop(p+1) - cb.quirk.below - b.m[css.Bottom] - b.pd[css.Bottom] - b.bd[css.Bottom] - end
		fill = min(fill, b.h+max(room, 0))
	}
	return max(fill, b.h)
}

// quirkCB returns the height the percentage heights of b's children
// resolve against in quirks mode when kcb, the containing block b gives
// them, has no definite one and b fills nothing (Quirks Mode Standard,
// 3.5): for a block container of auto height, cb's own,
// which its children see through it; a table part, a
// flex or grid container and an absolutely positioned box stop the walk,
// a table wrapper passes cb's to its table box.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints a quirks
//     mode div of height 50% in a body with 10 px margins and a 2 px
//     border on a 3 inch page 132 px tall, half the body's filled content
//     box, and a table of height 100% as tall as the page; with <!DOCTYPE
//     html> both are as tall as their content.
func quirkCB(b *box, cb, kcb cblock) *quirkHeight {
	switch {
	case kcb.hDefinite:
		return &quirkHeight{h: kcb.h}
	case b.tpart == partWrapper:
		// The table box resolves its percentage height in the wrapper's
		// own containing block (wrapperCB).
		return cb.quirk
	case b.abs || b.tpart != partNone || b.st.FlexOrGrid():
		return nil
	}
	return cb.quirk
}

// marginQuirks returns whether the top and bottom margins of b are quirky,
// and whether b is a quirky container (see [Node.Quirks]).
func marginQuirks(b *box) (top, bottom, container bool) {
	if b.node == nil || b.anon {
		return false, false, false
	}
	q := b.node.Quirks
	return q&QuirkyTop != 0, q&QuirkyBottom != 0, q&QuirkyContainer != 0
}

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
	// trimStart reports margins adjoining the block-start edge of a
	// container whose margin-trim trims it: a box laid out under them
	// takes no top margin, and an empty one no bottom margin either (CSS
	// Box 4, 3.3).
	trimStart bool
	// kept reports margins pending after a forced break or at a box's
	// content edge: they do not truncate at a page top (CSS Fragmentation
	// 3, 5.2).
	kept bool
	// floats are the floats placed while the margins were pending.
	floats []pendingFloat
	// abs are the absolutely positioned boxes met in the flow while a
	// box's top waited on the margins: their static position is that
	// box's top, wherever the margins resolve.
	abs []*box
}

// absPending records the absolutely positioned box a met in the flow
// while margins are pending and a box other than skip waits on them.
func (mc *marginState) absPending(a, skip *box) {
	if slices.ContainsFunc(mc.waiting, func(w *box) bool { return w != skip }) {
		mc.abs = append(mc.abs, a)
	}
}

// commitEnd resolves the pending margins at the end of a formatting
// context's flow, at y: the absolutely positioned boxes met under them
// keep their static position, where the margins before them put it, as
// Chromium draws them; only content that follows moves them
// ([marginState.commit]).
func (mc *marginState) commitEnd(y float64) {
	mc.abs = nil
	mc.commit(y)
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
// collapse through and which has left the waiting boxes, to the box still
// waiting around it, or lets them keep their place when none does.
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
	c.abs = slices.Clone(mc.abs)
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

// alignFloats moves the floats placed under the pending margins to y, the
// top a float placed now would take: they sit at the top of the box that
// waits on the margins, wherever those resolve (CSS 2.1, 9.5.1, rule 4),
// so a later float placed against them meets them where they will be.
func (mc *marginState) alignFloats(y float64) {
	for i := range mc.floats {
		f := &mc.floats[i]
		dy := y - f.y
		if math.Abs(dy) <= eps {
			continue
		}
		f.box.shift(0, dy)
		if f.idx >= 0 && f.idx < len(f.bc.floats) {
			f.bc.floats[f.idx].y0 += dy
			f.bc.floats[f.idx].y1 += dy
		}
		f.y = y
	}
}

// pendingClearTo returns the position below the floats placed under the
// pending margins that the clear value cl clears, were the margins to
// resolve at y, -Inf when it clears none. Such a float moves down with
// the margins: a box whose top margin would carry it along cannot get
// past it without clearance (CSS 2.1, 9.5.2).
func (mc *marginState) pendingClearTo(cl css.Clear, y float64) float64 {
	bottom := math.Inf(-1)
	for _, f := range mc.floats {
		if f.idx < 0 || f.idx >= len(f.bc.floats) {
			continue
		}
		r := f.bc.floats[f.idx]
		if cl == css.ClearBoth || (cl == css.ClearLeft && r.side == css.FloatLeft) || (cl == css.ClearRight && r.side == css.FloatRight) {
			bottom = max(bottom, r.y1+y-f.y)
		}
	}
	return bottom
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
//
// An absolutely positioned box met while a box's top waited on the margins
// takes that top as its static position (CSS 2.1, 10.6.4: the hypothetical
// box starts at the content edge of its parent, which the collapsed
// margins place), as Chromium draws it.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 puts the static position of an absolute
//     box first in a div of margin-top 10px whose first child has
//     margin-top 40px 40px below the div's margin edge, at the child's top,
//     and one between two siblings after the margin before it alone.
//   - wpt: css/css-transforms/css-scale-nested-001 (body margin 8px, a p of
//     margin-top 16px): Chromium draws the absolute box at 16px.
func (mc *marginState) commit(y float64) {
	mc.commitFloats(y)
	for _, b := range mc.waiting {
		b.y = y
	}
	for _, a := range mc.abs {
		a.sy = y
	}
	mc.abs = nil
	mc.waiting = nil
	mc.strut = strut{}
	mc.absorb = false
	mc.trimStart = false
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
// establishes a block formatting context, and at inline content; it passes
// over boxes out of flow and anonymous blocks of collapsible white space,
// which make no line box (9.4.2) and so separate no margins.
func leadingMargins(b *box, cbw float64) strut {
	var s strut
	for depth := range maxTreeDepth {
		pd, bd, _ := edgesOf(b, cbw)
		if pd[css.Top] != 0 || bd[css.Top] != 0 || b.inlineKids || (depth > 0 && b.establishesBFC()) {
			break
		}
		var first *box
		for _, k := range b.kids {
			if k.float == css.FloatNone && !k.inline && !k.abs && !blankAnon(k) {
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

// collapsingThrough reports whether the in-flow box b, laid out in cb, has
// top and bottom margins that adjoin (CSS 2.1, 8.3.1: no border, padding,
// height or line box between them), and returns the margins of its
// descendants that collapse through it with them. They take part in the
// hypothetical position clearance is computed from (9.5.2), which needs a
// layout of b's content: a percentage padding or an intrinsic width
// decides whether a descendant is empty.
func (e *engine) collapsingThrough(b *box, cb cblock) (strut, bool) {
	if b.bd[css.Top] != 0 || b.pd[css.Top] != 0 || b.bd[css.Bottom] != 0 || b.pd[css.Bottom] != 0 || minHeightSet(b.st.MinHeight) {
		return strut{}, false
	}
	if h, ok := specHeight(b, b.st.Height, cb); ok && h > eps || b.st.AspectRatio.Ratio() > 0 {
		return strut{}, false
	}
	blockWidths(b, cb.w, cb.rtl(b), 0, false)
	b.x = cb.x + b.m[css.Left]
	b.y = 0
	warnings := len(e.warnings)
	e.noPaginate++
	inner := &marginState{kept: true}
	kcb := wrapperCB(b, cblock{x: b.contentX(), w: b.w}, cb)
	end := formatterFor(b).layout(e, b, kcb, &bfc{}, b.contentY(), inner)
	e.noPaginate--
	e.warnings = e.warnings[:warnings]
	if math.Abs(end-b.contentY()) > eps {
		return strut{}, false
	}
	if marginTrim(b) != 0 {
		// The margins of b's children adjoin both its edges: margin-trim
		// truncates them (CSS Box 4, 3.3).
		return strut{}, true
	}
	return inner.strut, true
}

// marginTrim returns the edges at which b truncates the margins of its
// in-flow children and of the descendants whose margins collapse with
// theirs (CSS Box 4, 3.3 and 3.3.1). margin-trim applies to block
// containers and multi-column containers: a flex, grid or table
// container, whose formatter is not flow, trims none.
//
// # Corpus evidence
//
//   - wpt: css-box/margin-trim (29 block-container tests, 7
//     multicol-spanner tests) draws every reference in Chromium 156,
//     trimmed margins of self-collapsing children and of blocks split out
//     of an inline included.
func marginTrim(b *box) css.MarginTrim {
	if b.st.MarginTrim == 0 {
		return 0
	}
	switch formatterFor(b).(type) {
	case flowFormatter, multicolFormatter:
		return b.st.MarginTrim
	}
	return 0
}

// settleTrimmed moves the empty in-flow children that end b, whose
// margins collapse through them and adjoin b's trimmed block-end edge, to
// y, where b's content ends: with their margins truncated to zero they sit
// there (CSS Box 4, 3.3; CSS 2.1, 8.3.1 places a box whose margins
// collapse through it where its top border edge would be).
//
// # Corpus evidence
//
//   - wpt: css-box/margin-trim/block-container-block-end-self-collapsing-and-border
//     draws the outlines of two such children at the content's end in
//     Chromium 156.
func settleTrimmed(b *box, y float64) {
	for i := len(b.kids) - 1; i >= 0; i-- {
		k := b.kids[i]
		if k.abs || k.float != css.FloatNone || !k.laidOut {
			continue
		}
		if k.inline || k.borderH() > eps || k.y <= y+eps {
			return
		}
		k.shift(0, y-k.y)
	}
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
	// breakColumnsPage is a forced page break inside columns: the box
	// starts the first column of a row on the next page.
	breakColumnsPage
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
	if b.ortho {
		return orthoFormatter{}
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
	if lg := renderedLegend(b); lg != nil {
		// The legend takes room above the content, which a definite
		// height leaves less of (HTML Standard, 15.3.13).
		room := e.placeLegend(b, lg)
		cursor += room
		cb.h = max(cb.h-room, 0)
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
	mirrorFieldsetPadding(b)
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
	if !d.IsLength() {
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
	if !d.IsLength() {
		return 0, false
	}
	base := cb.h
	if d.HasPercent() && !cb.hDefinite {
		if cb.quirk == nil || d != b.st.Height || b.abs || b.tpart.tabular() {
			return 0, false
		}
		// The percentage height calculation quirk (Quirks Mode Standard,
		// 3.5): a static, relative or sticky box resolves its percentage
		// height past the block containers of auto height around it.
		base = cb.quirk.h
	}
	h := resolve(d, base)
	if borderBoxHeight(b) {
		h -= vEdges(b)
	}
	return max(h, 0), true
}

// borderBoxHeight reports whether the height properties of b size its
// border box: its box-sizing is border-box, or it is a td or th cell of a
// document in quirks mode (Quirks Mode Standard, 3.13).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 lays out a quirks
//     mode td of height 50px and padding 10px with a 30 px content box,
//     a 50 px one with <!DOCTYPE html>.
func borderBoxHeight(b *box) bool {
	return b.st.BoxSizing == css.BorderBox || b.tpart == partCell && b.node != nil && b.node.Quirks&QuirkyContainer != 0
}

// specMinHeight returns the used min-height d of b in cb: as
// [specHeight], except that a percentage of a height that is not definite
// counts as 0, its length part kept (CSS 2.1, 10.7: "the percentage value
// is treated as '0'"; CSS Values 4, 10.9 resolves a calc() term by term).
func specMinHeight(b *box, d css.Dimension, cb cblock) (float64, bool) {
	if !d.HasPercent() || cb.hDefinite {
		return specHeight(b, d, cb)
	}
	h := resolve(d, 0)
	if borderBoxHeight(b) {
		h -= vEdges(b)
	}
	return max(h, 0), true
}

// minHeightSet reports a computed min-height other than zero, which keeps
// a box's top and bottom margins apart (CSS 2.1, 8.3.1).
func minHeightSet(d css.Dimension) bool {
	switch d.Kind { //nolint:exhaustive // the keywords compute to auto or none
	case css.Points, css.Percent:
		return d.Value > 0 || d.SizeCalc() && d.Resolve(1) != 0
	case css.Calc:
		return d.Resolve(1) != 0 || d.Value != 0
	}
	return false
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
	if mn, ok := specMinHeight(b, b.st.MinHeight, cb); ok {
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
				ml = float64(rest / 2)
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
// page area (CSS 2.1, 10.1), as tall as [Options.ViewportHeight] when set.
func (e *engine) layoutRoot(rb *box) {
	cb := cblock{x: 0, w: e.pageW, h: e.icbHeight(), hDefinite: true}
	if e.opts.Mode == css.Quirks {
		cb.quirk = &quirkHeight{h: cb.h, fillEnd: cb.h, fill: true}
	}
	rb.setMotionCB(typeset.Rect{Width: cb.w, Height: cb.h})
	bc := &bfc{}
	mc := &marginState{}
	// The root is laid out once a pass.
	rb.settled = true
	if rb.abs {
		// An absolutely positioned root is laid out as any absolutely
		// positioned box, in the initial containing block or the page
		// area, its static position their top left corner (CSS 2.1, 10.1,
		// 10.3.7 and 10.6.4).
		setStatic(rb, 0, 0, false)
		e.layoutAbs(rb)
		return
	}
	e.layoutBlock(rb, cb, bc, 0, mc, breakNone)
}

// icbHeight returns the height of the initial containing block:
// [Options.ViewportHeight] when set, else the page area's.
func (e *engine) icbHeight() float64 {
	if e.opts.ViewportHeight > 0 {
		return e.opts.ViewportHeight
	}
	return e.pageH
}

// layoutBlock lays out the in-flow block-level box b whose margin top
// would sit at cursor before the margins pending in mc collapse, and
// returns the cursor after its border box, its bottom margin pending in
// mc.
func (e *engine) layoutBlock(b *box, cb cblock, bc *bfc, cursor float64, mc *marginState, mode breakMode) float64 {
	if e.err != nil {
		return cursor
	}
	e.tick()
	b.laidOut = true
	b.setColPin(0)
	resolveEdges(b, cb.w)
	e.resolveSizing(b, cb, -1, -1)
	if mc.absorb || mc.trimStart {
		b.m[css.Top] = 0
	}
	mt, mb := b.m[css.Top], b.m[css.Bottom]
	qt, qb, quirkyBox := marginQuirks(b)
	if mode != breakNone && e.paginating() {
		// Margins before a break are truncated; after a forced break the
		// top margin stays, after an unforced one it is truncated too (CSS
		// Fragmentation 3, 5.2).
		y := cursor
		switch {
		case mode == breakColumnsPage && e.cols != nil:
			y = e.cols.breakRow(y)
		case !e.atPageTop(y):
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
	if e.opts.PageGroups && e.paginating() && e.cols == nil && startsGroup(b) {
		// A page group holds the pages of its box (CSS GCPM 3, 3.2).
		e.startGroup(b, e.pageOf(max(cursor, 0)))
		defer e.endGroup(b)
	}
	cleared := false
	clearY := math.Inf(-1)
	if b.st.Clear != css.ClearNone {
		clearY = bc.clearTo(b.st.Clear)
		hyp := mc.strut.addFor(mt, qt)
		if b.kind != kindReplaced && !b.establishesBFC() {
			lead := leadingMargins(b, cb.w)
			hyp = hyp.add(lead.pos).add(lead.neg)
			if clearY > cursor+hyp.sum()+eps {
				if through, ok := e.collapsingThrough(b, cb); ok {
					hyp = mc.strut.add(mt).add(through.pos).add(through.neg).add(mb)
				}
			}
		}
		if at := cursor + hyp.sum(); clearY > at+eps || mc.pendingClearTo(b.st.Clear, at) > at+eps {
			mc.commit(cursor + mc.strut.sum())
			clearY = bc.clearTo(b.st.Clear)
			cleared = true
		}
	}
	if b.kind == kindReplaced || b.establishesBFC() {
		top := e.pastMargin(cursor, mc.strut.addFor(mt, qt), mc.kept)
		want := e.pageTopOf(b, top)
		if want > top+eps {
			// The box starts no higher than its distance from its page's
			// top edge (css.Office.PageTop).
			mc.commit(top)
			top = want
		}
		switch {
		case cleared:
			top = max(clearY, want)
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
		if y := bc.clearInitial(); y > top+eps {
			// A box that establishes a formatting context goes below an
			// initial letter (CSS Inline 3, 4.9.2).
			mc.commit(top)
			top = y
		}
		// The margins resolve at top: a box moved below the floats it does
		// not fit beside is placed there without clearance, so the boxes
		// whose top waits on its margin stay beside the floats (CSS 2.1,
		// 9.5).
		mc.commit(top)
		e.layoutBFCRoot(b, cb, bc, top)
		mc.strut = strut{}.addFor(mb, qb)
		return b.y + b.borderH()
	}
	blockWidths(b, cb.w, cb.rtl(b), 0, false)
	b.x = cb.x + b.m[css.Left]
	collapseTop := !cleared && b.bd[css.Top] == 0 && b.pd[css.Top] == 0
	trim := marginTrim(b)
	inner := mc
	start := cursor
	// outerTrim and outerStrut are the state of the margins b's top joins,
	// restored when b turns out empty and its margins collapse through.
	outerTrim := mc.trimStart
	var outerStrut strut
	switch {
	case cleared:
		b.y = clearY
		inner = &marginState{absorb: b.bd[css.Top] == 0 && b.pd[css.Top] == 0, kept: true, trimStart: trim&css.TrimBlockStart != 0}
		start = b.contentY()
	case collapseTop:
		mc.strut = mc.strut.addFor(mt, qt)
		mc.waiting = append(mc.waiting, b)
		b.y = e.pastMargin(cursor, mc.strut, mc.kept)
		outerStrut = mc.strut
		mc.trimStart = mc.trimStart || trim&css.TrimBlockStart != 0
	default:
		top := e.pastMargin(cursor, mc.strut.addFor(mt, qt), mc.kept)
		mc.commit(top)
		b.y = top
		inner = &marginState{kept: true, trimStart: trim&css.TrimBlockStart != 0}
		start = b.contentY()
	}
	if quirkyBox {
		// The quirky margins adjoining the start of a quirky container's
		// content are dropped (HTML Standard, 15.3.3).
		inner.strut.quirkStart = true
	}
	specH, hSet := specHeight(b, b.st.Height, cb)
	ratioH := false
	if !hSet {
		specH, ratioH = ratioHeight(b)
		hSet = ratioH
	}
	kh, kDefinite := usedSpecH(b, specH, hSet, cb), hSet
	if b.anon && !hSet {
		// A percentage that would refer to an anonymous block refers to
		// its closest non-anonymous ancestor (CSS 2.1, 9.2.1.1).
		kh, kDefinite = cb.h, cb.hDefinite
	}
	kcb := wrapperCB(b, cblock{x: b.contentX(), w: b.w, h: kh, hDefinite: kDefinite}, cb)
	if fill, filled := e.fillHeight(b, cb, hSet); filled {
		kcb.quirk = &quirkHeight{h: fill, fillEnd: b.contentY() + fill, fill: true, below: cb.quirk.below + b.m[css.Bottom] + b.pd[css.Bottom] + b.bd[css.Bottom]}
	} else if e.opts.Mode == css.Quirks {
		kcb.quirk = quirkCB(b, cb, kcb)
	}
	cloned := e.enterClone(b, b.y, hSet)
	end := formatterFor(b).layout(e, b, kcb, bc, start, inner)
	e.leaveClone(cloned, end)
	end = e.trimTextBox(b, end, nil)
	marker := e.markerExtent(b)
	// The last child's bottom margin adjoins b's when b's height is auto
	// (CSS 2.1, 8.3.1); a min-height that then makes b taller cuts that
	// margin off instead of moving it into b, as browsers do.
	collapseBottom := b.bd[css.Bottom] == 0 && b.pd[css.Bottom] == 0 && !hSet
	if i := slices.Index(mc.waiting, b); i >= 0 {
		// No content resolved the margins: b is empty. Its top and bottom
		// margins adjoin when its height is auto or zero (CSS 2.1, 8.3.1).
		zero := hSet && specH <= eps && !ratioH
		if (collapseBottom || zero && b.bd[css.Bottom] == 0 && b.pd[css.Bottom] == 0) && !minHeightSet(b.st.MinHeight) && marker <= 0 {
			mc.waiting = slices.Delete(mc.waiting, i, i+1)
			mc.collapsedThrough(b)
			if trim&css.TrimBlockEnd != 0 {
				// The margins of b's children adjoin its bottom edge.
				mc.strut = outerStrut
			}
			mc.trimStart = outerTrim
			if mc.trimStart {
				mb = 0
			}
			b.y = cursor + mc.strut.sum()
			b.h = 0
			if trim&css.TrimBlockEnd != 0 {
				settleTrimmed(b, b.y)
			}
			if !mc.absorb {
				// Under absorb, the margins of an empty box collapse into
				// the clearance above it, the bottom one as the top.
				mc.strut = mc.strut.addFor(mb, qb)
			}
			return cursor
		}
		mc.commit(e.pastMargin(cursor, mc.strut, mc.kept))
		end = b.contentY()
	}
	if marker > 0 {
		end = max(end, b.contentY()+marker)
	}
	trailing := inner.strut
	if quirkyBox {
		// So are those adjoining the end of its content.
		trailing.quirky = 0
	}
	if trailing.cleared {
		// Margins that collapse with the top margin of a box with
		// clearance do not collapse with b's bottom margin (8.3.1).
		collapseBottom = false
	}
	if trim&css.TrimBlockEnd != 0 {
		trailing = strut{}
		settleTrimmed(b, end)
	}
	contentBottom := end
	if !collapseBottom {
		contentBottom = e.pastMargin(end, trailing, false)
		if contentBottom < b.contentY() {
			contentBottom = b.contentY()
		}
		inner.commitEnd(contentBottom)
	}
	h := containedHeight(b, contentBottom-b.contentY())
	content := h
	if hSet {
		h = specH
		if ratioH && contentMinimum(b) {
			h = max(h, contentMinHeight(b, content, cb))
		}
	}
	b.h = usedHeight(b, max(h, 0), max(content, 0), hSet, cb)
	// Its top margin resolved: b fills down from where it lands.
	if fill, filled := e.fillHeight(b, cb, hSet); filled {
		// Filled as a min-height would fill it: the margins of its last
		// child do not collapse through its bottom.
		b.h = e.fillTo(b, cb, fill)
	}
	if isControl(b) {
		e.alignContent(b, e.placeholderLine(b, content))
	}
	e.cloneEdges(b, hSet)
	e.placeMarker(b)
	bottom := b.y + b.borderH()
	if collapseBottom && math.Abs(b.h-h) < eps {
		mc.strut = trailing.addFor(mb, qb)
		if cleared && b.h <= eps && b.bd[css.Top] == 0 && b.pd[css.Top] == 0 {
			// The margins of an empty box with clearance collapse with
			// the margins after it, which then stay inside its parent
			// (8.3.1). Clearance put its top border edge at the floats'
			// bottom with its own margins above it (9.5.2): the set of
			// margins starts that far above, so a following margin no
			// larger than its own moves nothing.
			own := strut{cleared: true}.add(mt).add(trailing.pos).add(trailing.neg).add(mb)
			mc.strut = own
			return bottom - own.sum()
		}
		return bottom
	}
	mc.strut = strut{}.addFor(mb, qb)
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
		// The sizing keywords resolve in the space the margin box is laid
		// out in, a stretch fit included (CSS Sizing 4, 6.1).
		lo, hi, avail := besideFloats(b, l, r, left, right)
		e.resolveSizing(b, cblock{w: avail, h: cb.h, hDefinite: cb.hDefinite}, -1, -1)
		if b.kind == kindReplaced {
			w, h := e.replacedSize(b, cb)
			blockWidths(b, avail, rtl, w, true)
			b.w, b.h = w, h
			b.x = besideX(b, lo, hi, rtl)
			b.y = e.fitWhole(top, b.borderH())
		} else {
			// A table's width comes from its columns, its auto margins
			// centre it (CSS 2.1, 17.4).
			w, wSet := 0.0, false
			if b.tpart == partWrapper {
				w, wSet = e.wrapperWidth(b, cb.w, avail), true
			}
			blockWidths(b, avail, rtl, w, wSet)
			b.x = besideX(b, lo, hi, rtl)
			b.y = top
			top = e.layoutContextIn(b, cb, top)
		}
		h := b.borderH()
		if !bc.narrowed(top, h, left, right) {
			return
		}
		l2, r2 := bc.band(top, h, left, right)
		if fitsBeside(b, l2, r2, left, right) || pass == maxFitPasses-1 {
			return
		}
		if (l2 > l+eps || r2 < r-eps) && !bc.crossed(top, h, left, right) {
			// Laid out in the space beside the floats at its top, the box
			// grows down beside a float that narrows it further: try the
			// narrower space at the same top before moving down, as a box
			// whose width depends on it may then fit. Floats whose edges
			// cross leave no space to try: even a box of no width would
			// overlap one of them (CSS 2.1, 9.5).
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

// usedSpecH returns the content height the children of b, of definite
// height specH when hSet, resolve their percentages against: specH within
// b's min-height and max-height, the used height (CSS 2.1, 10.5 and
// 10.7), as Chromium resolves them.
//
// # Corpus evidence
//
//   - wpt: css-values/calc-min-height (a child of min-height 100% in a
//     box of height 50px and min-height 100px) draws its reference in
//     Chromium 156.
func usedSpecH(b *box, specH float64, hSet bool, cb cblock) float64 {
	if !hSet {
		return specH
	}
	return constrainHeight(b, specH, cb)
}

// besideFloats returns the edges lo and hi the border box of b, a box that
// establishes a formatting context, lies between in the space l to r the
// floats leave of its containing block's left to right, and the width its
// margin box is laid out in. Beside no float its margins offset it from
// the containing block's edges as in normal flow. Beside a float a margin
// may lie over the float but never moves the box into it: only a positive
// margin, counted from the containing block's edge, narrows the space
// (CSS 2.1, 9.5 leaves the placement beside floats to the user agent;
// this is Chromium's).
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) places 48 boxes
//     with overflow: hidden beside a 50px left float in a 100px container
//     (direction ltr and rtl; width auto, 40px, 60px; a left or right
//     margin of -60, -20, 20, 51px) as this rule and [fitsBeside] say:
//     an auto-width box with margin-left: -20px fills the 50px beside the
//     float, one with -60px moves below it.
func besideFloats(b *box, l, r, left, right float64) (lo, hi, avail float64) {
	ml, mr := nonAutoMargin(b, css.Left), nonAutoMargin(b, css.Right)
	if l <= left+eps && r >= right-eps {
		lo, hi = l+ml, r-mr
	} else {
		lo, hi = max(l, left+max(ml, 0)), min(r, right-max(mr, 0))
	}
	return lo, hi, max(max(hi-lo, 0)+ml+mr, 0)
}

// nonAutoMargin returns the used margin of b on side, 0 for an auto one.
func nonAutoMargin(b *box, side css.Side) float64 {
	if b.st.Margin[side].Kind == css.Auto {
		return 0
	}
	return b.m[side]
}

// besideX returns the border box left of b, laid out between the edges lo
// and hi [besideFloats] returns: against lo, or against hi when its
// containing block is right to left, offset by what an auto margin takes.
func besideX(b *box, lo, hi float64, rtl bool) float64 {
	if rtl {
		return hi - (b.m[css.Right] - nonAutoMargin(b, css.Right)) - b.borderW()
	}
	return lo + b.m[css.Left] - nonAutoMargin(b, css.Left)
}

// fitsBeside reports whether the border box of b, placed by [besideX],
// fits in the space l to r the floats leave of its containing block's
// left to right: it crosses no edge a float makes, and beside a float it
// is no wider than the space.
func fitsBeside(b *box, l, r, left, right float64) bool {
	floatL, floatR := l > left+eps, r < right-eps
	switch {
	case floatL && b.x < l-eps:
		return false
	case floatR && b.x+b.borderW() > r+eps:
		return false
	case (floatL || floatR) && b.borderW() > r-l+eps:
		return false
	}
	return true
}

// layoutOwnContext lays out the content of a box that establishes a block
// formatting context, its width and position set: its floats stay inside
// and count toward its auto height (CSS 2.1, 10.6.7).
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) draws the WPT
//     test flex-aspect-ratio-008, a 100px wide column-wrap flex container
//     of aspect-ratio 1 holding two 50 by 100 items, as a 100px square,
//     and -040, a 100px wide row container of aspect-ratio 2 holding a
//     100px tall item, 100px tall.
func (e *engine) layoutOwnContext(b *box, cb cblock) {
	specH, hSet := specHeight(b, b.st.Height, cb)
	if !hSet && cb.shrinkScrollers && b.st.ScrollContainer() && b.st.Height.HasPercent() {
		specH, hSet = 0, true
	}
	ratioH := false
	if !hSet {
		specH, ratioH = ratioHeight(b)
		hSet = ratioH
	}
	own := &bfc{}
	if b == e.rootBox {
		// The document's formatting context keeps clear the boxes the
		// previous pass wrapped (framewrap.go).
		own.floats = e.seedWraps(e.wraps)
	}
	if ratioH && contentMinimum(b) && b.st.FlexOrGrid() && !wrapsColumns(b) {
		// A flex or grid container lays its content out against its
		// height: its content height, the automatic minimum, is measured
		// with the height indefinite (CSS Sizing 4, 4.3). A multi-line
		// column flex container wraps its items at that height, as
		// Chromium measures it.
		e.noPaginate++
		kcb := wrapperCB(b, cblock{x: b.contentX(), w: b.w}, cb)
		end := formatterFor(b).layout(e, b, kcb, &bfc{}, b.contentY(), &marginState{kept: true})
		e.noPaginate--
		specH = constrainHeight(b, max(specH, end-b.contentY()), cb)
	}
	trim := marginTrim(b)
	mc := &marginState{kept: true, trimStart: trim&css.TrimBlockStart != 0}
	_, _, quirkyBox := marginQuirks(b)
	mc.strut.quirkStart = quirkyBox
	kcb := wrapperCB(b, cblock{x: b.contentX(), w: b.w, h: usedSpecH(b, specH, hSet, cb), hDefinite: hSet}, cb)
	fill, filled := e.fillHeight(b, cb, hSet)
	if filled {
		kcb.quirk = &quirkHeight{h: fill, fillEnd: b.contentY() + fill, fill: true, below: cb.quirk.below + b.m[css.Bottom] + b.pd[css.Bottom] + b.bd[css.Bottom]}
	} else if e.opts.Mode == css.Quirks {
		kcb.quirk = quirkCB(b, cb, kcb)
	}
	saved := e.enterClamp(b, cb, b.contentY())
	cloned := e.enterClone(b, b.y, hSet)
	end := formatterFor(b).layout(e, b, kcb, own, b.contentY(), mc)
	e.leaveClone(cloned, end)
	e.leaveClamp(saved)
	end = e.trimTextBox(b, end, own)
	if marker := e.markerExtent(b); marker > 0 {
		end = max(end, b.contentY()+marker)
	}
	if trim&css.TrimBlockEnd != 0 {
		mc.strut = strut{}
	}
	if quirkyBox {
		mc.strut.quirky = 0
	}
	end = e.pastMargin(end, mc.strut, false)
	mc.commitEnd(end)
	if !lineClampContainer(b) {
		end = max(end, own.bottom())
	}
	end = max(end, b.contentY())
	h := containedHeight(b, end-b.contentY())
	content := h
	if hSet {
		h = specH
		if ratioH && contentMinimum(b) {
			h = max(h, contentMinHeight(b, content, cb))
		}
	}
	b.h = usedHeight(b, h, content, hSet, cb)
	if filled {
		b.h = e.fillTo(b, cb, fill)
	}
	content = e.placeholderLine(b, content)
	e.alignContent(b, content)
	e.cloneEdges(b, hSet)
	fillFieldset(b)
	e.placeMarker(b)
	e.reach(b.y + b.borderH())
}

// alignContent moves the content of the block container b, content tall,
// within its content box as its align-content asks (CSS Align 3, 5.1):
// center by half the room left, end and flex-end by all of it. Under safe
// a content taller than the box keeps its start; otherwise it overflows
// both ways or the start, as unsafe asks (4.4). Only a box laid out whole,
// an inline-block, a float or an absolutely positioned box, or a form
// control, whose one line no page break cuts, moves its content: a block a
// page break cuts keeps its content at its start.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 centres the text
//     of an input, a button and a select 40 or 50 pixels tall, which the
//     user agent sheet aligns with align-content: center, inline-level or
//     display: block.
func (e *engine) alignContent(b *box, content float64) {
	k := b.st.AlignContent.Keyword()
	if k != css.BoxCenter && k != css.BoxEnd && k != css.BoxFlexEnd || e.noPaginate == 0 && !isControl(b) ||
		b.flex != nil || b.grid != nil || b.tdata != nil || b.mc != nil {
		return
	}
	free := b.h - content
	if free < 0 && b.st.AlignContent&css.BoxSafe != 0 {
		return
	}
	d := free
	if k == css.BoxCenter {
		d = free / 2
	}
	if d == 0 {
		return
	}
	b.shift(0, d)
	b.y, b.sy, b.anchorY = b.y-d, b.sy-d, b.anchorY-d
}

// lineClampContainer reports a box of the line-clamp property, whose
// floats introduce no clearance that would make it taller (CSS Overflow
// 4, 5.3): its automatic height ends with its in-flow content. The legacy
// -webkit-line-clamp keeps the floats a block formatting context holds.
//
// # Corpus evidence
//
//   - wpt, css/css-overflow/line-clamp/line-clamp-with-floats-016.html
//     and -017.html: Chromium draws a line-clamp container of two 50px
//     lines and a 200px float 100px tall, the lines clamped or not.
func lineClampContainer(b *box) bool {
	return b.st.LineClamp.MaxLines > 0 || b.st.LineClamp.Auto
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
		e.rollbackWarnings(s.warnings)
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
	// placed are the in-flow children laid out so far that a break between
	// boxes looks at, prev last.
	var placed []*box
	// chainFromTop reports that the run of siblings ending at prev, each
	// avoiding a break after the one before, starts prev's page: its first
	// box follows a sibling ending on an earlier page, or nothing but its
	// own margin lies above it on the page. Moving prev
	// to the next page would only break the run higher, so the break stays
	// where it falls, as Writer ignores a keep-with-next chain that fills
	// its page.
	chainFromTop := func() bool {
		i := len(placed) - 1
		p := e.pageOf(prev.y)
		for i > 0 && avoidsBetween(placed[i-1], placed[i]) && e.pageOf(placed[i-1].y) == p {
			i--
		}
		if i == len(placed)-1 {
			return false
		}
		if e.atPageTop(placed[i].y) {
			return true
		}
		if i == 0 {
			// Only the first box's own margin lies above it on the page.
			first := placed[0]
			return first.y-e.pageTop(p) <= max(first.m[css.Top], 0)+eps
		}
		last := placed[i-1]
		return e.pageOf(last.y+last.borderH()-1e-3) < p
	}
	// filled reports an in-flow child with a border box before the one
	// being laid out.
	filled := false
	// after is the float just placed whose break-after forces a break
	// before the next box.
	var after *box
	// reached is the column the children laid out so far reached, -1 for
	// none ([engine.pinToColumn]).
	reached := -1
	// resume is where the box in flow after a float that breaks across
	// pages starts at the earliest ([engine.wordFloatResume]).
	resume := math.Inf(-1)
	release := e.releasing(b)
	for i, k := range b.kids {
		if e.err != nil {
			return cursor
		}
		if fieldsetPart(k) == FieldsetLegend && k == renderedLegend(b) {
			// The fieldset placed its legend before its content.
			continue
		}
		if e.clamp != nil && e.clamp.done && !k.abs {
			// A line clamp container discards what follows its last line
			// (CSS Overflow 4, 6): the box is not laid out, nor painted.
			// Phantom line boxes count as no line, so they make no clamp
			// point and leave the last line without its mark.
			if !e.phantomOnly(k) {
				e.clampDiscards()
			}
			k.laidOut = false
			continue
		}
		if k.float != css.FloatNone {
			top := cursor + mc.strut.sum()
			if after != nil {
				top = max(top, e.floatBreakAfter(after))
				after = nil
			}
			mc.alignFloats(top)
			mc.floatPlaced(k, bc, e.layoutFloat(k, cb, bc, top), top)
			resume = max(resume, e.wordFloatResume(k))
			if e.floatBreaks(k, k.st.BreakAfter) {
				after = k
			}
			continue
		}
		if k.abs {
			staticBlock(k, cb, cursor+mc.strut.sum())
			mc.absPending(k, nil)
			continue
		}
		if resume > cursor+mc.strut.sum()+eps {
			mc.commit(cursor + mc.strut.sum())
			cursor = resume
		}
		resume = math.Inf(-1)
		snap := take(cursor)
		mode := breakNone
		switch {
		case after != nil:
			// The forced break after a float lies between it and the box
			// in flow next to it (CSS Fragmentation 3, 4.1, class A): that
			// box starts on the page after the one the float ends on.
			mode = forcedMode(after.st.BreakAfter, k.st.BreakBefore)
			if y := e.floatBreakAfter(after); y > cursor+mc.strut.sum()+eps {
				mc.commit(cursor + mc.strut.sum())
				cursor = y
				snap = take(cursor)
			}
		case prev != nil && (e.forces(prev.st.BreakAfter) || e.forces(k.st.BreakBefore)):
			mode = forcedMode(prev.st.BreakAfter, k.st.BreakBefore)
		case prev == nil && e.forces(k.st.BreakBefore) && cursor+mc.strut.sum() > e.flowStart()+eps:
			mode = forcedMode(css.BreakAuto, k.st.BreakBefore)
		}
		pageAfter := after != nil && isForced(after.st.BreakAfter)
		after = nil
		if mode != breakNone && e.cols != nil && (pageAfter || prev != nil && isForced(prev.st.BreakAfter) || isForced(k.st.BreakBefore)) {
			// A page break wins over a column break at the same place, and
			// ends the columns on their page (CSS Fragmentation 3, 3.1).
			mode = breakColumnsPage
		}
		if mode == breakNone && prev != nil && e.cols == nil && namesDiffer(prev, k) {
			// Siblings whose page values differ meet at a forced break
			// (CSS Page 3, 4.1).
			mode = breakForced
		}
		start := e.startPage(cursor+mc.strut.sum(), mode)
		kcb := cbFor(start)
		k.settled = e.settles(b, prev, k, nextInFlow(b.kids, i), bc)
		last := prev
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
				prev.borderH()+k.borderH() <= e.areaH(e.pageOf(prev.y)) && !chainFromTop():
				// k may break neither inside nor before: the break moves
				// before prev, which goes to the next page with it (CSS
				// Fragmentation 3, 4.4, rule 1 before rule 2).
				if e.relayout(k.y) {
					cursor = e.layoutBlock(prev, cbFor(e.pageOf(prev.y)+1), bc, restore(prevSnap), mc, breakPush)
					cursor = e.layoutBlock(k, cbFor(e.pageOf(cursor)), bc, cursor, mc, breakNone)
				}
			case avoidsInside(k) && e.straddles(k) && !e.atPageTop(k.y) && (k.borderH() <= e.areaH(e.pageOf(k.y)+1) || filled || e.followsOnPage(b, k.y)):
				// The break before k violates no rule, the one inside it
				// does: k moves to the next page. One taller than a page
				// breaks inside there anyway, so it moves only past a
				// sibling with content, its own or an ancestor's, a class A
				// break point, rather than leave its page empty (CSS
				// Fragmentation 3, 4.3 and 4.4).
				if e.relayout(k.y) {
					cursor = e.layoutBlock(k, cbFor(e.pageOf(k.y)+1), bc, restore(snap), mc, breakPush)
				}
			case avoidsInside(k) && e.straddles(k) && e.cols != nil && e.atPageTop(k.y) && e.cols.rowAfter(k.y, k.borderH()) > k.y:
				// k avoids breaks and does not fit the columns of a row an
				// outer fragmentainer cuts below its own top: it moves to
				// the next row of columns, which starts on the next outer
				// fragmentainer ([colCtx.rowAfter]).
				if y := e.cols.rowAfter(k.y, k.borderH()); e.relayout(k.y) {
					restore(snap)
					cursor = e.layoutBlock(k, cbFor(e.pageOf(y)), bc, y, mc, breakNone)
				}
			case prev != nil && avoidsBetween(prev, k) && (e.pageOf(prev.y+prev.borderH()-1e-3) < e.pageOf(firstContent(k)) || blankSliced(e, k)) &&
				!e.atPageTop(prev.y) && prev.borderH()+k.borderH() <= e.areaH(e.pageOf(prev.y)) && !chainFromTop():
				// The break moves before prev: one through k's first
				// content, or through the blank height of a k with none,
				// separates prev from what follows it in k, as the break
				// before k would (CSS Fragmentation 3, 3.1 and 4.4).
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
			case !e.atPageTop(k.y) && !hasContent(k) && e.straddles(k) && e.edgesStraddle(k):
				if e.relayout(k.y) {
					cursor = e.layoutBlock(k, cbFor(e.pageOf(k.y)+1), bc, restore(snap), mc, breakPush)
				}
			}
		}
		if !k.anon || k.borderH() > 0 || len(k.lines) > 0 {
			// An anonymous block of collapsed white space alone is no
			// sibling a break between boxes looks at.
			reached = e.pinToColumn(k, reached)
			prev, prevSnap = k, snap
			placed = append(placed, k)
		} else if f := lastFloat(k); f != nil && e.floatBreaks(f, f.st.BreakAfter) {
			// Floats among that white space are the boxes before the next
			// one.
			after = f
		}
		filled = filled || k.borderH() > 0
		if release && last != nil && last != prev {
			// The loop lays out again only the child being placed and
			// the one before it: last is final.
			e.releaseShaped(last)
		}
	}
	return cursor
}

// followsOnPage reports a class A break point above the child of b at y
// on y's page (CSS Fragmentation 3, 4.1): b or a block ancestor of b, in
// the same block flow, follows an in-flow sibling whose border box ends
// on that page below its top. A child that moves to the next page breaks
// there and leaves content on the page it leaves, as Chromium moves a box
// that avoids breaks inside it however tall it is.
func (e *engine) followsOnPage(b *box, y float64) bool {
	p := e.pageOf(y)
	top := e.pageTop(p)
	for a, depth := b, 0; a.parent != nil && depth <= maxTreeDepth; a, depth = a.parent, depth+1 {
		up := a.parent
		if a.abs || a.float != css.FloatNone || a.inline || a.tpart != partNone || up.flex != nil || up.grid != nil {
			return false
		}
		for _, s := range up.kids {
			if s == a {
				break
			}
			if s.abs || s.float != css.FloatNone || !s.laidOut || s.borderH() <= 0 {
				continue
			}
			if bot := s.y + s.borderH(); bot > top+eps && e.pageOf(bot-eps) == p {
				return true
			}
		}
		if up.mc != nil {
			// The columns' flow starts in the multi-column container.
			return false
		}
	}
	return false
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

// floatBreaks reports whether the break value v of the float f forces a
// page break: break-before and break-after apply to a float whose
// containing block is in the normal flow of the root (CSS Fragmentation 3,
// 3.1, which says user agents should; CSS 2.1, 13.3.1 allows it), not to
// one inside an out-of-flow box or an atomic inline, nor in a monolithic
// layout. Chromium ignores them on a float on paper, where no screen
// rendering decides, and litho keeps the clause there. In columns Chromium
// ignores a column or page break on a float on screen as on paper, and
// litho follows it: such a float breaks nothing.
//
// # Corpus evidence
//
//   - wpt, css/CSS2/pagination/float-page-break-inside-avoid-5-print.html
//     and -7-print.html: Chromium 156 (gotenberg/gotenberg:9.4.1) keeps a
//     float of page-break-before: always on the first page of the
//     references.
//   - probe, html: the same Chromium lays a float of break-before: column,
//     of break-after: column (before an in-flow block and before a float)
//     and of page-break-before: always in a three-column container in its
//     first column, as if none were set.
func (e *engine) floatBreaks(f *box, v css.Break) bool {
	if !e.paginating() || e.cols != nil || !isForced(v) {
		return false
	}
	for a, depth := f.parent, 0; a != nil && a.parent != nil && depth <= maxTreeDepth; a, depth = a.parent, depth+1 {
		if a.abs || a.float != css.FloatNone || a.inline {
			return false
		}
	}
	return true
}

// lastFloat returns the last float child of b, nil for none.
func lastFloat(b *box) *box {
	for i := len(b.kids) - 1; i >= 0; i-- {
		if b.kids[i].float != css.FloatNone {
			return b.kids[i]
		}
	}
	return nil
}

// floatBreakAfter returns the least top of the box after the float f,
// whose break-after forces a page break: the top of the page after the
// one f ends on. A left or right page break starts the next page, whatever
// its side.
func (e *engine) floatBreakAfter(f *box) float64 {
	return e.nextPage(f.y - f.m[css.Top] + f.marginH() - eps)
}

// avoidsInside reports a box that avoids page or column breaks inside it,
// or that cannot break inside: a box of size containment is monolithic
// (CSS Containment 2, 3.1; CSS Fragmentation 3, 4.1), and moves to the next
// page whole as Chromium moves it, inside an absolutely positioned box too.
func avoidsInside(b *box) bool {
	v := b.st.BreakInside
	return v == css.InsideAvoid || v == css.InsideAvoidPage || v == css.InsideAvoidColumn || b.st.Containment()&css.ContainSize != 0
}

// avoidsBetween reports siblings a and b between which a page break is to
// be avoided.
func avoidsBetween(a, b *box) bool {
	av := func(v css.Break) bool {
		return v == css.BreakAvoid || v == css.BreakAvoidPage || v == css.BreakAvoidColumn
	}
	return av(a.st.BreakAfter) || av(b.st.BreakBefore)
}

// avoidedBefore reports whether a break before b would be one that
// break-before or break-after avoids: between b and its previous in-flow
// sibling, or, b being its parent's first, before the parent (CSS
// Fragmentation 3, 4.1: a break between a box and its first child is the
// break before the parent too).
func avoidedBefore(b *box) bool {
	for depth := 0; b != nil && b.parent != nil && depth < maxAvoidDepth; depth++ {
		var prev *box
		for _, k := range b.parent.kids {
			if k == b {
				break
			}
			if !k.inline && k.float == css.FloatNone && !k.abs {
				prev = k
			}
		}
		if prev != nil {
			return avoidsBetween(prev, b)
		}
		b = b.parent
	}
	return false
}

// maxAvoidDepth bounds the ancestors [avoidedBefore] climbs. Anti-DoS
// guard, not spec-mandated: a break before a box that deep is decided
// by its nearer ancestors already.
const maxAvoidDepth = 64

// straddles reports whether b's border box crosses a page cut.
func (e *engine) straddles(b *box) bool {
	return e.pageOf(b.y) != e.pageOf(b.y+b.borderH()-1e-3)
}

// blankSliced reports a block k with no content whose blank height a page
// or column cut runs through. Slicing it puts nothing of k before the cut
// that a break before k would not: under an avoided break before k, k
// moves with the box before it, as Chromium balances columns over such a
// block rather than cut it.
//
// # Corpus evidence
//
//   - wpt, css/css-multicol/balance-break-avoidance-002.html: Chromium
//     balances blocks of 25, 50, 25 (break-before: avoid, empty) and 50px
//     into columns of 100 and 50px.
//   - probe, html: Chromium balances the same blocks without the avoid,
//     or with break-after: avoid on the 50px block instead, as 75 and
//     75px and 100 and 50px.
func blankSliced(e *engine, k *box) bool {
	return !hasContent(k) && e.straddles(k)
}

// firstContent returns the top of b's first line box, or of its content
// box when it has none. A block of no height that holds no line, such as
// the anonymous block of collapsed white space before a child, starts
// nothing: the content after it decides.
func firstContent(b *box) float64 {
	if len(b.lines) > 0 {
		return b.lines[0].y
	}
	for _, k := range b.kids {
		if !k.inline && k.float == css.FloatNone && !k.abs && k.laidOut {
			if k.borderH() <= eps && !hasContent(k) {
				continue
			}
			return firstContent(k)
		}
	}
	return b.contentY()
}

// edgesStraddle reports whether the page cut after the top of b, a block
// with no content, runs through its block-start border and padding, or
// through a box whose content box is empty. Neither is a possible break point: a break
// inside a block falls between its content edge and its content (CSS
// Fragmentation 3, 4.4, class C), so the box moves to the next page
// whole, as Chromium moves it. A cut at the content edge itself stays,
// the border alone ending the page.
//
// # Corpus evidence
//
//   - probe, html: in four 100px columns, an empty box whose 10px top
//     border, or 10px border then 20px top padding, crosses the column
//     end moves whole to the next column, as do a 20px box of border
//     alone and a 30px box of border and bottom padding with no content
//     height; a box whose top border ends exactly at the column end keeps
//     it there and breaks its 80px content height into the next column.
func (e *engine) edgesStraddle(b *box) bool {
	cut := e.nextPage(b.y)
	return cut < b.contentY()-eps || b.h <= eps
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
	if f.st.WordAnchor.Set {
		return e.layoutWordFloat(f, cb, bc, minY)
	}
	if f.initial {
		return e.layoutInitialLetter(f, cb, bc, minY)
	}
	f.laidOut = true
	resolveEdges(f, cb.w)
	e.resolveSizing(f, cb, -1, -1)
	if f.st.Clear != css.ClearNone {
		// A float's top outer edge goes below the floats it clears (CSS
		// 2.1, 9.5.2: clear on a floating box adds rule 10 to 9.5.1).
		minY = max(minY, bc.clearFloatTo(f.st.Clear))
	}
	minY = max(minY, bc.besideInitial(f.float, minY))
	if e.floatBreaks(f, f.st.BreakBefore) && minY > e.flowStart()+eps && !e.atPageTop(minY) {
		// A left or right page break starts the next page, whatever its
		// side.
		minY = e.nextPage(minY)
	}
	clampPage := -1
	if f.st.PageClamp && e.paginating() && e.cols == nil {
		// The float lies on its anchor's page whole, inside the page box
		// (css.Style.PageClamp): no page cut runs through it.
		clampPage = e.pageOf(max(minY, 0))
		e.noPaginate++
		defer func() { e.noPaginate-- }()
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
	if clampPage < 0 && e.paginating() && avoidsInside(f) && e.straddles(f) {
		if top := f.y - f.m[css.Top]; !e.atPageTop(top) && f.borderH() <= e.areaH(e.pageOf(top)+1) && e.relayout(top) {
			// A float that avoids breaks inside it and fits the next page
			// or column starts there whole (CSS Fragmentation 3, 3.2 and
			// 4.4).
			x, y := bc.place(f.float, f.marginW(), f.marginH(), e.nextPage(top), cb.x, cb.x+cb.w)
			place(x, y)
		}
	}
	if clampPage >= 0 {
		e.clampToPage(f, clampPage)
	}
	i := e.addFloatRect(f, bc, false)
	if i >= 0 && f.st.ShapeOutside != nil && f.st.ShapeOutside.Set {
		r := &bc.floats[i]
		if r.area = e.fragmentedShape(f, f.st.ShapeOutside, cb.w, r.y0, r.y1); r.area == nil {
			r.area = e.newFloatShape(f, f.st.ShapeOutside, cb.w)
		}
	}
	if i >= 0 && clampPage >= 0 {
		// The text wraps around the part of the float over the page
		// area, which ends no lower than the page's.
		r := &bc.floats[i]
		top := e.pageTop(clampPage)
		r.y0 = max(f.y-f.m[css.Top], top)
		r.y1 = max(min(f.y-f.m[css.Top]+f.marginH(), top+e.areaH(clampPage)), r.y0)
		if r.area != nil {
			r.area.moveDown(f.y - f.m[css.Top] - r.y0)
		}
	}
	return i
}

// layoutAtomic lays the inline block or inline replaced box a out in the
// containing block lcb, its margin box at the origin, as one monolithic
// piece (CSS Fragmentation 3, 4.1); the line box moves it into place. A
// replaced box resolves a percentage height against lcb's height when it
// is definite (CSS 2.1, 10.5).
func (e *engine) layoutAtomic(a *box, lcb cblock) {
	if a.ruby != nil {
		e.layoutRuby(a, lcb)
		return
	}
	a.laidOut = true
	cbw := lcb.w
	resolveEdges(a, cbw)
	e.resolveSizing(a, cblock{w: cbw}, -1, -1)
	cb := cblock{w: cbw}
	if a.kind == kindReplaced {
		a.w, a.h = e.replacedSize(a, cblock{w: cbw, h: lcb.h, hDefinite: lcb.hDefinite})
		a.x, a.y = a.m[css.Left], a.m[css.Top]
		a.hasBaseline = false
		if p := a.picture; p != nil && p.HasBaseline {
			a.baseline, a.hasBaseline = a.bd[css.Top]+a.pd[css.Top]+p.Baseline, true
		}
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
	case widgetBaselined(a):
		a.baseline, a.hasBaseline = widgetBaseline(a), true
	case a.ortho && e.opts.BrowserRules:
		// The lines of an orthogonal flow root give no baseline in its
		// parent's writing mode: its baselines are synthesised from its
		// margin box (CSS Writing Modes 4, 4.3; CSS Inline 3, 4.2.1).
	case td != nil:
		// An inline table's baseline is its first row's (CSS 2.1, 10.8.1).
		if td.hasBaseline {
			a.baseline, a.hasBaseline = td.table.y+td.baseline-a.y, true
		}
	case a.flex != nil:
		// An inline flex container's baseline is its first main-axis
		// baseline (CSS Flexbox 1, 8.5), its last under baseline-source:
		// last (CSS Inline 3, 4.3).
		if y, ok := flexBaseline(a, a.st.BaselineSource == css.BaselineLast); ok {
			a.baseline, a.hasBaseline = y-a.y, true
		}
	case a.grid != nil:
		// An inline grid container's baseline is its first one (CSS
		// Grid 1, 10.7), or its last under baseline-source: last.
		if y, ok := gridBaseline(a, a.st.BaselineSource == css.BaselineLast); ok {
			a.baseline, a.hasBaseline = y-a.y, true
		}
	case flexOrGridContent(a) != nil:
		// An inline fieldset whose content box is a flex or a grid
		// container takes that container's baseline, as an inline flex
		// or grid container would.
		c, last := flexOrGridContent(a), a.st.BaselineSource == css.BaselineLast
		baseline := gridBaseline
		if c.flex != nil {
			baseline = flexBaseline
		}
		if y, ok := baseline(c, last); ok {
			a.baseline, a.hasBaseline = y-a.y, true
		}
	case a.st.BaselineAtBottom:
		// A Word inline drawing sits on the line by its bottom margin
		// edge, the baseline of a box without one.
	case !scrollContainer(a.st) && a.st.BaselineSource == css.BaselineFirst:
		// baseline-source: first aligns an inline-block by its first line
		// (CSS Inline 3, 4.3).
		if y, ok := firstBaseline(a); ok {
			a.baseline, a.hasBaseline = y-a.y, true
		}
	case !scrollContainer(a.st):
		if y, ok := lastBaseline(a); ok {
			a.baseline, a.hasBaseline = y-a.y, true
		}
	}
}

// scrollContainer reports a box styled st that is a scroll container,
// whose inline-block takes its bottom margin edge as its baseline rather
// than its last line's (CSS 2.1, 10.8.1, for an overflow other than
// visible; CSS Inline 3, 4.2.1, for a scroll container). overflow: clip
// clips without making one (CSS Overflow 3, 3.1), so a box it clips keeps
// its line's baseline, as an input does under the user agent's
// overflow: clip.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 sets an
//     inline-block 60 by 40 pixels holding one line on the line's
//     baseline under overflow: clip, overflow-x: clip and
//     overflow: clip visible, and its bottom margin edge on it under
//     overflow: hidden; a text input's value sits on the baseline of the
//     text around it.
func scrollContainer(st *css.Style) bool {
	return st.Overflow != css.OverflowVisible && !st.ClipOnly()
}

// lastBaseline returns the baseline of the last line box in flow inside b
// (CSS 2.1, 10.8.1, for inline-block).
func lastBaseline(b *box) (float64, bool) {
	if b.mc != nil {
		return b.mc.baseline(b, true)
	}
	return lastBaselineFlow(b)
}

// lastBaselineFlow is [lastBaseline] in the column flow of a multi-column
// container.
func lastBaselineFlow(b *box) (float64, bool) {
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
	for _, k := range slices.Backward(b.kids) {
		if k.inline || k.float != css.FloatNone || k.abs || k.kind != kindBlock || !k.laidOut {
			continue
		}
		if k.ortho {
			// An orthogonal flow root gives no baseline in its parent's
			// axis (CSS Align 3, 9.1).
			continue
		}
		if legacyScrollBaseline(k) && !isControl(b) {
			// A form control's own blocks (the placeholder of a text
			// field) stand for its inner editor, whose line the control
			// sits on.
			return k.y + k.borderH() + k.m[css.Bottom], true
		}
		if y, ok := lastBaseline(k); ok {
			return y, true
		}
	}
	return 0, false
}

// legacyScrollBaseline reports a block-level block container k whose last
// baseline is its block-end margin edge, whatever it holds: a scroll
// container under baseline-source: auto (CSS Align 3, 9.1, for legacy
// reasons). An inline-block's baseline found through its block children
// stops there.
//
// # Corpus evidence
//
//   - wpt: css/CSS2/linebox/baseline-block-with-overflow-001, where
//     Chromium sets an inline-block holding a 30px block of
//     overflow: hidden on the line 30px down whether that block is
//     empty or holds a line, and 60px down under its 30px bottom margin;
//     its PDF draws the text beside it on those baselines.
//   - wpt: css/css-writing-modes/baseline-with-orthogonal-flow-001, where
//     Chromium's PDF sets "aaa" and inline-blocks ending in a vertical-rl
//     block, scrolling or not, on one baseline.
//   - litho/html: forms-controls, where Chromium's reference sets an email
//     field showing its placeholder on the line of the text around it.
//   - wpt: css/css-align/baseline-rules/overflow-hidden-block-in-inline-baseline.
func legacyScrollBaseline(k *box) bool {
	return scrollContainer(k.st) && k.st.BaselineSource == css.BaselineAuto && k.flex == nil && k.grid == nil && k.tpart == partNone
}

// firstBaseline returns the baseline of the first line box in flow inside
// b, for the outside marker of a list item.
func firstBaseline(b *box) (float64, bool) {
	if b.mc != nil {
		return b.mc.baseline(b, false)
	}
	return firstBaselineFlow(b)
}

// baseline returns the first baseline, or the last when last, of the
// multi-column container b: the highest baseline of its columns and
// spanners, or the lowest for the last (CSS Align 3, 9.1). A line of the
// column flow is placed where its column puts it; a spanner is laid out
// across b in b's own flow (CSS Multicol 1, 5.1). Each column's first
// line is the highest of its lines, so the highest of all lines is the
// highest of the columns' first baselines.
func (d *mcolData) baseline(b *box, last bool) (float64, bool) {
	var best float64
	found := false
	take := func(y float64) {
		if !found || !last && y < best || last && y > best {
			best, found = y, true
		}
	}
	placed := func(y float64) { take(d.place(b, y)) }
	for _, l := range b.lines {
		placed(l.y + l.baseline)
	}
	for _, s := range d.sets {
		if s.spanner != nil {
			if y, ok := edgeBaseline(s.spanner, last); ok {
				take(y)
			}
			continue
		}
		for _, k := range s.kids {
			kidBaselines(k, placed)
		}
	}
	return best, found
}

// markerBaseline returns the baseline of the first line box in flow inside
// the list item b, in the order of the flow, which its outside marker sits
// on (CSS Lists 3, 3.5): in a multi-column container, the first line of
// its column flow or of a spanner, the sets taken in order.
func markerBaseline(b *box) (float64, bool) {
	d := b.mc
	if d == nil {
		return firstBaselineFlow(b)
	}
	if len(b.lines) > 0 || len(d.sets) == 0 {
		y, ok := firstBaselineFlow(b)
		return d.place(b, y), ok
	}
	for _, s := range d.sets {
		if s.spanner != nil {
			if y, ok := firstBaseline(s.spanner); ok {
				return y, true
			}
			continue
		}
		for _, k := range s.kids {
			if k.inline || k.float != css.FloatNone || k.abs || k.kind != kindBlock || !k.laidOut {
				continue
			}
			if y, ok := firstBaseline(k); ok {
				return d.place(b, y), true
			}
		}
	}
	return 0, false
}

// kidBaselines calls f with the baseline of every line in flow inside
// the block-level box k of a column flow, nested blocks included, and
// with the first and last baselines of a flex, grid, table or
// multi-column container among them, whose lines are its own. A box out
// of flow and a spanner, which is not in the column flow, give none.
func kidBaselines(k *box, f func(float64)) {
	if k.inline || k.float != css.FloatNone || k.abs || k.kind != kindBlock || !k.laidOut || isSpanner(k) {
		return
	}
	if k.flex != nil || k.grid != nil || k.tdata != nil || k.mc != nil {
		if y, ok := firstBaseline(k); ok {
			f(y)
		}
		if y, ok := lastBaseline(k); ok {
			f(y)
		}
		return
	}
	for _, l := range k.lines {
		f(l.y + l.baseline)
	}
	for _, c := range k.kids {
		kidBaselines(c, f)
	}
}

// edgeBaseline is [lastBaseline] when last, [firstBaseline] otherwise.
func edgeBaseline(b *box, last bool) (float64, bool) {
	if last {
		return lastBaseline(b)
	}
	return firstBaseline(b)
}

// firstBaselineFlow is [firstBaseline] in the column flow of a multi-column
// container.
func firstBaselineFlow(b *box) (float64, bool) {
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

// place returns where the column flow position y of the multi-column
// container b paints: the same place in the column it falls in, whose row
// lies where the container's rows put it.
func (d *mcolData) place(b *box, y float64) float64 {
	rel := y - b.y
	for _, s := range d.sets {
		for _, f := range s.frags {
			if rel >= f.vtop-eps && rel < f.end()-eps && f.row < len(s.rows) {
				return b.y + s.rows[f.row].top + f.lead + rel - f.vtop
			}
		}
	}
	return y
}

// pageTopOf returns the flow position b's border box starts at the least
// when its style places it from its page's top edge (css.Office.PageTop)
// and top is where the flow puts it, -Inf otherwise.
func (e *engine) pageTopOf(b *box, top float64) float64 {
	if b.st.PageTop <= 0 || !e.paginating() || e.cols != nil {
		return math.Inf(-1)
	}
	p := e.pageOf(max(top, 0))
	return e.pageTop(p) - e.box(p).Margin[css.Top] + b.st.PageTop
}
