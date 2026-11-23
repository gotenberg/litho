package css

import (
	"iter"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/text/unicode/bidi"
)

// elemInfo caches what the structural pseudo-classes and :lang() ask of
// an element.
type elemInfo struct {
	// index and count are the 1-based position among the element
	// siblings and their number; typeIndex and typeCount the same among
	// the siblings of the same type.
	index, count         int
	typeIndex, typeCount int
	lang                 string
	langDone             bool
	// rtl is the element's directionality (HTML, 3.2.6.4), set once
	// dirDone.
	rtl, dirDone bool
}

// matcher matches selectors against elements, charging each compound test
// to a step budget shared with the rest of the cascade.
type matcher struct {
	info     map[*html.Node]*elemInfo
	docLang  string
	steps    int
	maxSteps int
	over     bool
	// has holds what each :has() found for each element it was tested
	// on: the subtree it searches is searched once.
	has map[hasProbe]bool
	// quirks matches class and ID selectors ASCII case-insensitively, as
	// a document in quirks mode asks (Selectors 4, 6.6 and 6.7).
	quirks bool
}

// hasProbe names one :has() tested on one element.
type hasProbe struct {
	p *pseudoSel
	n *html.Node
}

func newMatcher(docLang string, maxSteps int, quirks bool) *matcher {
	return &matcher{info: map[*html.Node]*elemInfo{}, docLang: lower(docLang), maxSteps: maxSteps, quirks: quirks}
}

// charge spends n steps, reporting false once the budget is gone.
func (m *matcher) charge(n int) bool {
	if m.over {
		return false
	}
	m.steps += n
	if m.steps > m.maxSteps {
		m.over = true
		return false
	}
	return true
}

func isElement(n *html.Node) bool { return n != nil && n.Type == html.ElementNode }

func parentElement(n *html.Node) *html.Node {
	if p := n.Parent; isElement(p) {
		return p
	}
	return nil
}

func prevElement(n *html.Node) *html.Node {
	for s := n.PrevSibling; s != nil; s = s.PrevSibling {
		if s.Type == html.ElementNode {
			return s
		}
	}
	return nil
}

// attr returns the value of n's attribute name with no namespace, the
// names compared ASCII case-insensitively.
func attr(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && eqFold(a.Key, name) {
			return a.Val, true
		}
	}
	return "", false
}

func hasAttr(n *html.Node, name string) bool {
	_, ok := attr(n, name)
	return ok
}

// isHTML reports whether n is the HTML element tag.
func isHTML(n *html.Node, tag string) bool {
	return n.Type == html.ElementNode && n.Namespace == "" && n.Data == tag
}

// infoOf returns n's cached structural data, filling it for all of n's
// siblings at once.
func (m *matcher) infoOf(n *html.Node) *elemInfo {
	if i, ok := m.info[n]; ok {
		return i
	}
	first := n
	for first.PrevSibling != nil {
		first = first.PrevSibling
	}
	var sibs []*html.Node
	byType := map[string]int{}
	for s := first; s != nil; s = s.NextSibling {
		if s.Type != html.ElementNode {
			continue
		}
		sibs = append(sibs, s)
		byType[lower(s.Data)]++
	}
	m.charge(len(sibs))
	seen := map[string]int{}
	for i, s := range sibs {
		t := lower(s.Data)
		seen[t]++
		m.info[s] = &elemInfo{index: i + 1, count: len(sibs), typeIndex: seen[t], typeCount: byType[t]}
	}
	if i, ok := m.info[n]; ok {
		return i
	}
	i := &elemInfo{index: 1, count: 1, typeIndex: 1, typeCount: 1}
	m.info[n] = i
	return i
}

// langOf returns the language of n: its lang or xml:lang attribute, or
// its parent's, or the document's (HTML, 3.2.6.2).
func (m *matcher) langOf(n *html.Node) string {
	if i := m.infoOf(n); i.langDone {
		return i.lang
	}
	var chain []*html.Node
	inherited := m.docLang
	for e := n; isElement(e); e = e.Parent {
		if i := m.infoOf(e); i.langDone {
			inherited = i.lang
			break
		}
		chain = append(chain, e)
	}
	m.charge(len(chain))
	for _, e := range slices.Backward(chain) {
		lang := inherited
		if v, ok := attr(e, "xml:lang"); ok {
			lang = lower(strings.TrimSpace(v))
		} else if v, ok := attr(e, "lang"); ok {
			lang = lower(strings.TrimSpace(v))
		}
		i := m.info[e]
		i.lang, i.langDone = lang, true
		inherited = lang
	}
	return inherited
}

// matches reports whether the element n matches s, ignoring a
// pseudo-element s targets.
func (m *matcher) matches(s *complexSel, n *html.Node) bool {
	return m.matchFrom(s, len(s.parts)-1, n)
}

// matchFrom matches parts[:i+1] of s right to left, n standing for
// parts[i] (Selectors 4, 16).
func (m *matcher) matchFrom(s *complexSel, i int, n *html.Node) bool {
	if !m.compound(&s.parts[i], n) {
		return false
	}
	if i == 0 {
		return true
	}
	switch s.combs[i-1] {
	case '>':
		p := parentElement(n)
		return p != nil && m.matchFrom(s, i-1, p)
	case '+':
		p := prevElement(n)
		return p != nil && m.matchFrom(s, i-1, p)
	case '~':
		for p := prevElement(n); p != nil && !m.over; p = prevElement(p) {
			if m.matchFrom(s, i-1, p) {
				return true
			}
		}
	default:
		for p := parentElement(n); p != nil && !m.over; p = parentElement(p) {
			if m.matchFrom(s, i-1, p) {
				return true
			}
		}
	}
	return false
}

func (m *matcher) anyMatches(list []*complexSel, n *html.Node) bool {
	for _, s := range list {
		if s.pseudoElement == "" && m.matches(s, n) {
			return true
		}
	}
	return false
}

// compound matches one compound selector.
func (m *matcher) compound(c *compound, n *html.Node) bool {
	if !m.charge(1) || c.never || !c.ns.holds(n) {
		return false
	}
	// Names compare ASCII case-insensitively (Selectors 4, 5.1 and HTML,
	// 4.16.2), a foreign element's too: the selector's names are
	// lowercased when it parses, and SVG names such as foreignObject keep
	// their case in the tree.
	if c.tag != "" && !eqFold(n.Data, c.tag) {
		return false
	}
	for _, id := range c.ids {
		if v, ok := attr(n, "id"); !ok || v != id && (!m.quirks || !eqFold(v, id)) {
			return false
		}
	}
	if len(c.classes) > 0 {
		v, ok := attr(n, "class")
		if !ok {
			return false
		}
		for _, want := range c.classes {
			if !hasWord(v, want, m.quirks) {
				return false
			}
		}
	}
	for i := range c.attrs {
		if !matchAttr(&c.attrs[i], n) {
			return false
		}
	}
	for i := range c.pseudos {
		if !m.pseudo(&c.pseudos[i], n) {
			return false
		}
	}
	return true
}

// caseInsensitiveAttrs are the HTML attributes whose values selectors
// compare ASCII case-insensitively in an HTML document (HTML, 4.16.2).
var caseInsensitiveAttrs = map[string]bool{
	"accept": true, "accept-charset": true, "align": true, "alink": true, "axis": true,
	"bgcolor": true, "charset": true, "checked": true, "clear": true, "codetype": true,
	"color": true, "compact": true, "declare": true, "defer": true, "dir": true,
	"direction": true, "disabled": true, "enctype": true, "face": true, "frame": true,
	"hreflang": true, "http-equiv": true, "lang": true, "language": true, "link": true,
	"media": true, "method": true, "multiple": true, "nohref": true, "noresize": true,
	"noshade": true, "nowrap": true, "readonly": true, "rel": true, "rev": true,
	"rules": true, "scope": true, "scrolling": true, "selected": true, "shape": true,
	"target": true, "text": true, "type": true, "valign": true, "valuetype": true,
	"vlink": true,
}

// matchAttr matches an attribute selector (Selectors 4, 6).
func matchAttr(a *attrSel, n *html.Node) bool {
	v, ok := attr(n, a.name)
	if !ok {
		return false
	}
	want := a.value
	fold := a.fold || n.Namespace == "" && caseInsensitiveAttrs[a.name]
	if fold {
		v, want = lower(v), lower(want)
	}
	switch a.op {
	case attrExists:
		return true
	case attrEquals:
		return v == want
	case attrIncludes:
		if want == "" || strings.ContainsAny(want, asciiWhitespace) {
			return false
		}
		for w := range asciiFields(v) {
			if w == want {
				return true
			}
		}
		return false
	case attrDash:
		return v == want || strings.HasPrefix(v, want+"-")
	case attrPrefix:
		return want != "" && strings.HasPrefix(v, want)
	case attrSuffix:
		return want != "" && strings.HasSuffix(v, want)
	case attrSubstring:
		return want != "" && strings.Contains(v, want)
	}
	return false
}

// formElements are the elements :disabled and :enabled apply to (HTML,
// 4.16.3).
var formElements = map[string]bool{
	"button": true, "input": true, "select": true, "textarea": true,
	"optgroup": true, "option": true, "fieldset": true,
}

// disabled reports whether n is a disabled form element (HTML, 4.10.18.5),
// counting a disabled ancestor fieldset outside its first legend and a
// disabled parent optgroup.
func disabled(n *html.Node) bool {
	if hasAttr(n, "disabled") {
		return true
	}
	if isHTML(n, "option") {
		if p := parentElement(n); p != nil && isHTML(p, "optgroup") && hasAttr(p, "disabled") {
			return true
		}
	}
	child := n
	for p := parentElement(n); p != nil; p = parentElement(p) {
		if isHTML(p, "fieldset") && hasAttr(p, "disabled") {
			first := p.FirstChild
			for first != nil && !isHTML(first, "legend") {
				first = first.NextSibling
			}
			if first != child {
				return true
			}
		}
		child = p
	}
	return false
}

// pseudo matches a pseudo-class (Selectors 4, 4 to 15).
func (m *matcher) pseudo(p *pseudoSel, n *html.Node) bool {
	switch p.kind { //nolint:exhaustive // psNever and the rest match nothing
	case psRoot:
		// The top-level elements of a shadow tree are no root: their
		// parent is the shadow root (CSS Scoping 1, 3.1).
		return n.Parent != nil && n.Parent.Type == html.DocumentNode && !isShadowRoot(n.Parent)
	case psFirstChild:
		return m.infoOf(n).index == 1
	case psLastChild:
		i := m.infoOf(n)
		return i.index == i.count
	case psOnlyChild:
		return m.infoOf(n).count == 1
	case psFirstOfType:
		return m.infoOf(n).typeIndex == 1
	case psLastOfType:
		i := m.infoOf(n)
		return i.typeIndex == i.typeCount
	case psOnlyOfType:
		return m.infoOf(n).typeCount == 1
	case psNthChild, psNthLastChild:
		if p.of != nil {
			return m.nthOf(p, n)
		}
		i := m.infoOf(n)
		idx := i.index
		if p.kind == psNthLastChild {
			idx = i.count - i.index + 1
		}
		return nthMatches(p.a, p.b, idx)
	case psNthOfType:
		return nthMatches(p.a, p.b, m.infoOf(n).typeIndex)
	case psNthLastOfType:
		i := m.infoOf(n)
		return nthMatches(p.a, p.b, i.typeCount-i.typeIndex+1)
	case psEmpty:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode || c.Type == html.TextNode {
				return false
			}
		}
		return true
	case psNot:
		return !m.anyMatches(p.args, n)
	case psIs, psWhere:
		return m.anyMatches(p.args, n)
	case psLink:
		return n.Namespace == "" && (n.Data == "a" || n.Data == "area" || n.Data == "link") && hasAttr(n, "href")
	case psLang:
		// A range matches the language it names and its subtags, as
		// CSS 2.1, 5.11.4 and Chromium match it: Selectors 4, 7.2's
		// extended filtering, wildcards included, is not Chromium's.
		lang := m.langOf(n)
		for _, r := range p.langs {
			if lang == r || strings.HasPrefix(lang, r+"-") {
				return true
			}
		}
		return false
	case psChecked:
		if isHTML(n, "input") {
			t, _ := attr(n, "type")
			t = lower(t)
			return (t == "checkbox" || t == "radio") && hasAttr(n, "checked")
		}
		return isHTML(n, "option") && hasAttr(n, "selected")
	case psDisabled:
		return n.Namespace == "" && formElements[n.Data] && disabled(n)
	case psEnabled:
		return n.Namespace == "" && formElements[n.Data] && !disabled(n)
	case psAlways:
		return true
	case psHas:
		return m.hasMatch(p, n)
	case psDir:
		return p.a < 2 && m.rtlOf(n) == (p.a == 1)
	case psRequired, psOptional, psReadOnly, psReadWrite, psPlaceholderShown, psDefault,
		psIndeterminate, psValid, psInvalid, psInRange, psOutOfRange, psOpen:
		return formState(p.kind, n)
	}
	return false
}

// hasMatch matches :has() (Selectors 4, 4.5): some element in the relation
// its relative selector starts with to n, a descendant, a child, the next
// sibling or a following sibling, matches it. The elements searched are
// those the relation reaches and their descendants, each tested once per
// :has() and n; the cascade's step budget bounds the rest, a selector
// that searches past it failing the cascade.
func (m *matcher) hasMatch(p *pseudoSel, n *html.Node) bool {
	k := hasProbe{p, n}
	if v, ok := m.has[k]; ok {
		return v
	}
	found := false
	for _, s := range p.args {
		if found || m.over {
			break
		}
		last := len(s.parts) - 1
		var visit func(c *html.Node, depth int)
		visit = func(c *html.Node, depth int) {
			for ; c != nil && !found && !m.over; c = c.NextSibling {
				if c.Type != html.ElementNode {
					continue
				}
				if m.matchRel(s, last, c, n) {
					found = true
					return
				}
				if (last > 0 || s.lead == ' ') && depth < maxHasDepth {
					visit(c.FirstChild, depth+1)
				}
			}
		}
		switch s.lead {
		case '>', ' ':
			visit(n.FirstChild, 0)
		default:
			for c := n.NextSibling; c != nil && !found && !m.over; c = c.NextSibling {
				if c.Type != html.ElementNode {
					continue
				}
				if m.matchRel(s, last, c, n) {
					found = true
				} else if last > 0 {
					visit(c.FirstChild, 1)
				}
				if s.lead == '+' {
					break
				}
			}
		}
	}
	if m.has == nil {
		m.has = map[hasProbe]bool{}
	}
	m.has[k] = found
	return found
}

// maxHasDepth bounds the depth below its anchor :has() searches.
// Anti-DoS guard, not spec-mandated: the tree is no deeper than the
// parser's own nesting cap allows, far beyond real documents.
const maxHasDepth = 512

// matchRel matches parts[:i+1] of the relative selector s right to left,
// n standing for parts[i], its leftmost compound in the relation s.lead
// to anchor.
func (m *matcher) matchRel(s *complexSel, i int, n, anchor *html.Node) bool {
	if !m.compound(&s.parts[i], n) {
		return false
	}
	if i == 0 {
		switch s.lead {
		case '>':
			return n.Parent == anchor
		case '+':
			return prevElement(n) == anchor
		case '~':
			for p := prevElement(n); p != nil; p = prevElement(p) {
				if p == anchor {
					return true
				}
			}
			return false
		}
		for p := n.Parent; p != nil; p = p.Parent {
			if p == anchor {
				return true
			}
		}
		return false
	}
	switch s.combs[i-1] {
	case '>':
		p := parentElement(n)
		return p != nil && p != anchor && m.matchRel(s, i-1, p, anchor)
	case '+':
		p := prevElement(n)
		return p != nil && m.matchRel(s, i-1, p, anchor)
	case '~':
		for p := prevElement(n); p != nil && !m.over; p = prevElement(p) {
			if m.matchRel(s, i-1, p, anchor) {
				return true
			}
		}
	default:
		for p := parentElement(n); p != nil && p != anchor && !m.over; p = parentElement(p) {
			if m.matchRel(s, i-1, p, anchor) {
				return true
			}
		}
	}
	return false
}

// rtlOf reports whether the directionality of n is rtl (HTML, 3.2.6.4): its
// dir attribute's, ltr or rtl, the direction of its first strong
// character for auto, else its parent's; the root's is ltr.
func (m *matcher) rtlOf(n *html.Node) bool {
	if i := m.infoOf(n); i.dirDone {
		return i.rtl
	}
	var chain []*html.Node
	inherited := false
	for e := n; isElement(e); e = e.Parent {
		if i := m.infoOf(e); i.dirDone {
			inherited = i.rtl
			break
		}
		chain = append(chain, e)
	}
	m.charge(len(chain))
	for _, e := range slices.Backward(chain) {
		rtl := inherited
		v, ok := attr(e, "dir")
		switch v = lower(strings.TrimSpace(v)); {
		case ok && v == "ltr":
			rtl = false
		case ok && v == "rtl":
			rtl = true
		case ok && v == "auto" || isHTML(e, "bdi") && !ok:
			rtl = m.autoRTL(e)
		}
		i := m.info[e]
		i.rtl, i.dirDone = rtl, true
		inherited = rtl
	}
	return inherited
}

// autoRTL reports whether the first character of bidi class L, R or AL in
// the text of e, past the descendants that set their own direction and
// those whose text does not count, is R or AL (HTML, 3.2.6.4, the auto
// directionality); false when there is none.
func (m *matcher) autoRTL(e *html.Node) bool {
	rtl, _ := m.strongIn(e, 0)
	return rtl
}

// strongIn returns whether the first strong character under e, at depth
// below the element auto directionality is found for, is right to left,
// and whether there is one.
func (m *matcher) strongIn(e *html.Node, depth int) (rtl, found bool) {
	if depth >= maxHasDepth {
		return false, false
	}
	for c := e.FirstChild; c != nil; c = c.NextSibling {
		if !m.charge(1) {
			return false, false
		}
		switch c.Type { //nolint:exhaustive // comments and the rest hold no text
		case html.TextNode:
			for _, r := range c.Data {
				props, _ := bidi.LookupRune(r)
				switch props.Class() { //nolint:exhaustive // the other classes are weak or neutral
				case bidi.L:
					return false, true
				case bidi.R, bidi.AL:
					return true, true
				}
			}
		case html.ElementNode:
			if c.Namespace != "" || hasAttr(c, "dir") || c.Data == "bdi" || c.Data == "script" || c.Data == "style" || c.Data == "textarea" {
				continue
			}
			if rtl, ok := m.strongIn(c, depth+1); ok {
				return rtl, true
			}
		}
	}
	return false, false
}

// nthOf matches :nth-child(An+B of S) and :nth-last-child(An+B of S): the
// index counts the siblings that match S (Selectors 4, 14.3.1).
func (m *matcher) nthOf(p *pseudoSel, n *html.Node) bool {
	if !m.anyMatches(p.of, n) {
		return false
	}
	idx := 1
	step := func(s *html.Node) *html.Node { return s.PrevSibling }
	if p.kind == psNthLastChild {
		step = func(s *html.Node) *html.Node { return s.NextSibling }
	}
	for s := step(n); s != nil && !m.over; s = step(s) {
		if s.Type == html.ElementNode && m.anyMatches(p.of, s) {
			idx++
		}
	}
	return nthMatches(p.a, p.b, idx)
}

// asciiWhitespace separates the words of a class list or of an attribute
// value ~= reads (HTML, 2.3.8; Selectors 4, 6.3).
const asciiWhitespace = " \t\n\f\r"

// asciiFields iterates the words of s split on ASCII whitespace: a
// no-break space stays inside a word.
func asciiFields(s string) iter.Seq[string] {
	return strings.FieldsFuncSeq(s, func(r rune) bool { return strings.ContainsRune(asciiWhitespace, r) })
}

// hasWord reports whether the whitespace-separated list holds word,
// without splitting it, ASCII case-insensitively when fold is set.
func hasWord(list, word string, fold bool) bool {
	for len(list) > 0 {
		i := 0
		for i < len(list) && isASCIIWS(list[i]) {
			i++
		}
		j := i
		for j < len(list) && !isASCIIWS(list[j]) {
			j++
		}
		if j > i && (list[i:j] == word || fold && eqFold(list[i:j], word)) {
			return true
		}
		list = list[j:]
	}
	return false
}

// ancestorFilter is a counting Bloom filter of the tag, id and class
// features of the ancestors of the element being styled, so a selector
// needing an ancestor feature none has is rejected without walking the
// tree, as browsers do. A false positive only sends a selector on to the
// full match.
type ancestorFilter struct {
	counts [1 << bloomBits]uint32
}

// bloomBits is the width of a slot index of [ancestorFilter]: a feature
// hash gives two indexes, its low bloomBits bits and the next ones.
const (
	bloomBits = 12
	bloomMask = 1<<bloomBits - 1
)

// fnvOffset64 and fnvPrime64 are the 64-bit FNV-1a offset basis and
// prime.
const (
	fnvOffset64 = 0xcbf29ce484222325
	fnvPrime64  = 0x100000001b3
)

// featureHash hashes a feature: kind is 't', '#' or '.'.
func featureHash(kind byte, s string) uint64 {
	h := uint64(fnvOffset64)
	h = (h ^ uint64(kind)) * fnvPrime64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			// ASCII lowercase, as an ID or a class of a document in quirks
			// mode matches: the filter only rules candidates out.
			c += 'a' - 'A'
		}
		h = (h ^ uint64(c)) * fnvPrime64
	}
	return h
}

// elementFeatures returns the feature hashes of n.
func elementFeatures(n *html.Node) []uint64 {
	out := []uint64{featureHash('t', lower(n.Data))}
	if id, ok := attr(n, "id"); ok {
		out = append(out, featureHash('#', id))
	}
	if cl, ok := attr(n, "class"); ok {
		for c := range asciiFields(cl) {
			out = append(out, featureHash('.', c))
		}
	}
	return out
}

func (f *ancestorFilter) push(n *html.Node) {
	for _, h := range elementFeatures(n) {
		f.counts[h&bloomMask]++
		f.counts[h>>bloomBits&bloomMask]++
	}
}

func (f *ancestorFilter) pop(n *html.Node) {
	for _, h := range elementFeatures(n) {
		f.counts[h&bloomMask]--
		f.counts[h>>bloomBits&bloomMask]--
	}
}

// admits reports whether every feature in need may be present.
func (f *ancestorFilter) admits(need []uint64) bool {
	for _, h := range need {
		if f.counts[h&bloomMask] == 0 || f.counts[h>>bloomBits&bloomMask] == 0 {
			return false
		}
	}
	return true
}

// ancestorFeatures returns the features some ancestor of the subject must
// have for s to match: those of every compound joined to its right by a
// descendant or child combinator (Selectors 4, 15).
func ancestorFeatures(s *complexSel) []uint64 {
	var out []uint64
	for i := 0; i < len(s.parts)-1; i++ {
		if c := s.combs[i]; c != ' ' && c != '>' {
			continue
		}
		p := &s.parts[i]
		if p.never || p.ns == nsUnknown {
			continue
		}
		if p.tag != "" {
			out = append(out, featureHash('t', p.tag))
		}
		for _, id := range p.ids {
			out = append(out, featureHash('#', id))
		}
		for _, c := range p.classes {
			out = append(out, featureHash('.', c))
		}
	}
	return out
}
