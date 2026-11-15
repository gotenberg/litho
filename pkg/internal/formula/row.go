package formula

// child lays out k, the child at index i of e styled st, under c.
func (l *layouter) child(e, k *Element, i int, st *Style, c lctx, depth int) *box {
	return l.layout(k, l.childStyle(k, e, i, st), c, depth+1)
}

// childForms returns the form of each in-flow child of e, kids, as an
// embellished operator (MathML Core, 3.2.4.2). The child through which e
// is itself an embellished operator takes e's form, which c holds; in a
// grouping element, an mpadded, an msqrt or a table cell holding more
// than one in-flow child that is not space-like, the first such child is
// prefix and the last postfix; in a scripted element every child after
// the first is postfix; the rest are infix.
func (l *layouter) childForms(e *Element, kids []*Element, c lctx, through *Element, depth int) []form {
	forms := make([]form, len(kids))
	switch {
	case isScripted(e.Name):
		for i := 1; i < len(kids); i++ {
			forms[i] = postfix
		}
	case isGrouping(e.Name) || e.Name == "mpadded" || e.Name == "msqrt" || e.Name == "mtd":
		first, last, n := -1, -1, 0
		for i, k := range kids {
			if l.spaceLike(k, depth+1) {
				continue
			}
			if first < 0 {
				first = i
			}
			last = i
			n++
		}
		if n > 1 {
			forms[first], forms[last] = prefix, postfix
		}
	}
	for i, k := range kids {
		if through != nil && k == through {
			forms[i] = c.form
		}
	}
	return forms
}

// row lays the in-flow children of e out as an mrow (MathML Core,
// 3.3.1.2): stretchy operators stretched along the block axis over the
// other children (3.3.1.1), the children in a row on their baselines,
// spaced by the lspace and rspace of the embellished operators and
// their italic corrections.
//
// Under [Setter.BrowserRules], a row whose in-flow children all stretch
// lays them out without a stretch size constraint, where 3.3.1.1 gives
// them the constraint (0, 0): an mrow that is an embellished operator
// then stretches its operator over its own other children, as Chromium
// draws it. Chromium's screen and print share the layout, so they agree
// against the clause.
//
// # Corpus evidence
//
//   - wpt: padding-border-margin-003 stretches the operator of an mrow
//     holding an mspace 1500px tall and the operator alone, the mrow
//     alone in its math element; Chromium of gotenberg/gotenberg:9.4.1
//     draws the operator 1500px tall.
//   - probe, html: in the same Chromium, an operator after an mtext X in
//     an mrow alone in its math element grows to the X's ink, and one
//     after an mspace 200px tall to 200px; an mo alone keeps its glyph.
func (l *layouter) row(e *Element, st *Style, c lctx, depth int) *box {
	kids, index := inFlow(e)
	return l.rowOf(e, kids, index, st, c, depth)
}

// rowOf lays out kids, children of e at index in e's children, as row
// does.
func (l *layouter) rowOf(e *Element, kids []*Element, index []int, st *Style, c lctx, depth int) *box {
	var through *Element
	embellished := l.core(e, depth) != nil
	switch {
	case !embellished:
	case isGrouping(e.Name) || e.Name == "mpadded":
		through = l.embellishingChild(e, depth)
	case len(kids) > 0:
		// A scripted element or an mfrac laid out as an mrow is
		// embellished through its first child.
		through = kids[0]
	}
	boxes := make([]*box, len(kids))
	forms := l.childForms(e, kids, c, through, depth)
	if c.stretch {
		// The row is an embellished operator: its operator takes the
		// constraint, the rest none (3.3.1.1, first step).
		for i, k := range kids {
			kc := lctx{form: forms[i]}
			if k == through {
				kc = c
				kc.form = forms[i]
			}
			boxes[i] = l.child(e, k, index[i], st, kc, depth)
		}
	} else {
		var stretchers []int
		laid := false
		var uAsc, uDesc float64
		for i, k := range kids {
			if cr := l.core(k, depth+1); cr != nil {
				if op := l.operatorOf(cr, forms[i], nil); op.stretchy && !op.inline {
					stretchers = append(stretchers, i)
					continue
				}
			}
			b := l.child(e, k, index[i], st, lctx{form: forms[i]}, depth)
			boxes[i] = b
			if !laid {
				uAsc, uDesc, laid = b.inkAsc, b.inkDesc, true
			} else {
				uAsc, uDesc = max(uAsc, b.inkAsc), max(uDesc, b.inkDesc)
			}
		}
		for _, i := range stretchers {
			kc := lctx{form: forms[i], stretch: true, asc: uAsc, desc: uDesc}
			if !laid && l.s.BrowserRules {
				kc = lctx{form: forms[i]}
			}
			boxes[i] = l.child(e, kids[i], index[i], st, kc, depth)
		}
	}
	addSpace := e.Name == "math" || !embellished
	b := &box{}
	x, prevItalic := 0.0, 0.0
	for i, kb := range boxes {
		slanted := kb.op == nil && kb.italic != 0
		if !slanted {
			x += prevItalic
		}
		if kb.op != nil && addSpace {
			x += kb.op.lspace
		}
		b.add(kb, x, 0)
		x += kb.w
		prevItalic = 0
		if slanted {
			prevItalic = kb.italic
		}
		if kb.op != nil && addSpace {
			x += kb.op.rspace
		}
		if i == 0 {
			b.asc, b.desc, b.inkAsc, b.inkDesc = kb.asc, kb.desc, kb.inkAsc, kb.inkDesc
		} else {
			b.asc, b.desc = max(b.asc, kb.asc), max(b.desc, kb.desc)
			b.inkAsc, b.inkDesc = max(b.inkAsc, kb.inkAsc), max(b.inkDesc, kb.inkDesc)
		}
		if kids[i] == through {
			b.op = kb.op
		}
	}
	b.w = x + prevItalic
	b.italic = prevItalic
	return b
}
