package css

import (
	"iter"

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
	for _, host := range s.order {
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

// treeRoot returns the root of n's node tree: the document node, a shadow
// root, or the top of a detached subtree.
func treeRoot(n *html.Node) *html.Node {
	for n.Parent != nil {
		n = n.Parent
	}
	return n
}
