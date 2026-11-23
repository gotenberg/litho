package html

import (
	"context"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// elementCost is what one element of the parsed document costs the
// conversion's memory budget: its node, 112 bytes on a 64-bit platform,
// and what styling and building it hold for as long as the document lives,
// the slot of its computed style and its node of the content tree, a list
// item's marker or a heading's outline entry included. The style itself is
// charged apart, once whatever the number of elements sharing it
// ([limits.StyleCost]), and so are the boxes the layout makes of it.
//
// # Corpus evidence
//
//   - probe, html: 20,000 empty div elements, parsed and prepared, hold
//     596 bytes of live heap an element: 112 in the parsed tree, 484 in
//     the cascade and the content tree. A b inside a p holds about 580, an
//     h1 650, a list and its item 1,416 together (TestTreeCostsCoverTheTree).
const elementCost = 768

// textNodeCost is what one text, comment or doctype node of the parsed
// document costs the conversion's memory budget: its node and the run of
// the content tree it becomes. Its characters are a copy of the source's,
// which the caller charges for as long as the converter holds the source,
// the source itself being dropped once parsed; a source in a legacy
// encoding is charged what decoding grows it by ([chargeTree]).
//
// # Corpus evidence
//
//   - probe, html: 20,000 paragraphs of one word hold 364 bytes of live
//     heap a text node besides the paragraph's: 112 in the parsed tree,
//     252 in the content tree (TestTreeCostsCoverTheTree).
const textNodeCost = 384

// attrCost is what one attribute of the parsed document costs the
// conversion's memory budget: its three strings' headers, 48 bytes, twice
// over for the room a growing slice leaves. Its value is a copy of the
// source's bytes, charged as a text node's characters are.
//
// # Corpus evidence
//
//   - probe, html: 20,000 div elements of three attributes hold 64 bytes
//     of live heap an attribute, of one attribute 48
//     (TestTreeCostsCoverTheTree).
const attrCost = 96

// treeCost returns what the parsed document doc costs the conversion's
// budget at [elementCost], [textNodeCost] and [attrCost], and the bytes
// of its text and attribute values.
func treeCost(doc *dom.Node) (cost, text int64) {
	walk(doc, func(n *dom.Node) bool {
		switch n.Type {
		case dom.ElementNode:
			cost += elementCost + int64(len(n.Attr))*attrCost
			for _, a := range n.Attr {
				text += int64(len(a.Val))
			}
		case dom.TextNode, dom.CommentNode, dom.DoctypeNode:
			cost += textNodeCost
			text += int64(len(n.Data))
		case dom.DocumentNode, dom.ErrorNode, dom.RawNode:
		}
		return true
	})
	return cost, text
}

// chargeTree charges the conversion's budget what the document doc parsed
// from src bytes and its shadow trees hold until the conversion returns
// ([treeCost]), and the bytes of its text past src's length: the
// characters a legacy encoding grows by as it is decoded to UTF-8. It
// returns what it charged, which the caller releases once it drops the
// tree.
func chargeTree(ctx context.Context, doc *dom.Node, shadows *css.Shadows, src int) (int64, error) {
	cost, text := treeCost(doc)
	for r := range shadows.Roots() {
		c, t := treeCost(r)
		cost, text = cost+c, text+t
	}
	cost += max(text-int64(src), 0)
	if err := limits.BudgetFrom(ctx).Charge(cost, "the document tree"); err != nil {
		return 0, err
	}
	return cost, nil
}

// chargePages charges the conversion's budget what the pages of doc hold
// ([typeset.ContentCost]): the caller writes them all once Convert
// returns, and they live until the document is written, as their glyphs,
// which the layout charged, do. The charge ends with the conversion's
// budget.
func chargePages(ctx context.Context, doc *typeset.Document) error {
	mem := limits.BudgetFrom(ctx)
	for _, pg := range doc.Pages {
		if err := mem.Charge(typeset.ContentCost(pg.Content), "the pages"); err != nil {
			return err
		}
	}
	return nil
}
