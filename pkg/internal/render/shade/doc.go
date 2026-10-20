// Package shade paints PDF shadings (ISO 32000-1, 8.7.4.5) and shading
// patterns (8.7.4) onto a [surface.Canvas].
//
// [New] compiles a shading dictionary or stream of any of the seven types:
// function-based (1), axial (2), radial (3), free-form and lattice-form
// Gouraud-shaded triangle meshes (4, 5), and Coons and tensor-product patch
// meshes (6, 7). Mesh data is read once, at compile time, and bounded.
// [Shading.Paint] then paints it as the sh operator does, through a
// shading-to-device matrix and a clip, and [Pattern.Fill] paints a type 2
// pattern over a coverage mask, Background included.
//
// The package produces colour components in the shading's colour space; the
// caller converts them to sRGB through a [Converter], so this package knows
// nothing about colour management. Colours are sampled at pixel centres: a
// shading's own edges are not anti-aliased, while the clip and coverage
// masks the caller passes carry their own fractional coverage.
//
// Values of every type are safe for concurrent use after [New] or
// [NewPattern] returns: painting reads the compiled shading and writes only
// into the caller's canvas.
package shade
