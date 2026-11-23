package html

import (
	"strings"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// attachShadows attaches the declarative shadow roots of the parsed
// document doc and assigns their slots, returning nil when it has none.
//
// The parser attaches a shadow root for a template start tag whose
// shadowrootmode is open or closed, ASCII case-insensitively, to the
// element the template would be inserted in, the adjusted current node,
// when that element is not the topmost one and attachShadow succeeds on
// it; the template is then not inserted and its contents become the
// shadow root's (HTML Standard, 13.2.6.4.4). attachShadow succeeds on an
// HTML element of a valid custom element name or of a name the DOM
// Standard lists, that has no shadow root yet (DOM Standard, 4.2.14). A
// template whose attachment fails is inserted as a template, which draws
// nothing. Open and closed roots print alike. The parser of x/net/html
// keeps a template's contents as its children, so the attachment runs on
// the parsed tree, in tree order, each shadow tree attached before the
// templates inside it.
func attachShadows(doc *dom.Node) *css.Shadows {
	var s css.Shadows
	stack := []*dom.Node{doc}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		var next *dom.Node
		for c := n.FirstChild; c != nil; c = next {
			next = c.NextSibling
			if declarativeRoot(c) && validShadowHost(n) && s.Len() < maxShadowRoots {
				root := css.NewShadowRoot()
				if s.Attach(n, root) {
					n.RemoveChild(c)
					for k := c.FirstChild; k != nil; k = c.FirstChild {
						c.RemoveChild(k)
						root.AppendChild(k)
					}
					stack = append(stack, root)
					continue
				}
			}
		}
		for c := n.LastChild; c != nil; c = c.PrevSibling {
			stack = append(stack, c)
		}
	}
	if s.Len() == 0 {
		return nil
	}
	s.AssignSlots()
	return &s
}

// maxShadowRoots caps the shadow roots a document attaches; a template
// past it stays a template. Anti-DoS guard, not spec-mandated: each root
// costs a template element, which the element cap already counts, and
// the cap only bounds the maps the shadow trees keep. A page of
// server-rendered components attaches one root a component, some hundreds
// at most.
const maxShadowRoots = 16384

// declarativeRoot reports whether n is a template element whose
// shadowrootmode is open or closed (HTML Standard, 4.12.3).
func declarativeRoot(n *dom.Node) bool {
	if n.Type != dom.ElementNode || n.Namespace != "" || n.Data != "template" {
		return false
	}
	v, ok := attr(n, "shadowrootmode")
	return ok && (strings.EqualFold(v, "open") || strings.EqualFold(v, "closed"))
}

// shadowHostNames are the names of the HTML elements attachShadow accepts
// besides custom elements (DOM Standard, 4.2.14, the attach a shadow root
// steps).
var shadowHostNames = map[string]bool{
	"article": true, "aside": true, "blockquote": true, "body": true, "div": true,
	"footer": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "header": true, "main": true, "nav": true, "p": true,
	"section": true, "span": true,
}

// validShadowHost reports whether attachShadow accepts the element n: an
// HTML element of a valid custom element name or of a name of
// [shadowHostNames].
func validShadowHost(n *dom.Node) bool {
	return n.Type == dom.ElementNode && n.Namespace == "" && (shadowHostNames[n.Data] || validCustomElementName(n.Data))
}

// reservedCustomNames are the names with a hyphen that are no valid
// custom element names (HTML Standard, 4.13.2).
var reservedCustomNames = map[string]bool{
	"annotation-xml": true, "color-profile": true, "font-face": true, "font-face-src": true,
	"font-face-uri": true, "font-face-format": true, "font-face-name": true, "missing-glyph": true,
}

// validCustomElementName reports whether name is a valid custom element
// name (HTML Standard, 4.13.2): a lowercase ASCII letter, then
// characters of PCENChar, a hyphen among them, and none of the reserved
// names. The parser lowercases the ASCII letters of an HTML element's
// name, so an uppercase letter left is none of its.
func validCustomElementName(name string) bool {
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
