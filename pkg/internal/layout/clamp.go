package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// clampState counts the lines of a line clamp container (CSS Overflow 4,
// 6): the box whose style clamps, the lines of its block formatting
// context, in it and in the block boxes it holds that establish no
// formatting context of their own, counting toward max-lines or ending
// at its maximum height.
type clampState struct {
	root *box
	// max is the lines kept, 0 for no count; limit the flow position the
	// lines kept end at, +Inf for none.
	max   int
	limit float64
	// sized reports a limit set by the box's height or maximum height,
	// which may leave no line at all; the end of a page keeps one.
	sized bool
	mark  css.OverflowMark
	// count is the lines kept so far; done reports the content after the
	// last one discarded, marked whether that line now ends with the mark.
	count  int
	done   bool
	marked bool
	// prev is the last line kept, to end it with the mark once content
	// turns out to follow it.
	prev *clampLine
}

// clampLine is a line a clamp container kept, with what rebuilds it.
type clampLine struct {
	s          *inlineState
	p          *ifcPara
	l          *lineBox
	start, end int
	indent     float64
}

// enterClamp starts the line count of b, which establishes a block
// formatting context whose content starts at y, and returns the count it
// suspends: a line clamp container counts its own lines, any other one
// hides its lines from the container around it (CSS Overflow 4, 6.1).
func (e *engine) enterClamp(b *box, cb cblock, y float64) *clampState {
	saved := e.clamp
	e.clamp = nil
	if !b.st.Clamps() {
		return saved
	}
	c := &clampState{root: b, max: b.st.Lines(), limit: math.Inf(1), mark: b.st.ClampMark()}
	if b.st.LineClamp.Auto || b.st.LineClamp.Continue == css.ContinueDiscard {
		// The lines end at the box's height, else its maximum height
		// (CSS Overflow 4, 6.1, auto), and, for continue: discard, at the
		// end of the box's first page (6.3).
		if h, ok := specHeight(b, b.st.Height, cb); ok {
			c.limit, c.sized = y+h, true
		} else if h, ok := specHeight(b, b.st.MaxHeight, cb); ok {
			c.limit, c.sized = y+h, true
		}
		if b.st.LineClamp.Continue == css.ContinueDiscard && e.paginating() {
			p := e.pageOf(y + eps)
			c.limit = min(c.limit, e.pageTop(p)+e.areaH(p))
		}
	}
	e.clamp = c
	return saved
}

// leaveClamp ends the count enterClamp started, restoring the one it
// suspended.
func (e *engine) leaveClamp(saved *clampState) {
	e.clamp = saved
}

// clampSnapshot returns a copy of the line count in progress, for a
// layout pass that may be undone, nil for none.
func (e *engine) clampSnapshot() *clampState {
	if e.clamp == nil {
		return nil
	}
	c := *e.clamp
	return &c
}

// clampRestore puts back a count [engine.clampSnapshot] took.
func (e *engine) clampRestore(c *clampState) {
	if c != nil && e.clamp != nil && e.clamp.root == c.root {
		*e.clamp = *c
	}
}

// clampDiscards reports whether the content the flow reaches now is
// discarded, after the last line of a clamp container: the last line kept
// then ends with the container's mark (CSS Overflow 4, 6.4).
func (e *engine) clampDiscards() bool {
	c := e.clamp
	if c == nil || !c.done {
		return false
	}
	c.finish()
	return true
}

// finish ends the last line kept with the mark, once.
func (c *clampState) finish() {
	if c.marked || c.prev == nil {
		return
	}
	c.marked = true
	pl := c.prev
	if c.mark.Kind == css.MarkNone {
		return
	}
	pl.s.remark(pl, c.mark.String())
}

// clampOverflows reports whether the line l of height h at top ends past
// the clamp container's limit after a line was kept: it and what follows
// are discarded.
func (e *engine) clampOverflows(top, h float64) bool {
	c := e.clamp
	if c == nil || c.count == 0 && !c.sized || top+h <= c.limit+eps {
		return false
	}
	c.done = true
	c.finish()
	return true
}

// clampKeep records the line l, kept, and reports whether it is the last
// one the container keeps.
func (e *engine) clampKeep(cl *clampLine) bool {
	c := e.clamp
	if c == nil {
		return false
	}
	c.count++
	c.prev = cl
	if c.max > 0 && c.count >= c.max {
		c.done = true
		return true
	}
	return false
}

// remark rebuilds the line cl kept so that it ends with mark: the block
// overflow ellipsis of a clamp container, drawn after the line's content,
// whose end is cut at a grapheme boundary when both do not fit (CSS
// Overflow 4, 6.4).
func (s *inlineState) remark(cl *clampLine, mark string) {
	l := cl.l
	nl := s.marked(cl.p, cl.start, cl.end, l.x, l.w, cl.indent, mark, true)
	if nl == nil {
		return
	}
	s.align(nl, cl.indent, true)
	// The line keeps its place and height; its content keeps its
	// baseline.
	d := l.baseline - nl.baseline
	for _, it := range nl.items {
		it.baseline += d
		if it.text != nil {
			it.text.Origin.Y += d
		}
	}
	for _, f := range nl.frags {
		f.top += d
		f.bottom += d
	}
	l.items, l.frags, l.hangW = nl.items, nl.frags, nl.hangW
	s.settle(l)
}

// overflowMark ends the line l of the characters [start, end) of p, which
// overflows the block's end edge, with the block's text-overflow mark
// when the block clips its overflow (CSS Overflow 4, 5.1): the content is
// cut at the last grapheme boundary that leaves room for the mark.
func (s *inlineState) overflowMark(p *ifcPara, l *lineBox, start, end int, indent float64) *lineBox {
	st := s.root.st
	if st.Overflow == css.OverflowVisible || st.TextOverflow.Kind == css.MarkNone {
		return l
	}
	if contentWidth(l)-l.hangW <= l.w-indent+eps {
		return l
	}
	if nl := s.marked(p, start, end, l.x, l.w, indent, st.TextOverflow.String(), false); nl != nil {
		return nl
	}
	return l
}

// marked builds the line of the characters [start, end) of p in the band
// at x of width w, cut to leave room for mark after it, and the mark: nil
// when the root's font draws no mark. always keeps the whole content when
// it fits with the mark; else the line is cut as text-overflow cuts it.
func (s *inlineState) marked(p *ifcPara, start, end int, x, w, indent float64, mark string, always bool) *lineBox {
	e := s.e
	if mark == "" || e.fontSize(s.root.st) <= 0 {
		return nil
	}
	rtl := p.p.Direction() == text.RightToLeft
	dir := text.LeftToRight
	if rtl {
		dir = text.RightToLeft
	}
	mp, _, err := e.fonts.Shape(e.ctx, []text.ParagraphRun{{Text: mark, Style: e.textStyle(s.root.st, s.root.lang)}}, text.ParagraphOptions{Direction: dir})
	if err != nil || mp.Len() == 0 {
		return nil
	}
	ml := mp.Line(0, mp.Len(), false)
	mw := ml.Width
	avail := w - indent - mw
	widths := p.paraWidths(nil)
	// The content ends before the spaces that hang at its end.
	stop := end
	for stop > start && p.hangs(stop-1) {
		stop--
	}
	sum := func(k int) float64 {
		t := 0.0
		for i := start; i < k; i++ {
			t += widths[i]
		}
		return t
	}
	cut := stop
	if !always || sum(stop) > avail+eps {
		cut = start
		for k := stop; k > start; k-- {
			if (k == stop || p.p.GraphemeStart(k)) && sum(k) <= avail+eps {
				cut = k
				break
			}
		}
	}
	s.keep = true
	nl := s.buildLine(p, start, cut, x, x+w)
	s.keep = false
	nl.x, nl.w = x, w
	cw := contentWidth(nl)
	at := cw
	if rtl {
		for _, it := range nl.items {
			it.x += mw
		}
		for _, f := range nl.frags {
			f.x0 += mw
			f.x1 += mw
		}
		at = 0
	}
	for _, sp := range ml.Spans {
		if sp.Text == nil {
			continue
		}
		t := *sp.Text
		nl.items = append(nl.items, &lineItem{
			box: s.root, text: &t, x: at + sp.X, w: sp.Width,
			baseline: nl.baseline, ascent: sp.Ascent, descent: sp.Descent, gap: sp.LineGap,
			ulPos: sp.UnderlinePosition, ulThick: sp.UnderlineThickness, stPos: sp.StrikeoutPosition, stThick: sp.StrikeoutThickness,
			start: cut, end: cut, rtl: sp.RTL,
		})
	}
	return nl
}

// discard ends the inline formatting context at y, after the last line a
// line clamp container keeps: the absolutely positioned boxes of the
// paragraphs from p on still take their static position there, as
// Chromium lays them out, and the lines placed are the box's.
func (s *inlineState) discard(d *ifcData, p *ifcPara, y float64) float64 {
	from := false
	for _, q := range d.paras {
		from = from || q == p
		if !from {
			continue
		}
		for _, a := range q.abs {
			if !s.placed[a.box] {
				s.placed[a.box] = true
				staticBlock(a.box, s.cb, y)
			}
		}
	}
	s.root.lines = s.lines
	return y
}

// clampBlock reports whether the box k, laid out in a line clamp
// container's flow, establishes a formatting context that ends past the
// container's limit: no clamp point lies inside it, so it is discarded
// with what follows, the last line before it ending with the mark (CSS
// Overflow 4, 6.1).
func (e *engine) clampBlock(k *box) bool {
	c := e.clamp
	if c == nil || c.done || k.abs || !k.establishesBFC() || math.IsInf(c.limit, 1) || c.count == 0 && !c.sized {
		return false
	}
	return k.y+k.borderH() > c.limit+eps
}

// clampCut discards the box k after [engine.clampBlock] reported it.
func (e *engine) clampCut(k *box) {
	k.laidOut = false
	if c := e.clamp; c != nil {
		c.done = true
		c.finish()
	}
}
