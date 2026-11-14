package css

import (
	"sync"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// The styles of the default summary of a details element, kept in the
// pseudo-element map under keys no selector names.
const (
	pseudoDefaultSummary PseudoElement = numPseudos + iota
	pseudoDefaultSummaryMarker
)

// defaultSummaryDecls returns the declarations of the default summary of
// a closed details element and of an open one: the user agent sheet's
// summary rules and the :host summary rules of the details element's
// shadow tree (HTML Standard, 15.3.5 and 15.5.5). No author rule reaches
// an element of that tree, so these alone cascade. They are parsed once,
// after the property tables are built.
var defaultSummaryDecls = sync.OnceValues(func() (closed, open []Declaration) {
	const decls = "display: list-item; counter-increment: list-item 0; unicode-bidi: isolate; list-style: inside disclosure-"
	return ParseDeclarations(decls+"closed", ParseOptions{}), ParseDeclarations(decls+"open", ParseOptions{})
})

// DetailsSummary returns the summary of the details element n: its first
// summary element child, which the first slot of its shadow tree takes
// (HTML Standard, 15.5.5), nil when it has none.
func DetailsSummary(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Namespace == "" && c.DataAtom == atom.Summary {
			return c
		}
	}
	return nil
}

// DefaultSummary returns the styles of the default summary of the details
// element n and of its marker, nil when n has a summary child or
// generates no box. The default summary is the element the first slot
// of a details element's shadow tree holds when no summary child fills
// it (HTML Standard, 15.5.5): it inherits from n, and only the user
// agent's declarations style it and its ::marker. The Styles are shared:
// do not modify them.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws the
//     default summary of a details element in the details element's
//     font and colour, with neither an author summary rule nor an author
//     ::marker rule applied, on screen and in print.
func (c *Computed) DefaultSummary(n *html.Node) (summary, marker *Style) {
	return c.pseudo[pseudoKey{n, pseudoDefaultSummary}], c.pseudo[pseudoKey{n, pseudoDefaultSummaryMarker}]
}

// defaultSummary cascades the default summary of the details element n,
// styled st with the custom properties vars, and its ::marker: the
// declarations of [defaultSummaryDecls], then the user agent's ::marker
// rules alone.
func (cs *computeState) defaultSummary(n *html.Node, st *Style, vars map[string][]token) {
	decls, openDecls := defaultSummaryDecls()
	if _, open := attr(n, "open"); open {
		decls = openDecls
	}
	cands := make([]cand, len(decls))
	for i := range decls {
		cands[i] = cand{d: &decls[i], rank: rank(UserAgent, decls[i].Important), idx: i}
	}
	// The summary sits in a slot, a block of the shadow tree.
	cs.boxParent = Block
	ss, svars := cs.cascade(cands, st, vars, false, nil)
	cs.pseudo[pseudoKey{n, pseudoDefaultSummary}] = ss
	cs.elem++
	var ua []cand
	for _, c := range cs.match(n, &cs.r.pseudos[PseudoMarker]) {
		if originOf(c.rank) == UserAgent {
			ua = append(ua, c)
		}
	}
	ms, _ := cs.cascade(ua, ss, svars, false, nil)
	cs.pseudo[pseudoKey{n, pseudoDefaultSummaryMarker}] = ms
}
