// Package colorconv converts colours of every PDF colour space (ISO 32000-1,
// 8.6) to sRGB for the renderer.
//
// [For] returns the [Converter] of a colour space as the graphics machine
// recorded it, and [ForObject] that of a /ColorSpace object as a shading or
// an image dictionary holds it. The device spaces honour the /DefaultGray,
// /DefaultRGB and /DefaultCMYK spaces of the resources in force (8.6.5.6) and
// the output intents of the page and the document (14.11.5); uncalibrated
// DeviceCMYK goes through the caller's CMYK profile or litho's built-in one.
// CalGray, CalRGB, Lab, ICCBased, Indexed, Separation and DeviceN follow
// 8.6.5.2 to 8.6.6.5.
//
// The conversion is the image decoder's ([image.NewSampleColor]): a colour is
// quantised to the byte an 8-bit image sample holds and converted as that
// sample would be, so a filled shape and an image in the same space paint the
// same sRGB. The quantisation also keys a small per-space table, so a page
// that repeats a colour converts it once.
//
// Rendering intents map onto the ICC engine's: perceptual, relative
// colorimetric and saturation select a profile's AToB0, AToB1 and AToB2
// tables, each falling back to AToB0 and then to the profile's matrix and
// tone curves, and absolute colorimetric is rendered relative colorimetric
// (see [icc.Profile.NewTransform]). The CIE-based families have no intent to
// honour. NChannel attributes (8.6.6.5) only matter on separations, so they
// are ignored.
//
// [Converter] values and [Cache] are safe for concurrent use.
package colorconv
