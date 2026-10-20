// Package raster turns device-space paths into anti-aliased coverage: it
// flattens Bézier curves, scan-converts the result under the nonzero or
// even-odd rule (ISO 32000-1, 8.5.3.3), intersects clips (8.5.4), and paints
// a solid colour through a coverage onto a canvas.
//
// Coverage is exact-area, computed per pixel row in horizontal bands. Every
// vertex that falls inside a row splits a band, and each band adds the area
// of the spans between its edge crossings, measured at the band's middle.
// Between two vertices an edge is straight, so the midpoint crossing is the
// mean crossing over the band and the area is exact: a polygon whose edges do
// not cross one another gets its exact pixel coverage, whatever its angle.
// Where two edges cross inside a band (a self-intersecting star), the error
// is bounded by the band height, a fixed fraction of a pixel. Curves are
// flattened first, so their coverage is exact for the flattened polygon,
// which stays within a tolerance of the curve.
//
// Functions in this package keep no state and are safe for concurrent use on
// distinct outputs.
package raster
