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
// the size of its containers for size queries: 4. Anti-DoS guard, not
// spec-mandated: each layout after the first styles the document with the
// container sizes the one before measured, so a document converges in two
// layouts when no container's size depends on a query, and in one more
// for each level of containers whose size a query of an enclosing
// container changes; past the bound the last sizes measured answer, with
// a warning.
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
func prepareAndLayOut(ctx context.Context, doc *dom.Node, o Options, fonts *text.Fonts, setup printSetup, lay layoutFunc, adjust func(*prepared)) (*prepared, *layout.Result, error) {
	for pass := 1; ; pass++ {
		p, err := prepareDoc(ctx, doc, o, fonts, setup)
		if err != nil {
			return nil, nil, err
		}
		if adjust != nil {
			adjust(p)
		}
		res, err := layOut(ctx, lay, p)
		if err != nil {
			return nil, nil, err
		}
		if !p.layout.MeasureContainers {
			return p, res, nil
		}
		sizes := p.measured(res)
		if sizes.equal(setup.containers) {
			return p, res, nil
		}
		if pass >= maxContainerLayouts {
			p.warnings = append(p.warnings, typeset.Warning{
				Code: warn.LimitReached, Effect: warn.Approximated,
				Message: fmt.Sprintf("container queries were answered with the container sizes of layout %d: the sizes still changed after %d layouts", pass-1, maxContainerLayouts),
			})
			return p, res, nil
		}
		setup.containers = sizes
	}
}
