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
	// paletteValues holds the @font-palette-values rules whose media
	// conditions match, by name, in cascade order.
	paletteValues map[string][]*PaletteValues
	warns         []Warning
	// counters holds the counter styles, the predefined ones and those
	// of the @counter-style rules whose media conditions match.
	counters *CounterStyles
	// keyframes holds the @keyframes rules whose media conditions match,
	// the last of each name.
	keyframes keyframesAt
	// positionTries holds the @position-try rules whose media conditions
	// match, the last of each name in the highest layer.
	positionTries map[string]*positionTryRule
}

type resolvedRule struct {
	rule   *styleRule
	origin Origin
	order  int
	// layer is the position of the rule's cascade layer in its origin's
	// layer order (see [orderLayers]).
	layer int
}

type resolvedPage struct {
	rule   *pageRule
	origin Origin
	order  int
	layer  int
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
	// PseudoDetailsContent is ::details-content, the slot of a details
	// element that holds its content but its summary (HTML Standard,
	// 15.5.5; CSS Pseudo 4, 3.4).
	PseudoDetailsContent
	numPseudos
)

// pseudoNames maps the selector name of each generated pseudo-element to
// it.
var pseudoNames = map[string]PseudoElement{
	"before": PseudoBefore, "after": PseudoAfter, "marker": PseudoMarker,
	"footnote-call": PseudoFootnoteCall, "footnote-marker": PseudoFootnoteMarker,
	"note-call": PseudoNoteCall, "note-marker": PseudoNoteMarker, "note-callback": PseudoNoteCallback,
	"first-letter": PseudoFirstLetter, "first-line": PseudoFirstLine,
	"details-content": PseudoDetailsContent,
}

// NewResolver indexes the rules of sheets, in order, whose media
// conditions match media. A nil Sheet is skipped.
func NewResolver(media Media, sheets []Sheet) *Resolver {
	r := &Resolver{media: media}
	cache := map[*mediaCond]bool{}
	var w warnings
	order := 0
	var counterRules []*counterStyleRule
	var counterPos []int
	kfPos := map[string]int{}
	tryPos := map[string]int{}
	type layeredValues struct {
		v   FontFeatureValues
		pos int
	}
	var values []layeredValues
	type layeredPalette struct {
		v   *PaletteValues
		pos int
	}
	var palettes []layeredPalette
	var facePos []int
	lay := orderLayers(sheets)
	for si, sh := range sheets {
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
			r.rules = append(r.rules, resolvedRule{rule: sr, origin: sh.Origin, order: order, layer: lay.of(si, sh.Origin, sr.layer)})
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
				r.pages = append(r.pages, resolvedPage{rule: pr, origin: sh.Origin, order: order, layer: lay.of(si, sh.Origin, pr.layer)})
			}
		}
		for _, cr := range sh.Sheet.counterStyles {
			if cr.cond.matches(media, cache) {
				counterRules = append(counterRules, cr)
				counterPos = append(counterPos, lay.of(si, sh.Origin, cr.layer))
			}
		}
		for i := range sh.Sheet.faces {
			if f := &sh.Sheet.faces[i]; f.cond.matches(media, cache) {
				r.faces = append(r.faces, f.face)
				facePos = append(facePos, lay.of(si, sh.Origin, f.layer))
			}
		}
		for _, k := range sh.Sheet.keyframes {
			if k.cond.matches(media, cache) {
				if r.keyframes == nil {
					r.keyframes = keyframesAt{}
				}
				// The last rule of a name in the highest layer wins
				// (CSS Cascade 5, 6.4.3; CSS Animations 1, 3).
				pos := lay.of(si, sh.Origin, k.layer)
				if prev, ok := kfPos[k.name]; !ok || pos >= prev {
					kfPos[k.name] = pos
					r.keyframes[k.name] = k
				}
			}
		}
		for _, p := range sh.Sheet.positionTries {
			if p.cond.matches(media, cache) {
				if r.positionTries == nil {
					r.positionTries = map[string]*positionTryRule{}
				}
				// They cascade as @keyframes rules do (CSS Anchor
				// Positioning 1, 5.3).
				pos := lay.of(si, sh.Origin, p.layer)
				if prev, ok := tryPos[p.name]; !ok || pos >= prev {
					tryPos[p.name] = pos
					r.positionTries[p.name] = p
				}
			}
		}
		for i := range sh.Sheet.paletteValues {
			pr := &sh.Sheet.paletteValues[i]
			if pr.cond.matches(media, cache) {
				palettes = append(palettes, layeredPalette{&pr.values, lay.of(si, sh.Origin, pr.layer)})
			}
		}
		for i := range sh.Sheet.featureValues {
			if f := &sh.Sheet.featureValues[i]; f.cond.matches(media, cache) {
				values = append(values, layeredValues{f.values, lay.of(si, sh.Origin, f.layer)})
			}
		}
	}
	// A later layer's values and counter styles win over an earlier
	// one's, whatever their order (CSS Cascade 5, 6.4.3).
	slices.SortStableFunc(values, func(a, b layeredValues) int { return cmp.Compare(a.pos, b.pos) })
	for _, v := range values {
		r.addFeatureValues(v.v)
	}
	// A later layer's palettes win over an earlier one's (CSS Cascade 5,
	// 6.4.3), and a later rule over an earlier one of its layer.
	slices.SortStableFunc(palettes, func(a, b layeredPalette) int { return cmp.Compare(a.pos, b.pos) })
	for _, p := range palettes {
		if r.paletteValues == nil {
			r.paletteValues = map[string][]*PaletteValues{}
		}
		r.paletteValues[p.v.Name] = append(r.paletteValues[p.v.Name], p.v)
	}
	// A later layer's @font-face rules win over an earlier one's when
	// their descriptors collide (CSS Cascade 5, 6.4): the faces are
	// listed in layer order, a later face winning (CSS Fonts 4, 4.7).
	r.faces = sortedByLayer(r.faces, facePos)
	r.warns = w.result()
	r.counters = newCounterStyles(sortedByLayer(counterRules, counterPos))
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
	case last.never || last.ns == nsUnknown:
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
// cascade layer order, the rules outside any layer last, and in source
// order within a layer. The slice is a copy; the faces' slices are shared and must not be
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
	// families, weight and style select in the language lang of the
	// element styled (its lang or xml:lang attribute, inherited, "" for
	// unknown), as fractions of the font size, for the ex, ch, cap and ic
	// units and their root forms (CSS Values 4, 6.1.1): the language picks
	// the face of a generic family. nil, or a measure that is not
	// positive, takes the fallback [FontUnits] names.
	FontUnits func(st *Style, lang string) FontUnits
	// AnimationTime is the moment the animations are computed at,
	// [AnimationsAtStart] by default.
	AnimationTime AnimationTime
	// ContainerSize returns the width and height in points of the content
	// box of the element n, a container for size queries, as a layout
	// measured it, false when it has no box or none was measured: the
	// size features of @container rules and the container query units
	// read it (CSS Conditional 5, 6.1 and 7). nil measures none, which
	// makes every size feature unknown and every container query unit
	// the small viewport's.
	ContainerSize func(n *html.Node) (width, height float64, ok bool)
	// PositionOption returns the position option a layout chose for the
	// element n, an absolutely positioned box with position options, nil
	// for its fallback base styles or none chosen. The element's computed
	// values then hold the option's accepted @position-try properties,
	// which its descendants inherit (CSS Anchor Positioning 1, 5.4). nil
	// chooses none.
	PositionOption func(n *html.Node) *TryStyle
	// Mode is the document's mode: in quirks mode its style attributes take
	// quirky lengths and colours (see [ParseOptions.Quirks]) and its class
	// and ID selectors match ASCII case-insensitively (Selectors 4, 6.6
	// and 6.7).
	Mode DocumentMode
	// maxSteps lowers the step budget below maxComputeSteps, for tests;
	// zero keeps it.
	maxSteps int
	// noReuse computes every element's style without reusing an equal
	// cascade's (reuse.go), for tests.
	noReuse bool
}

// Computed holds the computed style of every element of a tree. It is
// immutable and safe for concurrent use.
type Computed struct {
	styles map[*html.Node]*Style
	pseudo map[pseudoKey]*Style
	warns  []Warning
	// table shares the styles the cascade computed, every one of them
	// charged to the conversion's budget when adopted.
	table *Styles
	// re cascades the typographic pseudo-elements, and the boxes on a
	// first line, again under another parent style, nil when no element
	// has a ::first-letter or a ::first-line.
	re *restyler
	// queried reports a style that read the size of a size container.
	queried bool
	// inheritsTry reports a style that inherits an accepted @position-try
	// property from a box with position options.
	inheritsTry bool
}

// InheritsPositionOptions reports whether a computed style inherits, by
// the inherit keyword, an accepted @position-try property of an element
// with position options: a style that the position option a layout
// chooses for that element changes ([ComputeOptions.PositionOption]).
func (c *Computed) InheritsPositionOptions() bool { return c.inheritsTry }

// QueriesContainers reports whether a computed style depends on the size
// of a container for size queries: a size feature of an @container rule
// evaluated against one, or a container query unit resolved against one
// (CSS Conditional 5, 6.1 and 7), with or without
// [ComputeOptions.ContainerSize] answering it.
func (c *Computed) QueriesContainers() bool { return c.queried }

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
	cs.lang = cs.unitLang(n)
	cs.parentLang = cs.lang
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
	cs.lang = cs.unitLang(n)
	cs.parentLang = cs.lang
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

// Charged reports whether st is a style the cascade computed and charged
// to the conversion's memory budget ([limits.StyleCost]), restyles
// included: the code that holds st holds it for less than the document
// tree lives, so it charges nothing more for it. A nil c or st reports
// false.
func (c *Computed) Charged(st *Style) bool {
	if c == nil {
		return false
	}
	return c.table.holds(st)
}

// initialStyle is the shared computed initial style.
var initialStyle = Initial()

// styleAttrSpec is the specificity of a style attribute's declarations,
// above any selector's (CSS Style Attributes, 3 and CSS Cascade 4, 6.4).
const styleAttrSpec = 1 << 31

// cand is one declaration competing in an element's cascade.
type cand struct {
	d    *Declaration
	rank int
	// layer orders the declarations of one rank by cascade layer
	// ([layerRank]), before specificity.
	layer int
	spec  uint64
	ord   int
	idx   int
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
	// lang is the language of node, parentLang the language of the
	// element its parent style comes from and rootLang the root
	// element's, which the font units are measured in.
	lang, parentLang, rootLang string
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
	// cur is the style of node before its animations, while animate runs.
	cur *Style
	// pend is the buffer of the longhands an element's var() values set,
	// reused from one cascade to the next.
	pend []pendingAt
	// letterCands and lineCands hold the declarations of each
	// ::first-letter and ::first-line, for [Computed.FirstLetter] and
	// [Computed.FirstLine], nil for none.
	letterCands, lineCands map[*html.Node][]cand
	// reuse remembers the styles of the cascades computed, for the
	// elements whose cascade is the same (reuse.go).
	reuse reuseCache
	// inPseudo reports a cascade of a pseudo-element of node, whose
	// container queries start at node itself.
	inPseudo bool
	// inheritsTry reports a style that inherits an accepted
	// @position-try property from an element with position options
	// ([Computed.InheritsPositionOptions]).
	inheritsTry bool
	// containerUnits reports a cascade with a container query unit or a
	// var() value among its declarations, whose lengths resolve against
	// the element's containers.
	containerUnits bool
	// sizeContainers counts the containers for size queries computed so
	// far, queried a style that read the size of one.
	sizeContainers int
	queried        bool
	// cqMemo holds the value of each @container condition for the
	// element or pseudo-element cqMemoAt counts.
	cqMemo   map[*containerCond]bool
	cqMemoAt int
}

// unitLang returns the language the font units of the element n are
// measured in: its lang or xml:lang attribute, inherited, the document's
// for nil, and "" without [ComputeOptions.FontUnits], which then reads
// none.
func (cs *computeState) unitLang(n *html.Node) string {
	switch {
	case cs.opts.FontUnits == nil:
		return ""
	case n == nil:
		return cs.m.docLang
	}
	// A short walk up to the attribute that states the language, or to
	// an element whose language the matcher knows, records nothing; a
	// longer one asks the matcher, which remembers each element's.
	e := n
	for range unitLangWalk {
		if !isElement(e) {
			return cs.m.docLang
		}
		if i, ok := cs.m.info[e]; ok && i.langDone {
			return i.lang
		}
		if v, ok := attr(e, "xml:lang"); ok {
			return lower(strings.TrimSpace(v))
		}
		if v, ok := attr(e, "lang"); ok {
			return lower(strings.TrimSpace(v))
		}
		e = e.Parent
	}
	return cs.m.langOf(n)
}

// unitLangWalk is how many elements [computeState.unitLang] reads up the
// tree before it asks the matcher, which remembers the language of each
// element it reaches at the cost of a record an element. A performance
// bound, not spec-mandated: most documents state their language within a
// few levels of any element.
const unitLangWalk = 16

// fontKey is what selects a font for the ex and ch units.
type fontKey struct {
	families string
	weight   int
	style    FontStyle
	lang     string
}

// fontUnits returns the font measures of st in language lang, zero for
// the fallbacks.
func (cs *computeState) fontUnits(st *Style, lang string) FontUnits {
	if cs.opts.FontUnits == nil || st == nil {
		return FontUnits{}
	}
	k := fontKey{families: strings.Join(st.FontFamilies, "\x00"), weight: st.FontWeight, style: st.FontStyle, lang: lang}
	if u, ok := cs.units[k]; ok {
		return u
	}
	u := cs.opts.FontUnits(st, lang)
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
// font size and colour first, as [propID] says, then the font selection
// and font-size-adjust, which the ex and ch units of every other length
// resolve against, then the rest in propID order. zoom is not among them:
// the effective zoom comes before them all (computeInto).
var computeOrder = func() []propID {
	first := []propID{pMathDepth, pFontSize, pColorScheme, pColor, pFontFamily, pFontWeight, pFontStyle, pFontSizeAdjust, pLineHeight}
	out := slices.Clone(first)
	for id := range numProps {
		if !slices.Contains(first, id) && id != pZoom {
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
		r: r, opts: opts, m: newMatcher(opts.Lang, cmp.Or(opts.maxSteps, maxComputeSteps)), mem: limits.BudgetFrom(ctx), styles: NewStyles(),
		out: map[*html.Node]*Style{}, pseudo: map[pseudoKey]*Style{}, vars: map[*html.Node]map[string][]token{}, anc: &ancestorFilter{},
		stamp: make([]int, len(r.rules)), spec: make([]uint32, len(r.rules)),
	}
	for _, w := range r.warns {
		cs.warns.add(w)
	}
	if root == nil {
		return &Computed{styles: cs.out, pseudo: cs.pseudo, warns: cs.warns.result(), table: cs.styles}, nil
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
	cs.reuse.on = !opts.noReuse
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
	// The styles a restyle computes inherit from styles the caller
	// gives, which no shared table holds: none is reused.
	cs.reuse = reuseCache{}
	c := &Computed{styles: cs.out, pseudo: cs.pseudo, warns: cs.warns.result(), table: cs.styles, queried: cs.queried, inheritsTry: cs.inheritsTry}
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
	cs.lang, cs.parentLang = cs.unitLang(n), cs.unitLang(parent)
	if parent == nil {
		cs.rootLang = cs.lang
	}
	cs.boxParent = cs.boxParentOf(parent)
	cs.noContents = contentsIsNone(n)
	st, vars := cs.cascade(cands, parentStyle, parentVars, parent == nil, nil)
	cs.noContents = false
	cs.cands = cands[:0]
	if st.Anchor != nil && len(st.Anchor.Fallbacks) > 0 && cs.opts.PositionOption != nil {
		if t := cs.opts.PositionOption(n); t != nil {
			st = cs.adopt(withOption(st, t))
		}
	}
	cs.out[n] = st
	cs.vars[n] = vars
	if st.SizeContainer() {
		cs.sizeContainers++
	}
	if parent == nil {
		cs.root = st
	}
	// A pseudo-element inherits from n, in n's language.
	cs.parentLang = cs.lang
	for pe := range numPseudos {
		x := &cs.r.pseudos[pe]
		if x.empty() || st.Display == DisplayNone || pe == PseudoMarker && st.Display != ListItem && st.Display != InlineListItem {
			// A ::marker exists on a list item alone (CSS Lists 3, 3.1).
			continue
		}
		if pe == PseudoDetailsContent && (n.Namespace != "" || n.DataAtom != atom.Details) {
			// A ::details-content exists on a details element alone.
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
		cs.inPseudo = true
		ps, _ := cs.cascade(pc, st, vars, false, only)
		cs.inPseudo = false
		cs.pseudo[pseudoKey{n, pe}] = ps
	}
	if st.Display != DisplayNone && n.Namespace == "" && n.DataAtom == atom.Details && DetailsSummary(n) == nil {
		cs.defaultSummary(n, st, vars)
	}
	if st.Display != DisplayNone && st.Display != Contents && isFieldset(n) {
		cs.fieldsetStyles(n, st, vars)
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
			// Presentational hints lose to every author rule, whatever
			// its layer (CSS Cascade 5, 6.2).
			cands = append(cands, cand{d: &hints[i], rank: rank(Author, false), layer: -attachedRank, ord: -1, idx: i})
		}
	}
	// The style attribute of an HTML, SVG and MathML element (MathML Core,
	// 2.1.4: the same syntax and semantics as on an HTML element).
	if v, ok := attr(n, "style"); ok && (n.Namespace == "" || n.Namespace == "svg" || n.Namespace == "math") {
		decls := ParseDeclarations(v, ParseOptions{BaseURL: cs.opts.BaseURL, Quirks: cs.opts.Mode == Quirks})
		for i := range decls {
			cands = append(cands, cand{d: &decls[i], rank: rank(Author, decls[i].Important), layer: attachedRank, spec: styleAttrSpec, ord: len(cs.r.rules) + 1, idx: i})
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
	cs.lang = cs.unitLang(n)
	cs.parentLang = cs.lang
	if elem {
		cs.boxParent = cs.boxParentOf(parentElement(n))
		cs.noContents = contentsIsNone(n)
		cs.parentLang = cs.unitLang(parentElement(n))
	} else {
		cs.boxParent = cs.boxParentOf(n)
	}
	cs.inPseudo = !elem
	st, _ := cs.cascade(cands, parent, parentVars, false, nil)
	cs.inPseudo = false
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
		if c := cmp.Compare(a.layer, b.layer); c != 0 {
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
	cs.containerUnits = cs.sizeContainers > 0 && slices.ContainsFunc(cands, func(c cand) bool { return c.d.containerUnits || len(c.d.pending) > 0 })
	reuse := cs.reusable(cands, only)
	var key reuseKey
	if reuse {
		key = cs.reuseKeyOf(cands, parentStyle, isRoot)
		if st := cs.reused(key, cands); st != nil {
			for _, c := range cands {
				for _, w := range c.d.warns {
					cs.warns.add(w)
				}
			}
			return st, parentVars
		}
	}
	var winners [numProps]any
	// imp records the longhands an !important declaration sets, which an
	// animation does not override (CSS Cascade 4, 6.2).
	var imp [numProps]bool
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
	// belowUser and belowAuthor hold the state of the cascade before the
	// first declaration of the user origin and of the author origin, which
	// a revert of that origin rolls back to (CSS Cascade 4, 7.3.4): the
	// normal declarations of the origins below sort before it, and the
	// important ones after, where they win anyway. Kept only when a
	// declaration needs them.
	var belowUser, belowAuthor *[numProps]any
	reverts := slices.ContainsFunc(cands, func(c cand) bool { return c.d.revert })
	// below holds the state of the cascade at the start of each rank and
	// layer, which a revert-layer rolls back to; kept only when a
	// declaration needs it.
	var below []layerState
	revertsLayer := slices.ContainsFunc(cands, func(c cand) bool { return c.d.revertLayer })
	// styled holds, for each longhand of [authorStyledProps], whether its
	// winning declaration is an author's or a user's.
	var styled [numProps]bool
	written := func(c cand, id propID) {
		if authorStyledProps[id] != 0 {
			styled[id] = originOf(c.rank) != UserAgent
		}
	}
	for i, c := range cands {
		for _, w := range c.d.warns {
			cs.warns.add(w)
		}
		if revertsLayer && (i == 0 || c.rank != cands[i-1].rank || c.layer != cands[i-1].layer) {
			below = append(below, layerState{rank: c.rank, layer: c.layer, winners: winners, imp: imp})
		}
		if reverts && belowAuthor == nil && c.rank >= rank(User, false) {
			snap := winners
			if belowUser == nil {
				belowUser = &snap
			}
			if c.rank >= rank(Author, false) {
				belowAuthor = &snap
			}
		}
		switch {
		case c.d.revertLayer:
			s := stateBelow(below, c)
			for _, e := range c.d.entries {
				id := side(c.d, e.id)
				winners[id], imp[id] = s.winners[id], s.imp[id]
			}
			continue
		case c.d.revert:
			// A revert of the user agent origin acts as unset, the value
			// its entries hold.
			var s *[numProps]any
			switch originOf(c.rank) {
			case Author:
				s = belowAuthor
			case User:
				s = belowUser
			case UserAgent:
			}
			for _, e := range c.d.entries {
				id := side(c.d, e.id)
				if s != nil && s[id] != nil {
					winners[id] = s[id]
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
				imp[side(c.d, id)] = c.d.Important
				written(c, side(c.d, id))
			}
		default:
			for _, e := range c.d.entries {
				winners[side(c.d, e.id)] = e.v
				imp[side(c.d, e.id)] = c.d.Important
				written(c, side(c.d, e.id))
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
	pend := cs.pend[:0]
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
		src := propID(id) // fits propID: id < numProps
		for _, e := range es {
			if int(side(p.d, e.id)) == id {
				winners[id], src = e.v, e.id
			}
		}
		pend = append(pend, pendingAt{id: propID(id), src: src, d: p.d}) // fits propID: id < numProps
	}
	cs.pend = pend[:0]
	if only == nil && cs.animates(winners[pAnimationName], parentStyle) {
		vars = cs.animate(&winners, &imp, pend, cands, parentStyle, parentVars, vars, isRoot)
	}
	st := cs.computeStyle(&winners, &imp, vars, parentStyle, isRoot, authorStyledBits(&styled))
	if reuse {
		cs.remember(key, cands, st)
	}
	return st, vars
}

// authorStyledBits returns the bits of [authorStyledProps] of the
// longhands styled marks as set by an author or a user.
func authorStyledBits(styled *[numProps]bool) uint8 {
	var bits uint8
	for id, on := range styled {
		if on {
			bits |= authorStyledProps[id]
		}
	}
	return bits
}

// sortedByLayer returns xs ordered by the layer positions pos, one per
// element, rules of one position keeping their order.
func sortedByLayer[T any](xs []T, pos []int) []T {
	if !slices.IsSorted(pos) {
		idx := make([]int, len(xs))
		for i := range idx {
			idx[i] = i
		}
		slices.SortStableFunc(idx, func(a, b int) int { return cmp.Compare(pos[a], pos[b]) })
		out := make([]T, len(xs))
		for i, j := range idx {
			out[i] = xs[j]
		}
		return out
	}
	return xs
}

// layerState is the cascade's winners and their importance at the start
// of one rank and cascade layer.
type layerState struct {
	rank, layer int
	winners     [numProps]any
	imp         [numProps]bool
}

// stateBelow returns the state a revert-layer declaration c rolls back to
// (CSS Cascade 5, 7.3.5): the cascade as if no rule of c's layer had been
// given. below holds the states at the start of each rank and layer up to
// c's, in cascade order. An important declaration of a layer also drops
// everything between the normal and the important declarations of its
// layer, so it rolls back to the start of the layer's normal
// declarations; one of a style attribute rolls back over the style
// attribute alone.
func stateBelow(below []layerState, c cand) *layerState {
	r, l := c.rank, c.layer
	if c.d.Important && l != attachedRank {
		r, l = rank(originOf(r), false), -l
	}
	for i := range below {
		if s := &below[i]; s.rank > r || s.rank == r && s.layer >= l {
			return s
		}
	}
	return &below[len(below)-1]
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
		if rr.rule.cont != nil && !cs.matchesContainers(rr.rule.cont, n, r != &cs.r.elems) {
			continue
		}
		for i := range rr.rule.decls {
			d := &rr.rule.decls[i]
			out = append(out, cand{d: d, rank: rank(rr.origin, d.Important), layer: layerRank(rr.layer, d.Important), spec: uint64(cs.spec[ri]), ord: rr.order, idx: i})
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
func (cs *computeState) computeStyle(winners *[numProps]any, imp *[numProps]bool, vars map[string][]token, parent *Style, isRoot bool, styled uint8) *Style {
	st := &cs.scratch
	cs.computeInto(st, winners, parent, isRoot)
	st.AuthorBorder, st.AuthorBackground = styled&authorBorder != 0, styled&authorBackground != 0
	cs.attachTries(st, winners, imp, vars, parent, isRoot)
	shared, adopted := cs.styles.shareCopy(st)
	if adopted {
		// A refusal sticks and cancels the conversion, which the
		// cascade's next context check stops at: a style has no error
		// to return. A style equal to one held costs nothing more. The
		// style lives as long as the document tree, and the layout
		// charges nothing more for the boxes that hold it
		// ([Computed.Charged]).
		cs.mem.Take(1, limits.StyleCost, "computed styles")
	}
	return shared
}

// computeInto computes into st every longhand from its cascaded value in
// winners, as [computeState.computeStyle] does, without sharing the
// result.
func (cs *computeState) computeInto(st *Style, winners *[numProps]any, parent *Style, isRoot bool) {
	*st = *parent
	s := &cs.sty
	*s = styler{parent: parent, root: cs.root, prefersDark: cs.r.media.prefersDark()}
	rootFS := mediumFontSize
	if cs.root != nil {
		rootFS = cs.root.FontSize
	}
	vw, vh := cs.r.media.viewport()
	// The effective zoom comes before every length (CSS Viewport 1, 4).
	// f is the element's own factor: what it inherits, the parent's font
	// size and line height its em, % and lh units measure, and the
	// root's its rem and rlh units measure, grow by it from the zoom
	// they hold.
	zf := 1.0
	if w, ok := winners[pZoom].(zoomSpec); ok {
		zf = zoomFactor(w)
	}
	st.Zoom = effectiveZoom(parent.UsedZoom(), zf)
	zoomed := st.Zoom != parent.UsedZoom()
	f := st.Zoom / parent.UsedZoom()
	toRoot := 1.0
	if cs.root != nil {
		toRoot = st.Zoom / cs.root.UsedZoom()
		rootFS = float64(rootFS * toRoot)
	}
	if st.Zoom == 1 {
		st.Zoom = 0
	}
	s.lc = lenCtx{fontSize: float64(parent.FontSize * f), rootFontSize: rootFS, width: vw, height: vh, dark: parent.DarkScheme, zoom: st.UsedZoom()}
	s.lc.font = cs.fontUnits(parent, cs.parentLang)
	s.lc.adjust = parent.FontSizeAdjust
	s.lc.rootFont = cs.fontUnits(cs.root, cs.rootLang)
	if cs.root != nil {
		s.lc.rootAdjust = cs.root.FontSizeAdjust
	}
	if cs.root != nil {
		s.lc.verticalRoot = cs.root.WritingMode != HorizontalTB
	}
	s.lc.lineHeight = float64(usedLineHeight(parent) * f)
	if cs.containerUnits {
		s.lc.cq = cs.containerUnitsOf(cs.node, cs.inPseudo)
	}
	s.lc.rootLineHeight = s.lc.lineHeight
	if cs.root != nil {
		s.lc.rootLineHeight = float64(usedLineHeight(cs.root) * toRoot)
	}
	for _, id := range computeOrder {
		def := &props[id]
		v := winners[id]
		if w, ok := v.(cssWide); ok {
			switch {
			case w == wideInherit, w == wideUnset && def.inherited:
				def.copyFrom(st, parent)
				if zoomed {
					zoomInherited(id, st, f)
				}
				v = nil
				if acceptedTry[id] && parent.Anchor != nil && len(parent.Anchor.Fallbacks) > 0 {
					cs.inheritsTry = true
				}
			default:
				v = def.initial
				if v == nil {
					def.copyFrom(st, initialStyle)
				}
			}
		} else if v == nil {
			if def.inherited {
				def.copyFrom(st, parent)
				if zoomed {
					zoomInherited(id, st, f)
				}
			} else {
				v = def.initial
				if v == nil {
					def.copyFrom(st, initialStyle)
				}
			}
		}
		if id == pColor && isRoot && v == nil && st.DarkScheme {
			// The root inherits from the initial values (CSS Cascade 4,
			// 7.2): its color is CanvasText, which a dark scheme makes
			// white.
			v = def.initial
		}
		if a, ok := v.(animatedValue); ok {
			def.copyFrom(st, a.st)
		} else if v != nil {
			def.compute(s, st, v)
		}
		switch id { //nolint:exhaustive // only the font longhands and the colour scheme change the length context
		case pColorScheme:
			s.lc.dark = st.DarkScheme
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
			s.lc.font = cs.fontUnits(st, cs.lang)
			if isRoot {
				s.lc.rootFont = s.lc.font
			}
		case pFontSizeAdjust:
			s.lc.adjust = st.FontSizeAdjust
			if isRoot {
				s.lc.rootAdjust = s.lc.adjust
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
