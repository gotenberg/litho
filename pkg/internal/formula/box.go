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
// background or a border side. x and y are its top-left corner.
type rect struct {
	w, h  float64
	color typeset.Color
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

// draw appends the fragments of b drawn with its left baseline point at
// x, y to out.
func (b *box) draw(x, y float64, out *[]typeset.Fragment) {
	for _, it := range b.items {
		switch {
		case it.child != nil:
			it.child.draw(float64(x+it.x), float64(y+it.y), out)
		case it.run != nil:
			t := *it.run
			t.Origin = typeset.Point{X: float64(x + it.x), Y: float64(y + it.y)}
			*out = append(*out, &t)
		case it.rect != nil:
			r := it.rect
			x0, y0 := float64(x+it.x), float64(y+it.y)
			*out = append(*out, &typeset.Path{
				Ops: []typeset.PathOp{
					{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x0, Y: y0}}},
					{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x0 + r.w, Y: y0}}},
					{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x0 + r.w, Y: y0 + r.h}}},
					{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x0, Y: y0 + r.h}}},
					{Kind: typeset.ClosePath},
				},
				Fill: &typeset.Fill{Color: r.color},
			})
		}
	}
}

// edges wraps the math content box c of an element of style st in its
// padding, border and margin, painting its background and borders
// (MathML Core, 3.1.2): the inline offsets grow by the inline-start
// sides, the ink baselines move to a border's outer edge when the border
// is wider than 0, and the italic correction and top accent attachment
// grow by the inline-end and inline-start sides. The math content box
// keeps its baseline.
func edges(c *box, st *Style) *box {
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
		inkAsc: c.inkAsc, inkDesc: c.inkDesc,
		italic: float64(c.italic+endPB) + m[right],
		accent: float64(c.accent+startPB) + m[left], hasAccent: c.hasAccent,
		op: c.op,
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
		b.fill(bx, by, bw, bh, st.Background)
		b.fill(bx, by, bw, bd[top], st.BorderColor[top])
		b.fill(bx, float64(by+bh)-bd[bottom], bw, bd[bottom], st.BorderColor[bottom])
		b.fill(bx, by, bd[left], bh, st.BorderColor[left])
		b.fill(float64(bx+bw)-bd[right], by, bd[right], bh, st.BorderColor[right])
	}
	b.add(c, float64(m[left]+startPB), 0)
	return b
}
