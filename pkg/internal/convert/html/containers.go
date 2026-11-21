package html

import (
	"context"
	"fmt"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxContainerLayouts bounds the layouts of a document whose styles read
// what a layout measures, the size of its containers for size queries or
// the position option an element took: 4. Anti-DoS guard, not
// spec-mandated: each layout after the first styles the document with
// what the one before measured, so a document converges in two layouts
// when no container's size depends on a query and no option on the
// styles its descendants inherit, and in one more for each level of
// containers whose size a query of an enclosing container changes; past
// the bound what the last layout measured answers, with a warning.
const maxContainerLayouts = 4

// maxContainers bounds the containers for size queries a document
// measures: 4,096. Anti-DoS guard, not spec-mandated: each holds an entry
// of the size table the cascade reads and the layout fills; past the
// bound a container answers no size query, with a warning, as an element
// without a box does. Real pages hold tens.
const maxContainers = 4096

// containerSizes holds the content box size in points of each container
// for size queries the layout measured, by element.
type containerSizes map[*dom.Node][2]float64

// size answers [css.ComputeOptions.ContainerSize].
func (s containerSizes) size(n *dom.Node) (float64, float64, bool) {
	v, ok := s[n]
	return v[0], v[1], ok
}

// equal reports whether s and o hold the same sizes.
func (s containerSizes) equal(o containerSizes) bool {
	if len(s) != len(o) {
		return false
	}
	for n, v := range s {
		if w, ok := o[n]; !ok || w != v {
			return false
		}
	}
	return true
}

// measured returns the sizes of the containers res laid out, by element.
func (p *prepared) measured(res *layout.Result) containerSizes {
	out := make(containerSizes, len(res.Containers))
	for node, v := range res.Containers {
		if n, ok := p.b.containers[node]; ok {
			out[n] = v
		}
	}
	return out
}

// prepareAndLayOut prepares doc under setup and lays it out with lay,
// adjust changing the prepared document first when not nil. When a style
// read the size of a container (CSS Conditional 5, 6.1 and 7), whose
// answer only a layout gives, it prepares and lays the document out
// again with the sizes the last layout measured, until a layout measures
// the sizes its styles read or [maxContainerLayouts] layouts ran. The
// cascade, the content tree and the layout run once a pass over the whole
// document: a container's size depends on its ancestors and, under size
// or inline-size containment, not on its content, so one pass answers
// every container at one level of nesting at once.
//
// So for the position options of anchored boxes, when a style inherits an
// accepted @position-try property from an element that has some (CSS
// Anchor Positioning 1, 5.4): the option is applied by interleaving, so
// the element's computed values hold it and its descendants inherit
// them, which needs the option chosen before their styles are computed.
// The next pass cascades each such element with the option the last
// layout chose and lays it out keeping that option while it fits, its
// last successful one (5.4.1.1); the passes end when a layout chooses the
// options its styles hold.
func prepareAndLayOut(ctx context.Context, doc *dom.Node, o Options, fonts *text.Fonts, setup printSetup, lay layoutFunc, adjust func(*prepared)) (*prepared, *layout.Result, error) {
	var choices map[int]int
	for pass := 1; ; pass++ {
		p, err := prepareDoc(ctx, doc, o, fonts, setup)
		if err != nil {
			return nil, nil, err
		}
		if adjust != nil {
			adjust(p)
		}
		if choices != nil {
			// The layout keeps the options the cascade's styles hold
			// while they fit (CSS Anchor Positioning 1, 5.4.1.1).
			p.layout.PositionChoices = choices
		}
		res, err := layOut(ctx, lay, p)
		if err != nil {
			return nil, nil, err
		}
		sizes := setup.containers
		if p.layout.MeasureContainers {
			sizes = p.measured(res)
		}
		options := p.chosen(res)
		if sizes.equal(setup.containers) && options.equal(setup.options) {
			return p, res, nil
		}
		if pass >= maxContainerLayouts {
			p.warnings = append(p.warnings, typeset.Warning{
				Code: warn.LimitReached, Effect: warn.Approximated,
				Message: fmt.Sprintf("container queries and position options were answered with what layout %d measured: it still changed after %d layouts", pass-1, maxContainerLayouts),
			})
			return p, res, nil
		}
		setup.containers, setup.options, choices = sizes, options, res.PositionChoices
	}
}
