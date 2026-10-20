// Package geom holds the geometry the renderer shares: points, affine
// matrices in the PDF convention, and paths of straight and cubic Bézier
// segments.
//
// A [Matrix] is the six numbers [a b c d e f] of ISO 32000-1, 8.3.3, mapping
// (x, y) to (a·x + c·y + e, b·x + d·y + f). Paths are plain values: they are
// built by one goroutine and may be read concurrently once built.
package geom
