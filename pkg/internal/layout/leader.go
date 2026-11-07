package layout

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
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
}

// isLeader reports a leader box: the inline block the converter makes of
// leader() (CSS Generated Content 3).
func isLeader(b *box) bool {
	return b != nil && b.node != nil && b.node.Leader && b.kind == kindBlock && b.inline
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
		for i := len(c.kids) - 1; i >= 0; i-- {
			stack = append(stack, c.kids[i])
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
	e.leaderOf(b).w = b.w
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
			start = end - (hi+1)*pw
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
