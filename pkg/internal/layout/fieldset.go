package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// fieldsetPart returns the fieldset part b's node plays, FieldsetNone for
// an anonymous box.
func fieldsetPart(b *box) FieldsetPart {
	if b == nil || b.node == nil {
		return FieldsetNone
	}
	return b.node.Fieldset
}

// renderedLegend returns the rendered legend of the fieldset box b, nil
// when b is no fieldset box or has none.
func renderedLegend(b *box) *box {
	if fieldsetPart(b) != FieldsetBox || len(b.kids) == 0 || fieldsetPart(b.kids[0]) != FieldsetLegend {
		return nil
	}
	return b.kids[0]
}

// mirrorFieldsetPadding gives the content box b of a fieldset the
// fieldset's used padding, and its negation as margins: the fieldset's
// padding applies to its anonymous content box (HTML Standard, 15.3.13),
// whose padding box then covers the fieldset's below the legend, while
// the fieldset keeps the padding that sizes it and that places its
// legend.
func mirrorFieldsetPadding(b *box) {
	f := b.parent
	if fieldsetPart(b) != FieldsetContent || fieldsetPart(f) != FieldsetBox {
		return
	}
	b.pd = f.pd
	for s := range b.m {
		b.m[s] = -f.pd[s]
	}
}

// placeLegend lays out lg, the rendered legend of the fieldset box f whose
// width and position are set, and returns how far below the block-start
// border the fieldset's content starts: the room the legend takes past
// the border (HTML Standard, 15.3.13). The legend establishes a
// formatting context; an auto width is the fit-content width in the
// fieldset's content box. Its border box is centred on the block-start
// border, whose painted top moves down by half the excess of a legend
// taller than it; the content starts below the lower of that border and
// the legend's bottom margin edge.
//
// Chromium departs from the clause, which reserves the legend's whole
// margin box: it lays out no block-start margin of a legend, on screen
// and in print.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws a 40px
//     legend of margin-top 20px or -10px in a 4px border with its top on
//     the fieldset's, the border from 18px down, the content from 40px; a
//     margin-bottom of 20px moves the content to 60px; a 20px legend in
//     a 60px border sits 20px down, the content from 60px.
func (e *engine) placeLegend(f, lg *box) float64 {
	lg.laidOut = true
	avail := f.w
	resolveEdges(lg, avail)
	e.resolveSizing(lg, cblock{w: avail}, -1, -1)
	if w, ok := specWidth(lg, lg.st.Width, avail); ok {
		lg.w = constrainWidth(lg, w, avail)
	} else {
		lg.w = constrainWidth(lg, e.shrinkToFit(lg, avail), avail)
	}
	lg.x = legendX(f, lg)
	lg.y = f.y
	e.noPaginate++
	e.layoutOwnContext(lg, cblock{w: avail})
	e.noPaginate--
	bd, size := f.bd[css.Top], lg.borderH()
	top := max((bd-size)/2, 0)
	lg.shift(0, top)
	return max(bd+max((size-bd)/2, 0), top+size+lg.m[css.Bottom]) - bd
}

// legendX returns the left edge of the border box of lg, the rendered
// legend of the fieldset box f, its width set: placed in f's content box
// by its auto margins as a block is, and otherwise by its text-align, in
// its own direction, offset by its start margin; a centred legend lies
// halfway across the room it leaves, past its start margin (HTML
// Standard, 15.3.13, leaves the placement to the margins and
// justify-self; Chromium places a legend by text-align and its legacy
// align attribute, and lays out no justify-self).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 places a 20px
//     legend of text-align: center in a 100px content box 40px in, 60px
//     in with margin-left: 20px, 40px in with margin-right: 20px, and in
//     a right-to-left fieldset with margin-left: 20px 40px from the end;
//     text-align: right and end, margin-left: auto and right-to-left
//     direction put it at the end, against its end margin; text-align:
//     right under margin: 0 auto centres it.
func legendX(f, lg *box) float64 {
	left, w, size := f.contentX(), f.w, lg.borderW()
	ml, mr := nonAutoMargin(lg, css.Left), nonAutoMargin(lg, css.Right)
	mlAuto, mrAuto := lg.st.Margin[css.Left].Kind == css.Auto, lg.st.Margin[css.Right].Kind == css.Auto
	switch {
	case mlAuto && mrAuto:
		return left + float64((w-size)/2)
	case mlAuto:
		return left + w - mr - size
	case mrAuto:
		return left + ml
	}
	rtl := lg.st.Direction == css.RTL
	switch lg.st.TextAlign { //nolint:exhaustive // justify and the rest place the legend at its start
	case css.AlignCenter:
		if rtl {
			return left + w - mr - float64((w-size)/2) - size
		}
		return left + ml + float64((w-size)/2)
	case css.AlignRight:
		return left + w - mr - size
	case css.AlignLeft:
		return left + ml
	case css.AlignEnd:
		rtl = !rtl
	}
	if rtl {
		return left + w - mr - size
	}
	return left + ml
}

// legendBorder returns how far below the top of the border box of the
// fieldset box f its block-start border and its background are drawn, and
// the rectangle, in f's flow coordinates, the border is not drawn behind:
// the legend lg's border box across, from f's top down the larger of the
// legend's and the border's height (HTML Standard, 15.3.13).
func legendBorder(f, lg *box) (float64, typeset.Rect) {
	bd, size := f.bd[css.Top], lg.borderH()
	return max((size-bd)/2, 0), typeset.Rect{X: lg.x, Y: f.y, Width: lg.borderW(), Height: max(size, bd)}
}

// cutBorder runs draw, which paints the border of the fieldset box b
// whose border box is r, so that nothing it paints lands in cut: an
// even-odd clip of r, grown to hold cut, and of cut.
func (p *painter) cutBorder(b *box, r, cut typeset.Rect, draw func()) {
	outer := union(r, cut)
	ops := append(rectOps(outer), rectOps(cut)...)
	g := &ditem{group: true, fx: &groupFx{opacity: 1, clip: &typeset.Path{Ops: ops, Fill: &typeset.Fill{Rule: typeset.EvenOdd}}}}
	c := p.holder(b, false)
	c.kids = append(c.kids, g)
	saved := p.into
	p.into = g
	draw()
	p.into = saved
}

// fillFieldset sizes the content box of the fieldset box f, whose height
// is set, to reach the bottom of f's padding box: the content box fills
// the fieldset (HTML Standard, 15.3.13, block-size: 100%) whatever bounds
// the fieldset's height, a max-height included, so its overflow clips
// there, as Chromium clips it.
//
// # Corpus evidence
//
//   - wpt: html/rendering/non-replaced-elements/the-fieldset-and-legend-elements/fieldset-overflow
//     (a fieldset of padding 50px, max-height 50px and overflow: scroll
//     clips its 200px child 150px down) draws its reference in Chromium
//     156.
func fillFieldset(f *box) {
	if fieldsetPart(f) != FieldsetBox || len(f.kids) == 0 {
		return
	}
	c := f.kids[len(f.kids)-1]
	if fieldsetPart(c) != FieldsetContent || !c.laidOut || c.abs || c.float != css.FloatNone {
		return
	}
	bottom := f.contentY() + f.h + f.pd[css.Bottom]
	c.h = max(bottom-c.y-vEdges(c), 0)
}

// flexOrGridContent returns the content box of the fieldset box f when it
// is a flex or a grid container, nil otherwise.
//
// # Corpus evidence
//
//   - wpt: html/rendering/non-replaced-elements/the-fieldset-and-legend-elements/fieldset-baseline
//     (a fieldset of display: inline-flex aligns its first line with the
//     text beside it) draws its reference in Chromium 156.
func flexOrGridContent(f *box) *box {
	if fieldsetPart(f) != FieldsetBox || len(f.kids) == 0 {
		return nil
	}
	c := f.kids[len(f.kids)-1]
	if fieldsetPart(c) != FieldsetContent || c.flex == nil && c.grid == nil {
		return nil
	}
	return c
}
