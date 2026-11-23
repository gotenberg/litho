package css

import (
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// DocumentMode is the mode of an HTML document (DOM Standard, 4.5; HTML
// Standard, 13.2.6.4.1): no-quirks, limited-quirks or quirks. The zero
// value is no-quirks, the mode of every document that is not HTML parsed
// from a source: an Office document, a header band, a frame's srcdoc.
type DocumentMode uint8

const (
	// NoQuirks is the mode of a document with an HTML5 DOCTYPE.
	NoQuirks DocumentMode = iota
	// LimitedQuirks is the mode of the transitional and frameset
	// DOCTYPEs of XHTML 1.0 and, with a system identifier, HTML 4.01: only
	// the line height quirks of the Quirks Mode Standard apply (3.3, 3.4).
	LimitedQuirks
	// Quirks is the mode of a document without a DOCTYPE, or with an old
	// or malformed one: every quirk of the Quirks Mode Standard applies.
	Quirks
)

// isBodyElement reports whether n is the document's body element: the
// first body child of an html document element (Quirks Mode Standard,
// 2.2).
func isBodyElement(n *html.Node) bool {
	if n == nil || n.Type != html.ElementNode || n.Namespace != "" || n.DataAtom != atom.Body {
		return false
	}
	p := n.Parent
	if p == nil || p.Type != html.ElementNode || p.Namespace != "" || p.DataAtom != atom.Html || p.Parent == nil || p.Parent.Type != html.DocumentNode {
		return false
	}
	for k := p.FirstChild; k != n; k = k.NextSibling {
		if k.Type == html.ElementNode && k.Namespace == "" && k.DataAtom == atom.Body {
			return false
		}
	}
	return true
}
