package svg

import (
	"cmp"
	"slices"

	"golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// matcherState wraps the selector matcher the cascade shares.
type matcherState struct {
	m *css.Matcher
}

func newMatcherState() *matcherState {
	return &matcherState{m: css.NewMatcher("", maxMatchSteps)}
}

// ranked is a declaration with what orders it in the cascade.
type ranked struct {
	d declaration
	// level is 0 for a presentation attribute, 1 for a style rule, 2 for
	// the style attribute.
	level int
	spec  uint32
	order int
}

// declsOf returns the declarations that apply to the element n in
// ascending precedence (CSS Cascade 4, 6): presentation attributes first,
// with the specificity zero of SVG 1.1, 6.4, then style rules by
// specificity and source order, then the style attribute, then the
// important declarations of the rules and of the style attribute. Applying
// them in order lets the last valid one win. The caller holds d.mu.
func (d *Document) declsOf(n *html.Node) []declaration {
	if v, ok := d.decls[n]; ok {
		return v
	}
	var all []ranked
	for _, a := range n.Attr {
		if a.Namespace == "" && presentationAttrs[a.Key] {
			all = append(all, ranked{d: declaration{prop: a.Key, value: a.Val}})
		}
	}
	order := 0
	for i := range d.rules {
		r := &d.rules[i]
		spec, ok := d.matcher.m.Match(r.sel, n)
		if !ok {
			continue
		}
		for _, dc := range r.decls {
			all = append(all, ranked{d: dc, level: 1, spec: spec, order: order})
			order++
		}
	}
	if s, ok := attrOK(n, "style"); ok {
		for _, dc := range parseDeclarations(s) {
			all = append(all, ranked{d: dc, level: 2, order: order})
			order++
		}
	}
	slices.SortStableFunc(all, func(a, b ranked) int {
		if a.d.important != b.d.important {
			if a.d.important {
				return 1
			}
			return -1
		}
		if c := cmp.Compare(a.level, b.level); c != 0 {
			return c
		}
		if c := cmp.Compare(a.spec, b.spec); c != 0 {
			return c
		}
		return cmp.Compare(a.order, b.order)
	})
	out := make([]declaration, len(all))
	for i, r := range all {
		out[i] = r.d
	}
	d.decls[n] = out
	return out
}

// computeStyle returns the computed style of the element n whose parent in
// the rendering tree has the computed style parent: for an element a use
// instantiates, the use element (SVG 1.1, 5.6).
func (d *Document) computeStyle(n *html.Node, parent *style) *style {
	d.mu.Lock()
	decls := d.declsOf(n)
	d.mu.Unlock()
	st := parent.inherit()
	for _, dc := range decls {
		st.apply(dc, parent)
	}
	return st
}

// styleOf returns the computed style of n in the document tree, its
// ancestors' styles inherited: what a gradient stop or a clipPath child
// is styled with, since they are not drawn where they sit (SVG 1.1, 13.2.4
// and 14.3.5).
func (d *Document) styleOf(n *html.Node, depth int) *style {
	d.mu.Lock()
	if st, ok := d.domStyle[n]; ok {
		d.mu.Unlock()
		return st
	}
	d.mu.Unlock()
	var parent *style
	p := n.Parent
	if n == d.root || p == nil || p.Type != html.ElementNode || p.Namespace != "svg" || depth > maxDepth {
		parent = initialStyle(d.opts.Context)
	} else {
		parent = d.styleOf(p, depth+1)
	}
	st := d.computeStyle(n, parent)
	d.mu.Lock()
	d.domStyle[n] = st
	d.mu.Unlock()
	return st
}
