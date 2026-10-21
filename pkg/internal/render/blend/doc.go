// Package blend composites transparent painting (ISO 32000-1, 11): the
// sixteen standard blend modes, transparency groups and soft masks.
//
// [Mode] holds a blend mode and [Mode.Blend] evaluates its blend function
// B(Cb, Cs), the separable modes of Table 136 and the non-separable ones of
// Table 137, by the formulas of ISO 32000-1 or, through [Mode.In] and
// [EditionFor], those ISO 32000-2 gives a PDF 2.0 document. A [Group] is one
// transparency group's stack (11.4): elements are painted into it pixel by
// pixel with [Group.Paint], which applies the
// general compositing formulas of 11.4.8 with shape and alpha kept apart, so
// isolated, non-isolated, knockout and non-knockout groups all follow the
// same arithmetic. [Group.Into] then composites the finished group into its
// parent as one object, the backdrop of a non-isolated group factored out.
// The page itself is a [Group]: isolated, non-knockout, over a transparent
// backdrop (11.4.7). [Group.PaintCanvas] and [Group.PaintCoverage] composite
// one element's raster or coverage, and [Group.Direct] hands out the colour
// plane of a group where Normal source-over painting needs no more.
// [AlphaMask] and [LuminosityMask] derive a soft mask from a rendered group
// (11.5.2, 11.5.3) through an optional /TR transfer function, as a [Mask]
// whose values cover the group's bounds alone.
//
// A group blends in a blending colour space (11.3.4), its [Attributes.Space]:
// sRGB, the nil [Space], or the grey or CMYK space a [Space] describes, whose
// [Converter] connects it with sRGB. Painted colours are [Components] of the
// group's space, rasters arrive in sRGB and are converted, and a group
// composited into a parent of another space is converted to it. A CMYK colour
// is held as its complement, so the subtractive rule of 11.3.5 and the
// compositing formulas of 11.4 share one arithmetic; the non-separable modes
// take K from one side (11.3.5.3). Spot colorants stay outside: a Separation
// colour reaches a group in its alternate space. Colours are stored
// premultiplied by alpha, the way [surface.Canvas] holds them.
//
// A [Group] is not safe for concurrent writes. A child reads its parent's
// planes only in [NewGroup] and writes them only in [Group.Into], so distinct
// groups may be painted from distinct goroutines between those calls. [Mode]
// values are immutable.
package blend
