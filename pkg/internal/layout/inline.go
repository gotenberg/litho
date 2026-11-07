package layout

import (
	"fmt"
	"math"
	"slices"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// lineBox is one line box of an inline formatting context (CSS 2.1,
// 9.4.2).
type lineBox struct {
	// x and y are the line box's top-left corner in flow coordinates, w
	// its width, h its height and baseline the offset of its baseline from
	// y.
	x, y, w, h float64
	baseline   float64
	items      []*lineItem
	frags      []*inlineFrag
	// overflows reports a line taller than its page area for an atomic
	// inline taller than the area: it overflows onto the pages below with
	// that monolithic content (CSS Fragmentation 3, 4.1). A line taller
	// than the area for its text alone takes one page and is cut there.
	overflows bool
	// hangW is the advance of the spaces that hang at the line's logical
	// end, drawn on its left when hangLeft (a right-to-left paragraph),
	// else on its right.
	hangW    float64
	hangLeft bool
}

// lineItem is one piece of a line: a glyph run of a text box, or an
// atomic inline. Positions are relative to the line box.
type lineItem struct {
	box *box
	// text is the glyph run, its Origin relative to the line box; nil for
	// an atomic inline or a piece that draws no glyph.
	text   *compose.Text
	atomic *box
	x, w   float64
	// baseline is the item's baseline below the line top, ascent and
	// descent its extent, and gap the line gap of a glyph run's face.
	baseline, ascent, descent float64
	gap                       float64
	ulPos, ulThick            float64
	stPos, stThick            float64
	// start and end are the item's characters in its paragraph, hang how
	// many of them hang at the line end, and rtl reports glyphs drawn
	// right to left.
	start, end, hang int
	rtl              bool
}

// inlineFrag is the part of an inline box on one line, painted with its
// background and borders (CSS 2.1, 8.6 and CSS Backgrounds 3, 5.4).
type inlineFrag struct {
	box *box
	// x0 and x1 are the border box's left and right edges, top and bottom
	// its top and bottom, relative to the line box.
	x0, x1, top, bottom float64
	// left and right report whether the fragment draws the box's left and
	// right edges (box-decoration-break: slice).
	left, right bool
}

// inlineEdges returns the widths inline box b takes at its logical start
// and end: margin, border and padding of its start side on its first
// fragment and of its end side on its last, the start side being the left
// one in a left-to-right box and the right one in a right-to-left box (CSS
// 2.1, 8.6 and 9.10).
func inlineEdges(b *box) (float64, float64) {
	st, en := css.Left, css.Right
	if b.st.Direction == css.RTL {
		st, en = en, st
	}
	l, r := 0.0, 0.0
	if b.first {
		l = b.m[st] + b.bd[st] + b.pd[st]
	}
	if b.last {
		r = b.m[en] + b.bd[en] + b.pd[en]
	}
	return l, r
}

// edgeSides reports whether the fragment of inline box b that
// holds its logical start (start) or end (end) draws its left or right
// edge: a left-to-right box draws its left edge on its first fragment, a
// right-to-left one on its last.
func edgeSides(b *box, start, end bool) (left, right bool) {
	if b.st.Direction == css.RTL {
		return b.last && end, b.first && start
	}
	return b.first && start, b.last && end
}

// prepareInline resolves the edges of the inline boxes of d and lays its
// atomic inlines out against the containing block width cbw, setting the
// advance of their object characters.
func (e *engine) prepareInline(d *ifcData, cbw float64) {
	for _, k := range d.boxes {
		if k.kind == kindInline {
			resolveEdges(k, cbw)
			// Vertical padding and borders paint but take no room.
		}
	}
	for _, p := range d.paras {
		if p.p == nil {
			continue
		}
		for i := range p.p.Len() {
			info := p.infos[p.p.RunOf(i)]
			switch info.kind {
			case runAtomic:
				if e.err != nil {
					return
				}
				e.layoutAtomic(info.box, cbw)
				p.p.SetAdvance(i, max(info.box.marginW(), 0))
			case runEmpty:
				p.p.SetAdvance(i, 0)
			case runText, runTab:
			}
		}
	}
}

// paraWidths returns the width every character of p takes on a line: its
// advance, the start edges of the boxes opening before it and the end edges
// of those closing after it. object, when set, gives an atomic's width
// instead of its laid-out advance.
func (p *ifcPara) paraWidths(object func(*box) float64) []float64 {
	n := p.p.Len()
	w := make([]float64, n)
	for i := range n {
		w[i] = p.p.Advance(i)
		if object != nil {
			if info := p.infos[p.p.RunOf(i)]; info.kind == runAtomic {
				w[i] = object(info.box)
			}
		}
	}
	for _, o := range p.opens {
		if o.pos < n {
			l, _ := inlineEdges(o.box)
			w[o.pos] += l
		}
	}
	for _, c := range p.closes {
		if c.pos > 0 && c.pos <= n {
			_, r := inlineEdges(c.box)
			w[c.pos-1] += r
		}
	}
	return w
}

// wsAt returns the white-space value in force at character i of p.
func (p *ifcPara) wsAt(i int) css.WhiteSpace {
	return p.infos[p.p.RunOf(i)].ws
}

// styleAt returns the style of the run of character i of p.
func (p *ifcPara) styleAt(i int) *css.Style {
	return p.infos[p.p.RunOf(i)].box.st
}

// allowed reports a soft wrap opportunity before character i of p (CSS
// Text 3, 5): a UAX #14 opportunity where white space allows wrapping and
// word-break keep-all does not suppress it, any grapheme boundary between
// letters under word-break: break-all, any grapheme boundary under
// line-break: anywhere (5.3), and the boundary after every preserved space
// under white-space: break-spaces (3).
func (p *ifcPara) allowed(i int) bool {
	if i <= 0 || i >= p.p.Len() {
		return false
	}
	before, after := p.wsAt(i-1), p.wsAt(i)
	if !wraps(before) && !wraps(after) {
		return false
	}
	a, b := p.p.Rune(i-1), p.p.Rune(i)
	if p.styleAt(i).LineBreak == css.LineBreakAnywhere && wraps(after) && p.p.GraphemeStart(i) {
		return true
	}
	if before == css.BreakSpaces && isPreservedSpace(a) && p.infos[p.p.RunOf(i-1)].kind != runAtomic {
		return true
	}
	if after == css.BreakSpaces && isPreservedSpace(b) {
		// Spaces stay with the word before them: they wrap only after
		// themselves.
		return false
	}
	if p.p.Break(i) == text.NoBreak {
		// break-all on either side opens the boundary between two letters,
		// as at the edge of a span that sets it.
		breakAll := p.styleAt(i).WordBreak == css.BreakAll || p.styleAt(i-1).WordBreak == css.BreakAll
		if breakAll && p.p.GraphemeStart(i) && !unicode.IsSpace(b) && !unicode.IsPunct(b) && !unicode.IsSpace(a) {
			return true
		}
		return p.tailoredBreak(i)
	}
	if p.strictBreak(i) {
		return false
	}
	return p.styleAt(i).WordBreak != css.KeepAll || !isWordChar(a) || !isWordChar(b)
}

// isPreservedSpace reports a space or tab that break-spaces keeps and
// wraps after.
func isPreservedSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\u3000'
}

// isWordChar reports a letter or digit, which keep-all keeps together.
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// breakAnywhere reports whether the run of character i may break between
// any two grapheme clusters when nothing else fits: overflow-wrap
// break-word or anywhere (CSS Text 3, 5.5).
func (p *ifcPara) breakAnywhere(i int) bool {
	st := p.styleAt(i)
	return wraps(st.WhiteSpace) && (st.OverflowWrap == css.BreakWord || st.OverflowWrap == css.Anywhere || st.WordBreak == css.BreakAll)
}

// hangs reports whether a space at character i hangs at a line end:
// preserved spaces under white-space: pre do not (CSS Text 3, 4.1.3).
func (p *ifcPara) hangs(i int) bool {
	return !keepsSpaces(p.wsAt(i)) && text.IsHangingSpace(p.p.Rune(i))
}

// collapsibleSpace reports a collapsible space at character i, removed at
// the start of a line (CSS Text 3, 4.1.2).
func (p *ifcPara) collapsibleSpace(i int) bool {
	return p.p.Rune(i) == ' ' && collapses(p.wsAt(i)) && p.infos[p.p.RunOf(i)].kind == runText
}

// tabWidth returns the advance of a preserved tab at line offset x: to the
// next tab stop, a multiple of tab-size (CSS Text 3, 4.2), the next one
// when closer than half a space. A number of spaces counts the space's
// advance with its letter-spacing and word-spacing, which the shaped space
// already carries.
func (e *engine) tabWidth(b *box, x float64) float64 {
	space := e.spaceWidth(b.st, b.lang)
	step := 8 * space
	switch b.st.TabSize.Kind {
	case css.Number:
		step = max(clamp(b.st.TabSize.Value), 0) * space
	case css.Points:
		step = max(clamp(b.st.TabSize.Value), 0)
	case css.Auto, css.Percent, css.None, css.Normal, css.FlexContent, css.MinContent, css.MaxContent, css.FitContent, css.Stretch:
	}
	if step <= eps {
		return 0
	}
	next := (math.Floor(x/step+eps) + 1) * step
	if next-x < space/2 {
		next += step
	}
	return next - x
}

// breakLine returns the end of the line that starts at character start of
// p, in a width of avail with the first offset x0, following the rules of
// CSS Text 3, 5: break at the last opportunity that fits, else at a
// grapheme boundary when overflow-wrap allows, else overflow to the next
// opportunity. fits reports whether anything fits.
func (e *engine) breakLine(p *ifcPara, w []float64, start int, x0, avail float64) (int, bool) {
	n := p.p.Len()
	x := x0
	last := -1
	for i := start; i < n; i++ {
		if i > start && p.allowed(i) {
			last = i
		}
		wi := w[i]
		if info := p.infos[p.p.RunOf(i)]; info.kind == runTab {
			wi = e.tabWidth(info.box, x)
			p.p.SetAdvance(i, wi)
			w[i] = wi
		}
		if x+wi > avail+eps && !p.hangs(i) {
			if last > start {
				return last, true
			}
			if i > start && p.breakAnywhere(i) {
				j := i
				for j > start+1 && !p.p.GraphemeStart(j) {
					j--
				}
				if j > start {
					return j, true
				}
			}
			if i == start || last < 0 {
				// Nothing fits: the line overflows to the next opportunity.
				for j := i + 1; j < n; j++ {
					if p.allowed(j) {
						return j, false
					}
				}
				return n, false
			}
		}
		x += wi
	}
	return n, true
}

// vbox is an inline box present on a line, for vertical alignment (CSS 2.1,
// 10.8): its baseline shift from the root baseline, positive down, and its
// extent above and below its baseline.
type vbox struct {
	b      *box
	parent *vbox
	shift  float64
	// above and below are the box's half-leading extent (A' and D'), ca and
	// cd its content area's.
	above, below float64
	ca, cd       float64
	size         float64
	lh           float64
	// xh is the box's x-height, for vertical-align: middle.
	xh float64
	// aligned reports top or bottom alignment, settled against the line
	// box; inAligned a box inside one.
	aligned   bool
	inAligned bool
}

// inlineState is the state of one pass over an inline formatting context.
type inlineState struct {
	e      *engine
	root   *box
	d      *ifcData
	cb     cblock
	bc     *bfc
	lines  []*lineBox
	vboxes map[*box]*vbox
	// rootV is the root inline box.
	rootV *vbox
	// glyphs counts the glyphs of the line being built.
	glyphs int
	// placed holds the floats already placed.
	placed map[*box]bool
	// keep makes buildLine keep the spaces at the line end, which a line
	// cut before an overflow mark draws (CSS Overflow 4, 5.1).
	keep bool
}

// layoutInline lays out the inline formatting context of b in the content
// box cb, from cursor with the margins pending in mc, and returns the
// bottom of its last line box. It takes up to [maxFragmentationPasses]
// passes to honour orphans and widows (CSS 2.1, 13.3.3).
func (e *engine) layoutInline(b *box, cb cblock, bc *bfc, cursor float64, mc *marginState) float64 {
	d := e.ifcOf(b)
	if e.err != nil {
		return cursor
	}
	e.prepareInline(d, cb.w)
	forced := map[int]bool{}
	floats, warnings := len(bc.floats), len(e.warnings)
	saved := mc.clone()
	clampSaved := e.clampSnapshot()
	var end float64
	for pass := range maxFragmentationPasses {
		e.clampRestore(clampSaved)
		end = e.flowLines(b, d, cb, bc, cursor, mc, forced)
		if !e.paginating() || e.err != nil || pass == maxFragmentationPasses-1 {
			break
		}
		if !e.fixOrphans(b, forced) || !e.relayout(cursor) {
			break
		}
		bc.floats = bc.floats[:floats]
		e.warnings = e.warnings[:warnings]
		*mc = saved.clone()
	}
	return end
}

// fixOrphans adds to forced the line indices that must start a page so
// that every page break inside b leaves at least orphans lines before it
// and widows lines after it, and reports whether it added one.
func (e *engine) fixOrphans(b *box, forced map[int]bool) bool {
	lines := b.lines
	orphans, widows := max(b.st.Orphans, 1), max(b.st.Widows, 1)
	changed := false
	segStart := 0
	for k := 1; k < len(lines); k++ {
		if e.pageOf(lines[k].y) == e.pageOf(lines[k-1].y) {
			continue
		}
		next := len(lines)
		for j := k + 1; j < len(lines); j++ {
			if e.pageOf(lines[j].y) != e.pageOf(lines[j-1].y) {
				next = j
				break
			}
		}
		before, after := k-segStart, next-k
		switch {
		case before < orphans && segStart == 0 && !forced[0] && !e.atPageTop(lines[0].y):
			forced[0] = true
			changed = true
		case after < widows && next == len(lines):
			// A block too short to leave orphans lines before the break
			// and widows after it keeps its orphans and breaks, as
			// Chromium does.
			if k2 := k - (widows - after); k2-segStart >= orphans && k2 > segStart && !forced[k2] {
				forced[k2] = true
				changed = true
			}
		}
		segStart = k
	}
	return changed
}

// flowLines breaks the paragraphs of d into line boxes and places them from
// cursor, line by line, beside the floats of bc, and returns the bottom of
// the last line. A line index in forced starts a new page.
func (e *engine) flowLines(b *box, d *ifcData, cb cblock, bc *bfc, cursor float64, mc *marginState, forced map[int]bool) float64 {
	s := &inlineState{e: e, root: b, d: d, cb: cb, bc: bc, vboxes: map[*box]*vbox{}, placed: map[*box]bool{}}
	for _, k := range d.boxes {
		k.anchorSet = false
	}
	b.lines = nil
	s.rootV = s.newVbox(b, nil)
	y := cursor
	first := true
	indent := resolve(b.st.TextIndent, cb.w)
	strutH := s.rootV.above + s.rootV.below
	for _, p := range d.paras {
		if e.err != nil {
			return y
		}
		if (p.forced || p.p != nil && p.p.Len() > 0) && e.clampDiscards() {
			// A line clamp container discards what follows its last line
			// (CSS Overflow 4, 6).
			return s.discard(d, p, y)
		}
		if p.p == nil || p.p.Len() == 0 {
			s.placeFloats(p, 0, math.MaxInt, pendingTop(y, mc, first), mc, first)
			s.staticRest(p, y, mc, first)
			if p.forced {
				top := y
				if first {
					top = e.pastMargin(y, mc.strut, mc.kept)
					mc.commitFloats(top)
				}
				l := &lineBox{x: cb.x, w: cb.w, h: strutH, baseline: s.rootV.above}
				top = s.position(top, strutH, forced)
				l.y = top
				if first {
					mc.commit(top)
					first = false
				}
				y = top + l.h
				s.lines = append(s.lines, l)
			}
			y = s.clearBreak(p, y)
			continue
		}
		w := p.paraWidths(nil)
		n := p.p.Len()
		pos := 0
		paraFirst := true
		for pos < n {
			if e.err != nil {
				return y
			}
			if len(s.lines) >= maxLinesPerBlock {
				e.warnOnce(codeTooManyLines, fmt.Sprintf("a block of more than %d lines was cut short", maxLinesPerBlock), y)
				b.lines = s.lines
				return y
			}
			if !paraFirst {
				for pos < n && p.collapsibleSpace(pos) {
					pos++
				}
				if pos >= n {
					break
				}
				if e.clampDiscards() {
					return s.discard(d, p, y)
				}
			}
			top := y
			if first {
				// The margins resolve here: the floats placed under them
				// take their place before the line fits beside them.
				top = e.pastMargin(y, mc.strut, mc.kept)
				mc.commitFloats(top)
			}
			s.placeFloats(p, pos, pos, top, mc, false)
			lineIndent := 0.0
			if len(s.lines) == 0 {
				lineIndent = indent
			}
			top, end, left, right, l := s.fitLine(p, w, pos, lineIndent, top, strutH)
			floatsBefore := len(bc.floats)
			s.placeFloatsBeside(p, pos, end, l, top)
			if newTop := s.position(top, l.h, forced); newTop != top {
				top = newTop
				nl, nw := bandOf(bc, top, l.h, cb)
				if len(bc.floats) == floatsBefore && (math.Abs(nl-left) > eps || math.Abs(nl+nw-right) > eps) {
					// The page cut moved the line to where the floats
					// leave another width: break it again there.
					top, end, _, _, l = s.fitLine(p, w, pos, lineIndent, top, strutH)
					top = s.position(top, l.h, forced)
					nl, nw = bandOf(bc, top, l.h, cb)
				}
				l.x, l.w = nl, nw
			}
			if e.clampOverflows(top, l.h) {
				return s.discard(d, p, y)
			}
			l = s.overflowMark(p, l, pos, end, lineIndent)
			last := end == n && (p.forced || isLastPara(d, p))
			if e.clampKeep(&clampLine{s: s, p: p, l: l, start: pos, end: end, indent: lineIndent}) {
				last = true
			}
			s.align(l, lineIndent, last)
			if first {
				mc.commit(top)
				first = false
			}
			l.y = top
			s.settle(l)
			s.lines = append(s.lines, l)
			s.recordAnchors(l)
			s.staticLine(p, pos, end, l)
			y = top + e.advance(l)
			s.placeFloats(p, pos+1, end, y, mc, false)
			paraFirst = false
			pos = end
		}
		s.placeFloats(p, n, math.MaxInt, pendingTop(y, mc, first), mc, first)
		s.staticRest(p, y, mc, first)
		y = s.clearBreak(p, y)
	}
	b.lines = s.lines
	return y
}

// pendingTop returns y with the margins pending in mc added when pending.
func pendingTop(y float64, mc *marginState, pending bool) float64 {
	if pending {
		return y + mc.strut.sum()
	}
	return y
}

// fitLine breaks the line of p starting at pos beside the floats, from
// top down, and returns where it lands, its end, the band it was broken in
// and the line built. The line is first fitted beside the floats over the
// strut's height h; when the line built turns out taller and a float
// narrows the band over its full height, it is broken again in that band
// (CSS 2.1, 9.5: line boxes beside a float are shortened to make room for
// its margin box).
func (s *inlineState) fitLine(p *ifcPara, w []float64, pos int, indent, top, h float64) (float64, int, float64, float64, *lineBox) {
	e, bc, cb := s.e, s.bc, s.cb
	var end int
	var left, right float64
	var l *lineBox
	for range maxFitPasses {
		for range len(bc.floats) + 1 {
			left, right = bc.band(top, h, cb.x, cb.x+cb.w)
			var fits bool
			end, fits = e.breakLine(p, w, pos, indent, right-left)
			if fits || !bc.narrowed(top, h, cb.x, cb.x+cb.w) {
				break
			}
			next := bc.below(top, h)
			if math.IsInf(next, 1) {
				break
			}
			top = next
		}
		l = s.buildLine(p, pos, end, left, right)
		if l.h <= h+eps {
			break
		}
		if l2, r2 := bc.band(top, l.h, cb.x, cb.x+cb.w); l2 <= left+eps && r2 >= right-eps {
			break
		}
		h = l.h
	}
	return top, end, left, right, l
}

// clearBreak returns where the line after the paragraph p starts when the
// paragraph ended at y: below the floats the clear value of its line break
// names (CSS 2.1, 9.5.2, which HTML applies to br).
func (s *inlineState) clearBreak(p *ifcPara, y float64) float64 {
	if !p.forced || p.clear == css.ClearNone {
		return y
	}
	return max(y, s.bc.clearTo(p.clear))
}

// advance returns how far the line l, placed, moves the flow down: its
// height, or the page area height for a line taller than its page for its
// text alone, which is cut at the page bottom. It sets l.overflows.
func (e *engine) advance(l *lineBox) float64 {
	l.overflows = false
	if !e.paginating() {
		return l.h
	}
	area := e.areaH(e.pageOf(l.y + eps))
	if l.h <= area+eps {
		return l.h
	}
	for _, it := range l.items {
		if it.atomic != nil && it.atomic.marginH() > area+eps {
			l.overflows = true
			return l.h
		}
	}
	return area
}

// isLastPara reports whether p is the last paragraph of d.
func isLastPara(d *ifcData, p *ifcPara) bool {
	return len(d.paras) > 0 && d.paras[len(d.paras)-1] == p
}

// bandOf returns the left edge and width of the line box space at y, for a
// line of height h, in cb beside the floats of bc.
func bandOf(bc *bfc, y, h float64, cb cblock) (float64, float64) {
	l, r := bc.band(y, h, cb.x, cb.x+cb.w)
	return l, r - l
}

// position returns where the line of height h whose top would be at top
// lands: on the next page when it straddles a cut or when its index is
// forced.
func (s *inlineState) position(top, h float64, forced map[int]bool) float64 {
	e := s.e
	if !e.paginating() {
		return top
	}
	if forced[len(s.lines)] && !e.atPageTop(top) {
		top = e.nextPage(top)
	}
	return e.fit(top, h)
}

// placeFloats lays out the floats of p anchored at a character position in
// [from, to] at or below top, recording them in mc when margins are
// pending.
func (s *inlineState) placeFloats(p *ifcPara, from, to int, top float64, mc *marginState, pending bool) {
	for _, f := range p.floats {
		if f.pos < from || f.pos > to || s.placed[f.box] {
			continue
		}
		s.placed[f.box] = true
		i := s.e.layoutFloat(f.box, s.cb, s.bc, top)
		if pending {
			mc.floatPlaced(f.box, s.bc, i, top)
		}
	}
}

// placeFloatsBeside places the floats anchored inside the line (start,
// end], the break after it included, at the line's top when they fit
// beside its content, moving the line beside them (CSS 2.1, 9.5.1, rule
// 8): a float met at the end of a line's content still belongs to that
// line. The others wait for the line's bottom.
func (s *inlineState) placeFloatsBeside(p *ifcPara, start, end int, l *lineBox, top float64) {
	for _, f := range p.floats {
		// A float at the paragraph's end belongs to its last line too.
		if f.pos <= start || f.pos > end || f.pos == end && end < p.p.Len() || s.placed[f.box] {
			continue
		}
		resolveEdges(f.box, s.cb.w)
		fw := f.box.m[css.Left] + f.box.m[css.Right] + hEdges(f.box)
		if f.box.kind == kindReplaced {
			w, _ := s.e.replacedSize(f.box, s.cb)
			fw += w
		} else if w, ok := specWidth(f.box, f.box.st.Width, s.cb.w); ok {
			fw += w
		} else {
			fw += s.e.shrinkToFit(f.box, s.cb.w)
		}
		if contentWidth(l)+fw > l.w+eps {
			continue
		}
		s.e.layoutFloat(f.box, s.cb, s.bc, top)
		s.placed[f.box] = true
		l.x, l.w = bandOf(s.bc, top, l.h, s.cb)
	}
}

// contentWidth returns the width the items of l span.
func contentWidth(l *lineBox) float64 {
	w := 0.0
	for _, it := range l.items {
		w = max(w, it.x+it.w)
	}
	return w
}

// newVbox returns the vertical alignment record of the inline box b, or
// of the root inline box when parent is nil.
func (s *inlineState) newVbox(b *box, parent *vbox) *vbox {
	e := s.e
	m := e.faceMetrics(b.st)
	lh := e.lineHeight(b.st)
	half := (lh - (m.Ascent + m.Descent)) / 2
	v := &vbox{b: b, parent: parent, above: m.Ascent + half, below: m.Descent + half, ca: m.Ascent, cd: m.Descent, size: e.fontSize(b.st), lh: lh, xh: m.XHeight}
	if parent == nil {
		return v
	}
	v.inAligned = parent.inAligned || parent.aligned
	v.shift = alignShift(b.st.VerticalAlign, parent, v.above, v.below, lh)
	if k := b.st.VerticalAlign.Kind; k == css.AlignTop || k == css.AlignBottom {
		v.aligned = true
		v.shift = parent.shift
	}
	return v
}

// alignShift returns the baseline shift of a box with extent above and
// below its baseline, aligned in parent by va (CSS 2.1, 10.8.1). lh is the
// box's line height, for percentages.
func alignShift(va css.VerticalAlign, parent *vbox, above, below, lh float64) float64 {
	ps := parent.shift
	switch va.Kind {
	case css.Sub:
		return ps + parent.size*0.2
	case css.Super:
		return ps - parent.size/3
	case css.TextTop:
		return ps - parent.ca + above
	case css.TextBottom:
		return ps + parent.cd - below
	case css.Middle:
		return ps - parent.xh/2 + (above-below)/2
	case css.AlignLength:
		return ps - resolve(va.Length, lh)
	case css.Baseline, css.AlignTop, css.AlignBottom:
	}
	return ps
}

// vboxOf returns the vertical alignment record of inline box b, creating
// the records of its ancestors up to the root.
func (s *inlineState) vboxOf(b *box) *vbox {
	if b == nil || b == s.root {
		return s.rootV
	}
	if v, ok := s.vboxes[b]; ok {
		return v
	}
	v := s.newVbox(b, s.vboxOf(b.parent))
	s.vboxes[b] = v
	return v
}

// buildLine builds the line box of characters [start, end) of p in the
// band from left to right, its items from the line start before
// alignment.
func (s *inlineState) buildLine(p *ifcPara, start, end int, left, right float64) *lineBox {
	e := s.e
	s.vboxes = map[*box]*vbox{}
	// The spaces at the line end hang (CSS Text 3, 4.1.3): they are drawn
	// but neither fit nor align. The collapsible ones among them are
	// removed instead (4.1.2) and draw nothing.
	hang := !s.keep && end > start && !keepsSpaces(p.wsAt(end-1))
	hangFrom := end
	for hang && hangFrom > start && text.IsHangingSpace(p.p.Rune(hangFrom-1)) {
		hangFrom--
	}
	trim := end
	for trim > hangFrom && p.collapsibleSpace(trim-1) {
		trim--
	}
	pl := p.p.LineTrimmed(start, end, trim)
	l := &lineBox{x: left, w: right - left, hangLeft: p.p.Direction() == text.RightToLeft}
	for i := hangFrom; i < trim; i++ {
		l.hangW += p.p.Advance(i)
	}
	type extent struct{ x0, x1 float64 }
	ext := map[*box]*extent{}
	widen := func(b *box, x0, x1 float64) {
		if g, ok := ext[b]; ok {
			g.x0, g.x1 = min(g.x0, x0), max(g.x1, x1)
			return
		}
		ext[b] = &extent{x0, x1}
	}
	s.glyphs = 0
	x := 0.0
	opened, closed := map[*box]bool{}, map[*box]bool{}
	for _, sp := range pl.Spans {
		info := p.infos[sp.Run]
		// A left-to-right box draws its start edge on the left of its
		// content and its end edge on the right; a right-to-left box the
		// other way round, whatever the direction of the text inside.
		s.closeEdges(p, sp.End, true, closed, &x, widen)
		s.openEdges(p, sp.Start, true, opened, &x, widen)
		ascent, descent, gap := sp.Ascent, sp.Descent, sp.LineGap
		if s.e.opts.PixelFontMetrics {
			ascent, descent, gap = pixelMetrics(ascent, descent, gap)
		}
		it := &lineItem{
			box: info.box, x: x, w: sp.Width, ascent: ascent, descent: descent, gap: gap,
			ulPos: sp.UnderlinePosition, ulThick: sp.UnderlineThickness, stPos: sp.StrikeoutPosition, stThick: sp.StrikeoutThickness,
			start: sp.Start, end: sp.End, rtl: sp.RTL, hang: max(sp.End-max(sp.Start, hangFrom), 0),
		}
		switch info.kind {
		case runAtomic:
			it.atomic = info.box
		case runText:
			if sp.Text != nil && s.glyphs+len(sp.Text.Glyphs) <= maxGlyphsPerLine {
				it.text = sp.Text
				s.glyphs += len(sp.Text.Glyphs)
			} else if sp.Text != nil {
				e.warnOnce(codeTooManyGlyphs, fmt.Sprintf("a line of more than %d glyphs was cut short", maxGlyphsPerLine), -1)
			}
		case runTab, runEmpty:
		}
		x += sp.Width
		l.items = append(l.items, it)
		for a := s.parentInline(info.box); a != nil; a = s.parentInline(a) {
			widen(a, it.x, it.x+it.w)
		}
		if info.kind == runEmpty {
			widen(info.box, it.x, it.x)
		}
		s.openEdges(p, sp.Start, false, opened, &x, widen)
		s.closeEdges(p, sp.End, false, closed, &x, widen)
	}
	l.frags = nil
	present := make([]*box, 0, len(ext))
	for k := range ext {
		if k.kind == kindInline {
			present = append(present, k)
		}
	}
	slices.SortFunc(present, func(a, b *box) int { return s.d.order[a] - s.d.order[b] })
	for _, k := range present {
		g := ext[k]
		f := &inlineFrag{box: k, x0: g.x0, x1: g.x1}
		f.left, f.right = edgeSides(k, p.opensIn(k, start, end), p.closesIn(k, start, end))
		l.frags = append(l.frags, f)
	}
	s.vertical(l)
	return l
}

// parentInline returns the inline box b sits in, nil at the root.
func (s *inlineState) parentInline(b *box) *box {
	if b == nil || b.parent == nil || b.parent == s.root || b.parent.kind != kindInline {
		return nil
	}
	return b.parent
}

// opensIn reports whether inline box b opens in the characters [start,
// end) of p.
func (p *ifcPara) opensIn(b *box, start, end int) bool {
	pos, ok := p.openPos[b]
	return ok && pos >= start && (pos < end || pos == end && end == p.n)
}

// closesIn reports whether inline box b closes in the characters [start,
// end) of p.
func (p *ifcPara) closesIn(b *box, start, end int) bool {
	pos, ok := p.closePos[b]
	return ok && (pos > start && pos <= end || pos == start && start == 0)
}

// openEdges advances x past the start edges of the inline boxes opening at
// pos that sit on the left of their content when left, a left-to-right
// box's, else on its right, a right-to-left box's.
func (s *inlineState) openEdges(p *ifcPara, pos int, left bool, opened map[*box]bool, x *float64, widen func(*box, float64, float64)) {
	// openAt lists the boxes outermost first: on the right of the content
	// the innermost comes first.
	list := p.openAt[pos]
	if !left {
		list = reversed(list)
	}
	for _, o := range list {
		if opened[o] || (o.st.Direction != css.RTL) != left {
			continue
		}
		opened[o] = true
		if o.first {
			s.edge(o, left, x, widen)
		}
	}
}

// closeEdges advances x past the end edges of the inline boxes closing at
// pos that sit on the left of their content when left, a right-to-left
// box's, else on its right, a left-to-right box's.
func (s *inlineState) closeEdges(p *ifcPara, pos int, left bool, closed map[*box]bool, x *float64, widen func(*box, float64, float64)) {
	// closeAt lists the boxes innermost first: on the left of the content
	// the outermost comes first.
	list := p.closeAt[pos]
	if left {
		list = reversed(list)
	}
	for _, c := range list {
		if closed[c] || (c.st.Direction == css.RTL) != left {
			continue
		}
		closed[c] = true
		if c.last {
			s.edge(c, left, x, widen)
		}
	}
}

// edge advances x past the left edge of b when left, its margin outside its
// border box, else past its right edge, recording the border box's side.
func (s *inlineState) edge(b *box, left bool, x *float64, widen func(*box, float64, float64)) {
	if left {
		*x += b.m[css.Left]
		widen(b, *x, *x)
		*x += b.bd[css.Left] + b.pd[css.Left]
		return
	}
	*x += b.pd[css.Right] + b.bd[css.Right]
	widen(b, *x, *x)
	*x += b.m[css.Right]
}

// vertical computes the line box height and the baseline of every item
// (CSS 2.1, 10.8).
func (s *inlineState) vertical(l *lineBox) {
	root := s.rootV
	top, bottom := -root.above, root.below
	include := func(v *vbox, t, b float64) {
		if v != nil && (v.inAligned || v.aligned) {
			return
		}
		top, bottom = min(top, t), max(bottom, b)
	}
	type atomicV struct {
		it           *lineItem
		v            *vbox
		above, below float64
		aligned      bool
	}
	var atomics []atomicV
	for _, it := range l.items {
		parent := s.vboxOf(s.parentInline(it.box))
		if it.atomic != nil {
			a := it.atomic
			above := a.marginH()
			if a.hasBaseline {
				above = a.m[css.Top] + a.baseline
			}
			below := a.marginH() - above
			av := atomicV{it: it, v: parent, above: above, below: below}
			shift := alignShift(a.st.VerticalAlign, parent, above, below, s.e.lineHeight(a.st))
			if k := a.st.VerticalAlign.Kind; k == css.AlignTop || k == css.AlignBottom {
				av.aligned = true
				shift = parent.shift
			}
			it.baseline = shift
			it.ascent, it.descent = above, below
			if !av.aligned {
				include(parent, shift-above, shift+below)
			}
			atomics = append(atomics, av)
			continue
		}
		var v *vbox
		if it.box.kind == kindInline {
			v = s.vboxOf(it.box)
		} else {
			v = parent
		}
		it.baseline = v.shift
		lh := v.lh
		if v.b.st.LineHeight.Kind == css.Normal && it.text != nil {
			// line-height: normal lays a glyph run out at the normal line
			// height of the face that draws it, a fallback face included,
			// as CSS Inline 3, 5.1 lets the user agent do and browsers do:
			// text a fallback face draws taller grows the line.
			lh = it.ascent + it.descent + max(it.gap, 0)
		}
		half := (lh - (it.ascent + it.descent)) / 2
		// An empty inline box kept for its edges has no glyphs to lay out:
		// its own box, included below, is all it adds (CSS 2.1, 10.8.1).
		empty := it.text == nil && it.ascent == 0 && it.descent == 0
		if it.text != nil || it.box.kind == kindInline && !empty {
			include(v, v.shift-wider(it.ascent+half, v.above), v.shift+wider(it.descent+half, v.below))
		}
	}
	for _, v := range s.vboxes {
		include(v, v.shift-v.above, v.shift+v.below)
	}
	// Top and bottom aligned boxes settle against the line box (CSS 2.1,
	// 10.8.1), growing it when taller.
	aligned := make([]*box, 0, len(s.vboxes))
	for k, v := range s.vboxes {
		if v.aligned && !v.inAligned {
			aligned = append(aligned, k)
		}
	}
	slices.SortFunc(aligned, func(a, b *box) int { return s.d.order[a] - s.d.order[b] })
	for _, k := range aligned {
		v := s.vboxes[k]
		h := v.above + v.below
		if h > bottom-top {
			if k.st.VerticalAlign.Kind == css.AlignTop {
				bottom = top + h
			} else {
				top = bottom - h
			}
		}
		old := v.shift
		if k.st.VerticalAlign.Kind == css.AlignTop {
			v.shift = top + v.above
		} else {
			v.shift = bottom - v.below
		}
		for _, w := range s.vboxes {
			for a := w.parent; a != nil; a = a.parent {
				if a == v {
					w.shift += v.shift - old
					break
				}
			}
		}
	}
	for _, av := range atomics {
		if !av.aligned {
			continue
		}
		h := av.above + av.below
		if h > bottom-top {
			if av.it.atomic.st.VerticalAlign.Kind == css.AlignTop {
				bottom = top + h
			} else {
				top = bottom - h
			}
		}
		if av.it.atomic.st.VerticalAlign.Kind == css.AlignTop {
			av.it.baseline = top + av.above
		} else {
			av.it.baseline = bottom - av.below
		}
	}
	for _, it := range l.items {
		if it.atomic == nil {
			v := s.vboxOf(s.parentInline(it.box))
			if it.box.kind == kindInline {
				v = s.vboxOf(it.box)
			}
			it.baseline = v.shift
		}
	}
	l.h = bottom - top
	l.baseline = -top
	for _, it := range l.items {
		it.baseline += l.baseline
	}
	for _, f := range l.frags {
		v := s.vboxOf(f.box)
		b := v.shift + l.baseline
		f.top = b - v.ca - f.box.pd[css.Top] - f.box.bd[css.Top]
		f.bottom = b + v.cd + f.box.pd[css.Bottom] + f.box.bd[css.Bottom]
	}
}

// wider returns a when it exceeds b by more than the rounding of shaped
// metrics, b otherwise: a face's shaped extent agrees with its indexed
// metrics to a few thousandths of a point.
func wider(a, b float64) float64 {
	if a > b+metricSlack {
		return a
	}
	return b
}

// metricSlack is the difference between two vertical metrics below which
// they count as equal.
const metricSlack = 0.01

// align applies text-align and text-indent to the items of l (CSS Text 3,
// 6.1 and 8.1), justifying at the word separators unless last.
func (s *inlineState) align(l *lineBox, indent float64, last bool) {
	st := s.root.st
	ltr := st.Direction != css.RTL
	width := 0.0
	for _, it := range l.items {
		width = max(width, it.x+it.w)
	}
	for _, f := range l.frags {
		width = max(width, f.x1)
	}
	width -= l.hangW
	free := l.w - width - indent
	start, endOff := indent, 0.0
	if !ltr {
		start, endOff = free, indent
	}
	off := start
	align := lineAlign(st, last)
	if free > 0 {
		switch align {
		case css.AlignStart, css.AlignAuto:
		case css.AlignEnd:
			off = endOff
			if ltr {
				off = indent + free
			}
		case css.AlignLeft:
			off = indent
			if !ltr {
				off = 0
			}
		case css.AlignRight:
			off = indent + free
			if !ltr {
				off = free
			}
		case css.AlignCenter:
			off = free / 2
			if ltr {
				off += indent
			}
		case css.AlignJustify, css.AlignJustifyAll:
			// A line with no justification opportunity aligns to its
			// start (CSS Text 3, 7.1).
			if s.justify(l, free) {
				off = indent
				if !ltr {
					off = 0
				}
			}
		}
	}
	if l.hangLeft {
		// The hanging spaces come first, left of the aligned content.
		off -= l.hangW
	}
	for _, it := range l.items {
		it.x += off
		if it.text != nil {
			it.text.Origin = compose.Point{X: it.x, Y: it.baseline}
		}
	}
	for _, f := range l.frags {
		f.x0 += off
		f.x1 += off
	}
}

// lineAlign returns the alignment of a line of a block of style st, last
// for the last line or one before a forced break: text-align-last there,
// whose auto takes text-align with justify as start (CSS Text 3, 6.3), and
// justify-all justifying every line (6.1).
func lineAlign(st *css.Style, last bool) css.TextAlign {
	align := st.TextAlign
	if align == css.AlignJustifyAll {
		align = css.AlignJustify
		if last && st.TextAlignLast == css.AlignAuto {
			return css.AlignJustify
		}
	}
	if !last {
		return align
	}
	switch st.TextAlignLast {
	case css.AlignAuto:
		if align == css.AlignJustify {
			return css.AlignStart
		}
		return align
	case css.AlignJustifyAll:
		return css.AlignJustify
	case css.AlignStart, css.AlignEnd, css.AlignLeft, css.AlignRight, css.AlignCenter, css.AlignJustify:
	}
	return st.TextAlignLast
}

// settle moves the atomic inlines of l, placed at l.y, to their place.
func (s *inlineState) settle(l *lineBox) {
	for _, it := range l.items {
		if a := it.atomic; a != nil {
			a.shift(l.x+it.x-(a.x-a.m[css.Left]), l.y+it.baseline-it.ascent-(a.y-a.m[css.Top]))
		}
	}
}

// recordAnchors records where the boxes of l first land, for
// Result.Anchors.
func (s *inlineState) recordAnchors(l *lineBox) {
	for _, f := range l.frags {
		if !f.box.anchorSet {
			f.box.anchorX, f.box.anchorY, f.box.anchorSet = l.x+f.x0, l.y+f.top, true
		}
	}
	for _, it := range l.items {
		if b := it.box; b != nil && !b.anchorSet {
			b.anchorX, b.anchorY, b.anchorSet = l.x+it.x, l.y+it.baseline-it.ascent, true
		}
	}
}

// inlineIntrinsic returns the min- and max-content widths of the inline
// formatting context of b (CSS Sizing 3, 5.1): its widest unbreakable
// piece and its widest paragraph.
func (e *engine) inlineIntrinsic(b *box) (float64, float64) {
	d := e.ifcOf(b)
	if e.err != nil {
		return 0, 0
	}
	for _, k := range d.boxes {
		if k.kind == kindInline {
			resolveEdges(k, 0)
		}
	}
	lo, hi := 0.0, 0.0
	indent := max(resolve(b.st.TextIndent, 0), 0)
	for pi, p := range d.paras {
		// The floats of a paragraph sit beside its line in max-content
		// layout, side by side until one clears the others (CSS Sizing 3,
		// 5.2, as browsers lay floats out at the max-content width).
		floats := 0.0
		for _, f := range p.floats {
			fl, fh := e.contribution(f.box)
			lo, hi = max(lo, fl), max(hi, fh)
			if f.box.st.Clear != css.ClearNone {
				floats = 0
			}
			floats += fh
			hi = max(hi, floats)
		}
		if p.p == nil {
			continue
		}
		minW := p.paraWidths(func(a *box) float64 { l, _ := e.contribution(a); return l })
		maxW := p.paraWidths(func(a *box) float64 { _, h := e.contribution(a); return h })
		n := p.p.Len()
		x := 0.0
		if pi == 0 {
			x = indent
		}
		trail := 0.0
		for i := range n {
			wi := maxW[i]
			if info := p.infos[p.p.RunOf(i)]; info.kind == runTab {
				wi = e.tabWidth(info.box, x)
			}
			x += wi
			if p.hangs(i) {
				trail += wi
			} else {
				trail = 0
			}
		}
		hi = max(hi, x-trail+floats)
		seg, trail := 0.0, 0.0
		anywhere := false
		for i := range n {
			if i > 0 && (p.allowed(i) || anywhere && p.p.GraphemeStart(i)) {
				lo = max(lo, seg-trail)
				seg, trail = 0, 0
			}
			st := p.styleAt(i)
			anywhere = (st.OverflowWrap == css.Anywhere || st.LineBreak == css.LineBreakAnywhere) && wraps(st.WhiteSpace)
			wi := minW[i]
			if info := p.infos[p.p.RunOf(i)]; info.kind == runTab {
				wi = e.tabWidth(info.box, seg)
			}
			seg += wi
			if p.hangs(i) {
				trail += wi
			} else {
				trail = 0
			}
		}
		lo = max(lo, seg-trail)
	}
	return lo, max(hi, lo)
}

// reversed returns a reversed copy of bs.
func reversed(bs []*box) []*box {
	out := slices.Clone(bs)
	slices.Reverse(out)
	return out
}
