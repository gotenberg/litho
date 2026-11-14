package css

import (
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// The used styles of a fieldset element's box and of its anonymous
// fieldset content box, kept in the pseudo-element map under keys no
// selector names.
const (
	pseudoFieldsetBox PseudoElement = pseudoDefaultSummaryMarker + 1 + iota
	pseudoFieldsetContent
)

// isFieldset reports whether n is an HTML fieldset element.
func isFieldset(n *html.Node) bool {
	return n != nil && n.Type == html.ElementNode && n.Namespace == "" && n.DataAtom == atom.Fieldset
}

// Fieldset returns the used styles of the fieldset element n, nil when n
// is not one or generates no box of its own: the style of its box, and
// the style of its anonymous fieldset content box, which holds its
// content but its rendered legend (HTML Standard, 15.3.13). The box
// establishes a block formatting context, as an inline block when its
// outer display type is inline and as a flow root otherwise, and leaves
// to the content box the properties the Standard has it inherit: the
// content box is a grid or a flex container when the fieldset's display
// says so, a flow root otherwise, takes the fieldset's overflow, column,
// flex, grid, alignment, border-radius, text-overflow and unicode-bidi
// values, and fills the fieldset's height. The fieldset keeps its
// padding, which the layout gives the content box (layout.FieldsetBox).
// The Styles are shared: do not modify them.
func (c *Computed) Fieldset(n *html.Node) (box, content *Style) {
	return c.pseudo[pseudoKey{n, pseudoFieldsetBox}], c.pseudo[pseudoKey{n, pseudoFieldsetContent}]
}

// RenderedLegend returns the rendered legend of the fieldset element n,
// nil for none: the first of its child boxes that a legend element
// generates, not floated and not absolutely positioned (HTML Standard,
// 15.3.13). The children of an element of display: contents are child
// boxes of n's box (CSS Display 3, 2.5).
func (c *Computed) RenderedLegend(n *html.Node) *html.Node {
	if !isFieldset(n) {
		return nil
	}
	return c.legendIn(n, 0)
}

// legendIn returns the first legend element among the child boxes of n,
// at depth elements of display: contents below the fieldset, that
// [Computed.RenderedLegend] takes.
func (c *Computed) legendIn(n *html.Node, depth int) *html.Node {
	for k := n.FirstChild; k != nil; k = k.NextSibling {
		if k.Type != html.ElementNode {
			continue
		}
		st := c.styles[k]
		if st == nil || st.Display == DisplayNone {
			continue
		}
		if st.Display == Contents {
			if depth < maxContentsDepth {
				if l := c.legendIn(k, depth+1); l != nil {
					return l
				}
			}
			continue
		}
		if k.Namespace == "" && k.DataAtom == atom.Legend && st.Float == FloatNone && st.Position != Absolute && st.Position != Fixed {
			return k
		}
	}
	return nil
}

// maxContentsDepth bounds how many nested elements of display: contents
// the search for a rendered legend goes through. Anti-DoS guard, not
// spec-mandated: the search runs once a fieldset, and a legend under more
// than 32 such elements in one fieldset is no real document's.
const maxContentsDepth = 32

// Blockify returns the block-level equivalent of the display value d
// (CSS Display 3, 2.7), d itself when it is block-level already.
func Blockify(d Display) Display {
	b, _ := blockify(d)
	return b
}

// fieldsetStyles computes the styles [Computed.Fieldset] returns for the
// fieldset element n, styled st with the custom properties vars.
func (cs *computeState) fieldsetStyles(n *html.Node, st *Style, vars map[string][]token) {
	// The content box is an anonymous box: it inherits what an element
	// without declarations inherits.
	cs.boxParent = Block
	anon, _ := cs.cascade(nil, st, vars, false, nil)
	c := *anon
	switch st.Display { //nolint:exhaustive // every other display makes a flow root
	case Grid, InlineGrid:
		c.Display = Grid
	case Flex, InlineFlex:
		c.Display = Flex
	default:
		c.Display = FlowRoot
	}
	c.AlignContent, c.AlignItems, c.JustifyContent, c.JustifyItems = st.AlignContent, st.AlignItems, st.JustifyContent, st.JustifyItems
	c.BorderRadius = st.BorderRadius
	c.Columns = st.Columns
	c.Columns.SpanAll = false
	c.FlexDirection, c.FlexWrap = st.FlexDirection, st.FlexWrap
	c.RowGap, c.ColumnGap = st.RowGap, st.ColumnGap
	c.GridAutoColumns, c.GridAutoRows, c.GridAutoFlow = st.GridAutoColumns, st.GridAutoRows, st.GridAutoFlow
	c.GridTemplateAreas, c.GridTemplateRows, c.GridTemplateColumns = st.GridTemplateAreas, st.GridTemplateRows, st.GridTemplateColumns
	c.Overflow, c.OverflowClipMargin = st.Overflow, st.OverflowClipMargin
	c.TextOverflow = st.TextOverflow
	c.UnicodeBidi = st.UnicodeBidi
	c.Height = Dimension{Kind: Percent, Value: 1}
	cs.pseudo[pseudoKey{n, pseudoFieldsetContent}] = cs.adopt(&c)

	f := *st
	if inlineLevel(st.Display) {
		f.Display = InlineBlock
	} else {
		f.Display = FlowRoot
	}
	f.flexOrGrid = false
	init := InitialValue()
	f.Overflow, f.OverflowClipMargin = init.Overflow, init.OverflowClipMargin
	f.Columns = init.Columns
	f.Columns.SpanAll = st.Columns.SpanAll
	f.TextOverflow = init.TextOverflow
	cs.pseudo[pseudoKey{n, pseudoFieldsetBox}] = cs.adopt(&f)
}

// inlineLevel reports a display value whose outer display type is inline
// (CSS Display 3, 2.1).
func inlineLevel(d Display) bool {
	switch d { //nolint:exhaustive // the other display values are block-level or internal
	case Inline, InlineBlock, InlineTable, InlineListItem, InlineFlex, InlineGrid, Ruby:
		return true
	}
	return false
}

// adopt returns the style the shared table holds equal to st, charging
// the conversion's budget for a style it adopts, as the cascade charges
// the styles it computes.
func (cs *computeState) adopt(st *Style) *Style {
	shared, adopted := cs.styles.shareCopy(st)
	if adopted {
		cs.mem.Take(1, limits.StyleCost, "computed styles")
	}
	return shared
}
