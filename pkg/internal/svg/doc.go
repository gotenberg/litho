// Package svg draws SVG documents as fragments of the typeset model:
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
//     the order of fill and stroke paint-order sets (SVG 2, 13.8), a
//     shape's non-scaling stroke drawn in the CSS pixels of the drawing's
//     viewport (SVG 2, 13.6; the other vector effects are invalid, as
//     Chromium has them), shape-rendering read as the hint it is and
//     drawing nothing of its own (SVG 2, 13.10.1),
//     opacity on groups, composited as one group, visibility and display.
//   - Gradients: linearGradient and radialGradient with gradientUnits,
//     gradientTransform, fx and fy, the href chain and stops (13.2), on
//     fills and strokes; a gradient stroke fills the stroke's outline.
//   - Clipping: clip-path naming a clipPath of shapes and text, with
//     clipPathUnits, clip-rule, the clip-path of each child intersecting
//     it and the clip-path of the clipPath intersecting the union (14.3).
//     Children of mixed clip rules or with clip paths of their own paint
//     the clipped content once each, so translucent content is painted
//     twice where they overlap.
//   - Masking and compositing: mask naming a mask element, with
//     maskUnits, maskContentUnits, its region and mask-type, as a soft
//     mask by luminance or alpha (CSS Masking 1, 7.1); mix-blend-mode, as
//     the PDF blend mode of the same name, and isolation (CSS Compositing
//     1, 3.2 and 3.3). plus-lighter is drawn as normal with a warning. A
//     drawing in which something blends is one isolated group.
//   - Filters: filter naming a filter element, with filterUnits,
//     primitiveUnits, its region and each primitive's subregion, the
//     in, in2 and result graph and color-interpolation-filters (Filter
//     Effects 1, 7 to 9 and 17), drawn by package effect as one image of
//     the filtered element: feGaussianBlur, feOffset, feFlood,
//     feColorMatrix, feComponentTransfer, feComposite, feBlend, feMerge,
//     feMorphology, feTile and feDropShadow. The source graphic is drawn
//     by litho's PDF renderer at a whole number of pixels a user unit,
//     at the resolution package effect plans for the smallest blur, a
//     user unit taken as a CSS pixel. linearRGB is held in eight bits a
//     channel: a round trip from sRGB moves a colour by up to 6 levels.
//     An element whose filter names no filter element is not drawn.
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
// Patterns and markers are not drawn, with a warning: a pattern paints
// its fallback. A filter holding feTurbulence, feConvolveMatrix,
// feDisplacementMap, feDiffuseLighting, feSpecularLighting or feImage,
// and a filter of CSS filter functions, are not drawn either, with a
// warning: the content is drawn unfiltered. BackgroundImage,
// BackgroundAlpha, FillPaint and StrokePaint read as transparent black.
// The lighter operator of feComposite composites as over. A mask that
// names no mask element is not applied. A gradient spreadMethod of reflect or repeat is drawn as pad, and a
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
