package css

import (
	"context"
	"math"

	"golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// scopeRule is an @scope rule (CSS Cascade 6, 2.5): the selectors of its
// scoping roots and of its scoping limits, and the @scope rule it nests
// in. The style rules inside it match only the elements in its scope.
type scopeRule struct {
	// start selects the scoping roots; nil is the implicit scope, whose
	// root is the parent element of the style sheet's owner node (2.5.1).
	start []*complexSel
	// end selects the scoping limits, descendants of a root that end its
	// scope with their own descendants; nil for none.
	end []*complexSel
	// parent is the @scope rule this one nests in, nil at the top: a
	// nested rule's roots lie in the scope of its parent's.
	parent *scopeRule
}

// maxScopeNesting caps the @scope rules nested in one another: 8. Anti-DoS
// guard, not spec-mandated: matching a nested rule walks the ancestors of
// each root of its parent, a cost that multiplies with each level, and
// real style sheets nest @scope once or twice.
const maxScopeNesting = 8

// scopeRule parses the @scope rule it (CSS Cascade 6, 2.5.1): an optional
// (<scope-start>), an optional to (<scope-end>), and a block of rules
// and declarations. Declarations directly in the block apply to the
// scoping root, as in a :where(:scope) rule. A prelude that does not
// parse drops the rule.
func (sp *sheetParser) scopeRule(ctx context.Context, it item, base string, cond *mediaCond, depth int, c *declCtx) {
	r, ok := parseScopePrelude(it.prelude, sp.scope != nil)
	if !ok {
		c.warn(warn.CSSInvalid, warn.Approximated, "an @scope rule with an invalid prelude was ignored")
		return
	}
	if sp.scopes >= maxScopeNesting {
		c.warn(warn.LimitReached, warn.Omitted, "an @scope rule nested more than %d deep was ignored", maxScopeNesting)
		return
	}
	if !resolveNamespaces(r.start, sp.ns, true) || !resolveNamespaces(r.end, sp.ns, true) {
		return
	}
	r.parent = sp.scope
	saved := sp.scope
	sp.scope = r
	sp.scopes++
	defer func() { sp.scope, sp.scopes = saved, sp.scopes-1 }()
	root := scopedPrelude(nil)
	sels, err := parseSelectorList(root, 0, false)
	if err != nil {
		return
	}
	var decls []Declaration
	flush := func() {
		if len(decls) == 0 || !sp.count() {
			return
		}
		sp.sheet.rules = append(sp.sheet.rules, styleRule{selectors: sels, decls: decls, cond: cond, cont: sp.container, scope: sp.scope, layer: sp.layer})
		decls = nil
	}
	var order ruleOrder
	p := newListParser(it.block)
	p.nested = true
	p.declarations(func(n item) bool {
		switch n.kind { //nolint:exhaustive // errors drop out
		case itemDeclaration:
			if d := c.declare(n.name, n.value, n.important); d.declares() {
				decls = append(decls, d)
			}
		case itemQualified, itemAtRule:
			flush()
			sp.rule(ctx, n, base, cond, depth, &order)
		}
		return sp.err == nil
	})
	flush()
}

// parseScopePrelude parses the prelude of an @scope rule: an optional
// (<scope-start>) and an optional to (<scope-end>), each a selector list.
// The limit's selectors, and a nested rule's roots, are relative to the
// root they bound or lie under ([scopedPrelude]).
func parseScopePrelude(prelude []token, nested bool) (*scopeRule, bool) {
	l := significant(prelude)
	r := &scopeRule{}
	if len(l) > 0 && l[0].typ == tLParen {
		list := l[0].children
		if nested {
			list = scopedPrelude(list)
		}
		sels, err := parseSelectorList(list, 0, false)
		if err != nil || hasPseudoElement(sels) {
			return nil, false
		}
		r.start = sels
		l = l[1:]
	}
	if len(l) > 0 {
		if len(l) != 2 || !l[0].isIdent("to") || l[1].typ != tLParen {
			return nil, false
		}
		sels, err := parseSelectorList(scopedPrelude(l[1].children), 0, false)
		if err != nil || hasPseudoElement(sels) {
			return nil, false
		}
		r.end = sels
	}
	return r, true
}

// hasPseudoElement reports a selector of sels that selects a
// pseudo-element, which no scoping root or limit can be.
func hasPseudoElement(sels []*complexSel) bool {
	for _, s := range sels {
		if s.pseudoElement != "" {
			return true
		}
	}
	return false
}

// scopedPrelude returns the selector list prelude of a scoped style rule
// as the absolute selectors it stands for (CSS Cascade 6, 2.5.2): a
// selector that holds :scope or & is kept, & standing for :scope, the
// scoping root, which Chromium gives the specificity of :scope; any other
// is relative to :where(:scope), by its leading combinator or as a
// descendant. An empty prelude is :where(:scope) itself.
func scopedPrelude(prelude []token) []token {
	where := []token{{typ: tColon}, {typ: tFunction, value: "where", children: []token{{typ: tColon}, {typ: tIdent, value: "scope"}}}}
	parts := splitCommas(trimWS(prelude))
	if len(parts) == 0 {
		return where
	}
	var out []token
	for _, sel := range parts {
		sel = trimWS(sel)
		if len(out) > 0 {
			out = append(out, token{typ: tComma})
		}
		switch {
		case hasNesting(sel) || hasScope(sel):
			out = append(out, nest(sel, []token{{typ: tColon}, {typ: tIdent, value: "scope"}})...)
		case len(sel) > 0 && (sel[0].isDelim('>') || sel[0].isDelim('+') || sel[0].isDelim('~')):
			out = append(out, where...)
			out = append(out, token{typ: tWhitespace})
			out = append(out, sel...)
		default:
			out = append(out, where...)
			out = append(out, token{typ: tWhitespace})
			out = append(out, sel...)
		}
	}
	return out
}

// hasScope reports a :scope pseudo-class anywhere in l.
func hasScope(l []token) bool {
	for i := range l {
		if l[i].typ == tColon && i+1 < len(l) && l[i+1].typ == tIdent && eqFold(l[i+1].value, "scope") {
			return true
		}
		if len(l[i].children) > 0 && hasScope(l[i].children) {
			return true
		}
	}
	return false
}

// scopeHit is one scoping root of an element: the root, nil for the
// document's own scope, and the generations between the element and it,
// its scope proximity (CSS Cascade 6, 6.6).
type scopeHit struct {
	root *html.Node
	hops int
}

// noProximity is the scope proximity of a declaration outside any @scope
// rule, which every scoped declaration beats (CSS Cascade 6, 6.6).
const noProximity int32 = math.MaxInt32

// scopeRoots returns the scoping roots of the @scope rule s for the
// element n, nearest first: the inclusive ancestors of n that s selects,
// within the scope of s's parent, under none of whose scoping limits n
// lies. owner is the owner node of the style sheet, whose parent is the
// root of an implicit scope. It returns nil once the match ran past the
// step budget.
func (cs *computeState) scopeRoots(s *scopeRule, owner, n *html.Node) []scopeHit {
	var out []scopeHit
	hops := 0
	a := n
	for ; isElement(a); a, hops = a.Parent, hops+1 {
		if cs.m.over {
			return nil
		}
		if !cs.scopeRoot(s, owner, a) {
			continue
		}
		if cs.limited(s, a, n) {
			continue
		}
		out = append(out, scopeHit{root: a, hops: hops})
	}
	if s.start == nil && s.parent == nil && (owner == nil || owner.Parent == a) {
		// A sheet without an owner node, or one whose owner's parent is
		// the document or a shadow root: the implicit scope is the whole
		// tree, its root above every element, the host of a shadow tree,
		// which Chromium makes the root :scope names (2.5.1).
		out = append(out, scopeHit{root: cs.opts.Shadows.Host(a), hops: hops})
	}
	return out
}

// scopeRoot reports whether the element a is a scoping root of s: the
// parent of the owner node for an implicit scope, else an element s's
// start selects; nested, it lies in the scope of a root of s's parent,
// which :scope then names.
func (cs *computeState) scopeRoot(s *scopeRule, owner, a *html.Node) bool {
	if s.start == nil {
		if owner == nil || owner.Parent != a {
			return false
		}
		return s.parent == nil || len(cs.scopeRoots(s.parent, owner, a)) > 0
	}
	if s.parent == nil {
		return cs.m.anyMatchesScoped(s.start, a, nil)
	}
	for _, h := range cs.scopeRoots(s.parent, owner, a) {
		if cs.m.anyMatchesScoped(s.start, a, h.root) {
			return true
		}
	}
	return false
}

// limited reports whether the element n lies under a scoping limit of
// root: an element of the path from below root down to n that s's end
// selects, :scope naming root.
func (cs *computeState) limited(s *scopeRule, root, n *html.Node) bool {
	if s.end == nil {
		return false
	}
	for a := n; a != nil && a != root; a = a.Parent {
		if cs.m.anyMatchesScoped(s.end, a, root) {
			return true
		}
	}
	return false
}

// anyMatchesScoped reports whether n matches one of sels with :scope
// naming root, the document's root element for nil.
func (m *matcher) anyMatchesScoped(sels []*complexSel, n, root *html.Node) bool {
	saved := m.scope
	m.scope = root
	defer func() { m.scope = saved }()
	for _, s := range sels {
		if m.matches(s, n) {
			return true
		}
	}
	return false
}

// matchScoped adds the selector ref of the scoped rule rr when the
// element n matches it from one of its scoping roots, at the proximity of
// the nearest such root (CSS Cascade 6, 6.6).
func (cs *computeState) matchScoped(ref selRef, rr *resolvedRule, n *html.Node, add func(selRef, int32)) {
	for _, h := range cs.scopeRoots(rr.rule.scope, rr.owner, n) {
		if cs.m.anyMatchesScoped([]*complexSel{ref.sel}, n, h.root) {
			add(ref, int32(min(h.hops, int(noProximity)))) //nolint:gosec // hops is clamped to the int32 range
			return
		}
	}
}

// anyMatchesHostScoped reports whether the featureless host n matches the
// selector s with :scope naming n.
func (m *matcher) anyMatchesHostScoped(s *complexSel, n *html.Node) bool {
	saved := m.scope
	m.scope = n
	defer func() { m.scope = saved }()
	return m.matchesHost(s, n)
}
