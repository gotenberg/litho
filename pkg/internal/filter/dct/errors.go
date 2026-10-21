package dct

import "errors"

// Sentinel errors for JPEG decoding and encoding failures.
var (
	// ErrMalformed indicates data that breaks the syntax of ITU-T T.81.
	ErrMalformed = errors.New("malformed JPEG data")

	// ErrUnsupported indicates a coding T.81 defines that this decoder does
	// not implement: lossless, arithmetic or hierarchical coding, or samples
	// other than 8 bits.
	ErrUnsupported = errors.New("unsupported JPEG coding")

	// ErrLimit indicates data that exceeds one of this package's anti-DoS
	// limits.
	ErrLimit = errors.New("JPEG data exceeds a decoding limit")

	// ErrInvalidInput indicates samples or options [Encode] cannot write:
	// a component count other than 1, 3 or 4, dimensions outside the frame
	// header's range, samples that do not fill the image, or an option out
	// of range.
	ErrInvalidInput = errors.New("invalid JPEG encoder input")
)
