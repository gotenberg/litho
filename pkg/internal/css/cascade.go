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
	// elems indexes the selectors that target elements, pseudos those
	// that target each generated pseudo-element.
	elems   selIndex
	pseudos [numPseudos]selIndex
	pages   []resolvedPage
	faces   []FontFace
	warns   []Warning
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

// selIndex holds selectors by the key of their rightmost compound, the
// way browsers bucket them, so an element is only tested against
// selectors that can match it.
type selIndex struct {
	byID, byClass, byTag map[string][]selRef
	universal            []selRef
}

// PseudoElement names a pseudo-element whose style [Computed.Pseudo]
// returns (CSS Pseudo 4).
type PseudoElement uint8

// The pseudo-elements litho generates boxes for.
const (
	// PseudoBefore is ::before, the first child of its element (CSS
	// Pseudo 4, 4.1).
	PseudoBefore PseudoElement = iota
	// PseudoAfter is ::after, its last child.
	PseudoAfter
	// PseudoMarker is ::marker, a list item's marker (CSS Lists 3, 3.1).
	PseudoMarker
	// PseudoFootnoteCall is ::footnote-call, the call a footnote element
	// leaves in the flow, and PseudoFootnoteMarker ::footnote-marker, the
	// marker its body starts with (CSS GCPM 3, 3.6 and 3.7).
	PseudoFootnoteCall
	PseudoFootnoteMarker
	numPseudos
)

// pseudoNames maps the selector name of each generated pseudo-element to
// it.
var pseudoNames = map[string]PseudoElement{
	"before": PseudoBefore, "after": PseudoAfter, "marker": PseudoMarker,
	"footnote-call": PseudoFootnoteCall, "footnote-marker": PseudoFootnoteMarker,
}

// NewResolver indexes the rules of sheets, in order, whose media
// conditions match media. A nil Sheet is skipped.
func NewResolver(media Media, sheets []Sheet) *Resolver {
	r := &Resolver{media: media}
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
				ref := selRef{rule: idx, sel: s, anc: ancestorFeatures(s)}
				if s.pseudoElement == "" {
					r.elems.add(ref)
				} else if pe, ok := pseudoNames[s.pseudoElement]; ok {
					r.pseudos[pe].add(ref)
				}
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

func (x *selIndex) add(ref selRef) {
	last := &ref.sel.parts[len(ref.sel.parts)-1]
	put := func(m *map[string][]selRef, k string) {
		if *m == nil {
			*m = map[string][]selRef{}
		}
		(*m)[k] = append((*m)[k], ref)
	}
	switch {
	case last.never:
	case len(last.ids) > 0:
		put(&x.byID, last.ids[0])
	case len(last.classes) > 0:
		put(&x.byClass, last.classes[0])
	case last.tag != "":
		put(&x.byTag, last.tag)
	default:
		x.universal = append(x.universal, ref)
	}
}

// empty reports an index without selectors.
func (x *selIndex) empty() bool {
	return len(x.byID) == 0 && len(x.byClass) == 0 && len(x.byTag) == 0 && len(x.universal) == 0
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
	// FontUnits returns the x-height and the advance of "0" of the first
	// font a style's families, weight and style select, as fractions of the
	// font size, for the ex and ch units (CSS Values 4, 6.1.1). nil, or a
	// result that is not positive, takes half an em for either.
	FontUnits func(st *Style) (ex, ch float64)
}

// Computed holds the computed style of every element of a tree. It is
// immutable and safe for concurrent use.
type Computed struct {
	styles map[*html.Node]*Style
	pseudo map[pseudoKey]*Style
	warns  []Warning
}

// pseudoKey names one pseudo-element of one element.
type pseudoKey struct {
	n     *html.Node
	which PseudoElement
}

// Style returns the computed style of the element n, nil for a node that
// is not an element of the computed tree. The Style is shared: do not
// modify it.
func (c *Computed) Style(n *html.Node) *Style { return c.styles[n] }

// Pseudo returns the computed style of the pseudo-element which of the
// element n (CSS Pseudo 4, 4), inheriting from n's style, or nil when no
// rule targets it: a ::before or ::after without a rule has content none
// and generates no box, a ::marker without one takes the item's style. A
// pseudo-element's content is normal unless a rule sets it. The Style is
// shared: do not modify it.
func (c *Computed) Pseudo(n *html.Node, which PseudoElement) *Style {
	return c.pseudo[pseudoKey{n, which}]
}

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
	r      *Resolver
	ctx    context.Context
	opts   ComputeOptions
	m      *matcher
	anc    *ancestorFilter
	warns  warnings
	out    map[*html.Node]*Style
	pseudo map[pseudoKey]*Style
	vars   map[*html.Node]map[string][]token
	stamp  []int
	spec   []uint32
	elem   int
	root   *Style
	nodes  int
	// units caches FontUnits by font selection.
	units map[fontKey][2]float64
}

// fontKey is what selects a font for the ex and ch units.
type fontKey struct {
	families string
	weight   int
	style    FontStyle
}

// fontUnits returns the ex and ch ratios of st, 0 for the fallback.
func (cs *computeState) fontUnits(st *Style) (float64, float64) {
	if cs.opts.FontUnits == nil {
		return 0, 0
	}
	k := fontKey{families: strings.Join(st.FontFamilies, "\x00"), weight: st.FontWeight, style: st.FontStyle}
	if u, ok := cs.units[k]; ok {
		return u[0], u[1]
	}
	ex, ch := cs.opts.FontUnits(st)
	if !finite(ex) || !finite(ch) {
		ex, ch = 0, 0
	}
	if cs.units == nil {
		cs.units = map[fontKey][2]float64{}
	}
	cs.units[k] = [2]float64{ex, ch}
	return ex, ch
}

// normalLineHeight is the factor of the font size the lh and rlh units
// take for line-height: normal, whose used value depends on the face the
// layout picks: 1.2, the value CSS 2.1, 10.8.2 suggests.
const normalLineHeight = 1.2

// usedLineHeight returns the line height of st in points, for the lh and
// rlh units (CSS Values 4, 6.1.1).
func usedLineHeight(st *Style) float64 {
	switch st.LineHeight.Kind { //nolint:exhaustive // the other kinds are normal
	case Points:
		return st.LineHeight.Value
	case Number:
		return st.LineHeight.Value * st.FontSize
	}
	return normalLineHeight * st.FontSize
}

// computeOrder is the order computeStyle computes the longhands in: the
// font size and colour first, as [propID] says, then the font selection,
// which the ex and ch units of every other length resolve against, then
// the rest in propID order.
var computeOrder = func() []propID {
	first := []propID{pFontSize, pColor, pFontFamily, pFontWeight, pFontStyle, pLineHeight}
	out := slices.Clone(first)
	for id := propID(0); id < numProps; id++ {
		if !slices.Contains(first, id) {
			out = append(out, id)
		}
	}
	return out
}()

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
		out: map[*html.Node]*Style{}, pseudo: map[pseudoKey]*Style{}, vars: map[*html.Node]map[string][]token{}, anc: &ancestorFilter{},
		stamp: make([]int, len(r.rules)), spec: make([]uint32, len(r.rules)),
	}
	for _, w := range r.warns {
		cs.warns.add(w)
	}
	if root == nil {
		return &Computed{styles: cs.out, pseudo: cs.pseudo, warns: cs.warns.result()}, nil
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
	return &Computed{styles: cs.out, pseudo: cs.pseudo, warns: cs.warns.result()}, nil
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
	cands := cs.match(n, &cs.r.elems)
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
	st, vars := cs.cascade(cands, parentStyle, parentVars, parent == nil)
	cs.out[n] = st
	cs.vars[n] = vars
	if parent == nil {
		cs.root = st
	}
	for pe := range numPseudos {
		x := &cs.r.pseudos[pe]
		if x.empty() || st.Display == DisplayNone {
			continue
		}
		cs.elem++
		pc := cs.match(n, x)
		if len(pc) == 0 {
			continue
		}
		ps, _ := cs.cascade(pc, st, vars, false)
		cs.pseudo[pseudoKey{n, pe}] = ps
	}
}

// cascade sorts the declarations competing for one element or
// pseudo-element and computes its style from them, and its custom
// properties.
func (cs *computeState) cascade(cands []cand, parentStyle *Style, parentVars map[string][]token, isRoot bool) (*Style, map[string][]token) {
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
	// The flow-relative properties take their sides from the direction
	// (CSS Logical Properties 1, 4).
	rtl := cascadedRTL(cands, parentStyle)
	side := func(d *Declaration, id propID) propID {
		if rtl && isInlineLogical(d.Property) {
			return mirrorInline(id)
		}
		return id
	}
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
				winners[side(c.d, id)] = pendingRef{c.d}
			}
		default:
			for _, e := range c.d.entries {
				winners[side(c.d, e.id)] = e.v
			}
		}
	}
	vars := parentVars
	if len(customNames) > 0 {
		vars = cs.resolveCustom(customNames, custom, parentVars)
	}
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
			if int(side(p.d, e.id)) == id {
				winners[id] = e.v
			}
		}
	}
	return cs.computeStyle(&winners, parentStyle, isRoot), vars
}

// match returns the declarations of the rules matching n.
func (cs *computeState) match(n *html.Node, r *selIndex) []cand {
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
		rr := &cs.r.rules[ri]
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
func (cs *computeState) computeStyle(winners *[numProps]any, parent *Style, isRoot bool) *Style {
	st := new(Style)
	*st = *parent
	s := &styler{parent: parent, root: cs.root}
	rootFS := mediumFontSize
	if cs.root != nil {
		rootFS = cs.root.FontSize
	}
	vw, vh := cs.r.media.viewport()
	s.lc = lenCtx{fontSize: parent.FontSize, rootFontSize: rootFS, width: vw, height: vh}
	s.lc.ex, s.lc.ch = cs.fontUnits(parent)
	s.lc.lineHeight = usedLineHeight(parent)
	s.lc.rootLineHeight = s.lc.lineHeight
	if cs.root != nil {
		s.lc.rootLineHeight = usedLineHeight(cs.root)
	}
	for _, id := range computeOrder {
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
		switch id { //nolint:exhaustive // only the font longhands change the length context
		case pFontSize:
			s.lc.fontSize = st.FontSize
			if isRoot {
				s.lc.rootFontSize = st.FontSize
			}
		case pFontFamily:
			// The family can resize the font (fixMonospaceSize), before
			// any other length reads it.
			fixMonospaceSize(st, parent)
			s.lc.fontSize = st.FontSize
			if isRoot {
				s.lc.rootFontSize = st.FontSize
			}
		case pFontStyle:
			s.lc.ex, s.lc.ch = cs.fontUnits(st)
		case pLineHeight:
			s.lc.lineHeight = usedLineHeight(st)
			if isRoot {
				s.lc.rootLineHeight = s.lc.lineHeight
			}
		}
	}
	fixupDisplay(st, isRoot)
	if !isRoot && (isFlexContainer(parent.Display) || isGridContainer(parent.Display)) {
		// The children of a flex or grid container are its items,
		// blockified (CSS Flexbox 1, 4; CSS Grid 1, 6; CSS Display 3,
		// 2.7).
		st.Display, _ = blockify(st.Display)
	}
	return st
}

// fixupDisplay applies CSS 2.1, 9.7: an absolutely positioned box does
// not float, and a floated, absolutely positioned or root box is
// blockified.
func fixupDisplay(st *Style, isRoot bool) {
	st.OuterInline = false
	if st.Display == DisplayNone {
		return
	}
	if st.Position == Absolute || st.Position == Fixed {
		st.Float = FloatNone
	} else if st.Float == FloatNone && !isRoot {
		return
	}
	st.Display, st.OuterInline = blockify(st.Display)
}
