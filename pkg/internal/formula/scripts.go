package formula

import (
	mt "github.com/gotenberg/litho/pkg/internal/typography/mathtable"
)

// baseContext returns the constraint the base of a scripted element or
// the numerator of an mfrac is laid out under: its parent's when the
// parent has one (MathML Core, 3.4.1.2), with the parent's form.
func baseContext(c lctx) lctx {
	if c.stretch {
		return c
	}
	return lctx{form: c.form}
}

// italics returns LargeOpItalicCorrection and ItalicCorrection of a base
// (MathML Core, 3.4.1.2 and 3.4.1.3): the base's italic correction, the
// first for an embellished large operator, the second for any other.
func italics(base *box) (largeOp, italic float64) {
	if base.op != nil && base.op.largeop {
		return base.italic, 0
	}
	return 0, base.italic
}

// shifts holds what the scripts of a base are placed by.
type shifts struct {
	sub, sup float64
}

// subShift returns SubShift of a subscript under base (MathML Core,
// 3.4.1.2).
func subShift(f *face, size float64, base, sub *box) float64 {
	return max(f.constant(mt.SubscriptShiftDown, size),
		sub.inkAsc-f.constant(mt.SubscriptTopMax, size),
		f.constant(mt.SubscriptBaselineDropMin, size)+base.inkDesc)
}

// supShift returns SuperShift of a superscript over base, cramped under
// math-shift: compact (MathML Core, 3.4.1.3).
func supShift(f *face, size float64, cramped bool, base, sup *box) float64 {
	up := f.constant(mt.SuperscriptShiftUp, size)
	if cramped {
		up = f.constant(mt.SuperscriptShiftUpCramped, size)
	}
	return max(up,
		f.constant(mt.SuperscriptBottomMin, size)+sup.inkDesc,
		base.inkAsc-f.constant(mt.SuperscriptBaselineDropMax, size))
}

// pairShifts returns SubShift and SuperShift of a subscript and a
// superscript on base, moved apart until their gap is at least
// SubSuperscriptGapMin (MathML Core, 3.4.1.4).
func pairShifts(f *face, size float64, cramped bool, base, sub, sup *box) shifts {
	s := shifts{sub: subShift(f, size, base, sub), sup: supShift(f, size, cramped, base, sup)}
	gapMin := f.constant(mt.SubSuperscriptGapMin, size)
	gap := (s.sub - sub.inkAsc) + (s.sup - sup.inkDesc)
	if gap < gapMin {
		if d := f.constant(mt.SuperscriptBottomMaxWithSubscript, size) - (s.sup - sup.inkDesc); d > 0 {
			d = min(d, gapMin-gap)
			s.sup += d
			gap += d
		}
		if d := gapMin - gap; d > 0 {
			s.sub += d
		}
	}
	return s
}

// scripts lays out msub, msup and msubsup (MathML Core, 3.4.1): the base,
// the subscript after it less LargeOpItalicCorrection and shifted down,
// the superscript after it plus ItalicCorrection and shifted up, then
// SpaceAfterScript. With another number of in-flow children it is laid
// out as an mrow.
func (l *layouter) scripts(e *Element, st *Style, c lctx, depth int) *box {
	kids, index := inFlow(e)
	want := 2
	if e.Name == "msubsup" {
		want = 3
	}
	if len(kids) != want {
		return l.rowOf(e, kids, index, st, c, depth)
	}
	base := l.child(e, kids[0], index[0], st, baseContext(c), depth)
	var sub, sup *box
	switch e.Name {
	case "msub":
		sub = l.child(e, kids[1], index[1], st, lctx{form: postfix}, depth)
	case "msup":
		sup = l.child(e, kids[1], index[1], st, lctx{form: postfix}, depth)
	default:
		sub = l.child(e, kids[1], index[1], st, lctx{form: postfix}, depth)
		sup = l.child(e, kids[2], index[2], st, lctx{form: postfix}, depth)
	}
	return l.attachScripts(base, sub, sup, st)
}

// attachScripts places a subscript, a superscript or both on base, styled
// st, as msub, msup and msubsup do (MathML Core, 3.4.1.2 to 3.4.1.4); a
// nil script is absent.
func (l *layouter) attachScripts(base, sub, sup *box, st *Style) *box {
	f := l.faceOf(st)
	size := st.Size
	largeOp, italic := italics(base)
	space := f.constant(mt.SpaceAfterScript, size)
	b := &box{op: base.op, asc: base.asc, desc: base.desc}
	b.add(base, 0, 0)
	var s shifts
	switch {
	case sub != nil && sup != nil:
		s = pairShifts(f, size, st.Cramped, base, sub, sup)
	case sub != nil:
		s.sub = subShift(f, size, base, sub)
	case sup != nil:
		s.sup = supShift(f, size, st.Cramped, base, sup)
	}
	if sub != nil {
		b.w = max(b.w, base.w-largeOp+sub.w+space)
		b.asc = max(b.asc, sub.asc-s.sub)
		b.desc = max(b.desc, sub.desc+s.sub)
		b.add(sub, base.w-largeOp, s.sub)
	}
	if sup != nil {
		b.w = max(b.w, base.w+italic+sup.w+space)
		b.asc = max(b.asc, sup.asc+s.sup)
		b.desc = max(b.desc, sup.desc-s.sup)
		b.add(sup, base.w+italic, -s.sup)
	}
	b.inkAsc, b.inkDesc = b.asc, b.desc
	return b
}

// underOver lays out munder, mover and munderover (MathML Core, 3.4.2):
// as scripts when the element is compact and its base an operator with
// movablelimits, else the scripts centred under and over the base,
// stretchy operators stretched along the inline axis to the widest
// child. With another number of in-flow children it is laid out as an
// mrow.
func (l *layouter) underOver(e *Element, st *Style, c lctx, depth int) *box {
	kids, index := inFlow(e)
	want := 2
	if e.Name == "munderover" {
		want = 3
	}
	if len(kids) != want {
		return l.rowOf(e, kids, index, st, c, depth)
	}
	if !st.Display {
		if cr := l.core(kids[0], depth+1); cr != nil && l.operatorOf(cr, c.form, nil).movablelimits {
			base := l.child(e, kids[0], index[0], st, baseContext(c), depth)
			var sub, sup *box
			switch e.Name {
			case "munder":
				sub = l.child(e, kids[1], index[1], st, lctx{form: postfix}, depth)
			case "mover":
				sup = l.child(e, kids[1], index[1], st, lctx{form: postfix}, depth)
			default:
				sub = l.child(e, kids[1], index[1], st, lctx{form: postfix}, depth)
				sup = l.child(e, kids[2], index[2], st, lctx{form: postfix}, depth)
			}
			return l.attachScripts(base, sub, sup, st)
		}
	}
	boxes := l.stretchInline(e, kids, index, st, c, depth)
	base := boxes[0]
	var under, over *box
	switch e.Name {
	case "munder":
		under = boxes[1]
	case "mover":
		over = boxes[1]
	default:
		under, over = boxes[1], boxes[2]
	}
	return l.attachUnderOver(e, base, under, over, st)
}

// stretchInline lays the in-flow children kids of e out by the algorithm
// for stretching operators along the inline axis (MathML Core, 3.4.2.2):
// the base under the constraint of c when there is one, the children
// that do not stretch inline, then those that do, to the widest of the
// others.
func (l *layouter) stretchInline(e *Element, kids []*Element, index []int, st *Style, c lctx, depth int) []*box {
	boxes := make([]*box, len(kids))
	formOf := func(i int) form {
		if i == 0 {
			return c.form
		}
		return postfix
	}
	if c.stretch {
		boxes[0] = l.child(e, kids[0], index[0], st, c, depth)
	}
	var stretchers []int
	t, laid := 0.0, false
	for i, k := range kids {
		if boxes[i] != nil {
			continue
		}
		if cr := l.core(k, depth+1); cr != nil {
			if op := l.operatorOf(cr, formOf(i), nil); op.stretchy && op.inline {
				stretchers = append(stretchers, i)
				continue
			}
		}
		boxes[i] = l.child(e, k, index[i], st, lctx{form: formOf(i)}, depth)
		t, laid = max(t, boxes[i].w), true
	}
	if !laid {
		t = 0
	}
	for _, i := range stretchers {
		boxes[i] = l.child(e, kids[i], index[i], st, lctx{form: formOf(i), stretch: true, inline: true, target: t}, depth)
	}
	return boxes
}

// attachUnderOver places an underscript, an overscript or both on base,
// styled st, as munder, mover and munderover do (MathML Core, 3.4.2.3 to
// 3.4.2.5); a nil script is absent.
//
// A script's baseline sits the base's ink line-descent, or ink
// line-ascent, plus UnderShift, or OverShift, from the base's baseline,
// as 3.4.2.3 and 3.4.2.4 place it, and the box's line-ascent and
// line-descent are those of that placement, where the formulas the two
// sections give for them leave the base's ink out. The overscript is
// placed by its top accent attachment, as the inline size 3.4.2.4 gives
// counts it.
//
// Under a base that stretches along the inline axis the underscript keeps
// StretchStackGapBelowMin from it and the overscript
// StretchStackGapAboveMin, the gaps the OpenType MATH table defines
// below and above a stretched element, where 3.4.2.3 and 3.4.2.4 name
// them the other way round; the tests of web-platform-tests
// (underover-parameters-2) read them as the MATH table does.
func (l *layouter) attachUnderOver(e *Element, base, under, over *box, st *Style) *box {
	f := l.faceOf(st)
	size := st.Size
	largeOp, _ := italics(base)
	stretchyInline := base.op != nil && base.op.stretchy && base.op.inline
	hi, lo := float64(base.w/2), -float64(base.w/2)
	halfOp := float64(largeOp / 2)
	if under != nil {
		hi = max(hi, float64(under.w/2)-halfOp)
		lo = min(lo, -float64(under.w/2)-halfOp)
	}
	attach := 0.0
	if over != nil {
		attach = float64(over.w / 2)
		if over.hasAccent {
			attach = over.accent
		}
		hi = max(hi, over.w-attach+halfOp)
		lo = min(lo, -attach+halfOp)
	}
	b := &box{w: hi - lo, op: base.op, asc: base.asc, desc: base.desc}
	b.add(base, -lo-float64(base.w/2), 0)
	if under != nil {
		var shift, extra float64
		switch {
		case base.op != nil && base.op.largeop:
			shift = max(f.constant(mt.LowerLimitBaselineDropMin, size), f.constant(mt.LowerLimitGapMin, size)+under.asc)
		case stretchyInline:
			shift = max(f.constant(mt.StretchStackBottomShiftDown, size), f.constant(mt.StretchStackGapBelowMin, size)+under.asc)
		default:
			if !e.attrIs("accentunder", "true") {
				shift = f.constant(mt.UnderbarVerticalGap, size)
			}
			extra = f.constant(mt.UnderbarExtraDescender, size)
		}
		y := base.inkDesc + shift
		b.asc = max(b.asc, under.asc-y)
		b.desc = max(b.desc, under.desc+y+extra)
		b.add(under, -lo-float64(under.w/2)-halfOp, y)
	}
	if over != nil {
		var shift, extra float64
		switch {
		case base.op != nil && base.op.largeop:
			shift = max(f.constant(mt.UpperLimitBaselineRiseMin, size), f.constant(mt.UpperLimitGapMin, size)+over.desc)
		case stretchyInline:
			shift = max(f.constant(mt.StretchStackTopShiftUp, size), f.constant(mt.StretchStackGapAboveMin, size)+over.desc)
		default:
			if e.attrIs("accent", "true") {
				shift = max(0, f.constant(mt.AccentBaseHeight, size)-base.asc)
			} else {
				shift = f.constant(mt.OverbarVerticalGap, size)
			}
			extra = f.constant(mt.OverbarExtraAscender, size)
		}
		y := base.inkAsc + shift
		b.asc = max(b.asc, over.asc+y+extra)
		b.desc = max(b.desc, over.desc-y)
		b.add(over, -lo-attach+halfOp, -y)
	}
	b.inkAsc, b.inkDesc = b.asc, b.desc
	return b
}

// multiscripts lays out mmultiscripts (MathML Core, 3.4.3): the
// prescripts before the base, the postscripts after it, each pair a
// subscript and a superscript at the shifts that suit every pair. An
// element that is not valid is laid out as an mrow.
func (l *layouter) multiscripts(e *Element, st *Style, c lctx, depth int) *box {
	kids, index := inFlow(e)
	pre := -1
	for i, k := range kids {
		if k.Name == "mprescripts" {
			if pre >= 0 || i == 0 {
				return l.rowOf(e, kids, index, st, c, depth)
			}
			pre = i
		}
	}
	posts, pres := kids[1:], []*Element(nil)
	if pre >= 0 {
		posts, pres = kids[1:pre], kids[pre+1:]
	}
	if len(kids) == 0 || len(posts)%2 != 0 || len(pres)%2 != 0 {
		return l.rowOf(e, kids, index, st, c, depth)
	}
	base := l.child(e, kids[0], index[0], st, baseContext(c), depth)
	boxes := make([]*box, len(kids))
	for i := 1; i < len(kids); i++ {
		boxes[i] = l.child(e, kids[i], index[i], st, lctx{form: postfix}, depth)
	}
	f := l.faceOf(st)
	size := st.Size
	space := f.constant(mt.SpaceAfterScript, size)
	largeOp, italic := italics(base)
	type pair struct{ sub, sup *box }
	var postPairs, prePairs []pair
	for i := 1; i+1 < len(kids); i += 2 {
		if pre >= 0 && i >= pre {
			break
		}
		postPairs = append(postPairs, pair{boxes[i], boxes[i+1]})
	}
	if pre >= 0 {
		for i := pre + 1; i+1 < len(kids); i += 2 {
			prePairs = append(prePairs, pair{boxes[i], boxes[i+1]})
		}
	}
	var s shifts
	for _, p := range append(slicesConcat(prePairs, postPairs), pair{}) {
		if p.sub == nil {
			continue
		}
		ps := pairShifts(f, size, st.Cramped, base, p.sub, p.sup)
		s.sub, s.sup = max(s.sub, ps.sub), max(s.sup, ps.sup)
	}
	b := &box{op: base.op, asc: base.asc, desc: base.desc}
	grow := func(p pair) {
		b.asc = max(b.asc, p.sub.asc-s.sub, p.sup.asc+s.sup)
		b.desc = max(b.desc, p.sub.desc+s.sub, p.sup.desc-s.sup)
	}
	x := 0.0
	for _, p := range prePairs {
		x += space
		w := max(p.sub.w, p.sup.w)
		b.add(p.sub, x+w-p.sub.w, s.sub)
		b.add(p.sup, x+w-p.sup.w, -s.sup)
		grow(p)
		x += w
	}
	b.add(base, x, 0)
	x += base.w
	b.w = x
	for _, p := range postPairs {
		w := max(p.sub.w, p.sup.w)
		b.add(p.sub, x-largeOp, s.sub)
		b.add(p.sup, x+italic, -s.sup)
		b.w = max(b.w, x+p.sub.w-largeOp, x+p.sup.w+italic)
		grow(p)
		x += w + space
		b.w = max(b.w, x)
	}
	b.inkAsc, b.inkDesc = b.asc, b.desc
	return b
}

// slicesConcat returns the pairs of a then those of b.
func slicesConcat[T any](a, b []T) []T {
	out := make([]T, 0, len(a)+len(b))
	return append(append(out, a...), b...)
}
