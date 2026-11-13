// Package render holds litho's clean-room PDF renderer.
//
// The renderer is built from small packages that share the types of [geom]
// (device-space geometry: points, matrices, paths) and [surface] (the raster
// targets: an RGBA canvas and a coverage mask). Package page is the driver that
// walks a page's content and paints through the others. Nothing in it depends
// on a third-party graphics library.
//
// # Determinism
//
// Every product that is summed is rounded on its own before the sum, by an
// explicit conversion to its own type: The Go Programming Language
// Specification lets a compiler fuse x*y + z into one instruction that
// rounds once, and forbids it across an explicit conversion. arm64, and
// amd64 built for GOAMD64=v3, fuse wherever they may; the renderer's
// arithmetic leaves them nowhere to, so it draws the same bytes on every
// architecture for the same input. The math package's Hypot, Sincos,
// Atan2 and Acos, which the renderer calls on stroke and curve geometry,
// differ between architectures by a unit in the last place on a few
// inputs, and the packages the renderer reads colour, fonts and functions
// through are not held to this rule.
package render
