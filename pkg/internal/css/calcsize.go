package css

// This file parses calc-size() (CSS Sizing 4, 7): a calculation over the
// used size of a basis. A basis that is a length or a percentage, or any,
// folds into the calculation, which then computes as calc() does (7.2.1).
// A keyword basis keeps its keyword as the [Dimension]'s kind, so that
// the box behaves as that keyword for every purpose but sizing, and the
// calculation, its size keyword standing for the basis's used size, in
// the dimension: [Dimension.OfSize] applies it once the layout knows that
// size (7.1).
//
// Chromium parses calc-size() on width, height, their minimums and
// maximums, their logical forms and flex-basis, a keyword basis being one
// the property itself takes, none excepted, and refuses the one-argument
// form; litho parses what Chromium parses.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) CSS.supports
//     accepts calc-size() on width, height, min-width, min-height,
//     inline-size, block-size and flex-basis with the bases auto,
//     fit-content, min-content, max-content, stretch, any, a length, a
//     percentage and a nested calc-size(); the -webkit- spellings of the
//     sizing keywords on width, content on flex-basis and on no other
//     property; refuses calc-size(auto) and a none basis, an auto basis on
//     max-width and max-height, a -moz- spelling, fit-content(10px), and
//     calc-size() on margin, padding, left, grid-template-columns,
//     font-size and line-height.

// maxCalcSizeNodes caps the nodes of a calc-size() calculation once each
// size keyword is replaced by its basis: 1024, as [maxCalcNodes]. Nested
// bases multiply the nodes a size keyword stands for. Anti-DoS guard, not
// spec-mandated: CSS Sizing 4, 7.2.1 lets the substitution fail past a
// limit the user agent defines.
const maxCalcSizeNodes = maxCalcNodes

// isCalcSize reports a calc-size() function.
func isCalcSize(t *token) bool {
	return t.typ == tFunction && lower(t.value) == "calc-size"
}

// parseCalcSize parses the calc-size() t of a size property whose
// percentages resolve against a length and whose keyword bases are bases:
// a dimSpec of kind Points holding the folded calculation for a length,
// percentage or any basis, or of the basis keyword's kind holding the
// calculation in size.
func parseCalcSize(t *token, bases map[string]DimKind) (dimSpec, bool) {
	kind, n, ok := calcSizeTree(t, bases, 0)
	if !ok || n.nodeCount(maxCalcSizeNodes+1) > maxCalcSizeNodes {
		return dimSpec{}, false
	}
	if kind != Points {
		return dimSpec{kind: kind, size: n}, true
	}
	return dimSpec{kind: Points, l: treeLength(n, false)}, true
}

// calcSizeTree parses the calc-size() t at nesting depth: the kind of its
// basis, one of bases or Points for one that folds into the calculation,
// and the calculation, its size keywords replaced by a folded basis.
func calcSizeTree(t *token, bases map[string]DimKind, depth int) (DimKind, *calcNode, bool) {
	if depth > maxCalcDepth {
		return 0, nil, false
	}
	parts := splitCommas(t.children)
	if len(parts) != 2 {
		return 0, nil, false
	}
	basis := significant(parts[0])
	if len(basis) == 0 {
		return 0, nil, false
	}
	kind, inner, folded := Points, (*calcNode)(nil), false
	switch b := basis[0]; {
	case len(basis) == 1 && b.typ == tIdent && lower(b.value) == "any":
		// The any basis is a definite size the calculation does not
		// read: it takes no size keyword (CSS Sizing 4, 7).
		p := &calcParser{pctLength: true}
		n, ty, ok := p.sum(parts[1], 1)
		return Points, n, ok && ty == typeLength
	case len(basis) == 1 && b.typ == tIdent:
		k, ok := bases[lower(b.value)]
		if !ok || k == None {
			return 0, nil, false
		}
		kind = k
	case len(basis) == 1 && isCalcSize(&b):
		// A nested calc-size() is its basis with its calculation as the
		// size (CSS Sizing 4, 7.2.1).
		k, n, ok := calcSizeTree(&b, bases, depth+1)
		if !ok {
			return 0, nil, false
		}
		kind, inner, folded = k, n, k == Points
	default:
		p := &calcParser{pctLength: true}
		n, ty, ok := p.sum(parts[0], 1)
		if !ok || ty != typeLength {
			return 0, nil, false
		}
		inner, folded = n, true
	}
	p := &calcParser{pctLength: true, size: true}
	n, ty, ok := p.sum(parts[1], 1)
	if !ok || ty != typeLength {
		return 0, nil, false
	}
	if inner != nil {
		n = n.withSize(inner)
	}
	if folded {
		kind = Points
	}
	return kind, n, true
}

// withSize returns a copy of the tree whose size keywords are basis. The
// copy shares basis and every subtree without a size keyword.
func (n *calcNode) withSize(basis *calcNode) *calcNode {
	if n.op == opSize {
		return basis
	}
	if !n.hasSize() {
		return n
	}
	c := *n
	c.kids = make([]*calcNode, len(n.kids))
	for i, k := range n.kids {
		c.kids[i] = k.withSize(basis)
	}
	return &c
}

// hasSize reports whether the tree holds a size keyword.
func (n *calcNode) hasSize() bool {
	if n.op == opSize {
		return true
	}
	for _, k := range n.kids {
		if k.hasSize() {
			return true
		}
	}
	return false
}

// nodeCount returns the nodes of the tree, a shared subtree counted at
// each place, stopping once the count reaches limit.
func (n *calcNode) nodeCount(limit int) int {
	c := 1
	for _, k := range n.kids {
		if c >= limit {
			break
		}
		c += k.nodeCount(limit - c)
	}
	return c
}

// SizeCalc reports a dimension that holds the calculation of a
// calc-size() over a keyword basis: its kind is the basis, and
// [Dimension.OfSize] applies the calculation to the basis's used size.
func (d Dimension) SizeCalc() bool {
	return d.expr != nil && d.Kind != Calc && d.Kind != FitContentOf
}

// OfSize returns the used size of d whose basis takes the used size
// size: the calculation of a calc-size() with size standing for it and
// its percentages resolved against ref, 0 for an indefinite one (CSS
// Sizing 4, 7.1), clamped to the largest length litho lays out; size
// itself for any other dimension.
func (d Dimension) OfSize(size, ref float64) float64 {
	if !d.SizeCalc() {
		return size
	}
	return clampLen(censor(d.expr.eval(&calcScope{basis: clampLen(ref), size: clampLen(size)})))
}
