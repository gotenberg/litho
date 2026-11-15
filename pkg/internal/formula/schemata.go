package formula

import (
	"strings"

	mt "github.com/gotenberg/litho/pkg/internal/typography/mathtable"
)

// mspace lays out the blank space e, styled st (MathML Core, 3.2.5): its
// width attribute's width, its height above the baseline and its depth
// below, each a length that is not a percentage, a negative one read as
// 0. Where CSS sized the element, the presentational hints of its
// attributes among the rules, a height attribute that stands alone is
// the height CSS resolved, in whatever unit it is written.
func (l *layouter) mspace(e *Element, st *Style) *box {
	b := &box{}
	b.w = l.nonPercent(e, "width", st)
	b.asc = l.nonPercent(e, "height", st)
	b.desc = l.nonPercent(e, "depth", st)
	if _, ok := e.Attr("depth"); st.HasHeight && !ok && l.nonPercent(e, "height", st) == 0 {
		if v, ok := e.Attr("height"); ok && !strings.HasSuffix(strings.TrimSpace(v), "%") {
			b.asc = st.Height
		}
	}
	b.inkAsc, b.inkDesc = b.asc, b.desc
	return b
}

// nonPercent returns the attribute name of e as a length in points, 0
// when it is absent, invalid, a percentage or negative.
func (l *layouter) nonPercent(e *Element, name string, st *Style) float64 {
	v, ok := e.Attr(name)
	if !ok {
		return 0
	}
	n, ok := l.parseLength(v, st)
	if !ok || n.percent {
		return 0
	}
	return max(n.v, 0)
}

// mfrac lays the fraction e out, styled st (MathML Core, 3.3.2): its
// numerator over its denominator, both centred, the bar on the math axis,
// or a stack when the line thickness is zero. Without two in-flow
// children it is laid out as an mrow.
func (l *layouter) mfrac(e *Element, st *Style, c lctx, depth int) *box {
	kids, index := inFlow(e)
	if len(kids) != 2 {
		return l.rowOf(e, kids, index, st, c, depth)
	}
	nc := c
	if !c.stretch {
		nc = lctx{form: c.form}
	}
	num := l.child(e, kids[0], index[0], st, nc, depth)
	den := l.child(e, kids[1], index[1], st, lctx{}, depth)
	f := l.faceOf(st)
	size := st.Size
	thickness := f.constant(mt.FractionRuleThickness, size)
	if v, ok := e.Attr("linethickness"); ok {
		if n, ok := l.parseLength(v, st); ok {
			thickness = n.v
			if n.percent {
				thickness = float64(n.v * f.constant(mt.FractionRuleThickness, size))
			}
		}
	}
	thickness = max(thickness, 0)
	b := &box{op: num.op}
	if l.core(e, depth) == nil {
		b.op = nil
	}
	b.w = max(num.w, den.w)
	if st.HasWidth {
		// The bar spans the content box CSS sizes, the numerator and the
		// denominator centred in it (3.3.2.1).
		b.w = max(st.Width, 0)
	}
	axis := f.constant(mt.AxisHeight, size)
	var numShift, denShift float64
	if thickness > 0 {
		upC, downC, numGapC, denGapC := mt.FractionNumeratorShiftUp, mt.FractionDenominatorShiftDown, mt.FractionNumeratorGapMin, mt.FractionDenominatorGapMin
		if st.Display {
			upC, downC, numGapC, denGapC = mt.FractionNumeratorDisplayStyleShiftUp, mt.FractionDenominatorDisplayStyleShiftDown, mt.FractionNumDisplayStyleGapMin, mt.FractionDenomDisplayStyleGapMin
		}
		numShift = max(f.constant(upC, size), axis+float64(thickness/2)+f.constant(numGapC, size)+num.inkDesc)
		denShift = max(f.constant(downC, size), float64(thickness/2)+f.constant(denGapC, size)+den.inkAsc-axis)
		b.asc = max(numShift+num.asc, den.asc-denShift, axis+float64(thickness/2))
		b.desc = max(num.desc-numShift, denShift+den.desc, float64(thickness/2)-axis)
		if !st.Hidden {
			b.fill(0, -(axis + float64(thickness/2)), b.w, thickness, st.Color)
		}
	} else {
		topC, bottomC, gapC := mt.StackTopShiftUp, mt.StackBottomShiftDown, mt.StackGapMin
		if st.Display {
			topC, bottomC, gapC = mt.StackTopDisplayStyleShiftUp, mt.StackBottomDisplayStyleShiftDown, mt.StackDisplayStyleGapMin
		}
		numShift, denShift = f.constant(topC, size), f.constant(bottomC, size)
		gap := (denShift - den.inkAsc) + (numShift - num.inkDesc)
		if delta := f.constant(gapC, size) - gap; delta > 0 {
			half := float64(delta / 2)
			numShift += half
			denShift += delta - half
		}
		b.asc = max(numShift+num.asc, den.asc-denShift)
		b.desc = max(num.desc-numShift, denShift+den.desc)
	}
	b.inkAsc, b.inkDesc = b.asc, b.desc
	b.add(num, float64((b.w-num.w)/2), -numShift)
	b.add(den, float64((b.w-den.w)/2), denShift)
	return b
}

// radical returns the surd of a radical over a base of ink height
// baseInk, styled st (MathML Core, 3.3.3.1): the glyph of U+221A SQUARE
// ROOT stretched to the rule thickness, the gap and that height, or the
// glyph as it is when the font cannot stretch it.
func (l *layouter) radical(st *Style, baseInk float64) (*box, float64, float64, float64) {
	f := l.faceOf(st)
	size := st.Size
	gap := f.constant(mt.RadicalVerticalGap, size)
	if st.Display {
		gap = f.constant(mt.RadicalDisplayStyleVerticalGap, size)
	}
	rule := f.constant(mt.RadicalRuleThickness, size)
	extra := f.constant(mt.RadicalExtraAscender, size)
	if f == nil || f.glyphs == nil {
		return &box{}, gap, rule, extra
	}
	gid, ok := f.glyphs.NominalGlyph('√')
	if !ok || gid > 0xFFFF {
		return l.textBox("√", st), gap, rule, extra
	}
	g := l.stretchGlyph(f, uint16(gid), true, rule+gap+baseInk, size) // fits uint16: checked above
	if g == nil {
		g = l.singleGlyph(f, uint16(gid), size) // fits uint16: checked above
	}
	return l.glyphBox(g, '√', st, 0), gap, rule, extra
}

// sqrtBox lays base out under a radical, styled st (MathML Core, 3.3.3.2):
// the surd on the left, the overbar over the base.
func (l *layouter) sqrtBox(base *box, st *Style) *box {
	surd, gap, rule, extra := l.radical(st, base.inkAsc+base.inkDesc)
	b := &box{w: surd.w + base.w}
	b.asc = max(base.asc, base.inkAsc+gap+rule+extra)
	b.desc = max(base.desc, surd.asc+surd.desc+extra-b.asc)
	b.inkAsc, b.inkDesc = b.asc, b.desc
	barTop := b.asc - extra
	// The surd comes first, so the text read from the page is the
	// radical's character, then its base.
	b.add(surd, 0, -(barTop - surd.asc))
	if !st.Hidden {
		b.fill(surd.w, -barTop, base.w, rule, st.Color)
	}
	b.add(base, surd.w, 0)
	return b
}

// msqrt lays the square root e out, styled st (MathML Core, 3.3.3.2): its
// in-flow children as the anonymous mrow of its base.
func (l *layouter) msqrt(e *Element, st *Style, c lctx, depth int) *box {
	base := l.row(e, st, c.constrained(), depth)
	return l.sqrtBox(base, st)
}

// mroot lays the root e out, styled st (MathML Core, 3.3.3.3): its base
// under the radical, its index raised over the surd's left. Without two
// in-flow children it is laid out as an mrow.
//
// The index's baseline sits RadicalDegreeBottomRaisePercent of the
// radical's height plus the index's line-descent above the radical's
// bottom, as 3.3.3.3 places it, and the box's line-ascent and
// line-descent are those of that placement; the formulas 3.3.3.3 gives
// for them leave the index's line-descent out of the one and count its
// line-ascent in the other. The base follows the index and the kern
// after it, as the inline size 3.3.3.3 gives counts that kern.
func (l *layouter) mroot(e *Element, st *Style, c lctx, depth int) *box {
	kids, index := inFlow(e)
	if len(kids) != 2 {
		return l.rowOf(e, kids, index, st, c, depth)
	}
	base := l.child(e, kids[0], index[0], st, lctx{}, depth)
	idx := l.child(e, kids[1], index[1], st, lctx{}, depth)
	rb := l.sqrtBox(base, st)
	f := l.faceOf(st)
	before := max(0, f.constant(mt.RadicalKernBeforeDegree, st.Size))
	after := max(-idx.w, f.constant(mt.RadicalKernAfterDegree, st.Size))
	raise := float64(f.constant(mt.RadicalDegreeBottomRaisePercent, st.Size) * rb.height())
	idxBase := -rb.desc + raise + idx.desc
	b := &box{w: before + idx.w + after + rb.w}
	b.asc = max(rb.asc, idxBase+idx.asc)
	b.desc = max(rb.desc, idx.desc-idxBase)
	b.inkAsc, b.inkDesc = b.asc, b.desc
	b.add(idx, before, -idxBase)
	b.add(rb, before+idx.w+after, 0)
	return b
}

// mpadded lays e out, styled st (MathML Core, 3.3.6): its in-flow
// children as an anonymous mrow, the inner box, in a box of the width,
// height and depth its attributes request, the inner box moved by its
// lspace and voffset.
func (l *layouter) mpadded(e *Element, st *Style, c lctx, depth int) *box {
	inner := l.row(e, st, c, depth)
	length := func(name string) (float64, bool) {
		v, ok := e.Attr(name)
		if !ok {
			return 0, false
		}
		n, ok := l.parseLength(v, st)
		if !ok || n.percent {
			return 0, false
		}
		return n.v, true
	}
	b := &box{w: inner.w, asc: inner.asc, desc: inner.desc, op: inner.op}
	if v, ok := length("width"); ok {
		b.w = max(v, 0)
	}
	if v, ok := length("height"); ok {
		b.asc = max(v, 0)
	}
	if v, ok := length("depth"); ok {
		b.desc = max(v, 0)
	}
	lspace, _ := length("lspace")
	voffset, _ := length("voffset")
	b.inkAsc, b.inkDesc = b.asc, b.desc
	b.add(inner, max(lspace, 0), -voffset)
	return b
}
