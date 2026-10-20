package image

import "errors"

// Sentinel errors of image decoding.
var (
	// ErrImageNotFound reports that a page shows no image under the requested
	// name.
	ErrImageNotFound = errors.New("image not found")

	// ErrImageTooLarge reports an image past the document's pixel budget,
	// limits.Limits.MaxImagePixels: its Width x Height, or its decoded
	// samples past four bytes a pixel of it. pkg/pdf re-exports it as
	// pdf.ErrImageTooLarge.
	ErrImageTooLarge = errors.New("image exceeds the pixel budget")

	// ErrUnsupportedColorSpace reports a colour space [NewSampleColor] cannot
	// convert: an unknown family, an indexed space with no readable colour
	// table, or a tinted space whose tint transform or alternate cannot be
	// read.
	ErrUnsupportedColorSpace = errors.New("colour space cannot be converted")
)
