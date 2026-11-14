package html

import (
	"context"
	"slices"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// defaultSummaryText is the text of the default summary of a details
// element without a summary child, which the HTML Standard leaves to the
// implementation (15.5.5): Chromium's in English.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws "Details"
//     after the disclosure triangle of a details element without a
//     summary child, on screen and in print.
const defaultSummaryText = "Details"

// isDetails reports whether n is an HTML details element.
func isDetails(n *dom.Node) bool {
	return n.Type == dom.ElementNode && n.Namespace == "" && n.DataAtom == atom.Details
}

// closeExclusiveDetails removes the open attribute of every details
// element of doc that a details element before it in tree order, of the
// same details name group, already holds open: the insertion steps of a
// details element, which the parser runs as it inserts each one (HTML
// Standard, 4.11.1, "ensure details exclusivity by closing the given
// element if needed"). A name group is the details elements of one tree
// whose name attributes are equal and not empty.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints the
//     second of two details elements of one name, both written open,
//     closed, on screen and in print.
func closeExclusiveDetails(doc *dom.Node) {
	var open map[string]bool
	walk(doc, func(n *dom.Node) bool {
		if !isDetails(n) {
			return true
		}
		name, named := attr(n, "name")
		if !named || name == "" {
			return true
		}
		i := openIndex(n)
		if i < 0 {
			return true
		}
		if open[name] {
			n.Attr = append(n.Attr[:i:i], n.Attr[i+1:]...)
			return true
		}
		if open == nil {
			open = map[string]bool{}
		}
		open[name] = true
		return true
	})
}

// openIndex returns the index of the open attribute of n, -1 for none.
func openIndex(n *dom.Node) int {
	for i, a := range n.Attr {
		if a.Namespace == "" && a.Key == "open" {
			return i
		}
	}
	return -1
}

// detailsChildren builds the children of the details element n styled
// st as its shadow tree lays them out (HTML Standard, 15.5.5): first its
// summary, the first summary element child wherever it stands, or the
// default summary when it has none; then the ::details-content box that
// holds every other child, its contents skipped while the element is
// closed, as the user agent sheet's content-visibility: hidden asks.
func (b *builder) detailsChildren(ctx context.Context, n *dom.Node, st *css.Style, depth int) ([]*layout.Node, error) {
	var out []*layout.Node
	sum := css.DetailsSummary(n)
	if sum != nil {
		kids, err := b.element(ctx, sum, st, depth)
		if err != nil {
			return nil, err
		}
		out = append(out, kids...)
	} else if ss, ms := b.computed.DefaultSummary(n); ss != nil && ss.Display != css.DisplayNone {
		out = append(out, b.defaultSummary(ss, ms))
	}
	cs := b.computed.Pseudo(n, css.PseudoDetailsContent)
	if cs == nil || cs.Display == css.DisplayNone {
		return out, nil
	}
	b.applyCounters(nil, cs)
	pushed := slices.Clone(b.counters.pushed)
	defer b.counters.leave(pushed)
	if cs.ContentVisibility == css.VisibilityHidden {
		return append(out, hiddenContents(cs)), nil
	}
	slot := &layout.Node{Kind: layout.Element, Style: cs}
	b.counters.open()
	defer b.counters.close()
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c == sum:
		case c.Type == dom.TextNode && c.Data != "":
			slot.Children = append(slot.Children, &layout.Node{Kind: layout.Text, Style: cs, Text: c.Data})
		case c.Type == dom.ElementNode:
			kids, err := b.element(ctx, c, cs, depth)
			if err != nil {
				return nil, err
			}
			slot.Children = append(slot.Children, kids...)
		}
	}
	return append(out, slot), nil
}

// defaultSummary returns the node of the default summary styled ss, its
// marker styled ms: a list item holding [defaultSummaryText].
func (b *builder) defaultSummary(ss, ms *css.Style) *layout.Node {
	v := b.applyCounters(nil, ss)
	b.counters.leave(b.counters.pushed)
	node := &layout.Node{Kind: layout.Element, Style: ss, Children: []*layout.Node{{Kind: layout.Text, Style: ss, Text: defaultSummaryText}}}
	if isListItem(ss.Display) {
		node.ListValue = v
		node.Marker = &layout.Node{Kind: layout.Element, Style: ms}
	}
	return node
}
