package svg

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// ClipRegions returns the clipping regions the clipPath element of id
// gives an element of an HTML document whose reference box is w by h CSS
// pixels (CSS Masking 1, 5.1 and 14.3): in pixels from the box's top left,
// the user space of an element with a CSS layout box, and the box itself
// as its bounding box for clipPathUnits objectBoundingBox. The content
// shows where it lies inside every path of a region, in any of the
// regions. found reports an id that names a clipPath element; found with
// no region hides the element, as a clip path with nothing to clip with
// does (14.3.5).
func (d *Document) ClipRegions(ctx context.Context, id string, w, h float64) (regions [][]*typeset.Path, found bool) {
	if d.disabled || !numeric.Finite(w) || !numeric.Finite(h) || w < 0 || h < 0 {
		return nil, false
	}
	cp := d.ids[id]
	if cp == nil || cp.Data != "clipPath" {
		return nil, false
	}
	r := newRenderer(ctx, d, w, h)
	regs, empty := r.clipPath(d.root, id, &bbox{x1: w, y1: h, ok: true}, nil)
	if empty {
		return nil, true
	}
	if len(regs) == 0 {
		// A clip path that leads back into its own chain clips nothing.
		return nil, false
	}
	out := make([][]*typeset.Path, len(regs))
	for i, reg := range regs {
		out[i] = reg
	}
	return out, true
}
