package pdf

import (
	"context"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/graphics/colorusage"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractColorSpaces lists the colour spaces the given 1-based page's content
// paints in: every colour operator (g, rg, k, cs and their stroking forms),
// image XObject, inline image, shading and shading pattern, and transparency
// group blending space, through the form XObjects, tiling patterns, Type 3
// glyphs and annotation appearances the page draws. Each entry counts the uses
// of one space in one way; see [models.ColorSpaceUse]. Use it to check what a
// page asks of an output device, or that [Writer.ConvertColors] left nothing
// outside its target.
//
// It reports what the content selects, as written: a device family stays a
// device family whatever Default colour space the resources define for it
// (ISO 32000-1, 8.6.5.6), and a soft mask's group, which shapes opacity rather
// than colour (11.6.5.2), is not walked. Each stream is read once, and the
// walk stops at the form nesting budget of [Limits].
func (r *Reader) ExtractColorSpaces(ctx context.Context, pageNum int) (_ []models.ColorSpaceUse, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractColorSpaces", &err)
	page, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	uses, err := colorusage.Page(ctx, r.doc, page)
	if err != nil {
		return nil, err
	}
	out := make([]models.ColorSpaceUse, 0, len(uses))
	for _, u := range uses {
		use := colorSpaceUse(u.Space)
		use.Source, use.Operator, use.Name, use.Object, use.Count = u.Source, u.Operator, u.Name, u.Object, u.Count
		out = append(out, use)
	}
	return out, nil
}

// colorSpaceUse translates one internal space description, alternates
// included, to the public model.
func colorSpaceUse(s colorusage.Space) models.ColorSpaceUse {
	use := models.ColorSpaceUse{Family: s.Family, Components: s.Components, Colorants: slices.Clone(s.Colorants)}
	if s.Alternate != nil {
		alt := colorSpaceUse(*s.Alternate)
		use.Alternate = &alt
	}
	return use
}
