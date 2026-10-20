// Package render holds litho's clean-room PDF renderer.
//
// The renderer is built from small packages that share the types of [geom]
// (device-space geometry: points, matrices, paths) and [surface] (the raster
// targets: an RGBA canvas and a coverage mask). Nothing in it depends on a
// third-party graphics library.
package render
