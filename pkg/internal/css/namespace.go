package css

import (
	"golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxNamespaces caps the @namespace rules one style sheet declares: an
// anti-DoS guard, not spec-mandated. A style sheet binds a handful of
// prefixes; past the cap a rule is ignored with a warning.
const maxNamespaces = 64

// The namespace names of the elements an HTML parser makes (HTML, 13.2;
// Infra, 8).
const (
	htmlNamespace   = "http://www.w3.org/1999/xhtml"
	svgNamespace    = "http://www.w3.org/2000/svg"
	mathmlNamespace = "http://www.w3.org/1998/Math/MathML"
)

// namespaces are the @namespace rules of one style sheet (CSS Namespaces
// 3, 3): the prefixes it declares and its default namespace.
type namespaces struct {
	prefixes []namespacePrefix
	// def is the default namespace, nsAny when the sheet declares none.
	def elementNS
}

// namespacePrefix is one declared prefix and the namespace it names.
type namespacePrefix struct {
	prefix string
	ns     elementNS
}

// elementNSOf maps a namespace name to the namespace of the elements in
// it.
func elementNSOf(url string) elementNS {
	switch url {
	case htmlNamespace:
		return nsHTML
	case svgNamespace:
		return nsSVG
	case mathmlNamespace:
		return nsMathML
	}
	return nsUnknown
}

// holds reports whether the element n is in the namespace e.
func (e elementNS) holds(n *html.Node) bool {
	switch e {
	case nsAny:
		return true
	case nsHTML:
		return n.Namespace == ""
	case nsSVG:
		return n.Namespace == "svg"
	case nsMathML:
		return n.Namespace == "math"
	case nsUnknown:
	}
	return false
}

// lookup returns the namespace a prefix is bound to. Prefixes compare
// case-sensitively (CSS Namespaces 3, 3).
func (ns *namespaces) lookup(prefix string) (elementNS, bool) {
	if ns == nil {
		return nsUnknown, false
	}
	for i := len(ns.prefixes) - 1; i >= 0; i-- {
		if ns.prefixes[i].prefix == prefix {
			return ns.prefixes[i].ns, true
		}
	}
	return nsUnknown, false
}

// declare reads the prelude of an @namespace rule (CSS Namespaces 3, 2):
// an optional prefix, then the namespace name as a string or a url(). A
// prelude of another shape makes the rule invalid. A prefix declared
// again takes its last binding; a second default namespace replaces the
// first.
func (ns *namespaces) declare(prelude []token, c *declCtx) {
	l := significant(prelude)
	prefix := ""
	if len(l) == 2 && l[0].typ == tIdent {
		prefix, l = l[0].value, l[1:]
	}
	if len(l) != 1 {
		c.warn(warn.CSSInvalid, warn.Approximated, "an invalid @namespace rule was ignored")
		return
	}
	var url string
	switch t := l[0]; {
	case t.typ == tString || t.typ == tURL:
		url = t.value
	case t.typ == tFunction && eqFold(t.value, "url"):
		a := significant(t.children)
		if len(a) != 1 || a[0].typ != tString {
			c.warn(warn.CSSInvalid, warn.Approximated, "an invalid @namespace rule was ignored")
			return
		}
		url = a[0].value
	default:
		c.warn(warn.CSSInvalid, warn.Approximated, "an invalid @namespace rule was ignored")
		return
	}
	e := elementNSOf(url)
	if prefix == "" {
		ns.def = e
		if url == "" {
			ns.def = nsAny
		}
		return
	}
	if len(ns.prefixes) >= maxNamespaces {
		c.warn(warn.LimitReached, warn.Omitted, "more than %d @namespace rules in one style sheet: the rest were ignored", maxNamespaces)
		return
	}
	ns.prefixes = append(ns.prefixes, namespacePrefix{prefix: prefix, ns: e})
}

// resolveNamespaces binds the namespace prefixes of a selector list to the
// @namespace rules of its style sheet: a type selector's prefix names its
// element's namespace, a compound without one takes the default namespace,
// and a prefix the sheet does not declare makes the selector invalid
// (Selectors 4, 5.1, 5.2 and 6.5; CSS Namespaces 3, 4). Inside :is() and
// :where(), whose lists forgive, an invalid selector is dropped; inside
// :not() and :has() it fails the selector. The default namespace applies
// to the compounds of the selector itself, not to those inside a pseudo-
// class argument.
//
// # Corpus evidence
//
//   - wpt: css/css-conditional at-media-003, at-supports-045,
//     at-media-content-001, at-supports-content-001 and
//     at-supports-namespace-002 hold @namespace rules after a conditional
//     group rule or inside one; Chromium 156's screenshot of each shows
//     no red, every selector using such a prefix dropped.
func resolveNamespaces(list []*complexSel, ns *namespaces, top bool) bool {
	for _, s := range list {
		for i := range s.parts {
			p := &s.parts[i]
			if p.prefix != "" {
				e, ok := ns.lookup(p.prefix)
				if !ok {
					return false
				}
				p.ns = e
			} else if top && p.ns == nsAny && !p.anyNS && ns != nil {
				p.ns = ns.def
			}
			for _, a := range p.attrPrefixes {
				if _, ok := ns.lookup(a); !ok {
					return false
				}
			}
			for j := range p.pseudos {
				ps := &p.pseudos[j]
				switch ps.kind { //nolint:exhaustive // the other pseudo-classes hold no forgiving list
				case psIs, psWhere:
					kept := ps.args[:0]
					for _, a := range ps.args {
						if resolveNamespaces([]*complexSel{a}, ns, false) {
							kept = append(kept, a)
						}
					}
					ps.args = kept
				default:
					if !resolveNamespaces(ps.args, ns, false) || !resolveNamespaces(ps.of, ns, false) {
						return false
					}
				}
			}
		}
	}
	return true
}
