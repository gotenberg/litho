// Package svg draws SVG documents as fragments of the composition model:
// paths, shadings, images and glyph runs. It reads a standalone document
// ([Parse]) or an svg element of an HTML document ([FromNode]), and draws
// it into any viewport ([Document.Draw]) as a replaced element does (CSS
// Images 3, 4 and 5).
//
// # Coverage
//
// SVG 1.1 second edition, and SVG 2 where the two agree:
//
//   - Structure: svg with width, height, viewBox and every
//     preserveAspectRatio value (7.7, 7.8, 7.9), g, defs, symbol, use with
//     x and y (5.6), switch with requiredExtensions and systemLanguage
//     (5.8), title and desc ignored, unknown elements skipped.
//   - Shapes: rect with rounded corners, circle, ellipse, line, polyline,
//     polygon, and path with the whole path grammar (8.3), arcs converted
//     to Bézier curves (F.6); image with a raster picture (5.7).
//   - Painting: fill and stroke with colours, none, currentColor and
//     url() references with a fallback (11.2), their opacities and fill
//     rules, stroke width, caps, joins, miter limit and dashes (11.4),
//     opacity on groups, visibility and display.
//   - Gradients: linearGradient and radialGradient with gradientUnits,
//     gradientTransform, fx and fy, the href chain and stops (13.2), on
//     fills and strokes; a gradient stroke fills the stroke's outline.
//   - Clipping: clip-path naming a clipPath of shapes and text, with
//     clipPathUnits, clip-rule, the clip-path of each child intersecting
//     it and the clip-path of the clipPath intersecting the union (14.3).
//     Children of mixed clip rules or with clip paths of their own paint
//     the clipped content once each, so translucent content is painted
//     twice where they overlap.
//   - Transforms: the transform attribute grammar (7.6).
//   - Text: text and tspan with x, y, dx, dy lists, text-anchor and the
//     font properties, shaped with the text package (10); a stroke unlike
//     the fill strokes the glyphs' outlines.
//   - Styling: presentation attributes (6.4), the style attribute and
//     style elements, matched by the css package's selector engine, with
//     specificity, !important and inheritance (6); units px, pt, pc, mm,
//     cm, in, Q, em, ex, rem and percentages (7.10).
//
// # Not supported
//
// Patterns, masks, filters and markers are not drawn, with a warning: a
// pattern paints its fallback, masked and filtered content is drawn as it
// is. A gradient spreadMethod of reflect or repeat is drawn as pad, and a
// gradient text fill in the gradient's middle colour, each with a
// warning. textPath, textLength, rotate, foreignObject, animation,
// scripting and external style sheets are not supported. Elements
// written with a namespace prefix are SVG elements only when the prefix
// is bound to the SVG namespace.
//
// # Limits
//
// The source, the elements, their nesting, use instantiations, path
// segments, gradient stops, text characters and the fragments of one
// rendering are capped; past a cap the rest is left out with a warning.
// No input makes the package panic.
package svg
