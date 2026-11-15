package formula

import (
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// box is the margin box of an element laid out (MathML Core, 3.1.1): its
// metrics about its alphabetic baseline and what it holds, placed from
// its left baseline point. Ascents are positive above the baseline,
// descents below.
type box struct {
	// w is the inline size.
	w float64
	// asc and desc are the line-ascent and line-descent; inkAsc and
	// inkDesc the ink line-ascent and ink line-descent.
	asc, desc, inkAsc, inkDesc float64
	// italic is the italic correction, 0 when the box has none.
	italic float64
	// accent is the top accent attachment, hasAccent whether the box has
	// one.
	accent    float64
	hasAccent bool
	// op holds the properties of an embellished operator, nil for a box
	// that is not one (MathML Core, 3.2.4.1).
	op *operator
	// items are what the box holds, in paint order.
	items []item
}

// item is one thing a box holds: a child box, a glyph run or a filled
// rectangle, placed by its offset from the box's left baseline point, y
// downward.
type item struct {
	x, y  float64
	child *box
	run   *typeset.Text
	rect  *rect
}

// rect is a filled rectangle: a fraction bar, a radical's overbar, a
// background or a border side. x and y are its top-left corner. A ring,
// the border of one colour all round, is the rectangle less the one
// inset from it by the widths of its sides, indexed top, right, bottom,
// left.
type rect struct {
	w, h  float64
	color typeset.Color
	ring  *[4]float64
	// decor marks a background or a border, painted under every glyph
	// and bar of the formula.
	decor bool
}

// operator holds the properties of an embellished operator (MathML Core,
// 3.2.4.2), its spaces resolved in points against its core operator's
// font.
type operator struct {
	form                                        form
	stretchy, symmetric, largeop, movablelimits bool
	lspace, rspace                              float64
	// inline is the stretch axis: true for inline, false for block.
	inline bool
}

// height returns the box's block size.
func (b *box) height() float64 { return b.asc + b.desc }

// add places child at x on its parent's baseline moved down by y.
func (b *box) add(child *box, x, y float64) {
	b.items = append(b.items, item{x: x, y: y, child: child})
}

// fill adds a rectangle of colour c whose top-left corner is at x, y.
func (b *box) fill(x, y, w, h float64, c typeset.Color) {
	if w <= 0 || h <= 0 || c.A <= 0 {
		return
	}
	b.items = append(b.items, item{x: x, y: y, rect: &rect{w: w, h: h, color: c}})
}

// decorate adds a background or a border rectangle of colour c whose
// top-left corner is at x, y.
func (b *box) decorate(x, y, w, h float64, c typeset.Color) {
	if w <= 0 || h <= 0 || c.A <= 0 {
		return
	}
	b.items = append(b.items, item{x: x, y: y, rect: &rect{w: w, h: h, color: c, decor: true}})
}

// borders paints the borders of st on the border box of top-left
// corner x, y, w wide and h tall: one ring when every side that has a
// width has the same colour, as the HTML layout fills a border of one
// colour, else a rectangle a side.
func (b *box) borders(x, y, w, h float64, st *Style) {
	bd := st.Border
	if bd == [4]float64{} {
		return
	}
	var c typeset.Color
	ringed, seen := true, false
	for s, v := range bd {
		switch {
		case v <= 0:
		case !seen:
			c, seen = st.BorderColor[s], true
		case st.BorderColor[s] != c:
			ringed = false
		}
	}
	if ringed {
		if w > 0 && h > 0 && c.A > 0 {
			in := bd
			b.items = append(b.items, item{x: x, y: y, rect: &rect{w: w, h: h, color: c, ring: &in, decor: true}})
		}
		return
	}
	b.decorate(x, y, w, bd[top], st.BorderColor[top])
	b.decorate(x, float64(y+h)-bd[bottom], w, bd[bottom], st.BorderColor[bottom])
	b.decorate(x, y, bd[left], h, st.BorderColor[left])
	b.decorate(float64(x+w)-bd[right], y, bd[right], h, st.BorderColor[right])
}

// draw appends the fragments of b drawn with its left baseline point at
// x, y to out: the backgrounds and borders of every element first, in
// tree order, then the glyphs and bars, as CSS paints the backgrounds of
// block-level descendants under inline content (CSS 2.1, Appendix E.2,
// steps 4 and 7) and as Chromium paints the boxes of a formula.
func (b *box) draw(x, y float64, out *[]typeset.Fragment) {
	b.paint(x, y, true, out)
	b.paint(x, y, false, out)
}

// paint appends the fragments of b drawn with its left baseline point at
// x, y to out: its backgrounds and borders when decor is set, else the
// rest.
func (b *box) paint(x, y float64, decor bool, out *[]typeset.Fragment) {
	for _, it := range b.items {
		switch {
		case it.child != nil:
			it.child.paint(float64(x+it.x), float64(y+it.y), decor, out)
		case decor != (it.rect != nil && it.rect.decor):
		case it.run != nil:
			t := *it.run
			t.Origin = typeset.Point{X: float64(x + it.x), Y: float64(y + it.y)}
			*out = append(*out, &t)
		case it.rect != nil:
			r := it.rect
			x0, y0 := float64(x+it.x), float64(y+it.y)
			p := &typeset.Path{Ops: rectOps(x0, y0, r.w, r.h), Fill: &typeset.Fill{Color: r.color}}
			if in := r.ring; in != nil {
				if w, h := r.w-in[left]-in[right], r.h-in[top]-in[bottom]; w > 0 && h > 0 {
					p.Ops = append(p.Ops, rectOps(x0+in[left], y0+in[top], w, h)...)
				}
				p.Fill.Rule = typeset.EvenOdd
			}
			*out = append(*out, p)
		}
	}
}

// contentSize returns the content box of the math content box c of an
// element of style st: c itself, or a box of the width and height st
// gives, c's inline-start and block-start edges on the content box's,
// or, when centred, c's block-start edge on the content box's and their
// middles aligned (MathML Core, 3.1.2, 3.3.2.1 and 3.4.2.3). The
// inline-start edge of a right-to-left element is its right edge.
func contentSize(c *box, st *Style, centred bool) *box {
	if !st.HasWidth && !st.HasHeight {
		return c
	}
	b := &box{
		w: c.w, asc: c.asc, desc: c.desc, inkAsc: c.inkAsc, inkDesc: c.inkDesc, italic: c.italic,
		accent: c.accent, hasAccent: c.hasAccent, op: c.op,
	}
	x := 0.0
	if st.HasWidth {
		b.w = max(st.Width, 0)
		switch {
		case centred:
			x = float64((b.w - c.w) / 2)
		case st.RTL:
			x = b.w - c.w
		}
	}
	if st.HasHeight {
		b.desc = max(st.Height, 0) - c.asc
		b.inkAsc, b.inkDesc = b.asc, b.desc
	}
	b.add(c, x, 0)
	return b
}

// edges wraps the math content box c of an element of style st in its
// padding, border and margin, painting its background and borders
// (MathML Core, 3.1.2): the inline offsets grow by the inline-start
// sides, the ink baselines move to a border's outer edge when the border
// is wider than 0, and the italic correction and top accent attachment
// grow by the inline-end and inline-start sides. The math content box
// keeps its baseline.
func edges(c *box, st *Style, centred bool) *box {
	c = contentSize(c, st, centred)
	p, bd, m := st.Padding, st.Border, st.Margin
	if p == [4]float64{} && bd == [4]float64{} && m == [4]float64{} && st.Background.A == 0 {
		return c
	}
	startPB := float64(p[left] + bd[left])
	endPB := float64(p[right] + bd[right])
	b := &box{
		w:      float64(m[left]+startPB) + c.w + float64(endPB+m[right]),
		asc:    float64(m[top]+bd[top]) + float64(p[top]+c.asc),
		desc:   float64(m[bottom]+bd[bottom]) + float64(p[bottom]+c.desc),
		inkAsc: c.inkAsc, inkDesc: c.inkDesc, hasAccent: c.hasAccent, op: c.op,
	}
	// A box without an italic correction or a top accent attachment
	// keeps none: only one it has grows by the edges.
	if c.italic != 0 {
		b.italic = float64(c.italic+endPB) + m[right]
	}
	if c.hasAccent {
		b.accent = float64(c.accent+startPB) + m[left]
	}
	if bd[top] > 0 {
		b.inkAsc = float64(c.asc+p[top]) + bd[top]
	}
	if bd[bottom] > 0 {
		b.inkDesc = float64(c.desc+p[bottom]) + bd[bottom]
	}
	// The border box, from the margin box's left baseline point.
	bx, by := m[left], -float64(b.asc-m[top])
	bw := float64(b.w-m[left]) - m[right]
	bh := float64(b.asc+b.desc) - float64(m[top]+m[bottom])
	if !st.Hidden {
		b.decorate(bx, by, bw, bh, st.Background)
		b.borders(bx, by, bw, bh, st)
	}
	b.add(c, float64(m[left]+startPB), 0)
	return b
}

// rectOps returns the closed path of the rectangle of top-left corner x,
// y, w wide and h tall.
func rectOps(x, y, w, h float64) []typeset.PathOp {
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x, Y: y}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x + w, Y: y}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x + w, Y: y + h}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x, Y: y + h}}},
		{Kind: typeset.ClosePath},
	}
}
