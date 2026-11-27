package css

import (
	"strconv"

	"golang.org/x/net/html"
)

// counterScopes holds the @counter-style rules of shadow trees: each
// rule of an author sheet scoped to a shadow tree is registered under a
// name of its own, which no rule of another tree and no author can write,
// and a counter style name a declaration of the tree, or of a tree inside
// it, refers to resolves to it (CSS Scoping 1, 3.5).
type counterScopes struct {
	// ids numbers the shadow roots whose sheets define counter styles.
	ids map[*html.Node]int
	// names maps a name a tree defines to the name it is registered
	// under, and trees each registered name to its tree.
	names map[scopedName]string
	trees map[string]*html.Node
}

// scopedCounterName returns the name the counter style name of the tree
// numbered id is registered under: a NUL cannot appear in an identifier
// a sheet writes (CSS Syntax 3, 3.3 replaces it), so no rule of the
// document's sheets names it.
func scopedCounterName(id int, name string) string {
	return "\x00" + strconv.Itoa(id) + "\x00" + name
}

// add registers the @counter-style rule cr of the tree of the shadow root
// scope and returns a copy of it under its scoped name; the copy's own
// references are scoped by [counterScopes.rescope].
func (c *counterScopes) add(scope *html.Node, cr *counterStyleRule) *counterStyleRule {
	if c.ids == nil {
		c.ids, c.names, c.trees = map[*html.Node]int{}, map[scopedName]string{}, map[string]*html.Node{}
	}
	id, ok := c.ids[scope]
	if !ok {
		id = len(c.ids)
		c.ids[scope] = id
	}
	key := scopedCounterName(id, cr.name)
	c.names[scopedName{scope: scope, name: cr.name}] = key
	c.trees[key] = scope
	out := *cr
	out.name = key
	return &out
}

// rescope points the extends and fallback names of the scoped rules to
// the styles of their own tree where it defines them, else to the
// document's: a rule's references are resolved from its tree (CSS
// Scoping 1, 3.5). A tree nested in another reaches the document's
// styles, not the outer tree's: a rule knows its tree, not the host of
// the tree.
func (c *counterScopes) rescope(rules []*counterStyleRule) {
	if len(c.names) == 0 {
		return
	}
	for _, r := range rules {
		scope, ok := c.trees[r.name]
		if !ok {
			continue
		}
		if k, ok := c.names[scopedName{scope: scope, name: r.extends}]; ok && r.extends != "" {
			r.extends = k
		}
		if k, ok := c.names[scopedName{scope: scope, name: r.fallback}]; ok && r.fallback != "" {
			r.fallback = k
		}
	}
}

// counterNameOf returns the name the counter style name, referred to by a
// declaration of the tree of the shadow root scope (nil for the
// document's), resolves to: the style of that tree, else of the nearest
// tree around it that defines one, else the document's name as written.
func (cs *computeState) counterNameOf(name string, scope *html.Node) string {
	names := cs.r.counterScopes.names
	for depth := 0; scope != nil && depth <= maxShadowAncestors; depth++ {
		if k, ok := names[scopedName{scope: scope, name: name}]; ok {
			return k
		}
		h := cs.opts.Shadows.Host(scope)
		if h == nil {
			break
		}
		scope = treeScope(h)
	}
	return name
}

// scopeCounterNames rewrites the counter style names st holds that the
// declarations of list-style-type and content refer to from the trees of
// listScope and contentScope, nil for the document's, to the styles they
// resolve to ([computeState.counterNameOf]). st is copied before it
// changes; the copy is returned.
func (cs *computeState) scopeCounterNames(st *Style, listScope, contentScope *html.Node) *Style {
	if len(cs.r.counterScopes.names) == 0 || listScope == nil && contentScope == nil {
		return st
	}
	out := st
	if listScope != nil && st.ListStyleType == ListNamed {
		if k := cs.counterNameOf(st.ListStyleString, listScope); k != st.ListStyleString {
			c := *st
			c.ListStyleString = k
			out = &c
		}
	}
	if contentScope == nil {
		return out
	}
	var items []ContentItem
	for i, it := range out.Content.Items {
		if it.Style != ListNamed {
			continue
		}
		if k := cs.counterNameOf(it.StyleName, contentScope); k != it.StyleName {
			if items == nil {
				items = append([]ContentItem(nil), out.Content.Items...)
			}
			items[i].StyleName = k
		}
	}
	if items != nil {
		if out == st {
			c := *st
			out = &c
		}
		out.Content.Items = items
	}
	return out
}
