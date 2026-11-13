package layout

import (
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// maxLeaderCopies bounds the copies of its string one leader draws on a
// line: 4096. Anti-DoS guard, not spec-mandated: a line is at most a page
// wide, 14,400 points, and a leader string narrower than 3.5 points on
// such a line is a pattern no reader tells apart from a shorter one; the
// copies past the cap are left out.
const maxLeaderCopies = 4096

// leaderInfo is the string of a leader and the width of one copy of it.
type leaderInfo struct {
	pattern string
	w       float64
	// space is a space in the leader's style, shaped once, when spaced
	// is set: the word break the leader stands for in the text
	// ([painter.leaderBreak]); nil when the style draws no space.
	space  *typeset.Text
	spaced bool
}

// isLeader reports a leader box: the inline block the converter makes of
// leader() (CSS Generated Content 3).
func isLeader(b *box) bool {
	return b != nil && b.node != nil && b.node.Leader && b.kind == kindBlock && b.inline
}

// inLeader reports a box of a leader, or one inside it. A leader's copies
// are a layout artifact (ISO 32000-1, 14.8.2.2.2: purely cosmetic
// typographical elements; ISO 14289-1, 7.1): a reader hears the text on
// either side of it, a title and its page number, not a run of dots.
func inLeader(b *box) bool {
	return leaderAncestor(b) != nil
}

// leaderAncestor returns the leader b is or sits in, nil for none.
func leaderAncestor(b *box) *box {
	for depth := 0; b != nil && depth < 3; depth++ {
		if isLeader(b) {
			return b
		}
		b = b.parent
	}
	return nil
}

// leaderText returns the text box of the leader b, nil when it has none.
func leaderText(b *box) *box {
	stack := []*box{b}
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if c.kind == kindText {
			return c
		}
		for _, v := range slices.Backward(c.kids) {
			stack = append(stack, v)
		}
	}
	return nil
}

// leaderOf returns the string and copy width of the leader b, recording
// its string the first time.
func (e *engine) leaderOf(b *box) *leaderInfo {
	if li, ok := e.leaders[b]; ok {
		return li
	}
	li := &leaderInfo{}
	if tb := leaderText(b); tb != nil {
		li.pattern = tb.text
	}
	if e.leaders == nil {
		e.leaders = map[*box]*leaderInfo{}
	}
	e.leaders[b] = li
	return li
}

// layoutLeader lays the leader b out with k copies of its string in a
// containing block of width cbw.
func (e *engine) layoutLeader(b *box, k int, cbw float64) {
	li := e.leaderOf(b)
	if tb := leaderText(b); tb != nil {
		tb.text = strings.Repeat(li.pattern, min(max(k, 0), maxLeaderCopies))
	}
	b.ifc, b.measured = nil, false
	e.layoutAtomic(b, cblock{w: cbw})
}

// prepareLeader lays the leader b out with one copy of its string, which
// gives the line its height, and records the copy's width. The leader
// itself takes no width on the line until the line is aligned.
func (e *engine) prepareLeader(b *box, cbw float64) {
	e.layoutLeader(b, 1, cbw)
	li := e.leaderOf(b)
	li.w = b.w
	if tb := leaderText(b); tb != nil && !li.spaced {
		li.spaced = true
		li.space = e.shapedSpace(tb)
	}
}

// shapedSpace returns one space shaped in the style of the text box tb, nil
// when shaping fails or gives no single glyph.
func (e *engine) shapedSpace(tb *box) *typeset.Text {
	para, _, err := e.fonts.Shape(e.ctx, []text.ParagraphRun{{Text: " ", Style: e.textStyle(tb.st, tb.lang)}}, text.ParagraphOptions{})
	if err != nil {
		e.shapeFailed(err, "leader", -1)
		return nil
	}
	if para.Len() != 1 {
		return nil
	}
	pl := para.Line(0, 1, false)
	if len(pl.Spans) != 1 || pl.Spans[0].Text == nil || len(pl.Spans[0].Text.Glyphs) != 1 {
		return nil
	}
	return pl.Spans[0].Text
}

// leaderBreak adds, before the first fragment t of the leader l's copies,
// a space that draws nothing in the structure element t would hang from:
// the copies are an artifact, so without it the text on either side of
// them, a title and its page number, reads as one word. ISO 32000-1,
// 14.8.2.5 asks a Tagged PDF to state word breaks by space characters
// rather than by the room between glyphs.
//
// # Corpus evidence
//
//   - probe, html: a link "Chapter one" whose ::after holds leader('.')
//     and target-counter(attr(href), page) read "Chapter one" then "2"
//     under its Link element (pdfinfo -struct-text); Chromium of
//     gotenberg/gotenberg:9.4.1, which draws no leader(), tags the dots of
//     its twin, typed as text, as content of the Link, read between the
//     two.
func (p *painter) leaderBreak(l *box, t *typeset.Text, c *ditem, y0, y1 float64, page int) {
	li := p.e.leaders[l]
	if li == nil || li.space == nil || t.Parent == nil || p.broken[l] {
		return
	}
	if p.broken == nil {
		p.broken = map[*box]bool{}
	}
	p.broken[l] = true
	sp := *li.space
	sp.Glyphs = []typeset.Glyph{{GID: li.space.Glyphs[0].GID, Text: " "}}
	sp.Origin, sp.Color, sp.Parent, sp.Order = t.Origin, t.Color, t.Parent, t.Order
	c.kids = append(c.kids, &ditem{frag: &sp, y0: y0, y1: y1, page: page})
}

// fillLeaders gives the leaders of the line l the room its content leaves,
// shared equally, before the line is aligned, indent being the line's
// text-indent (CSS Generated Content 3, leader()): each leader draws the
// copies of its string that fit whole in its room on a grid that starts
// at the line's end edge, so that the leaders of a block line up; the
// content after a leader moves to the end of its room. A line with no
// room left draws no copy.
func (s *inlineState) fillLeaders(l *lineBox, indent float64) {
	var leaders []*lineItem
	for _, it := range l.items {
		if isLeader(it.atomic) {
			leaders = append(leaders, it)
		}
	}
	if len(leaders) == 0 {
		return
	}
	width := 0.0
	for _, it := range l.items {
		width = max(width, it.x+it.w)
	}
	for _, f := range l.frags {
		width = max(width, f.x1)
	}
	free := l.w - (width - l.hangW) - indent
	share := max(free, 0) / float64(len(leaders))
	ltr := s.root.st.Direction != css.RTL
	end := l.w - indent
	origin := make([]float64, len(leaders))
	for i, it := range leaders {
		origin[i] = it.x
	}
	acc := 0.0
	for _, it := range l.items {
		it.x += acc
		if !isLeader(it.atomic) {
			continue
		}
		room0 := it.x
		acc += share
		s.drawLeader(it, room0, share, end, ltr)
	}
	for _, f := range l.frags {
		d0, d1 := 0.0, 0.0
		for _, x := range origin {
			if x < f.x0-eps {
				d0 += share
			}
			if x < f.x1+eps {
				d1 += share
			}
		}
		f.x0 += d0
		f.x1 += d1
	}
}

// drawLeader lays the leader item it out in its room, from x0 and avail
// wide, with the copies of its string that fit whole on the grid anchored
// at the line's end edge: end in a left-to-right line, 0 in a
// right-to-left one.
func (s *inlineState) drawLeader(it *lineItem, x0, avail, end float64, ltr bool) {
	e := s.e
	a := it.atomic
	pw := e.leaderOf(a).w
	k, start := 0, x0
	if pw > eps && avail >= pw-eps {
		var lo, hi float64
		if ltr {
			lo = math.Ceil((end-x0-avail)/pw - 1e-6)
			hi = math.Floor((end-x0)/pw+1e-6) - 1
			start = end - float64((hi+1)*pw)
		} else {
			lo = math.Ceil(x0/pw - 1e-6)
			hi = math.Floor((x0+avail)/pw+1e-6) - 1
			start = lo * pw
		}
		if n := hi - lo + 1; n >= 1 {
			k = int(min(n, maxLeaderCopies)) // bounded by maxLeaderCopies
		}
	}
	if k == 0 {
		start = x0
	}
	e.layoutLeader(a, k, s.cb.w)
	it.x, it.w = start, float64(k)*pw
}

// leaderAt reports a leader at character i of p. A soft wrap opportunity
// comes before every leader, whatever precedes it, a no-break space
// included, as WeasyPrint breaks leaders: the leader and what follows it
// move to the next line together.
func (p *ifcPara) leaderAt(i int) bool {
	for _, lp := range p.leaders {
		if lp.pos == i {
			return true
		}
	}
	return false
}

// leaderGlue reports whether the content after a leader of p reaches
// character i: no soft wrap opportunity falls between a leader and the
// end of the inline box it sits in, so that what follows the leader, a
// page number, stays on its line.
func (p *ifcPara) leaderGlue(i int) bool {
	for _, lp := range p.leaders {
		if i <= lp.pos {
			continue
		}
		end := p.n
		if pos, ok := p.closePos[lp.box.parent]; ok && pos > lp.pos {
			end = pos
		}
		if i < end {
			return true
		}
	}
	return false
}
