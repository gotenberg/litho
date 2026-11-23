package html

import (
	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// quirksOf returns the quirks of the HTML element n of style st
// in a document in quirks mode: the root html element and the document's
// body element fill the viewport (Quirks Mode Standard, 3.6 and 3.7). For
// the margin collapsing quirks (HTML Standard, 15.3.3), as Chromium lays
// them out: the document's body element, a td
// and a th are quirky containers, which drop the quirky margins that adjoin
// the start or the end of their content; the top margin the user agent
// sheet gives a p, pre, xmp, listing, plaintext, h1 to h6, ul, ol, dl,
// menu, dir or blockquote is quirky, and the bottom one it gives a p, pre,
// xmp, listing or plaintext. A margin an author sets is never quirky. A td
// or th of a nowrap attribute and a width of a length other than zero takes
// that width as its minimum (Quirks Mode Standard, 3.9).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 drops in quirks
//     mode both margins of a p, pre, xmp and listing alone in a td, the
//     top margin alone of h1 to h6, ul, ol, dl, menu, dir and blockquote,
//     neither of a figure; through a div, at the start of the body, after
//     an empty span or an empty p, under a cell's padding, and at the end
//     of a floated body; an author's margin: 1em 0 stays.
func quirksOf(n *dom.Node, st *css.Style) layout.Quirks {
	if st == nil || st.WritingMode != css.HorizontalTB {
		return 0
	}
	var q layout.Quirks
	switch n.DataAtom { //nolint:exhaustive // the other elements have no margin quirk
	case atom.Td, atom.Th:
		q = layout.QuirkyContainer
		if _, ok := attr(n, "nowrap"); ok && st.Width.Kind == css.Points && st.Width.Value != 0 {
			q |= layout.QuirkyNowrap
		}
	case atom.Html:
		if n.Parent != nil && n.Parent.Type == dom.DocumentNode {
			q = layout.FillsViewport
		}
	case atom.Body:
		if p := n.Parent; p != nil && p.DataAtom == atom.Html && p.Namespace == "" && p.Parent != nil && p.Parent.Type == dom.DocumentNode {
			q = layout.QuirkyContainer | layout.FillsViewport
		}
	case atom.P, atom.Pre, atom.Xmp, atom.Listing, atom.Plaintext:
		if !st.AuthorMarginBottom {
			q |= layout.QuirkyBottom
		}
		fallthrough
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Ul, atom.Ol, atom.Dl, atom.Menu, atom.Dir, atom.Blockquote:
		if !st.AuthorMarginTop {
			q |= layout.QuirkyTop
		}
	}
	return q
}

// stickyImage returns the quirk of the replaced box of an img element, an
// input element of type image or an object or embed element showing a
// picture, in a document in quirks mode (Quirks Mode Standard, 3.8, which
// names img elements; Chromium treats the other three as it treats an
// img): see [layout.StickyImage].
func (b *builder) stickyImage() layout.Quirks {
	if b.frameOpts.mode != css.Quirks {
		return 0
	}
	return layout.StickyImage
}
