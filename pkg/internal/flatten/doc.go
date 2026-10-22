// Package flatten removes live transparency from page content: it composites
// each region where transparent content paints into one opaque image, and
// keeps everything outside those regions, and every glyph inside them, as
// vector content.
//
// [Analyze] reads a content stream into units, the painting elements of ISO
// 32000-2, 8.2 (a path from its first construction operator to its painting
// operator, an image, a shading, a form XObject, a text object), each with a
// conservative box in default user space and whether it paints with
// transparency (11.3.5 blend modes, 11.3.7.2 constant alpha, 11.6.5 soft
// masks, 11.6.6 transparency groups). It groups the transparent units into
// regions: a region holds the transparent units whose boxes overlap, and
// every other unit it must take with them so that nothing still drawn as
// vector content needs the composite to be drawn above it. A unit no
// transparent unit overlaps stays as it is.
//
// [Page] renders the page without its text, crops each region from the
// render, and splices the result into the content: the region's units are
// removed and the image is drawn where the first of them was, so the units
// drawn after it keep their order. Text is never rasterised: it stays in the
// content stream, above the image, its marked content intact.
//
// [Annotations] runs first: it draws into the page content the appearance of
// each annotation that paints with transparency, so the page flattening
// composites it with the rest.
//
// [Redaction] runs the same machinery for redaction's raster fallback: the
// regions grow from the form XObjects and shading paints a redaction cannot
// remove exactly, and their images are cut out of the redaction's regions.
package flatten
