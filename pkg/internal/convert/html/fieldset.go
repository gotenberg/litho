package html

import (
	"slices"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// renderedLegend is the rendered legend of a fieldset: its element, and
// the node built for it, nil until built.
type renderedLegend struct {
	dom  *dom.Node
	node *layout.Node
}

// legendNode marks node, built for a rendered legend, as one, blockified
// (HTML Standard, 15.3.13), and returns it.
func legendNode(node *layout.Node) *layout.Node {
	node.Fieldset = layout.FieldsetLegend
	if d := css.Blockify(node.Style.Display); d != node.Style.Display {
		blocked := *node.Style
		blocked.Display = d
		node.Style = &blocked
	}
	return node
}

// detachLegend removes legend from the children of node, or of an element
// of display: contents among them, and reports whether it found it.
func detachLegend(node, legend *layout.Node, depth int) bool {
	for i, k := range node.Children {
		if k == legend {
			node.Children = slices.Delete(node.Children, i, i+1)
			return true
		}
		if k.Kind == layout.Element && k.Style != nil && k.Style.Display == css.Contents && depth < maxLegendDepth && detachLegend(k, legend, depth+1) {
			return true
		}
	}
	return false
}

// maxLegendDepth bounds how deep under elements of display: contents a
// rendered legend is looked for, as css.Computed.RenderedLegend bounds it.
// Anti-DoS guard, not spec-mandated: real documents nest none or one.
const maxLegendDepth = 32

// fieldsetBoxes turns node, the box of a fieldset element whose children
// are built, into the boxes of a fieldset (HTML Standard, 15.3.13): its
// box styled fs, of children its rendered legend, taken out of the
// children or of an element of display: contents among them, then its
// anonymous fieldset content box styled cs, which holds the rest,
// ::before and ::after included. A fieldset box draws no marker.
func fieldsetBoxes(node *layout.Node, fs, cs *css.Style, legend *layout.Node) {
	content := &layout.Node{Kind: layout.Element, Style: cs, Fieldset: layout.FieldsetContent}
	switch {
	case legend != nil && detachLegend(node, legend, 0):
		content.Children = node.Children
		node.Children = []*layout.Node{legend, content}
	case legend != nil:
		legend.Fieldset = layout.FieldsetNone
		fallthrough
	default:
		content.Children = node.Children
		node.Children = []*layout.Node{content}
	}
	node.Style = fs
	node.Fieldset = layout.FieldsetBox
	node.Marker = nil
	node.ListValue = 0
}
