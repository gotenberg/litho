// Package imgpaint paints decoded images onto a [surface.Canvas].
//
// An image occupies the unit square of its own user space (ISO 32000-1,
// 8.9.4), and the caller passes the matrix that maps that square to device
// space. Every device pixel whose centre the square covers is painted with
// the image sampled at the inverse-mapped centre, so the edges of an image are
// hard: a pixel is in or out. The loops run over the image's device bounding
// box intersected with the canvas, never over the whole canvas, so painting
// costs O(pixels covered × taps per pixel).
//
// The colour comes from the decoder of pkg/internal/graphics/image as sRGB,
// already colour managed; this package does no colour conversion. Opacity
// comes from [Alpha] planes: a soft mask ([SoftMask], ISO 32000-1, 11.6.5.3),
// a stencil mask ([StencilMask], 8.9.6.3) or a colour-key mask ([ColorKey],
// 8.9.6.4), multiplied by the constant alpha and the clip. An image mask
// (8.9.6.2) is painted in a fill colour by [DrawStencil].
//
// # Sampling
//
// Each axis of a sample grid is filtered on its own, by the footprint one
// device pixel covers on that axis:
//
//   - Downsampling (a pixel covers more than one sample): a box filter, the
//     mean of up to maxBoxTaps evenly spaced samples across the footprint,
//     whatever /Interpolate says. Picking one sample there aliases, and an
//     exact 2:1 reduction averages exactly the two samples it covers.
//   - Upsampling with /Interpolate true: bilinear between sample centres
//     (ISO 32000-1, 8.9.5.3 asks for interpolation and leaves the method to
//     the reader).
//   - Upsampling with /Interpolate false: nearest sample, so enlarged samples
//     stay square.
//
// The footprint is measured along the axes of the sample grid, so a rotated
// image is box-filtered over an axis-aligned approximation of the pixel's
// footprint.
//
// A mask plane on the image's own grid is folded into the colour taps, so the
// colour is interpolated premultiplied by it and a masked sample's colour
// never bleeds into its neighbours. A plane with other dimensions is sampled
// on its own grid, with its own /Interpolate.
//
// Poppler 26.09.0 agrees to within one 8-bit level on crafted images drawn at
// 1:1, 2:1 and 3:1, upscaled 8x, rotated, flipped, and through each kind of
// mask, and differs in three places. It interpolates an upsampled image below
// a 4x enlargement even without /Interpolate. It paints one more pixel past an
// image edge that falls exactly on a pixel boundary, then resamples the image
// across the wider span. Its bilinear filter is not centred on the samples:
// it places each sample's value at the low corner of the sample's cell, which
// shifts an interpolated enlargement half a sample toward the grid's origin.
// litho keeps the centred reading: 8.9.4 maps the image onto the unit square,
// each sample filling a cell of it, so the value a cell holds belongs at its
// centre, and enlarging an image must not move it.
//
// Functions in this package keep no state; they may run concurrently on
// distinct canvases.
//
// # Corpus evidence
//
//   - litho inlineinterp_2b.pdf: a 4 by 1 interpolated image enlarged to 250
//     pixels a sample; poppler peaks each sample at the left edge of its cell
//     (x 100, 350, 600 for samples 0, 1, 2), litho at its centre (225, 475,
//     725). The page mismatches the render golden on 148309 pixels for it.
package imgpaint
