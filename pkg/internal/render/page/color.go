package page

import (
	"errors"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/render/colorconv"
)

// fallbackConverter paints black, the initial colour of the graphics state
// (ISO 32000-1, 8.4.1, Table 52), a colour whose space cannot be converted:
// a missing /ColorSpace resource, an unknown family, or a tint transform or
// colour table that does not read.
type fallbackConverter struct{}

func (fallbackConverter) RGB([]float64) (r, g, b float32) {
	return 0, 0, 0
}

// colorOptions returns what a conversion depends on beyond the space: the
// rendering intent of the current graphics state (8.6.5.8), the page, whose
// output intents replace the document's (ISO 32000-2, 14.11.5), and the
// render's cache.
func (r *renderer) colorOptions() colorconv.Options {
	return colorconv.Options{Intent: r.m.Current().RenderingIntent, Page: r.page.Dict, Cache: r.colorCache}
}

// colorFor returns the converter of the colour space a fill or stroke colour
// is in, against the resources of the stream being run, whose default spaces
// stand in for the device spaces (8.6.5.6). A Pattern space converts through
// its base (8.7.3.3). It reports false when the space paints nothing, a
// Separation or DeviceN over the colorant None only (8.6.6.4, 8.6.6.5), and
// the caller paints nothing. A space that cannot be converted is skipped and
// paints [fallbackConverter]'s black. The converter applies the transfer
// function of the current graphics state (10.5) after the conversion.
func (r *renderer) colorFor(s *scope, space graphics.ColorSpace) (colorconv.Converter, bool) {
	conv, err := colorconv.For(r.ctx, r.doc, space, s.resources, r.colorOptions())
	return r.converterOrFallback(conv, err)
}

// colorForObject returns the converter of a /ColorSpace object as a shading
// dictionary holds it, under the rules of [renderer.colorFor].
func (r *renderer) colorForObject(s *scope, obj ast.PdfObject) (colorconv.Converter, bool) {
	conv, err := colorconv.ForObject(r.ctx, r.doc, obj, s.resources, r.colorOptions())
	return r.converterOrFallback(conv, err)
}

func (r *renderer) converterOrFallback(conv colorconv.Converter, err error) (colorconv.Converter, bool) {
	switch {
	case err == nil:
		return r.withTransfer(conv), true
	case errors.Is(err, colorconv.ErrPaintsNothing):
		return nil, false
	}
	r.skip("colour space", err)
	return r.withTransfer(fallbackConverter{}), true
}
