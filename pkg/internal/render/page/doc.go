// Package page renders one PDF page into a [surface.Canvas]: it is the
// content-stream driver that joins the renderer's parts.
//
// [Render] maps the page's crop box to device space at a resolution, with its
// /Rotate and /UserUnit (ISO 32000-1, 7.7.3.3, Table 30), and walks the
// page's content with a [graphics.Machine] holding the graphics state.
// The driver itself handles what paints: path construction and painting
// (8.5.2, 8.5.3) through [raster] and [stroke], clipping (8.5.4) with true
// path masks, colour converted to sRGB by [colorconv] (8.6), images and
// inline images (8.9) through the image decoder and [imgpaint], shadings and
// shading patterns (8.7.4) through [shade], tiling patterns (8.7.3) whose
// cells it runs into tiles or over the painted area, and form XObjects (8.10)
// with their matrix, bounding box and resources. Content hidden by optional
// content (8.11) paints nothing, its clipping included as a viewer applies
// it. Over the content it draws the annotation appearances [annot.Collect]
// selects (12.5.5), for view or for print.
//
// Text (9.4) is drawn glyph by glyph through [glyph]: each outline under the
// text rendering matrix, the text position advancing by the widths the font
// declares (9.4.4), in every rendering mode of 9.3.6, the clipping modes
// adding the outlines to the clip at ET. A Type 3 glyph runs its description
// (9.6.5), its colours its own after d0 and the fill colour's after d1. A
// font the document does not embed draws with a program [Options.Fonts]
// supplies, or as placeholder boxes, counted in the [Report].
//
// What the renderer does not draw yet is counted in the [Report] rather than
// drawn wrongly: soft masks, blend modes other than Normal and transparency
// groups, which a form XObject's content is painted without, and annotations
// with no appearance stream.
//
// Every allocation sized by the page is held to the render pixel budget, and
// the work of one render to [maxRenderOperators] operators; ctx is checked
// every [opsPerContextCheck] operators.
//
// [Render] keeps no state between calls and reads the document only, so calls
// may run concurrently on one document.
package page
