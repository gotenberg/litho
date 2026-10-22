// Package page renders one PDF page into a [surface.Canvas]: it is the
// content-stream driver that joins the renderer's parts.
//
// [Render] maps the page's crop box, or the boundary [Options.Box] selects
// (ISO 32000-1, 14.11.2), to device space at a resolution, with its /Rotate
// and /UserUnit (7.7.3.3, Table 30), and walks the
// page's content with a [graphics.Machine] holding the graphics state.
// The driver itself handles what paints: path construction and painting
// (8.5.2, 8.5.3) through [raster] and [stroke], clipping (8.5.4) with true
// path masks, a rectangle widened to the device rows it touches as viewers
// clip one, and to its whole pixels for a filled glyph or a transparency
// group, a filled glyph taking each pixel the other clipping paths reach
// whole or not at all, colour converted to sRGB by [colorconv] (8.6), images and
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
// Transparency (11) goes through one sink: every painting, fills, strokes,
// glyphs, images, image masks, shadings, pattern cells and the forms of
// annotation appearances, is an element composited into the transparency
// group being painted by [blend], with the blend mode, constant alpha, alpha
// source flag and soft mask of the graphics state. The page is the root
// group (11.4.7), a form XObject with a transparency /Group paints into a
// group of its own (11.6.6), isolated or knockout as it says, and a soft
// mask's group renders into a mask (11.6.5.2). With TK true the glyphs of a
// text object composite as one element (9.3.8). An element that blends with
// Normal and no soft mask, in a group that lets it, is painted straight onto
// the group's colour, and a group that would composite exactly as its
// content painted into its parent gets no raster of its own, so opaque
// content costs what it did before transparency was drawn. A group blends in
// the grey or CMYK space its /CS names (11.3.4) when its content paints with
// transparency, through [blend.Space]: a colour of that space, or one that
// space converts from by the device formulas of 10.3, keeps its components,
// any other arrives through its sRGB, and the page leaves in sRGB. RGB
// spaces blend in sRGB. [Options.CMYK] draws the page for a CMYK device
// instead: the page group blends in the device's CMYK whatever its /CS
// names, and the page leaves as inks ([Result.CMYK]).
//
// What the renderer does not draw is counted in the [Report] rather than
// drawn wrongly: annotations with no appearance stream that ISO 32000-1
// gives no geometry for, and transparency whose rasters would pass the render
// pixel budget.
//
// Every allocation sized by the page is held to the render pixel budget, and
// the work of one render to [maxRenderOperators] operators; ctx is checked
// every [opsPerContextCheck] operators.
//
// [Render] keeps no state between calls and reads the document only, so calls
// may run concurrently on one document.
package page
