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
	// descent its extent.
	baseline, ascent, descent float64
	ulPos, ulThick            float64
	stPos, stThick            float64
}

// inlineFrag is the part of an inline box on one line, painted with its
// background and borders (CSS 2.1, 8.6 and CSS Backgrounds 3, 5.4).
type inlineFrag struct {
	box *box
	// x0 and x1 are the border box's left and right edges, top and bottom
	// its top and bottom, relative to the line box.
	x0, x1, top, bottom float64
	// start and end report whether the fragment draws the box's start and
	// end edges (box-decoration-break: slice).
	start, end bool
}

// edges returns the start and end edge widths of inline box b: margin,
// border and padding.
func inlineEdges(b *box) (float64, float64) {
	l, r := 0.0, 0.0
	if b.first {
		l = b.m[css.Left] + b.bd[css.Left] + b.pd[css.Left]
	}
	if b.last {
		r = b.m[css.Right] + b.bd[css.Right] + b.pd[css.Right]
	}
	return l, r
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
// word-break keep-all does not suppress it, or any grapheme boundary
// between letters under word-break: break-all.
func (p *ifcPara) allowed(i int) bool {
	if i <= 0 || i >= p.p.Len() {
		return false
	}
	before, after := p.wsAt(i-1), p.wsAt(i)
	if !wraps(before) && !wraps(after) {
		return false
	}
	a, b := p.p.Rune(i-1), p.p.Rune(i)
	if p.p.Break(i) == text.NoBreak {
		st := p.styleAt(i)
		return st.WordBreak == css.BreakAll && p.p.GraphemeStart(i) && !unicode.IsSpace(b) && !unicode.IsPunct(b) && !unicode.IsSpace(a)
	}
	return p.styleAt(i).WordBreak != css.KeepAll || !isWordChar(a) || !isWordChar(b)
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
	return p.wsAt(i) != css.Pre && text.IsHangingSpace(p.p.Rune(i))
}

// collapsibleSpace reports a collapsible space at character i, removed at
// the start of a line (CSS Text 3, 4.1.2).
func (p *ifcPara) collapsibleSpace(i int) bool {
	return p.p.Rune(i) == ' ' && collapses(p.wsAt(i)) && p.infos[p.p.RunOf(i)].kind == runText
}

// tabWidth returns the advance of a preserved tab at line offset x: to the
// next tab stop, a multiple of tab-size (CSS Text 3, 4.2), the next one
// when closer than half a space.
func (e *engine) tabWidth(b *box, x float64) float64 {
	space := e.spaceWidth(b.st, b.lang)
	step := 8 * space
	switch b.st.TabSize.Kind {
	case css.Number:
		step = max(clamp(b.st.TabSize.Value), 0) * (space + max(resolve(b.st.LetterSpacing, 0), 0))
	case css.Points:
		step = max(clamp(b.st.TabSize.Value), 0)
	case css.Auto, css.Percent, css.None, css.Normal:
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
	saved := marginState{strut: mc.strut, waiting: slices.Clone(mc.waiting)}
	var end float64
	for pass := range maxFragmentationPasses {
		end = e.flowLines(b, d, cb, bc, cursor, mc, forced)
		if !e.paginating() || e.err != nil || pass == maxFragmentationPasses-1 {
			break
		}
		if !e.fixOrphans(b, forced) || !e.relayout(cursor) {
			break
		}
		bc.floats = bc.floats[:floats]
		e.warnings = e.warnings[:warnings]
		mc.strut, mc.waiting = saved.strut, slices.Clone(saved.waiting)
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
		if p.p == nil || p.p.Len() == 0 {
			s.placeFloats(p, 0, math.MaxInt, y, mc, first)
			if p.forced {
				top := y
				if first {
					top = y + mc.strut.sum()
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
			}
			top := y
			if first {
				top = y + mc.strut.sum()
			}
			s.placeFloats(p, pos, pos, top, mc, first)
			lineIndent := 0.0
			if len(s.lines) == 0 {
				lineIndent = indent
			}
			var end int
			var left, right float64
			for range len(bc.floats) + 1 {
				left, right = bc.band(top, strutH, cb.x, cb.x+cb.w)
				var fits bool
				end, fits = e.breakLine(p, w, pos, lineIndent, right-left)
				if fits || !bc.narrowed(top, strutH, cb.x, cb.x+cb.w) {
					break
				}
				next := bc.below(top, strutH)
				if math.IsInf(next, 1) {
					break
				}
				top = next
			}
			last := end == n && (p.forced || isLastPara(d, p))
			l := s.buildLine(p, pos, end, left, right)
			s.placeFloatsBeside(p, pos, end, l, top, mc, first)
			if newTop := s.position(top, l.h, forced); newTop != top {
				top = newTop
				l.x, l.w = bandOf(bc, top, l.h, cb)
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
			adv := l.h
			if e.paginating() {
				adv = min(adv, e.pageH)
			}
			y = top + adv
			s.placeFloats(p, pos+1, end, y, mc, false)
			paraFirst = false
			pos = end
		}
		s.placeFloats(p, n, math.MaxInt, y, mc, first)
	}
	b.lines = s.lines
	return y
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
// forced, warning when it is taller than a page.
func (s *inlineState) position(top, h float64, forced map[int]bool) float64 {
	e := s.e
	if !e.paginating() {
		return top
	}
	if forced[len(s.lines)] && !e.atPageTop(top) {
		top = e.nextPage(top)
	}
	top = e.fit(top, h)
	if h > e.pageH+eps {
		e.warn(codeClipped, "a line box taller than the page area was clipped", top)
	}
	return top
}

// placeFloats lays out the floats of p anchored at a character position in
// [from, to] at or below y.
func (s *inlineState) placeFloats(p *ifcPara, from, to int, y float64, mc *marginState, pending bool) {
	for _, f := range p.floats {
		if f.pos < from || f.pos > to || s.placed[f.box] {
			continue
		}
		s.placed[f.box] = true
		top := y
		if pending {
			top = y + mc.strut.sum()
		}
		s.e.layoutFloat(f.box, s.cb, s.bc, top)
	}
}

// placeFloatsBeside places the floats anchored inside the line [start, end)
// at the line's top when they fit beside its content, moving the line
// beside them (CSS 2.1, 9.5.1, rule 8). The others wait for the line's
// bottom.
func (s *inlineState) placeFloatsBeside(p *ifcPara, start, end int, l *lineBox, top float64, mc *marginState, pending bool) {
	for _, f := range p.floats {
		if f.pos <= start || f.pos >= end || s.placed[f.box] {
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
		t := top
		if pending {
			t = top + mc.strut.sum()
		}
		s.e.layoutFloat(f.box, s.cb, s.bc, t)
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
	v := &vbox{b: b, parent: parent, above: m.Ascent + half, below: m.Descent + half, ca: m.Ascent, cd: m.Descent, size: e.fontSize(b.st), lh: lh}
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
		// The parent's x-height is taken as half its em.
		return ps - parent.size/4 + (above-below)/2
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
	hang := end > start && p.wsAt(end-1) != css.Pre
	pl := p.p.Line(start, end, hang)
	l := &lineBox{x: left, w: right - left}
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
	opened := map[*box]bool{}
	for _, sp := range pl.Spans {
		info := p.infos[sp.Run]
		s.leadEdges(p, sp.Start, opened, &x, widen)
		it := &lineItem{
			box: info.box, x: x, w: sp.Width, ascent: sp.Ascent, descent: sp.Descent,
			ulPos: sp.UnderlinePosition, ulThick: sp.UnderlineThickness, stPos: sp.StrikeoutPosition, stThick: sp.StrikeoutThickness,
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
		s.trailEdges(p, sp.End, &x, widen)
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
		f.start = k.first && p.opensIn(k, start, end)
		f.end = k.last && p.closesIn(k, start, end)
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

// leadEdges advances x past the start edges of the inline boxes opening at
// pos, recording where their border boxes start.
func (s *inlineState) leadEdges(p *ifcPara, pos int, opened map[*box]bool, x *float64, widen func(*box, float64, float64)) {
	for _, o := range p.openAt[pos] {
		if opened[o] {
			continue
		}
		opened[o] = true
		if !o.first {
			continue
		}
		*x += o.m[css.Left]
		widen(o, *x, *x)
		*x += o.bd[css.Left] + o.pd[css.Left]
	}
}

// trailEdges advances x past the end edges of the inline boxes closing at
// pos, recording where their border boxes end.
func (s *inlineState) trailEdges(p *ifcPara, pos int, x *float64, widen func(*box, float64, float64)) {
	for _, c := range p.closeAt[pos] {
		if !c.last {
			continue
		}
		*x += c.pd[css.Right] + c.bd[css.Right]
		widen(c, *x, *x)
		*x += c.m[css.Right]
	}
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
		half := (v.lh - (it.ascent + it.descent)) / 2
		if it.text != nil || it.box.kind == kindInline {
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
	free := l.w - width - indent
	start, endOff := indent, 0.0
	if !ltr {
		start, endOff = free, indent
	}
	off := start
	align := st.TextAlign
	if align == css.AlignJustify && last {
		align = css.AlignStart
	}
	if free > 0 {
		switch align {
		case css.AlignStart:
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
		case css.AlignJustify:
			s.justify(l, free)
			off = indent
			if !ltr {
				off = 0
			}
		}
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

// settle moves the atomic inlines of l, placed at l.y, to their place.
func (s *inlineState) settle(l *lineBox) {
	for _, it := range l.items {
		if a := it.atomic; a != nil {
			a.shift(l.x+it.x-(a.x-a.m[css.Left]), l.y+it.baseline-it.ascent-(a.y-a.m[css.Top]))
		}
	}
}

// justify spreads free points over the word separators of l, shifting the
// glyphs after each one (CSS Text 3, 7.1): the spaces at the line end,
// which hang, take none.
func (s *inlineState) justify(l *lineBox, free float64) {
	if free <= eps {
		return
	}
	type sep struct {
		it *lineItem
		g  int
	}
	var seps []sep
	for _, it := range l.items {
		if it.text == nil {
			continue
		}
		for gi, g := range it.text.Glyphs {
			if g.Text == " " || g.Text == " " {
				seps = append(seps, sep{it, gi})
			}
		}
	}
	// Drop the trailing separators: they hang.
	for len(seps) > 0 {
		last := seps[len(seps)-1]
		if last.g != len(last.it.text.Glyphs)-1 || last.it != lastTextItem(l) {
			break
		}
		seps = seps[:len(seps)-1]
		if len(seps) > 0 && seps[len(seps)-1].it == last.it && seps[len(seps)-1].g == last.g-1 {
			continue
		}
	}
	if len(seps) == 0 {
		return
	}
	extra := free / float64(len(seps))
	acc := 0.0
	k := 0
	for _, it := range l.items {
		it.x += acc
		if it.text == nil {
			continue
		}
		local := 0.0
		for gi := range it.text.Glyphs {
			it.text.Glyphs[gi].X += local
			if k < len(seps) && seps[k].it == it && seps[k].g == gi {
				local += extra
				k++
			}
		}
		it.w += local
		acc += local
	}
	for _, f := range l.frags {
		f.x1 += acc
	}
}

// lastTextItem returns the last item of l that draws glyphs.
func lastTextItem(l *lineBox) *lineItem {
	for i := len(l.items) - 1; i >= 0; i-- {
		if l.items[i].text != nil {
			return l.items[i]
		}
	}
	return nil
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
		for _, f := range p.floats {
			fl, fh := e.contribution(f.box)
			lo, hi = max(lo, fl), max(hi, fh)
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
		hi = max(hi, x-trail)
		seg, trail := 0.0, 0.0
		anywhere := false
		for i := range n {
			if i > 0 && (p.allowed(i) || anywhere && p.p.GraphemeStart(i)) {
				lo = max(lo, seg-trail)
				seg, trail = 0, 0
			}
			st := p.styleAt(i)
			anywhere = st.OverflowWrap == css.Anywhere && wraps(st.WhiteSpace)
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
