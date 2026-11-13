package css

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// Origin is a cascade origin (CSS Cascade 4, 6.2).
type Origin uint8

// The origins, in increasing precedence for normal declarations.
const (
	// UserAgent is the engine's default stylesheet.
	UserAgent Origin = iota
	// User is the stylesheets the caller gives for any document.
	User
	// Author is the document's own stylesheets, style attributes and
	// presentational hints.
	Author
)

// Sheet is a stylesheet and the origin it cascades in.
type Sheet struct {
	Sheet  *Stylesheet
	Origin Origin
}

// rank orders declarations by origin and importance (CSS Cascade 4, 6.2):
// normal user agent, normal user, normal author, important author,
// important user, important user agent. Important declarations reverse
// the order of the origins.
func rank(o Origin, important bool) int {
	if !important {
		return int(o)
	}
	return 2*int(Author) + 1 - int(o)
}

// originOf returns the origin of a declaration of rank r, the inverse of
// [rank] over the origin.
func originOf(r int) Origin {
	switch r {
	case rank(Author, false), rank(Author, true):
		return Author
	case rank(User, false), rank(User, true):
		return User
	}
	return UserAgent
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
	// featureValues holds the merged @font-feature-values rules whose
	// media conditions match, by lowercased family name.
	featureValues map[string]*FontFeatureValues
	warns         []Warning
	// counters holds the counter styles, the predefined ones and those
	// of the @counter-style rules whose media conditions match.
	counters *CounterStyles
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
	// PseudoNoteCall is ::note-call, the call a note element leaves in the
	// flow, PseudoNoteMarker ::note-marker, the marker its body starts
	// with, and PseudoNoteCallback ::note-callback, what its body ends
	// with: the pseudo-elements of position: note() (see [Style.Note]).
	PseudoNoteCall
	PseudoNoteMarker
	PseudoNoteCallback
	// PseudoFirstLetter is ::first-letter, the first typographic letter
	// unit of the first formatted line of a block container (CSS Pseudo 4,
	// 2.3). The converter cuts it from the text that holds it.
	PseudoFirstLetter
	// PseudoFirstLine is ::first-line, the contents of the first formatted
	// line of a block container (CSS Pseudo 4, 2.1). Its style holds the
	// properties that apply to it alone ([Computed.FirstLine]).
	PseudoFirstLine
	numPseudos
)

// pseudoNames maps the selector name of each generated pseudo-element to
// it.
var pseudoNames = map[string]PseudoElement{
	"before": PseudoBefore, "after": PseudoAfter, "marker": PseudoMarker,
	"footnote-call": PseudoFootnoteCall, "footnote-marker": PseudoFootnoteMarker,
	"note-call": PseudoNoteCall, "note-marker": PseudoNoteMarker, "note-callback": PseudoNoteCallback,
	"first-letter": PseudoFirstLetter, "first-line": PseudoFirstLine,
}

// NewResolver indexes the rules of sheets, in order, whose media
// conditions match media. A nil Sheet is skipped.
func NewResolver(media Media, sheets []Sheet) *Resolver {
	r := &Resolver{media: media}
	cache := map[*mediaCond]bool{}
	var w warnings
	order := 0
	var counterRules []*counterStyleRule
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
		for _, cr := range sh.Sheet.counterStyles {
			if cr.cond.matches(media, cache) {
				counterRules = append(counterRules, cr)
			}
		}
		for i := range sh.Sheet.faces {
			if f := &sh.Sheet.faces[i]; f.cond.matches(media, cache) {
				r.faces = append(r.faces, f.face)
			}
		}
		for i := range sh.Sheet.featureValues {
			if f := &sh.Sheet.featureValues[i]; f.cond.matches(media, cache) {
				r.addFeatureValues(f.values)
			}
		}
	}
	r.warns = w.result()
	r.counters = newCounterStyles(counterRules)
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
		// A selector that never matches needs no bucket.
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

// CounterStyles returns the counter styles of the sheets: the predefined
// ones and those the @counter-style rules whose media conditions match
// define, the last rule of a name winning.
func (r *Resolver) CounterStyles() *CounterStyles { return r.counters }

// addFeatureValues merges the values of one @font-feature-values rule into
// those of its family: a name defined again takes its later values (CSS
// Fonts 4, 6.9.1).
func (r *Resolver) addFeatureValues(v FontFeatureValues) {
	if r.featureValues == nil {
		r.featureValues = map[string]*FontFeatureValues{}
	}
	key := strings.ToLower(v.Family)
	dst := r.featureValues[key]
	if dst == nil {
		dst = &FontFeatureValues{Family: v.Family, Values: map[string]map[string][]int{}}
		r.featureValues[key] = dst
	}
	for kind, names := range v.Values {
		if dst.Values[kind] == nil {
			dst.Values[kind] = map[string][]int{}
		}
		maps.Copy(dst.Values[kind], names)
	}
}

// FontFeatureValues returns the feature values the @font-feature-values
// rules define for family, matched without regard to ASCII case, nil for
// none. The result is shared and must not be modified.
func (r *Resolver) FontFeatureValues(family string) *FontFeatureValues {
	return r.featureValues[strings.ToLower(family)]
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
	// FontUnits returns the measures of the first font a style's
	// families, weight and style select, as fractions of the font size,
	// for the ex, ch, cap and ic units and their root forms (CSS Values 4,
	// 6.1.1). nil, or a measure that is not positive, takes the fallback
	// [FontUnits] names.
	FontUnits func(st *Style) FontUnits
}

// Computed holds the computed style of every element of a tree. It is
// immutable and safe for concurrent use.
type Computed struct {
	styles map[*html.Node]*Style
	pseudo map[pseudoKey]*Style
	warns  []Warning
	// re cascades the typographic pseudo-elements, and the boxes on a
	// first line, again under another parent style, nil when no element
	// has a ::first-letter or a ::first-line.
	re *restyler
}

// restyler cascades an element, a pseudo-element of it, its
// ::first-letter or its ::first-line again under another parent style. It
// keeps the state of the Compute call that made it, which mu guards.
type restyler struct {
	mu sync.Mutex
	cs *computeState
	// letters and lines hold the declarations of each ::first-letter and
	// ::first-line.
	letters, lines map[*html.Node][]cand
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

// FirstLetter returns the style of the ::first-letter of the element n
// when the letter sits in an inline box styled parent, a descendant of n
// on its first line (CSS Pseudo 4, 2.3): the letter inherits from the box
// that holds it, as the fictional tag sequence nests it there. A nil
// parent, or n's own style, gives [Computed.Pseudo]'s style. It returns
// nil when no rule targets n's ::first-letter. The Style is shared: do
// not modify it.
func (c *Computed) FirstLetter(n *html.Node, parent *Style) *Style {
	ps := c.pseudo[pseudoKey{n, PseudoFirstLetter}]
	if ps == nil || parent == nil || parent == c.styles[n] || c.re == nil {
		return ps
	}
	cands, ok := c.re.letters[n]
	if !ok {
		return ps
	}
	c.re.mu.Lock()
	defer c.re.mu.Unlock()
	cs := c.re.cs
	cs.node = n
	// The letter sits in a block container, whatever contents element
	// holds its text.
	cs.boxParent = Block
	st, _ := cs.cascade(cands, parent, cs.vars[n], false, nil)
	return st
}

// FirstLine returns the style of the ::first-line of the element n
// inheriting from parent, the style of the block container whose first
// formatted line it styles: n's box or a descendant's (CSS Pseudo 4,
// 2.1.1). A nil parent, or n's own style, gives [Computed.Pseudo]'s style.
// It holds the declarations of the properties that apply to ::first-line
// alone (see firstLineProps); the others take their inherited or initial
// values. It returns nil when no rule targets n's ::first-line. The Style
// is shared: do not modify it.
func (c *Computed) FirstLine(n *html.Node, parent *Style) *Style {
	ps := c.pseudo[pseudoKey{n, PseudoFirstLine}]
	if ps == nil || parent == nil || parent == c.styles[n] || c.re == nil {
		return ps
	}
	cands, ok := c.re.lines[n]
	if !ok {
		return ps
	}
	c.re.mu.Lock()
	defer c.re.mu.Unlock()
	cs := c.re.cs
	cs.node = n
	cs.boxParent = Block
	st, _ := cs.cascade(cands, parent, cs.vars[n], false, &firstLineProps)
	return st
}

// Restyle returns the style the element n computes when it inherits from
// parent rather than from its parent element's style: the style of the
// fragment of n on a first formatted line, which inherits from the
// ::first-line (CSS Pseudo 4, 2.1.3). Its custom properties still inherit
// from the parent element. A nil parent, or the parent element's own
// style, gives [Computed.Style]'s; so does a document without a
// ::first-line. The Style is shared: do not modify it.
func (c *Computed) Restyle(n *html.Node, parent *Style) *Style {
	st := c.styles[n]
	if st == nil || parent == nil || c.re == nil || c.re.lines == nil {
		return st
	}
	p := n.Parent
	if isElement(p) && c.styles[p] == parent {
		return st
	}
	c.re.mu.Lock()
	defer c.re.mu.Unlock()
	cs := c.re.cs
	var parentVars map[string][]token
	if isElement(p) {
		parentVars = cs.vars[p]
	}
	out, ok := cs.restyle(n, &cs.r.elems, parent, parentVars, true)
	if !ok {
		return st
	}
	return out
}

// RestylePseudo returns the style of the pseudo-element which of the
// element n when it inherits from parent rather than from n's style: a
// ::before, ::after or ::marker on a first formatted line, which inherits
// from the ::first-line (CSS Pseudo 4, 2.1.3). It returns
// [Computed.Pseudo]'s style when parent is nil or n's own style, or when
// the document has no ::first-line, and nil when no rule targets the
// pseudo-element. The Style is shared: do not modify it.
func (c *Computed) RestylePseudo(n *html.Node, which PseudoElement, parent *Style) *Style {
	ps := c.pseudo[pseudoKey{n, which}]
	if ps == nil || parent == nil || parent == c.styles[n] || c.re == nil || c.re.lines == nil {
		return ps
	}
	if which == PseudoFirstLetter {
		return c.FirstLetter(n, parent)
	}
	c.re.mu.Lock()
	defer c.re.mu.Unlock()
	cs := c.re.cs
	out, ok := cs.restyle(n, &cs.r.pseudos[which], parent, cs.vars[n], false)
	if !ok {
		return ps
	}
	return out
}

// firstLineProps are the longhands a ::first-line declaration sets: the
// properties CSS Pseudo 4, 2.1.2 lists as applying to it that Chromium
// applies, and those Chromium applies beyond the list (opacity,
// visibility, box-shadow). litho follows Chromium: it drops text-emphasis
// style and position, which the list's inline typesetting properties
// include, and vertical-align, which Chromium computes on ::first-line
// and does not lay out.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 computes on
//     ::first-line the font properties, color, opacity, visibility, the
//     background properties (font-feature-settings, font-size-adjust,
//     font-synthesis and font-variant among the font properties, not
//     font-language-override), box-shadow, text-shadow, the
//     text-decoration longhands, text-underline-offset and -position,
//     text-emphasis-color, word-spacing, letter-spacing, text-transform,
//     line-height, vertical-align, ruby-position and text-justify, and
//     drops text-align, text-align-last, text-indent, white-space,
//     word-break, overflow-wrap, line-break, tab-size, hyphens,
//     direction, unicode-bidi, writing-mode, the box properties, float,
//     position, text-emphasis-style and -position, ruby-align,
//     text-autospace and text-wrap-style; a vertical-align of 20px moves
//     no glyph.
var firstLineProps = func() [numProps]bool {
	var on [numProps]bool
	for _, id := range []propID{
		pFontSize, pColor, pVisibility, pOpacity, pFontFamily, pFontWeight, pFontStyle, pFontVariant,
		pFontStretch, pFontSynthesisWeight, pFontSynthesisStyle, pFontKerning, pLineHeight,
		pBgColor, pBgImage, pBgRepeat, pBgPosition, pBgPositionY, pBgSize, pBgOrigin, pBgClip, pBgAttachment,
		pBoxShadow, pTextShadow, pTextDecoration, pTextDecorationColor, pTextDecorationStyle,
		pTextDecorationThickness, pTextDecorationSkipInk, pTextUnderlineOffset, pTextUnderlinePosition,
		pTextEmphasisColor, pTextTransform, pLetterSpacing, pWordSpacing, pRubyPosition, pTextJustify,
		pFontFeatureSettings, pFontVariantLigatures, pFontVariantNumeric, pFontVariantEastAsian,
		pFontVariantPosition, pFontVariantAlternates, pFontVariantEmoji, pFontSynthesisSmallCaps, pFontSizeAdjust,
	} {
		on[id] = true
	}
	return on
}()

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
	// node is the element being cascaded, whose attributes attr()
	// reads; a pseudo-element's is its originating element.
	node *html.Node
	// boxParent is the display of the nearest ancestor of node that is
	// not display: contents, the box whose layout blockifies the boxes a
	// contents parent hands it (CSS Display 3, 2.5 and 2.7).
	boxParent Display
	// noContents reports node an element display: contents computes to
	// none on, while node itself is cascaded (CSS Display 3, Appendix B).
	noContents bool
	// units caches FontUnits by font selection.
	units map[fontKey]FontUnits
	// mem is the conversion's memory budget each distinct computed style
	// is charged to, nil for none.
	mem *limits.Budget
	// styles shares the computed styles of the tree: the elements of one
	// class, and the many that inherit alike, hold one style.
	styles *Styles
	// scratch is the style computeStyle computes into: a style equal to
	// one the tree holds costs no allocation, and a new one is copied
	// out of it. sty is the styler it computes with.
	scratch Style
	sty     styler
	// cands, matched and classes are the buffers of match, reused from
	// one element to the next: an element's declarations are cascaded
	// before the next match.
	cands   []cand
	matched []int
	classes []string
	// letterCands and lineCands hold the declarations of each
	// ::first-letter and ::first-line, for [Computed.FirstLetter] and
	// [Computed.FirstLine], nil for none.
	letterCands, lineCands map[*html.Node][]cand
}

// fontKey is what selects a font for the ex and ch units.
type fontKey struct {
	families string
	weight   int
	style    FontStyle
}

// fontUnits returns the font measures of st, zero for the fallbacks.
func (cs *computeState) fontUnits(st *Style) FontUnits {
	if cs.opts.FontUnits == nil || st == nil {
		return FontUnits{}
	}
	k := fontKey{families: strings.Join(st.FontFamilies, "\x00"), weight: st.FontWeight, style: st.FontStyle}
	if u, ok := cs.units[k]; ok {
		return u
	}
	u := cs.opts.FontUnits(st)
	if cs.units == nil {
		cs.units = map[fontKey]FontUnits{}
	}
	cs.units[k] = u
	return u
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
	for id := range numProps {
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
		r: r, opts: opts, m: newMatcher(opts.Lang, maxComputeSteps), mem: limits.BudgetFrom(ctx), styles: NewStyles(),
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
		for _, kid := range slices.Backward(kids) {
			stack = append(stack, frame{n: kid, parent: parent})
		}
	}
	c := &Computed{styles: cs.out, pseudo: cs.pseudo, warns: cs.warns.result()}
	if cs.letterCands != nil || cs.lineCands != nil {
		c.re = &restyler{cs: cs, letters: cs.letterCands, lines: cs.lineCands}
	}
	return c, nil
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
	cands := cs.elementCands(n)
	cs.node = n
	cs.boxParent = cs.boxParentOf(parent)
	cs.noContents = contentsIsNone(n)
	st, vars := cs.cascade(cands, parentStyle, parentVars, parent == nil, nil)
	cs.noContents = false
	cs.cands = cands[:0]
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
		var only *[numProps]bool
		switch pe { //nolint:exhaustive // the other pseudo-elements are cascaded once
		case PseudoFirstLetter:
			// The buffer match returns is reused by the next element.
			pc = slices.Clone(pc)
			if cs.letterCands == nil {
				cs.letterCands = map[*html.Node][]cand{}
			}
			cs.letterCands[n] = pc
		case PseudoFirstLine:
			pc = slices.Clone(pc)
			if cs.lineCands == nil {
				cs.lineCands = map[*html.Node][]cand{}
			}
			cs.lineCands[n] = pc
			only = &firstLineProps
		}
		ps, _ := cs.cascade(pc, st, vars, false, only)
		cs.pseudo[pseudoKey{n, pe}] = ps
	}
}

// elementCands returns the declarations competing for the element n: its
// matching rules, its presentational hints and its style attribute, in a
// buffer the next match reuses.
func (cs *computeState) elementCands(n *html.Node) []cand {
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
	return cands
}

// restyle matches the element n again against the selectors of x, its
// element rules with its hints and style attribute when elem is set, and
// cascades what matches under parent. It reports false when the match
// ran past the step budget, which leaves the style unknown. It runs after
// Compute, so it fills the ancestor filter with n's ancestors first.
func (cs *computeState) restyle(n *html.Node, x *selIndex, parent *Style, parentVars map[string][]token, elem bool) (*Style, bool) {
	saved := cs.anc
	cs.anc = &ancestorFilter{}
	for a := n.Parent; isElement(a); a = a.Parent {
		cs.anc.push(a)
	}
	defer func() { cs.anc = saved }()
	cs.elem++
	var cands []cand
	if elem {
		cands = cs.elementCands(n)
	} else {
		cands = cs.match(n, x)
	}
	if cs.m.over {
		return nil, false
	}
	cs.node = n
	if elem {
		cs.boxParent = cs.boxParentOf(parentElement(n))
		cs.noContents = contentsIsNone(n)
	} else {
		cs.boxParent = cs.boxParentOf(n)
	}
	st, _ := cs.cascade(cands, parent, parentVars, false, nil)
	cs.noContents = false
	cs.cands = cands[:0]
	return st, true
}

// boxParentOf returns the display of the box the children of the element
// p sit in: p's own, or the nearest ancestor's past the elements of
// display contents, Block for none.
func (cs *computeState) boxParentOf(p *html.Node) Display {
	for ; p != nil; p = p.Parent {
		if ps := cs.out[p]; ps != nil && ps.Display != Contents {
			return ps.Display
		}
	}
	return Block
}

// cascade sorts the declarations competing for one element or
// pseudo-element and computes its style from them, and its custom
// properties. only, when not nil, names the longhands the declarations
// may set: the others take their inherited or initial values.
func (cs *computeState) cascade(cands []cand, parentStyle *Style, parentVars map[string][]token, isRoot bool, only *[numProps]bool) (*Style, map[string][]token) {
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
	// and the writing mode (CSS Logical Properties 1, 4).
	flow := cascadedFlow(cands, parentStyle)
	mapped := !flow.identity()
	side := func(d *Declaration, id propID) propID {
		if mapped && isFlowRelative(d.Property) {
			return flow.mapID(id)
		}
		return id
	}
	// user holds, per longhand, the value the user origin's declarations
	// give it, which an author revert rolls back to (CSS Cascade 4,
	// 7.3.4). The user origin's normal declarations sort before every
	// author declaration, so it is complete when an author revert is met.
	var user *[numProps]any
	for _, c := range cands {
		for _, w := range c.d.warns {
			cs.warns.add(w)
		}
		switch {
		case c.d.revert && originOf(c.rank) == Author && user != nil:
			for _, e := range c.d.entries {
				id := side(c.d, e.id)
				if v := user[id]; v != nil {
					winners[id] = v
				} else {
					winners[id] = e.v
				}
			}
			continue
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
		if originOf(c.rank) == User && !c.d.isCustom {
			if user == nil {
				user = new([numProps]any)
			}
			if len(c.d.pending) > 0 {
				for _, id := range longhandsOf(c.d.Property) {
					user[side(c.d, id)] = pendingRef{c.d}
				}
			}
			for _, e := range c.d.entries {
				user[side(c.d, e.id)] = e.v
			}
		}
	}
	if only != nil {
		for id := range winners {
			if !only[id] {
				winners[id] = nil
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

// linearClasses is how many distinct classes of an element match tells
// apart by scanning them before it keeps them in a map: an element names a
// few classes, and a scan of a few costs less than a map.
const linearClasses = 8

// match returns the declarations of the rules matching n, in a buffer the
// next call reuses.
func (cs *computeState) match(n *html.Node, r *selIndex) []cand {
	matched := cs.matched[:0]
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
		// The classes seen are scanned while they are few and kept in a
		// map past linearClasses, so a long class list costs linear time.
		seen := cs.classes[:0]
		var many map[string]bool
		for c := range asciiFields(cl) {
			switch {
			case many != nil:
				if many[c] {
					continue
				}
				many[c] = true
			case slices.Contains(seen, c):
				continue
			default:
				seen = append(seen, c)
				if len(seen) > linearClasses {
					many = make(map[string]bool, 2*len(seen))
					for _, x := range seen {
						many[x] = true
					}
				}
			}
			test(r.byClass[c])
		}
		cs.classes = seen[:0]
	}
	test(r.byTag[lower(n.Data)])
	test(r.universal)
	cs.matched = matched[:0]
	out := cs.cands[:0]
	for _, ri := range matched {
		rr := &cs.r.rules[ri]
		for i := range rr.rule.decls {
			d := &rr.rule.decls[i]
			out = append(out, cand{d: d, rank: rank(rr.origin, d.Important), spec: uint64(cs.spec[ri]), ord: rr.order, idx: i})
		}
	}
	cs.cands = out[:0]
	return out
}

// computeStyle computes every longhand from its cascaded value (CSS
// Cascade 4, 4 and CSS 2.1, 6.1 to 6.2): inherited longhands without a
// value take the parent's, the others their initial value. It returns the
// style of the tree equal to the result when there is one, so elements
// styled alike share one [Style].
func (cs *computeState) computeStyle(winners *[numProps]any, parent *Style, isRoot bool) *Style {
	st := &cs.scratch
	*st = *parent
	s := &cs.sty
	*s = styler{parent: parent, root: cs.root}
	rootFS := mediumFontSize
	if cs.root != nil {
		rootFS = cs.root.FontSize
	}
	vw, vh := cs.r.media.viewport()
	s.lc = lenCtx{fontSize: parent.FontSize, rootFontSize: rootFS, width: vw, height: vh}
	s.lc.font = cs.fontUnits(parent)
	s.lc.rootFont = cs.fontUnits(cs.root)
	if cs.root != nil {
		s.lc.verticalRoot = cs.root.WritingMode != HorizontalTB
	}
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
			s.lc.font = cs.fontUnits(st)
			if isRoot {
				s.lc.rootFont = s.lc.font
			}
		case pLineHeight:
			s.lc.lineHeight = usedLineHeight(st)
			if isRoot {
				s.lc.rootLineHeight = s.lc.lineHeight
			}
		}
	}
	fixupDisplay(st, isRoot)
	if st.Display == Contents && cs.noContents {
		st.Display = DisplayNone
	}
	pd := parent.Display
	if pd == Contents {
		pd = cs.boxParent
	}
	if !isRoot && (isFlexContainer(pd) || isGridContainer(pd)) {
		// The children of a flex or grid container are its items,
		// blockified (CSS Flexbox 1, 4; CSS Grid 1, 6; CSS Display 3,
		// 2.7), and so are those a display: contents child of it hands
		// it (2.5).
		st.Display, _ = blockify(st.Display)
	}
	if !isRoot && (parent.Display == Ruby || parent.Display == RubyText) && st.Float == FloatNone && st.Position != Absolute && st.Position != Fixed {
		// CSS Ruby 1, 2.1: the in-flow block-level children of a ruby
		// container or annotation are inlinified.
		st.Display = inlinify(st.Display)
	}
	shared, adopted := cs.styles.shareCopy(st)
	if adopted {
		// A refusal sticks and cancels the conversion, which the
		// cascade's next context check stops at: a style has no error
		// to return. A style equal to one held costs nothing more.
		cs.mem.Take(1, styleCost, "computed styles")
	}
	return shared
}

// noContentsElements are the HTML elements display: contents computes to
// none on: the replaced elements and form controls whose rendering is not
// entirely controlled by CSS (CSS Display 3, 2.5 and Appendix B). Immutable
// lookup table.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws neither the
//     content nor the fallback content of each of these elements, nor of
//     an svg or a math element, under display: contents; it draws the
//     content of a fieldset, a legend, a details, a button and a label.
var noContentsElements = map[atom.Atom]bool{
	atom.Br: true, atom.Wbr: true, atom.Meter: true, atom.Progress: true, atom.Canvas: true,
	atom.Embed: true, atom.Object: true, atom.Audio: true, atom.Iframe: true, atom.Img: true,
	atom.Video: true, atom.Frame: true, atom.Frameset: true, atom.Input: true, atom.Textarea: true,
	atom.Select: true,
}

// contentsIsNone reports an element display: contents computes to none on
// (CSS Display 3, Appendix B): an HTML element of [noContentsElements], an
// svg element with CSS box layout, one whose parent is no SVG element, and
// every MathML element. The other SVG elements keep contents: the SVG
// renderer draws their content in their place.
func contentsIsNone(n *html.Node) bool {
	switch n.Namespace {
	case "":
		return noContentsElements[n.DataAtom]
	case "svg":
		p := n.Parent
		return n.Data == "svg" && (p == nil || p.Type != html.ElementNode || p.Namespace != "svg")
	case "math":
		return true
	}
	return false
}

// fixupDisplay applies CSS 2.1, 9.7: an absolutely positioned box does
// not float, and a floated, absolutely positioned or root box is
// blockified. display: contents generates no box to blockify (CSS Display
// 3, 2.7), except on the root, where it computes to block (2.8).
func fixupDisplay(st *Style, isRoot bool) {
	st.OuterInline = false
	if st.Display == DisplayNone || st.Display == Contents && !isRoot {
		return
	}
	if st.Display == Contents {
		st.Display = Block
	}
	if st.Position == Absolute || st.Position == Fixed {
		st.Float = FloatNone
	} else if st.Float == FloatNone && !isRoot {
		return
	}
	st.Display, st.OuterInline = blockify(st.Display)
}
