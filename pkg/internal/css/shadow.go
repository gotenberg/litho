package css

import (
	"iter"
	"strings"

	"golang.org/x/net/html"
)

// shadowRootData marks the node that stands for a shadow root: a document
// node detached from every tree, the parent of the shadow tree's
// top-level nodes. Detached, it stops the matcher's combinators at the
// shadow tree's edge.
const shadowRootData = "#shadow-root"

// NewShadowRoot returns a node that stands for a shadow root, to which the
// top-level nodes of a shadow tree are appended.
func NewShadowRoot() *html.Node {
	return &html.Node{Type: html.DocumentNode, Data: shadowRootData}
}

// isShadowRoot reports whether n stands for a shadow root.
func isShadowRoot(n *html.Node) bool {
	return n != nil && n.Type == html.DocumentNode && n.Data == shadowRootData
}

// Shadows holds the shadow trees of a document and the assignment of its
// slots (DOM Standard, 4.2.2): the node trees stay as parsed, each shadow
// tree under a root of its own, and the flat tree is a view over them
// (CSS Scoping 1, 2.2). A nil *Shadows is a document without shadow
// trees, whose flat tree is its tree. A Shadows is built once, by
// [Shadows.Attach] then [Shadows.AssignSlots], and read only afterwards:
// it is then safe for concurrent use.
type Shadows struct {
	roots map[*html.Node]*html.Node
	hosts map[*html.Node]*html.Node
	// assigned holds each slot's assigned nodes, in tree order; slotOf
	// the slot each assigned node is assigned to.
	assigned map[*html.Node][]*html.Node
	slotOf   map[*html.Node]*html.Node
	// order lists the hosts as they were attached, which keeps every walk
	// over them in document order.
	order []*html.Node
	// depths holds each shadow root's depth: 1 for a tree whose host is
	// in the document's, one more for each tree around it.
	depths map[*html.Node]int
}

// Attach records root, made by [NewShadowRoot], as the shadow root of
// host. It reports false, recording nothing, when host has one already
// (DOM Standard, 4.2.14: a host has one shadow root).
func (s *Shadows) Attach(host, root *html.Node) bool {
	if s.roots == nil {
		s.roots, s.hosts = map[*html.Node]*html.Node{}, map[*html.Node]*html.Node{}
	}
	if _, ok := s.roots[host]; ok {
		return false
	}
	s.roots[host], s.hosts[root] = root, host
	s.order = append(s.order, host)
	return true
}

// Len returns the number of shadow roots.
func (s *Shadows) Len() int {
	if s == nil {
		return 0
	}
	return len(s.order)
}

// Root returns the shadow root of host, nil for an element that hosts
// none.
func (s *Shadows) Root(host *html.Node) *html.Node {
	if s == nil {
		return nil
	}
	return s.roots[host]
}

// Host returns the host of the shadow root root, nil for any other node.
func (s *Shadows) Host(root *html.Node) *html.Node {
	if s == nil {
		return nil
	}
	return s.hosts[root]
}

// Roots returns the shadow roots in the order they were attached, a host's
// before the roots inside its shadow tree.
func (s *Shadows) Roots() iter.Seq[*html.Node] {
	return func(yield func(*html.Node) bool) {
		if s == nil {
			return
		}
		for _, h := range s.order {
			if !yield(s.roots[h]) {
				return
			}
		}
	}
}

// AssignSlots assigns every host's element and text children to the slots
// of its shadow tree (DOM Standard, 4.2.2.3 and 4.2.2.4): a child goes to
// the first slot, in tree order, whose name is its slot attribute, the
// empty string for a text node and for an element without one. A slot
// inside a template's contents, or inside a nested shadow tree, is no
// slot of this tree.
func (s *Shadows) AssignSlots() {
	if s == nil {
		return
	}
	s.assigned, s.slotOf = map[*html.Node][]*html.Node{}, map[*html.Node]*html.Node{}
	s.depths = map[*html.Node]int{}
	for _, host := range s.order {
		// A host's tree was attached before the trees inside it.
		s.depths[s.roots[host]] = s.depths[treeScope(host)] + 1
		slots := map[string]*html.Node{}
		stack := []*html.Node{s.roots[host]}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if n.Type == html.ElementNode && n.Namespace == "" {
				if n.Data == "template" {
					continue
				}
				if n.Data == "slot" {
					name, _ := attr(n, "name")
					if _, ok := slots[name]; !ok {
						slots[name] = n
					}
				}
			}
			for c := n.LastChild; c != nil; c = c.PrevSibling {
				stack = append(stack, c)
			}
		}
		for c := host.FirstChild; c != nil; c = c.NextSibling {
			name := ""
			switch c.Type { //nolint:exhaustive // only elements and text are slottables
			case html.ElementNode:
				name, _ = attr(c, "slot")
			case html.TextNode:
			default:
				continue
			}
			if slot := slots[name]; slot != nil {
				s.assigned[slot] = append(s.assigned[slot], c)
				s.slotOf[c] = slot
			}
		}
	}
}

// depth returns the depth of the tree whose shadow root is root, 0 for the
// document's (nil) and for a nil s.
func (s *Shadows) depth(root *html.Node) int {
	if s == nil || root == nil {
		return 0
	}
	return s.depths[root]
}

// InShadowTree reports whether n is a node of a shadow tree.
func (s *Shadows) InShadowTree(n *html.Node) bool {
	return s != nil && treeScope(n) != nil
}

// Slot returns the slot n is assigned to, nil for none.
func (s *Shadows) Slot(n *html.Node) *html.Node {
	if s == nil {
		return nil
	}
	return s.slotOf[n]
}

// isSlot reports whether n is a slot of a shadow tree: a slot element
// outside every shadow tree is a plain element.
func (s *Shadows) isSlot(n *html.Node) bool {
	return s != nil && len(s.roots) > 0 && n.Type == html.ElementNode && n.Namespace == "" && n.Data == "slot" && isShadowRoot(treeRoot(n))
}

// Children returns the children of n in the flat tree (CSS Scoping 1,
// 2.2): a host's are its shadow root's children, a slot's its assigned
// nodes or, when it has none, its own children (its fallback content),
// anyone else's its own children.
func (s *Shadows) Children(n *html.Node) iter.Seq[*html.Node] {
	return func(yield func(*html.Node) bool) {
		if r := s.Root(n); r != nil {
			n = r
		} else if s.isSlot(n) {
			if as := s.assigned[n]; len(as) > 0 {
				for _, c := range as {
					if !yield(c) {
						return
					}
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if !yield(c) {
				return
			}
		}
	}
}

// treeScope returns the shadow root of the tree n is in, nil for the
// document's.
func treeScope(n *html.Node) *html.Node {
	if r := treeRoot(n); isShadowRoot(r) {
		return r
	}
	return nil
}

// treeRoot returns the root of n's node tree: the document node, a shadow
// root, or the top of a detached subtree.
func treeRoot(n *html.Node) *html.Node {
	for n.Parent != nil {
		n = n.Parent
	}
	return n
}

// matchShadow adds, through addAt at their scope proximity, the rules of
// other trees that match the element n, or its pseudo-element of index
// x: a host's own shadow tree's rules that match it as a featureless host
// (CSS Scoping 1, 3.2.1), the rules of an @scope rule without a prelude,
// whose root it is, among them, and for an element the ::slotted() rules
// of the trees of the slots it is assigned to, through slots assigned to
// slots (3.2.2), and the ::part() rules of the trees its part names reach
// through its hosts' exportparts (CSS Shadow Parts 1, 4.1 and 5).
func (cs *computeState) matchShadow(n *html.Node, x *selIndex, addAt func(selRef, int32)) {
	sh, m := cs.opts.Shadows, cs.m
	add := func(ref selRef) {
		if cs.r.rules[ref.rule].rule.scope == nil {
			addAt(ref, noProximity)
		}
	}
	if root := sh.Root(n); root != nil {
		for _, ref := range x.universal {
			rr := &cs.r.rules[ref.rule]
			if rr.origin != Author || rr.scope != root {
				continue
			}
			if s := rr.rule.scope; s != nil {
				// The host is the root of an @scope rule of its shadow
				// tree without a prelude (CSS Cascade 6, 2.5.1).
				if s.start == nil && s.parent == nil && rr.owner != nil && rr.owner.Parent == root && m.anyMatchesHostScoped(ref.sel, n) {
					addAt(ref, 0)
				}
				continue
			}
			if m.matchesHost(ref.sel, n) {
				add(ref)
			}
		}
	}
	if x != &cs.r.elems {
		return
	}
	steps := 0
	for s := sh.Slot(n); s != nil && steps < maxShadowAncestors && !m.over; s, steps = sh.Slot(s), steps+1 {
		scope := treeScope(s)
		for _, ref := range cs.r.slotted {
			if cs.r.rules[ref.rule].scope != scope {
				continue
			}
			last := len(ref.sel.parts) - 1
			if m.anyMatches(ref.sel.parts[last].slotted, n) && m.matches(ref.sel, s) {
				add(ref)
			}
		}
	}
	if len(cs.r.parts) == 0 {
		return
	}
	v, ok := attr(n, "part")
	if !ok {
		return
	}
	names := strings.Fields(v)
	host := sh.Host(treeScope(n))
	for steps = 0; host != nil && len(names) > 0 && steps < maxShadowAncestors && !m.over; steps++ {
		outer := treeScope(host)
		for _, ref := range cs.r.parts {
			scope := cs.r.rules[ref.rule].scope
			if scope != outer && scope != sh.Root(host) || !holdsAll(names, ref.sel.parts[len(ref.sel.parts)-1].partNames) {
				continue
			}
			// The rules of the host's tree reach its part names, and
			// :host::part() rules of its shadow tree do too.
			if scope == outer && m.matches(ref.sel, host) || scope != outer && m.matchesHost(ref.sel, host) {
				add(ref)
			}
		}
		names = exportedParts(host, names)
		host = sh.Host(outer)
	}
}

// holdsAll reports whether names holds every name of want, compared
// case-sensitively (CSS Shadow Parts 1, 4.1).
func holdsAll(names, want []string) bool {
	for _, w := range want {
		found := false
		for _, n := range names {
			if n == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// exportedParts returns the part names the host's exportparts attribute
// gives, to the tree around host, the elements of its shadow tree that
// hold names (CSS Shadow Parts 1, 5): each mapping "inner: outer" or
// "name" whose inner name names holds exports its outer name; a mapping
// that does not parse is skipped.
func exportedParts(host *html.Node, names []string) []string {
	v, ok := attr(host, "exportparts")
	if !ok {
		return nil
	}
	var out []string
	for item := range strings.SplitSeq(v, ",") {
		inner, outer, mapped := strings.Cut(item, ":")
		inner, outer = strings.TrimSpace(inner), strings.TrimSpace(outer)
		if !mapped {
			outer = inner
		}
		if inner == "" || outer == "" || strings.ContainsAny(inner+outer, " \t\n\f\r:") {
			continue
		}
		if holdsAll(names, []string{inner}) {
			out = append(out, outer)
		}
	}
	return out
}

// reservedCustomNames are the names with a hyphen that are no valid
// custom element names (HTML Standard, 4.13.2).
var reservedCustomNames = map[string]bool{
	"annotation-xml": true, "color-profile": true, "font-face": true, "font-face-src": true,
	"font-face-uri": true, "font-face-format": true, "font-face-name": true, "missing-glyph": true,
}

// ValidCustomElementName reports whether name is a valid custom element
// name (HTML Standard, 4.13.2): a lowercase ASCII letter, then
// characters of PCENChar, a hyphen among them, and none of the reserved
// names. The parser lowercases the ASCII letters of an HTML element's
// name, so an uppercase letter left is none of its.
func ValidCustomElementName(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' || !strings.Contains(name, "-") || reservedCustomNames[name] {
		return false
	}
	for _, r := range name {
		if !pcenChar(r) {
			return false
		}
	}
	return true
}

// pcenChar reports whether r is a PCENChar (HTML Standard, 4.13.2).
func pcenChar(r rune) bool {
	switch {
	case r == '-' || r == '.' || r == '_' || r == 0xB7:
		return true
	case r >= '0' && r <= '9', r >= 'a' && r <= 'z':
		return true
	case r >= 0xC0 && r <= 0xD6, r >= 0xD8 && r <= 0xF6, r >= 0xF8 && r <= 0x37D,
		r >= 0x37F && r <= 0x1FFF, r >= 0x200C && r <= 0x200D, r >= 0x203F && r <= 0x2040,
		r >= 0x2070 && r <= 0x218F, r >= 0x2C00 && r <= 0x2FEF, r >= 0x3001 && r <= 0xD7FF,
		r >= 0xF900 && r <= 0xFDCF, r >= 0xFDF0 && r <= 0xFFFD, r >= 0x10000 && r <= 0xEFFFF:
		return true
	}
	return false
}
