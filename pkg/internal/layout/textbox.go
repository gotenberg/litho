package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// trimTextBox applies text-box-trim to the block container b whose
// content ends at end, and returns where it ends then (CSS Inline 3,
// 5.1): trim-start moves the content up by the space over its first
// line's text-box-edge over metric, trim-end ends it at its last line's
// under metric. A first or last line behind a border or a padding of a
// descendant, or a block that lays out a flex, grid or table formatting
// context, is not trimmed; margins do not stop the trim. A line edge a
// descendant already trimmed is left as it trimmed it: the innermost
// block container that asks decides (CSS Inline 3, 6.2). own is the formatting context b establishes,
// nil when b lays its content out in its parent's: its floats move with
// the content.
func (e *engine) trimTextBox(b *box, end float64, own *bfc) float64 {
	if b.st.TextBoxTrim == css.TrimNone || b.flex != nil || b.grid != nil || b.tdata != nil || b.mc != nil {
		// A multi-column container trims the lines of each column as they
		// are placed ([inlineState.columnTrim]).
		return end
	}
	trim := b.st.TextBoxTrim
	if trim == css.TrimStart || trim == css.TrimBoth {
		if l, owner := edgeLine(b, false); l != nil && !l.trimmedOver {
			l.trimmedOver = true
			st := owner.st
			if fl := e.firstLineStyle(owner); fl != nil {
				// The first line's root inline box takes the ::first-line
				// font (CSS Pseudo 4, 2.1.2).
				st = fl
			}
			over, _ := e.trimEdges(st, owner.lang)
			if d := l.baseline - over; d > 0 {
				if cut, ok := e.pushedAt(b, l.y); ok {
					// Under box-decoration-break: slice only the start of
					// the box's first fragment is trimmed (CSS Inline 3,
					// 6.2): the content flowing on from it moves up, the
					// content a break moved to the top of a later page or
					// column keeps its place, and all after it.
					shiftBefore(b, cut, -d, 0)
					own.shiftFloatsBefore(cut, -d)
				} else {
					b.shift(0, -d)
					b.y, b.sy, b.anchorY = b.y+d, b.sy+d, b.anchorY+d
					own.shiftFloats(math.Inf(-1), -d)
					end -= d
				}
			}
		}
	}
	if trim == css.TrimEnd || trim == css.TrimBoth {
		if l, owner := edgeLine(b, true); l != nil && !l.trimmedUnder {
			l.trimmedUnder = true
			_, under := e.trimEdges(owner.st, owner.lang)
			// The block's growth for the annotations under the line is
			// trimmed with it.
			if d := l.h + l.annEnd - l.baseline - under; d > 0 {
				// A float after the last line is placed at the line's
				// end, which the trim moves up, as Chromium places it.
				y := l.y + l.h
				shiftFloatsAfter(owner, y, -d)
				own.shiftFloats(y, -d)
				end -= d
			}
		}
	}
	return end
}

// pushedAt returns the first flow position below the line top y at which
// a line or a box inside b starts at the top of a page or column: where a
// break moved content past a cut, false when none does or when the
// engine does not paginate.
func (e *engine) pushedAt(b *box, y float64) (float64, bool) {
	if !e.paginating() {
		return 0, false
	}
	at := math.Inf(1)
	var walk func(c *box, depth int)
	walk = func(c *box, depth int) {
		for _, l := range c.lines {
			if l.y > y+eps && l.y < at && e.atPageTop(l.y) {
				at = l.y
			}
		}
		if depth >= maxEdgeLineDepth {
			return
		}
		for _, k := range c.kids {
			if k.kind != kindBlock && k.kind != kindReplaced || k.inline || k.abs || !k.laidOut || k.y >= at {
				continue
			}
			if k.y > y+eps && e.atPageTop(k.y) {
				at = k.y
				continue
			}
			walk(k, depth+1)
		}
	}
	walk(b, 0)
	return at, !math.IsInf(at, 1)
}

// shiftBefore moves by dy the content of b that starts above the flow
// position cut: its lines, the blocks above the cut whole, and the part
// above it of a block the cut crosses, whose top moves while its end
// stays. b itself keeps its place.
func shiftBefore(b *box, cut, dy float64, depth int) {
	for _, l := range b.lines {
		if l.y >= cut-eps {
			continue
		}
		l.y += dy
		for _, it := range l.items {
			if it.atomic != nil {
				it.atomic.shift(0, dy)
			}
		}
	}
	if b.outside != nil && b.outside.baseline < cut {
		b.outside.baseline += dy
		b.outside.sym.Y += dy
	}
	for _, k := range b.kids {
		switch k.kind {
		case kindBlock, kindReplaced:
			switch {
			case k.inline:
			case k.y+k.borderH() <= cut+eps:
				k.shift(0, dy)
			case k.y < cut && depth < maxEdgeLineDepth:
				k.y, k.sy, k.anchorY = k.y+dy, k.sy+dy, k.anchorY+dy
				k.h -= dy
				shiftBefore(k, cut, dy, depth+1)
			}
		case kindInline:
			if k.anchorY < cut {
				k.shiftInline(0, dy)
			}
		case kindText, kindBreak:
			if k.anchorY < cut {
				k.anchorY += dy
			}
		}
	}
}

// shiftFloatsAfter moves by dy the floats of the inline content of b
// whose margin box starts at y or below.
func shiftFloatsAfter(b *box, y, dy float64) {
	for _, k := range b.kids {
		switch {
		case k.float != css.FloatNone:
			if k.y-k.m[css.Top] >= y-eps {
				k.shift(0, dy)
			}
		case k.kind == kindInline:
			shiftFloatsAfter(k, y, dy)
		}
	}
}

// shiftFloats moves by dy the float rectangles of c whose top is at y or
// below; a nil c holds none.
func (c *bfc) shiftFloats(y, dy float64) {
	if c == nil {
		return
	}
	for i := range c.floats {
		if f := &c.floats[i]; !f.initial && f.y0 >= y-eps {
			f.y0 += dy
			f.y1 += dy
		}
	}
}

// shiftFloatsBefore moves by dy the float rectangles of c whose top is
// above y; a nil c holds none.
func (c *bfc) shiftFloatsBefore(y, dy float64) {
	if c == nil {
		return
	}
	for i := range c.floats {
		if f := &c.floats[i]; !f.initial && f.y0 < y-eps {
			f.y0 += dy
			f.y1 += dy
		}
	}
}

// edgeLine returns the first line box in flow inside b, or the last when
// last, and the block container that holds it; nil when a border, a
// padding, a box that is not a block container or a block without a
// line comes first. The empty pieces of an inline box split around a
// block are passed over.
func edgeLine(b *box, last bool) (*lineBox, *box) {
	return edgeLineAt(b, last, 0)
}

// edgeLineAt is [edgeLine] depth blocks down.
func edgeLineAt(b *box, last bool, depth int) (*lineBox, *box) {
	if len(b.lines) > 0 {
		if last {
			return b.lines[len(b.lines)-1], b
		}
		return b.lines[0], b
	}
	if depth >= maxEdgeLineDepth {
		return nil, nil
	}
	side := css.Top
	if last {
		side = css.Bottom
	}
	for i := range b.kids {
		k := b.kids[i]
		if last {
			k = b.kids[len(b.kids)-1-i]
		}
		if k.inline || k.float != css.FloatNone || k.abs || k.kind != kindBlock || !k.laidOut {
			continue
		}
		if k.flex != nil || k.grid != nil || k.tdata != nil || k.bd[side] != 0 || k.pd[side] != 0 {
			return nil, nil
		}
		if l, owner := edgeLineAt(k, last, depth+1); l != nil {
			return l, owner
		}
		if !k.anon || k.borderH() > eps || !splitsInline(b) && !holdsNoInline(k) {
			// A block without a line ends the search: b has no first
			// or last formatted line (CSS Pseudo 4, 2.1.1).
			return nil, nil
		}
	}
	return nil, nil
}

// splitsInline reports whether b holds a block that an inline box was
// split around (CSS 2.1, 9.2.1.1): the empty anonymous blocks of the
// inline's pieces beside it hold no line of their own, as Chromium lays
// the block out inside the inline's line flow.
func splitsInline(b *box) bool {
	for _, k := range b.kids {
		if k.host != nil {
			return true
		}
	}
	return false
}

// holdsNoInline reports whether the anonymous block k holds nothing but
// text and out-of-flow boxes: laid out with no line, its text is white
// space that collapses away, which generates no box (CSS 2.1, 9.2.2.1;
// CSS Text 3, 4.1.1), so k is no block-level child to look into.
func holdsNoInline(k *box) bool {
	for _, c := range k.kids {
		if c.kind != kindText && !c.abs {
			return false
		}
	}
	return true
}

// maxEdgeLineDepth caps the nested blocks edgeLine descends. Anti-DoS
// guard, not spec-mandated: the box tree's depth is capped higher; a
// first line some hundred blocks down is left untrimmed.
const maxEdgeLineDepth = 256

// trimEdges returns the distances from the baseline up to the over metric
// and down to the under metric of text-box-edge for the first available
// font of st (CSS Inline 3, 5.2): text the font's ascent and descent, cap
// the cap height, ex the x-height, alphabetic the baseline, ideographic
// the em box's edges.
func (e *engine) trimEdges(st *css.Style, lang string) (float64, float64) {
	m := e.faceMetrics(st, lang)
	var over, under float64
	switch st.TextBoxEdge.Over {
	case css.EdgeText:
		over = m.Ascent
	case css.EdgeCap:
		over = e.capHeight(st, lang)
	case css.EdgeEx:
		over = m.XHeight
	case css.EdgeIdeographic:
		over = m.EmAscent
	case css.EdgeAlphabetic:
	}
	switch st.TextBoxEdge.Under {
	case css.EdgeText:
		under = m.Descent
	case css.EdgeIdeographic:
		under = m.EmDescent
	case css.EdgeAlphabetic, css.EdgeCap, css.EdgeEx:
	}
	return over, under
}
