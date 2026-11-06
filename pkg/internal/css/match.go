package css

import (
	"strings"

	"golang.org/x/net/html"
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
}

// matcher matches selectors against elements, charging each compound test
// to a step budget shared with the rest of the cascade.
type matcher struct {
	info     map[*html.Node]*elemInfo
	docLang  string
	steps    int
	maxSteps int
	over     bool
}

func newMatcher(docLang string, maxSteps int) *matcher {
	return &matcher{info: map[*html.Node]*elemInfo{}, docLang: lower(docLang), maxSteps: maxSteps}
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

// sameName compares element or attribute names: ASCII case-insensitively
// for HTML elements in an HTML document, exactly otherwise (Selectors 4,
// 5.1 and HTML, 4.16.2).
func sameName(n *html.Node, got, want string) bool {
	if n.Namespace == "" {
		return got == want || eqFold(got, want)
	}
	return eqFold(got, want)
}

// attr returns the value of n's attribute name with no namespace.
func attr(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && sameName(n, a.Key, name) {
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
	for k := len(chain) - 1; k >= 0; k-- {
		e := chain[k]
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
	if !m.charge(1) || c.never {
		return false
	}
	if c.tag != "" && !sameName(n, n.Data, c.tag) {
		return false
	}
	for _, id := range c.ids {
		if v, ok := attr(n, "id"); !ok || v != id {
			return false
		}
	}
	if len(c.classes) > 0 {
		v, ok := attr(n, "class")
		if !ok {
			return false
		}
		for _, want := range c.classes {
			if !hasWord(v, want) {
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
	fold := a.fold || (!a.sensitive && n.Namespace == "" && caseInsensitiveAttrs[a.name])
	if fold {
		v, want = lower(v), lower(want)
	}
	switch a.op {
	case attrExists:
		return true
	case attrEquals:
		return v == want
	case attrIncludes:
		if want == "" || strings.ContainsAny(want, " \t\n\f\r") {
			return false
		}
		for _, w := range strings.Fields(v) {
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
		return n.Parent != nil && n.Parent.Type == html.DocumentNode
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
		lang := m.langOf(n)
		for _, r := range p.langs {
			if r == "*" && lang != "" || lang == r || strings.HasPrefix(lang, r+"-") {
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
	}
	return false
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

// hasWord reports whether the whitespace-separated list holds word,
// without splitting it.
func hasWord(list, word string) bool {
	for len(list) > 0 {
		i := 0
		for i < len(list) && isASCIIWS(list[i]) {
			i++
		}
		j := i
		for j < len(list) && !isASCIIWS(list[j]) {
			j++
		}
		if list[i:j] == word && j > i {
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
	counts [1 << 12]uint32
}

const bloomMask = 1<<12 - 1

// featureHash hashes a feature: kind is 't', '#' or '.'.
func featureHash(kind byte, s string) uint64 {
	h := uint64(14695981039346656037)
	h = (h ^ uint64(kind)) * 1099511628211
	for i := 0; i < len(s); i++ {
		h = (h ^ uint64(s[i])) * 1099511628211
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
		for _, c := range strings.Fields(cl) {
			out = append(out, featureHash('.', c))
		}
	}
	return out
}

func (f *ancestorFilter) push(n *html.Node) {
	for _, h := range elementFeatures(n) {
		f.counts[h&bloomMask]++
		f.counts[h>>12&bloomMask]++
	}
}

func (f *ancestorFilter) pop(n *html.Node) {
	for _, h := range elementFeatures(n) {
		f.counts[h&bloomMask]--
		f.counts[h>>12&bloomMask]--
	}
}

// admits reports whether every feature in need may be present.
func (f *ancestorFilter) admits(need []uint64) bool {
	for _, h := range need {
		if f.counts[h&bloomMask] == 0 || f.counts[h>>12&bloomMask] == 0 {
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
		if p.never {
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
