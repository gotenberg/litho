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
// as its bounding box for clipPathUnits objectBoundingBox. Percentages in
// user units resolve against the viewport of the drawing that holds the
// clip path. The content
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
	vw, vh := d.viewport()
	r := newRenderer(ctx, d, vw, vh)
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

// MaskContent returns the content of the mask element of id for an element
// of an HTML document whose border box is w by h CSS pixels (CSS Masking
// 1, 7.1): in pixels from the box's top left, clipped to the mask's
// region, the box being its bounding box for objectBoundingBox units; and
// whether its mask-type is alpha rather than luminance. Percentages in user
// units resolve against the viewport of the drawing that holds the mask. found reports an
// id that names a mask element; found with no content masks everything
// out.
func (d *Document) MaskContent(ctx context.Context, id string, w, h float64) (content []typeset.Fragment, alpha, found bool) {
	if d.disabled || !numeric.Finite(w) || !numeric.Finite(h) || w < 0 || h < 0 {
		return nil, false, false
	}
	vw, vh := d.viewport()
	r := newRenderer(ctx, d, vw, vh)
	m, found := r.mask(d.root, initialStyle(d.opts.Context), id, &bbox{x1: w, y1: h, ok: true}, 0)
	if !found {
		return nil, false, false
	}
	return m.Content, m.Kind == typeset.MaskAlpha, true
}

// viewport returns the size of the drawing's viewport in user units: its
// natural size, or its viewBox's, 0 where it has none.
//
// # Corpus evidence
//
//   - probe, html: Chromium masks a div by a mask element whose rect is
//     100% wide, inside an svg 0 pixels wide, with nothing: the
//     percentage resolves against that svg's viewport.
func (d *Document) viewport() (float64, float64) {
	if d.viewBox.ok {
		return d.viewBox.w, d.viewBox.h
	}
	var w, h float64
	if d.size.HasWidth {
		w = d.size.Width / ptPerPx
	}
	if d.size.HasHeight {
		h = d.size.Height / ptPerPx
	}
	return w, h
}
