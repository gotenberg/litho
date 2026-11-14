package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// fragRect is one fragment of an inline box on a line, in flow
// coordinates with the relative offsets in force.
type fragRect struct {
	r    typeset.Rect
	frag *inlineFrag
}

// effectHost returns the innermost inline element of a group effect that
// the block-level box k was split out of (CSS 2.1, 9.2.1.1), nil for
// none: k paints with that element.
func effectHost(k *box) *box {
	for a := k.host; a != nil && a.kind == kindInline; a = a.parent {
		if effectInline(a) {
			return a
		}
	}
	return nil
}

// effectOwner returns the innermost inline element of a group effect that
// k sits in, k included when it is an inline box, up to its block
// container, nil for none. parent gives a box's parent, which
// ::first-line changes on the first line.
func effectOwner(k *box, parent func(*box) *box) *box {
	if k != nil && k.kind != kindInline {
		k = parent(k)
	}
	for a := k; a != nil && a.kind == kindInline; a = parent(a) {
		if effectInline(a) {
			return a
		}
	}
	return nil
}

// parentBox returns k's parent.
func parentBox(k *box) *box { return k.parent }

// lead returns the inline box that paints the element of k, an inline box
// of a group effect: the first of its pieces met in tree order.
func (p *painter) lead(k *box) *box {
	if l, ok := p.leads[k.node]; ok {
		return l
	}
	if p.leads == nil {
		p.leads = map[*Node]*box{}
	}
	p.leads[k.node] = k
	return k
}

// paints reports whether the line painter paints content whose inline
// element of a group effect is owner: the content of no such element
// outside [painter.inlineUnit], that of its element inside it.
func (p *painter) paints(owner *box) bool {
	if p.only == nil {
		return owner == nil
	}
	return owner != nil && owner.node == p.only.node
}

// inlineScope returns the block container whose lines and kids hold every
// piece of the inline element of b and every block split out of it: its
// block container, or the parent of that container when it is the
// anonymous block that wraps the pieces before a split (CSS 2.1, 9.2.1.1).
func inlineScope(b *box) *box {
	c := b.parent
	for c != nil && c.kind == kindInline {
		c = c.parent
	}
	if c != nil && c.anon && c.parent != nil {
		return c.parent
	}
	return c
}

// lineHolders returns the block containers within scope whose lines may
// hold the pieces of an inline element: scope itself and its anonymous
// block children, in tree order.
func lineHolders(scope *box) []*box {
	out := []*box{scope}
	for _, k := range scope.kids {
		if k.anon && k.kind == kindBlock && !k.inline && k.float == css.FloatNone && !k.abs {
			out = append(out, k)
		}
	}
	return out
}

// inlineFrags returns the fragments of the pieces of the inline element
// b on the lines of its block containers, in the order the lines run.
func (p *painter) inlineFrags(b *box) []fragRect {
	scope := inlineScope(b)
	if scope == nil {
		return nil
	}
	var out []fragRect
	for _, c := range lineHolders(scope) {
		for _, l := range c.lines {
			for _, f := range l.frags {
				if f.box.node != b.node || f.box.kind != kindInline {
					continue
				}
				ox, oy := inlineOffset(f.box)
				r := typeset.Rect{X: l.x + f.x0 + p.dx + ox, Y: l.y + f.top + p.dy + oy, Width: f.x1 - f.x0, Height: f.bottom - f.top}
				out = append(out, fragRect{r: r, frag: f})
			}
		}
	}
	return out
}

// fragEdges returns the borders and paddings the fragment f of an inline
// box draws: none on a side where the box breaks (CSS Fragmentation 3,
// 5.4).
func fragEdges(f *inlineFrag) (bd, pd [4]float64) {
	bd, pd = f.box.bd, f.box.pd
	if !f.left {
		bd[css.Left], pd[css.Left] = 0, 0
	}
	if !f.right {
		bd[css.Right], pd[css.Right] = 0, 0
	}
	return bd, pd
}

// inlineReference returns the reference box g of the inline element b in
// flow coordinates and the radii of its corners: that of its first
// fragment, false for an element on no line.
//
// CSS Masking 1, 5.1 takes, for a box broken over lines, the reference
// box of each fragment under box-decoration-break clone, and that of the
// fragments sliced together otherwise. Chromium, on screen and in print,
// takes the box of the first fragment under either value, and clips away
// what the element paints on the lines after it: litho follows Chromium.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 draws the second and third lines of a
//     span of polygon(0 0, 100% 0, 100% 100%, 0 100%) over three lines
//     not at all, under slice and clone, in its screenshot and its PDF;
//     polygon(0 0, 50% 0, 50% 100%, 0 100%) keeps the left half of the
//     first line.
//   - wpt: css-masking/clip-path/clip-path-inline-001.
func (p *painter) inlineReference(b *box, g css.GeometryBox) (typeset.Rect, [4][2]float64, bool) {
	fs := p.inlineFrags(b)
	if len(fs) == 0 {
		return typeset.Rect{}, [4][2]float64{}, false
	}
	r, radii := fragReference(fs[0], g)
	return r, radii, true
}

// fragReference returns the reference box g of the inline box fragment fr
// and its corner radii, the corners where the box breaks square.
func fragReference(fr fragRect, g css.GeometryBox) (typeset.Rect, [4][2]float64) {
	b := fr.frag.box
	r := fr.r
	bd, pd := fragEdges(fr.frag)
	radii := radiiOf(b, r.Width, r.Height)
	if !fr.frag.left {
		radii[css.TopLeft], radii[css.BottomLeft] = [2]float64{}, [2]float64{}
	}
	if !fr.frag.right {
		radii[css.TopRight], radii[css.BottomRight] = [2]float64{}, [2]float64{}
	}
	switch g {
	case css.GeoBorder:
	case css.GeoPadding:
		r, radii = insetRect(r, radii, bd)
	case css.GeoContent:
		r, radii = insetRect(r, radii, addSides(bd, pd))
	case css.GeoMargin:
		// The margin box holds the vertical margins, which take no room
		// on the line (CSS 2.1, 10.6.1).
		m := b.m
		if c := relContainer(b); c != nil {
			_, _, all := edgesOf(b, c.w)
			m[css.Top], m[css.Bottom] = all[css.Top], all[css.Bottom]
		}
		if !fr.frag.left {
			m[css.Left] = 0
		}
		if !fr.frag.right {
			m[css.Right] = 0
		}
		r = typeset.Rect{X: r.X - m[css.Left], Y: r.Y - m[css.Top], Width: r.Width + m[css.Left] + m[css.Right], Height: r.Height + m[css.Top] + m[css.Bottom]}
		radii = marginRadii(r, radii, m)
	}
	return r, radii
}

// inlineBounds returns the union of the fragments of the inline element b,
// false for none.
func (p *painter) inlineBounds(b *box) (typeset.Rect, bool) {
	fs := p.inlineFrags(b)
	if len(fs) == 0 {
		return typeset.Rect{}, false
	}
	out := fs[0].r
	for _, f := range fs[1:] {
		out = union(out, f.r)
	}
	return out, true
}

// inlineMask returns the soft mask of the inline element b: each fragment
// masked as a background image paints it (CSS Masking 1, 6.1 and CSS
// Fragmentation 3, 5.4), its layers laid over the fragments of its box
// sliced together under box-decoration-break slice, over the fragment
// alone under clone. nil for none.
func (p *painter) inlineMask(b *box) *typeset.SoftMask {
	var out *typeset.SoftMask
	for _, fr := range p.inlineFrags(b) {
		strip := fr
		if !clones(b.st) {
			s := p.imageStrip(fr.frag, fr.r)
			before := s.before
			if b.st.Direction == css.RTL {
				before = s.total - s.before - fr.r.Width
			}
			strip.r = typeset.Rect{X: fr.r.X - before, Y: fr.r.Y, Width: s.total, Height: fr.r.Height}
			strip.frag = &inlineFrag{box: fr.frag.box, left: true, right: true}
		}
		saved := p.maskRef
		p.maskRef = &maskRef{origin: strip, clip: fr}
		m := p.maskOf(b)
		p.maskRef = saved
		if m == nil {
			continue
		}
		if out == nil {
			out = &typeset.SoftMask{Kind: m.Kind}
		}
		out.Content = append(out.Content, m.Content...)
	}
	return out
}

// maskRef is the fragment of an inline box whose mask is drawn: origin is
// the box its layers are positioned in, clip the fragment.
type maskRef struct {
	origin, clip fragRect
}

// inlineUnit paints the inline element b of a group effect and what it
// holds, after the effects of its group are opened: the floats, the blocks
// split out of it and the content of its pieces on every line, in the
// order of CSS 2.1, Appendix E, its positioned descendants in its own
// layers (CSS Color 4, 15; CSS Masking 1, 5.1 and 6.1; Filter Effects 1,
// 6: the effect applies to the element's whole rendering, block and float
// descendants included).
func (p *painter) inlineUnit(b *box) {
	scope := inlineScope(b)
	if scope == nil {
		return
	}
	var blocks, floats, layers, hosted []*box
	var pieces func(k *box, depth int)
	pieces = func(k *box, depth int) {
		for _, c := range k.kids {
			if c.kind != kindInline || depth >= maxTreeDepth {
				continue
			}
			if c.node == b.node {
				p.collect(c, &blocks, &floats, &layers)
				continue
			}
			pieces(c, depth+1)
		}
	}
	holders := lineHolders(scope)
	for _, k := range scope.kids {
		switch {
		case k.kind == kindInline && k.node == b.node:
			p.collect(k, &blocks, &floats, &layers)
		case k.kind == kindInline:
			pieces(k, 0)
		case k.anon && k.kind == kindBlock:
			pieces(k, 0)
		}
		if k.kind != kindInline && effectHost(k) != nil && effectHost(k).node == b.node {
			hosted = append(hosted, k)
			var bl, fl []*box
			p.collect(k, &bl, &fl, &layers)
		}
	}
	neg, zero, pos := p.stack(layers)
	for _, c := range neg {
		p.lifted(c)
	}
	for _, f := range floats {
		p.floating(f)
	}
	saved := p.only
	p.only = b
	for _, c := range holders {
		for i, l := range c.lines {
			var fl *firstLine
			if i == 0 {
				fl = p.e.firstLines[c]
			}
			p.line(l, fl)
		}
	}
	p.only = saved
	for _, k := range hosted {
		p.unit(k, false)
	}
	for _, c := range zero {
		p.lifted(c)
	}
	for _, c := range pos {
		p.lifted(c)
	}
}
