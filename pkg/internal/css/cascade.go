package css

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// Origin is a cascade origin (CSS Cascade 4, 6.2). There is no user
// origin.
type Origin uint8

// The origins, in increasing precedence for normal declarations.
const (
	UserAgent Origin = iota
	Author
)

// Sheet is a stylesheet and the origin it cascades in.
type Sheet struct {
	Sheet  *Stylesheet
	Origin Origin
}

// rank orders declarations by origin and importance (CSS Cascade 4, 6.2):
// important declarations reverse the order of the origins.
func rank(o Origin, important bool) int {
	switch {
	case !important && o == UserAgent:
		return 0
	case !important:
		return 1
	case o == Author:
		return 2
	}
	return 3
}

// Resolver cascades a fixed set of stylesheets for one medium. It is
// immutable once [NewResolver] returns and safe for concurrent use.
type Resolver struct {
	media Media
	rules []resolvedRule
	// The rules' selectors are indexed by the key of their rightmost
	// compound, the way browsers bucket them, so an element is only
	// tested against selectors that can match it.
	byID, byClass, byTag map[string][]selRef
	universal            []selRef
	pages                []resolvedPage
	faces                []FontFace
	warns                []Warning
}

type resolvedRule struct {
	rule   *styleRule
	origin Origin
	order  int
}

type resolvedPage struct {
	rule   *pageRule
	origin Origin
	order  int
}

// selRef is one selector of a rule in the index, with the ancestor
// features it needs.
type selRef struct {
	rule int
	sel  *complexSel
	anc  []uint64
}

// NewResolver indexes the rules of sheets, in order, whose media
// conditions match media. A nil Sheet is skipped.
func NewResolver(media Media, sheets []Sheet) *Resolver {
	r := &Resolver{media: media, byID: map[string][]selRef{}, byClass: map[string][]selRef{}, byTag: map[string][]selRef{}}
	cache := map[*mediaCond]bool{}
	var w warnings
	order := 0
	for _, sh := range sheets {
		if sh.Sheet == nil {
			continue
		}
		for _, x := range sh.Sheet.warns {
			w.add(x)
		}
		for i := range sh.Sheet.rules {
			sr := &sh.Sheet.rules[i]
			order++
			if !sr.cond.matches(media, cache) {
				continue
			}
			idx := len(r.rules)
			r.rules = append(r.rules, resolvedRule{rule: sr, origin: sh.Origin, order: order})
			for _, s := range sr.selectors {
				if s.pseudoElement != "" {
					continue
				}
				r.index(selRef{rule: idx, sel: s, anc: ancestorFeatures(s)})
			}
		}
		for i := range sh.Sheet.pages {
			pr := &sh.Sheet.pages[i]
			order++
			if pr.cond.matches(media, cache) {
				r.pages = append(r.pages, resolvedPage{rule: pr, origin: sh.Origin, order: order})
			}
		}
		for i := range sh.Sheet.faces {
			if f := &sh.Sheet.faces[i]; f.cond.matches(media, cache) {
				r.faces = append(r.faces, f.face)
			}
		}
	}
	r.warns = w.result()
	return r
}

func (r *Resolver) index(ref selRef) {
	last := &ref.sel.parts[len(ref.sel.parts)-1]
	switch {
	case last.never:
	case len(last.ids) > 0:
		r.byID[last.ids[0]] = append(r.byID[last.ids[0]], ref)
	case len(last.classes) > 0:
		r.byClass[last.classes[0]] = append(r.byClass[last.classes[0]], ref)
	case last.tag != "":
		r.byTag[last.tag] = append(r.byTag[last.tag], ref)
	default:
		r.universal = append(r.universal, ref)
	}
}

// FontFaces returns the @font-face rules whose media conditions match, in
// order. The slice is a copy; the faces' slices are shared and must not be
// modified.
func (r *Resolver) FontFaces() []FontFace { return append([]FontFace(nil), r.faces...) }

// ComputeOptions configures [Resolver.Compute].
type ComputeOptions struct {
	// Hints returns the presentational hints of an element (CSS 2.1,
	// 6.4.4), [HTMLHints] for HTML. nil means none.
	Hints func(*html.Node) []Declaration
	// Lang is the document's language for :lang(), when no ancestor
	// carries a lang attribute; "" for unknown.
	Lang string
	// BaseURL resolves the url() values of style attributes: the
	// document's URL. Empty keeps them as written.
	BaseURL string
}

// Computed holds the computed style of every element of a tree. It is
// immutable and safe for concurrent use.
type Computed struct {
	styles map[*html.Node]*Style
	warns  []Warning
}

// Style returns the computed style of the element n, nil for a node that
// is not an element of the computed tree. The Style is shared: do not
// modify it.
func (c *Computed) Style(n *html.Node) *Style { return c.styles[n] }

// Warnings returns what the cascade and the stylesheets dropped or
// approximated, deduplicated and capped, in a deterministic order. The
// slice is a copy.
func (c *Computed) Warnings() []Warning { return append([]Warning(nil), c.warns...) }

// initialStyle is the shared computed initial style.
var initialStyle = Initial()

// styleAttrSpec is the specificity of a style attribute's declarations,
// above any selector's (CSS Style Attributes, 3 and CSS Cascade 4, 6.4).
const styleAttrSpec = 1 << 31

// cand is one declaration competing in an element's cascade.
type cand struct {
	d    *Declaration
	rank int
	spec uint64
	ord  int
	idx  int
}

// pendingRef marks a longhand whose winning declaration holds var().
type pendingRef struct{ d *Declaration }

// computeState is the state of one Compute call.
type computeState struct {
	r     *Resolver
	ctx   context.Context
	opts  ComputeOptions
	m     *matcher
	anc   *ancestorFilter
	warns warnings
	out   map[*html.Node]*Style
	vars  map[*html.Node]map[string][]token
	stamp []int
	spec  []uint32
	elem  int
	root  *Style
	nodes int
}

// Compute computes the style of root and of every element under it (CSS
// Cascade 4 and CSS 2.1, 6). root is a document node or an element; an
// element root inherits from the initial style. The result depends only on
// the resolver, the tree and opts.
//
// Compute returns [ErrTooComplex] when the cascade needs more than
// maxComputeSteps steps, and ctx's error when it is done.
func (r *Resolver) Compute(ctx context.Context, root *html.Node, opts ComputeOptions) (*Computed, error) {
	cs := &computeState{
		r: r, ctx: ctx, opts: opts, m: newMatcher(opts.Lang, maxComputeSteps),
		out: map[*html.Node]*Style{}, vars: map[*html.Node]map[string][]token{}, anc: &ancestorFilter{},
		stamp: make([]int, len(r.rules)), spec: make([]uint32, len(r.rules)),
	}
	for _, w := range r.warns {
		cs.warns.add(w)
	}
	if root == nil {
		return &Computed{styles: cs.out, warns: cs.warns.result()}, nil
	}
	for a := root.Parent; isElement(a); a = a.Parent {
		cs.anc.push(a)
	}
	type frame struct {
		n      *html.Node
		parent *html.Node
		exit   bool
	}
	stack := []frame{{n: root}}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.n
		parent := f.parent
		if f.exit {
			cs.anc.pop(n)
			continue
		}
		if n.Type == html.ElementNode {
			cs.nodes++
			if cs.nodes%100 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			cs.element(n, parent)
			if cs.m.over {
				return nil, fmt.Errorf("computing styles past %d steps: %w", maxComputeSteps, ErrTooComplex)
			}
			parent = n
			cs.anc.push(n)
			stack = append(stack, frame{n: n, exit: true})
		}
		var kids []*html.Node
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode || c.Type == html.DocumentNode {
				kids = append(kids, c)
			}
		}
		for i := len(kids) - 1; i >= 0; i-- {
			stack = append(stack, frame{n: kids[i], parent: parent})
		}
	}
	return &Computed{styles: cs.out, warns: cs.warns.result()}, nil
}

// element computes the style of n, whose parent element (nil for the
// top of the tree) is already computed.
func (cs *computeState) element(n, parent *html.Node) {
	cs.elem++
	parentStyle := initialStyle
	var parentVars map[string][]token
	if parent != nil {
		parentStyle = cs.out[parent]
		parentVars = cs.vars[parent]
	}
	cands := cs.match(n)
	if cs.opts.Hints != nil {
		hints := cs.opts.Hints(n)
		for i := range hints {
			cands = append(cands, cand{d: &hints[i], rank: rank(Author, false), ord: -1, idx: i})
		}
	}
	if v, ok := attr(n, "style"); ok && (n.Namespace == "" || n.Namespace == "svg") {
		decls := ParseDeclarations(v, ParseOptions{BaseURL: cs.opts.BaseURL})
		for i := range decls {
			cands = append(cands, cand{d: &decls[i], rank: rank(Author, decls[i].Important), spec: styleAttrSpec, ord: len(cs.r.rules) + 1, idx: i})
		}
	}
	slices.SortStableFunc(cands, func(a, b cand) int {
		if c := cmp.Compare(a.rank, b.rank); c != 0 {
			return c
		}
		if c := cmp.Compare(a.spec, b.spec); c != 0 {
			return c
		}
		if c := cmp.Compare(a.ord, b.ord); c != 0 {
			return c
		}
		return cmp.Compare(a.idx, b.idx)
	})
	var winners [numProps]any
	var customNames []string
	custom := map[string][]token{}
	for _, c := range cands {
		for _, w := range c.d.warns {
			cs.warns.add(w)
		}
		switch {
		case c.d.isCustom:
			if _, seen := custom[c.d.Property]; !seen {
				customNames = append(customNames, c.d.Property)
			}
			custom[c.d.Property] = c.d.custom
		case len(c.d.pending) > 0:
			for _, id := range longhandsOf(c.d.Property) {
				winners[id] = pendingRef{c.d}
			}
		default:
			for _, e := range c.d.entries {
				winners[e.id] = e.v
			}
		}
	}
	vars := parentVars
	if len(customNames) > 0 {
		vars = cs.resolveCustom(customNames, custom, parentVars)
	}
	cs.vars[n] = vars
	expanded := map[*Declaration][]entry{}
	for id := range winners {
		p, ok := winners[id].(pendingRef)
		if !ok {
			continue
		}
		es, done := expanded[p.d]
		if !done {
			es = cs.substituteDecl(p.d, vars)
			expanded[p.d] = es
		}
		winners[id] = wideUnset
		for _, e := range es {
			if int(e.id) == id {
				winners[id] = e.v
			}
		}
	}
	st := cs.computeStyle(&winners, parentStyle, n, parent == nil)
	cs.out[n] = st
	if parent == nil {
		cs.root = st
	}
}

// match returns the declarations of the rules matching n.
func (cs *computeState) match(n *html.Node) []cand {
	var matched []int
	test := func(refs []selRef) {
		for _, ref := range refs {
			if cs.m.over {
				return
			}
			if !cs.anc.admits(ref.anc) || !cs.m.matches(ref.sel, n) {
				continue
			}
			if cs.stamp[ref.rule] != cs.elem {
				cs.stamp[ref.rule] = cs.elem
				cs.spec[ref.rule] = ref.sel.spec
				matched = append(matched, ref.rule)
			} else {
				cs.spec[ref.rule] = max(cs.spec[ref.rule], ref.sel.spec)
			}
		}
	}
	r := cs.r
	if id, ok := attr(n, "id"); ok {
		test(r.byID[id])
	}
	if cl, ok := attr(n, "class"); ok {
		seen := map[string]bool{}
		for _, c := range strings.Fields(cl) {
			if !seen[c] {
				seen[c] = true
				test(r.byClass[c])
			}
		}
	}
	test(r.byTag[lower(n.Data)])
	test(r.universal)
	var out []cand
	for _, ri := range matched {
		rr := &r.rules[ri]
		for i := range rr.rule.decls {
			d := &rr.rule.decls[i]
			out = append(out, cand{d: d, rank: rank(rr.origin, d.Important), spec: uint64(cs.spec[ri]), ord: rr.order, idx: i})
		}
	}
	return out
}

// computeStyle computes every longhand from its cascaded value (CSS
// Cascade 4, 4 and CSS 2.1, 6.1 to 6.2): inherited longhands without a
// value take the parent's, the others their initial value.
func (cs *computeState) computeStyle(winners *[numProps]any, parent *Style, n *html.Node, isRoot bool) *Style {
	st := new(Style)
	*st = *parent
	s := &styler{parent: parent, root: cs.root}
	rootFS := mediumFontSize
	if cs.root != nil {
		rootFS = cs.root.FontSize
	}
	s.lc = lenCtx{fontSize: parent.FontSize, rootFontSize: rootFS, width: cs.r.media.Width, height: cs.r.media.Height}
	for id := propID(0); id < numProps; id++ {
		def := &props[id]
		v := winners[id]
		if w, ok := v.(cssWide); ok {
			switch {
			case w == wideInherit, w == wideUnset && def.inherited:
				def.copyFrom(st, parent)
				v = nil
			default:
				v = def.initial
				if v == nil {
					def.copyFrom(st, initialStyle)
				}
			}
		} else if v == nil {
			if def.inherited {
				def.copyFrom(st, parent)
			} else {
				v = def.initial
				if v == nil {
					def.copyFrom(st, initialStyle)
				}
			}
		}
		if v != nil {
			def.compute(s, st, v)
		}
		if id == pFontSize {
			s.lc.fontSize = st.FontSize
			if isRoot {
				s.lc.rootFontSize = st.FontSize
			}
		}
	}
	fixupDisplay(st, isRoot)
	return st
}

// fixupDisplay applies CSS 2.1, 9.7: an absolutely positioned box does
// not float, and a floated, absolutely positioned or root box is
// blockified.
func fixupDisplay(st *Style, isRoot bool) {
	if st.Display == DisplayNone {
		return
	}
	if st.Position == Absolute || st.Position == Fixed {
		st.Float = FloatNone
	} else if st.Float == FloatNone && !isRoot {
		return
	}
	switch st.Display { //nolint:exhaustive // the other display values are already block-level
	case InlineTable:
		st.Display = Table
	case Inline, InlineBlock, TableRowGroup, TableColumn, TableColumnGroup,
		TableHeaderGroup, TableFooterGroup, TableRow, TableCell, TableCaption:
		st.Display = Block
	}
}
