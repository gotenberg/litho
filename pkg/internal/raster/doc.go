// Package raster draws coverage masks of vector paths and filters them:
// the blurs, dilations and erosions of the effects a vector page cannot
// express, painted as a bitmap whose alpha becomes a soft mask.
//
// A mask is an [image.Alpha]: 0 is uncovered, 255 fully covered. [Draw]
// covers a mask with filled and stroked [compose.PathOp] paths through a
// transform from path units to pixels; [Blur] convolves a mask with a
// separable [Kernel] ([Tent] or [Gaussian]); [Dilate] and [Erode] grow and
// shrink its covered area by a square of pixels; [Threshold] and
// [Multiply] combine coverages; [Tint], [Paint] and [Mask] turn masks into
// colour. Every function is deterministic and safe for concurrent use on
// distinct images; none keeps a reference to its arguments.
package raster
