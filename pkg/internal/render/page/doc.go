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
// shading patterns (8.7.4) through [shade], and form XObjects (8.10) with their
// matrix, bounding box and resources. Content hidden by optional content
// (8.11) paints nothing, its clipping included as a viewer applies it.
//
// What the renderer does not draw yet is counted in the [Report] rather than
// drawn wrongly: text, tiling patterns, soft masks, blend modes other than
// Normal and transparency groups, which a form XObject's content is painted
// without. Annotations are not drawn.
//
// Every allocation sized by the page is held to the render pixel budget, and
// the work of one render to [maxRenderOperators] operators; ctx is checked
// every [opsPerContextCheck] operators.
//
// [Render] keeps no state between calls and reads the document only, so calls
// may run concurrently on one document.
package page
