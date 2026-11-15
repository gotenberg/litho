package formula

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/typeset"
	mt "github.com/gotenberg/litho/pkg/internal/typography/mathtable"
)

// operatorOf returns the properties of the embellished operator whose
// core operator is the mo element op, in form f unless op's form
// attribute says otherwise (MathML Core, 3.2.4.2): the attributes on op,
// else the operator dictionary's values for its text. With st, the
// style of op, lspace and rspace resolve in points against op's font;
// with nil they stay 0, for the parent that only needs the flags.
func (l *layouter) operatorOf(op *Element, f form, st *Style) *operator {
	explicit := false
	if v, ok := op.Attr("form"); ok {
		if g, ok := parseForm(v); ok {
			f, explicit = g, true
		}
	}
	content := strings.Join(strings.Fields(op.Text), " ")
	d := categoryDefaults[dictionaryCategory(content, f, explicit)]
	o := &operator{
		form: f, stretchy: d.stretchy, symmetric: d.symmetric, largeop: d.largeop,
		movablelimits: d.movablelimits,
	}
	for _, b := range []struct {
		name string
		v    *bool
	}{{"stretchy", &o.stretchy}, {"symmetric", &o.symmetric}, {"largeop", &o.largeop}, {"movablelimits", &o.movablelimits}} {
		switch {
		case op.attrIs(b.name, "true"):
			*b.v = true
		case op.attrIs(b.name, "false"):
			*b.v = false
		}
	}
	if rs := []rune(content); len(rs) == 1 {
		o.inline = stretchesInline(rs[0])
	}
	if st != nil {
		o.lspace = l.space(op, "lspace", d.lspace, st)
		o.rspace = l.space(op, "rspace", d.rspace, st)
	}
	return o
}

// space returns the lspace or rspace of op styled st: its attribute, a
// percentage of the dictionary's value em, or the dictionary's value
// (MathML Core, 3.2.4.2). A negative space stays as given.
func (l *layouter) space(op *Element, name string, em float64, st *Style) float64 {
	def := float64(em * st.Size)
	v, ok := op.Attr(name)
	if !ok {
		return def
	}
	n, ok := l.parseLength(v, st)
	switch {
	case !ok:
		return def
	case n.percent:
		return float64(n.v * def)
	}
	return n.v
}

// mo lays the operator e out, styled st (MathML Core, 3.2.4.3): a single
// character stretched to the constraint c gives along its stretch axis,
// or drawn at its display size as a large operator, and otherwise the
// layout of mtext.
func (l *layouter) mo(e *Element, st *Style, c lctx) *box {
	op := l.operatorOf(e, c.form, st)
	var b *box
	if rs := []rune(strings.TrimSpace(e.Text)); len(rs) == 1 {
		b = l.moGlyph(e, rs[0], op, st, c)
	}
	if b == nil {
		b = l.token(e, st)
	}
	b.op = op
	return b
}

// moGlyph lays out the operator of one character r, or returns nil to
// fall back to the layout of mtext.
//
// Under [Setter.BrowserRules], a stretchy operator along the block axis
// whose first available font gives its glyph no construction keeps its
// glyph's size and is moved to the middle of the target sizes, as a stretched
// glyph is, where MathML Core, 3.2.4.3 lays it out as an mtext. The target
// of a symmetric operator is centred on the math axis, so the property
// still moves the glyph. Chromium's screen and print share the layout.
//
// # Corpus evidence
//
//   - wpt: op-dict-3, a parenthesis in Liberation Serif, which has no
//     MATH table, beside an mspace 5em high and 5em deep, symmetric false
//     against symmetric true; Chromium of gotenberg/gotenberg:9.4.1 draws
//     the first 5px lower, its glyph centred on the baseline where the
//     second's is centred on the axis.
func (l *layouter) moGlyph(e *Element, r rune, op *operator, st *Style, c lctx) *box {
	f := l.faceOf(st)
	if f == nil || f.glyphs == nil {
		return nil
	}
	gid16, ok := f.glyphs.NominalGlyph(r)
	if !ok || gid16 > math.MaxUint16 {
		return nil
	}
	gid := uint16(gid16) // fits uint16: checked above
	if st.RTL {
		if m, ok := l.mirroredGlyph(f, r, st); ok {
			gid = m
		}
	}
	switch {
	case op.stretchy && op.inline:
		if _, _, ok := f.math.Construction(gid, false); !ok || !c.stretch || !c.inline {
			return nil
		}
		g := l.stretchGlyph(f, gid, false, c.target, st.Size)
		if g == nil {
			return nil
		}
		return l.glyphBox(g, r, st, 0)
	case op.stretchy:
		if !c.stretch || c.inline {
			return nil
		}
		_, _, ok := f.math.Construction(gid, true)
		scale := !ok && f.distances != nil && f.distances.BracketExcess > 0
		if !ok && !l.s.BrowserRules && !scale {
			return nil
		}
		axis := f.constant(mt.AxisHeight, st.Size)
		tAsc, tDesc := c.asc, c.desc
		if scale {
			return l.scaledBracket(f, gid, r, st, tAsc, tDesc)
		}
		if op.symmetric {
			half := max(tAsc-axis, tDesc+axis)
			tAsc, tDesc = half+axis, half-axis
		}
		glyphH := l.glyphHeight(f, gid, st.Size)
		minSize, maxSize := glyphH, math.Inf(1)
		if v, ok := e.Attr("minsize"); ok {
			if n, ok := l.parseLength(v, st); ok {
				minSize = n.v
				if n.percent {
					minSize = float64(n.v * glyphH)
				}
			}
		}
		if v, ok := e.Attr("maxsize"); ok {
			if n, ok := l.parseLength(v, st); ok {
				maxSize = n.v
				if n.percent {
					maxSize = float64(n.v * glyphH)
				}
			}
		}
		minSize = max(minSize, 0)
		maxSize = max(maxSize, minSize)
		switch t := tAsc + tDesc; {
		case t <= 0:
			tAsc = float64(minSize/2) + axis
			tDesc = minSize - tAsc
		case t < minSize:
			tAsc = max(0, float64(float64(tAsc-axis)*minSize)/t+axis)
			tDesc = minSize - tAsc
		case maxSize < t:
			tAsc = max(0, float64(float64(tAsc-axis)*maxSize)/t+axis)
			tDesc = maxSize - tAsc
		}
		g := l.stretchGlyph(f, gid, true, tAsc+tDesc, st.Size)
		if g == nil {
			// A glyph the font cannot stretch keeps its size and is
			// centred on the target as a stretched one is.
			g = l.singleGlyph(f, gid, st.Size)
		}
		// Centre the glyph on the target: shift it down by delta.
		delta := (float64(g.asc-g.desc) - float64(tAsc-tDesc)) / 2
		return l.glyphBox(g, r, st, delta)
	case op.largeop && st.Display:
		variants, _, ok := f.math.Construction(gid, true)
		if d := f.distances; (!ok || len(variants) == 0) && d != nil && d.LargeOperatorScale > 1 {
			// The glyph drawn larger, its middle kept.
			g0 := l.singleGlyph(f, gid, st.Size)
			g := l.singleGlyph(f, gid, float64(st.Size*d.LargeOperatorScale))
			delta := float64(float64(g.asc-g.desc)-float64(g0.asc-g0.desc)) / 2
			return l.glyphBox(g, r, st, delta)
		}
		if !ok || len(variants) == 0 {
			return nil
		}
		minH := f.constant(mt.DisplayOperatorMinHeight, st.Size)
		pick := variants[len(variants)-1]
		for _, v := range variants {
			if f.units(float64(v.Advance), st.Size) >= minH {
				pick = v
				break
			}
		}
		g := l.singleGlyph(f, pick.GID, st.Size)
		return l.glyphBox(g, r, st, 0)
	}
	return nil
}

// scaledBracket returns the glyph gid of f for the character r scaled
// vertically, its width kept, so that its ink covers tAsc above the
// baseline and tDesc below and [Distances.BracketExcess] of their sum
// more, centred on them; a glyph that covers them already keeps its size.
func (l *layouter) scaledBracket(f *face, gid uint16, r rune, st *Style, tAsc, tDesc float64) *box {
	g := l.singleGlyph(f, gid, st.Size)
	h := g.asc + g.desc
	want := float64((tAsc + tDesc) * (1 + f.distances.BracketExcess))
	if h > 0 && want > h {
		k := min(want/h, 1/typeset.MinHScale)
		w := g.w
		g = l.singleGlyph(f, gid, float64(st.Size*k))
		g.w, g.hscale, g.italic = w, 1/k, float64(g.italic/k)
	}
	delta := (float64(g.asc-g.desc) - float64(tAsc-tDesc)) / 2
	b := l.glyphBox(g, r, st, delta)
	return b
}

// glyphHeight returns the ink height of glyph gid of f at size.
func (l *layouter) glyphHeight(f *face, gid uint16, size float64) float64 {
	g := l.singleGlyph(f, gid, size)
	return g.asc + g.desc
}
