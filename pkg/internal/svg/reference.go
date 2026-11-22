package svg

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/effect"
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
	r := newRenderer(ctx, d, vw, vh, identity)
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
	r := newRenderer(ctx, d, vw, vh, identity)
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

// Filter is a filter element of the drawing resolved for an element of an
// HTML document (Filter Effects 1, 7): its region and its primitives, in
// a user space whose origin is the element's border box's top left and
// whose unit is one CSS pixel. A Filter holds no context; it is safe for
// use by one goroutine at a time.
type Filter struct {
	d    *Document
	id   string
	w, h float64
	// X, Y, W and H are the filter region in that user space; Sigma the
	// smallest standard deviation of its blurs, 0 for none.
	X, Y, W, H float64
	Sigma      float64
	// Skipped, when not nil, says why the filter is not drawn: it holds a
	// primitive the renderer has not. The element is then drawn
	// unfiltered.
	Skipped error
}

// Filter returns the filter element of id for an element of an HTML
// document whose border box is w by h CSS pixels, the box being its
// bounding box for objectBoundingBox units. found reports an id that names
// a filter element. A filter whose region has no area draws nothing.
func (d *Document) Filter(ctx context.Context, id string, w, h float64) (f *Filter, found bool) {
	if d.disabled || !numeric.Finite(w) || !numeric.Finite(h) || w < 0 || h < 0 {
		return nil, false
	}
	g, found := d.graph(ctx, id, w, h)
	if !found {
		return nil, false
	}
	f = &Filter{d: d, id: id, w: w, h: h}
	if g.empty() {
		return f, true
	}
	f.X, f.Y, f.W, f.H, f.Sigma, f.Skipped = g.x, g.y, g.w, g.h, g.sigma(), g.check()
	return f, true
}

// graph resolves the filter element of id for a box of w by h user units.
func (d *Document) graph(ctx context.Context, id string, w, h float64) (*filterGraph, bool) {
	vw, vh := d.viewport()
	r := newRenderer(ctx, d, vw, vh, identity)
	return r.filterGraph(id, bbox{x1: w, y1: h, ok: true})
}

// Apply runs the filter on src, the element's rendering, an RGBA surface
// of k pixels a CSS pixel whose pixel (0, 0) has its top left corner at
// (ox, oy) in the filter's user space, and returns the result: a new
// surface of src's size and grid, in sRGB, transparent outside the
// filter region. The caller releases both.
func (f *Filter) Apply(ctx context.Context, src *effect.Surface, k, ox, oy float64) (*effect.Surface, error) {
	g, found := f.d.graph(ctx, f.id, f.w, f.h)
	if !found || g.empty() {
		return effect.New(ctx, effect.RGBA, src.W, src.H, src.Grid)
	}
	return g.run(ctx, src, k, ox, oy)
}

// ShapePath returns the path of the shape element of id in its own user
// space, in user units: the geometry of a path, rect, circle, ellipse,
// line, polyline or polygon as SVG 2 draws it, its transform attribute and
// those of its ancestors left out, and pathLength too (CSS Motion Path 1,
// 2.1: an offset path names a shape by url()). Percentages resolve against
// the viewport of the drawing that holds it. found reports an id that names
// such an element; a shape that draws nothing has no path.
func (d *Document) ShapePath(ctx context.Context, id string) (ops []typeset.PathOp, found bool) {
	if d.disabled {
		return nil, false
	}
	el := d.ids[id]
	if el == nil || !shapes[el.Data] {
		return nil, false
	}
	vw, vh := d.viewport()
	r := newRenderer(ctx, d, vw, vh, identity)
	return r.geometry(el, d.styleOf(el, 0)), true
}
